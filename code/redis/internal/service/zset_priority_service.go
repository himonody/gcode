package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// PriorityTask 优先级任务结构
type PriorityTask struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Payload  interface{}            `json:"payload"`
	Priority float64                `json:"priority"` // 优先级分数，越高越优先
	CreateAt int64                  `json:"create_at"`
	Extra    map[string]interface{} `json:"extra,omitempty"`
}

// PriorityQueueService 优先级队列服务接口
// 基于 Redis ZSet 实现的优先级队列
// 适用场景：
// - 任务调度系统
// - 消息优先级处理
// - 工单处理系统
// - 紧急事件处理
// - 带权重的任务分配
type PriorityQueueService interface {
	// AddTask 添加任务
	// priority: 优先级分数，越高越优先
	AddTask(ctx context.Context, queueName string, task *PriorityTask) error

	// AddTaskWithPriority 添加任务并指定优先级
	AddTaskWithPriority(ctx context.Context, queueName string, task *PriorityTask, priority float64) error

	// PopHighestPriority 弹出最高优先级的任务
	// 原子操作，删除并返回
	PopHighestPriority(ctx context.Context, queueName string) (*PriorityTask, error)

	// PopLowestPriority 弹出最低优先级的任务
	PopLowestPriority(ctx context.Context, queueName string) (*PriorityTask, error)

	// PeekHighest 查看最高优先级的任务（不删除）
	PeekHighest(ctx context.Context, queueName string) (*PriorityTask, error)

	// PeekLowest 查看最低优先级的任务（不删除）
	PeekLowest(ctx context.Context, queueName string) (*PriorityTask, error)

	// PeekN 查看前N个高优先级任务
	PeekN(ctx context.Context, queueName string, count int) ([]*PriorityTask, error)

	// PopBatch 批量弹出高优先级任务
	// count: 弹出数量
	PopBatch(ctx context.Context, queueName string, count int) ([]*PriorityTask, error)

	// GetCount 获取队列中任务总数
	GetCount(ctx context.Context, queueName string) (int64, error)

	// GetCountByPriority 获取指定优先级范围的任务数
	// minPriority, maxPriority: 优先级范围
	GetCountByPriority(ctx context.Context, queueName string, minPriority, maxPriority float64) (int64, error)

	// UpdatePriority 更新任务优先级
	UpdatePriority(ctx context.Context, queueName string, taskID string, newPriority float64) error

	// RemoveTask 移除指定任务
	RemoveTask(ctx context.Context, queueName string, taskID string) error

	// Clear 清空队列
	Clear(ctx context.Context, queueName string) error

	// GetTasksByPriority 获取指定优先级范围的任务
	GetTasksByPriority(ctx context.Context, queueName string, minPriority, maxPriority float64) ([]*PriorityTask, error)

	// IncreasePriority 提升任务优先级
	// delta: 增加的优先级分数
	IncreasePriority(ctx context.Context, queueName string, taskID string, delta float64) (float64, error)
}

type priorityQueueService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewPriorityQueueService 创建优先级队列服务
func NewPriorityQueueService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) PriorityQueueService {
	if prefix == "" {
		prefix = "priority_queue"
	}
	return &priorityQueueService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *priorityQueueService) buildKey(queueName string) string {
	return fmt.Sprintf("%s:%s", s.prefix, queueName)
}

func (s *priorityQueueService) serializeTask(task *PriorityTask) (string, error) {
	if task.CreateAt == 0 {
		task.CreateAt = time.Now().Unix()
	}

	data, err := json.Marshal(task)
	if err != nil {
		return "", rediserr.Wrap(err, rediserr.ErrCodeSerialization, "serialize task failed")
	}
	return string(data), nil
}

func (s *priorityQueueService) deserializeTask(data string) (*PriorityTask, error) {
	var task PriorityTask
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeSerialization, "deserialize task failed")
	}
	return &task, nil
}

func (s *priorityQueueService) AddTask(ctx context.Context, queueName string, task *PriorityTask) error {
	return s.AddTaskWithPriority(ctx, queueName, task, task.Priority)
}

func (s *priorityQueueService) AddTaskWithPriority(ctx context.Context, queueName string, task *PriorityTask, priority float64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.add_task", time.Since(start), true)
	}()

	if queueName == "" || task == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task cannot be empty")
	}

	task.Priority = priority
	key := s.buildKey(queueName)

	data, err := s.serializeTask(task)
	if err != nil {
		return err
	}

	// 使用负数作为分数，使得分数越大的排在前面（ZSet默认升序）
	// 或者使用 ZRevRange 来获取
	err = s.client.GetClient().ZAdd(ctx, key, redis.Z{
		Score:  -priority, // 负数使得高优先级排在前面
		Member: data,
	}).Err()

	if err != nil {
		s.logger.Error("添加任务失败",
			logger.String("queue", queueName),
			logger.String("task_id", task.ID),
			logger.Float64("priority", priority),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add task failed")
	}

	s.logger.Info("添加优先级任务",
		logger.String("queue", queueName),
		logger.String("task_id", task.ID),
		logger.String("type", task.Type),
		logger.Float64("priority", priority))

	return nil
}

func (s *priorityQueueService) PopHighestPriority(ctx context.Context, queueName string) (*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.pop_highest", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	// 使用 Lua 脚本保证原子性
	script := `
		local tasks = redis.call('ZRANGE', KEYS[1], 0, 0)
		if #tasks > 0 then
			redis.call('ZREM', KEYS[1], tasks[1])
			return tasks[1]
		else
			return nil
		end
	`

	result, err := s.client.GetClient().Eval(ctx, script, []string{key}).Result()
	if err != nil {
		s.logger.Error("弹出最高优先级任务失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "pop highest failed")
	}

	if result == nil {
		return nil, nil
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, nil
	}

	task, err := s.deserializeTask(taskData)
	if err != nil {
		return nil, err
	}

	s.logger.Info("弹出最高优先级任务",
		logger.String("queue", queueName),
		logger.String("task_id", task.ID),
		logger.Float64("priority", task.Priority))

	return task, nil
}

func (s *priorityQueueService) PopLowestPriority(ctx context.Context, queueName string) (*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.pop_lowest", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	script := `
		local tasks = redis.call('ZRANGE', KEYS[1], -1, -1)
		if #tasks > 0 then
			redis.call('ZREM', KEYS[1], tasks[1])
			return tasks[1]
		else
			return nil
		end
	`

	result, err := s.client.GetClient().Eval(ctx, script, []string{key}).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "pop lowest failed")
	}

	if result == nil {
		return nil, nil
	}

	taskData, ok := result.(string)
	if !ok {
		return nil, nil
	}

	task, err := s.deserializeTask(taskData)
	if err != nil {
		return nil, err
	}

	s.logger.Info("弹出最低优先级任务",
		logger.String("queue", queueName),
		logger.String("task_id", task.ID))

	return task, nil
}

func (s *priorityQueueService) PeekHighest(ctx context.Context, queueName string) (*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.peek_highest", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().ZRange(ctx, key, 0, 0).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "peek highest failed")
	}

	if len(results) == 0 {
		return nil, nil
	}

	return s.deserializeTask(results[0])
}

func (s *priorityQueueService) PeekLowest(ctx context.Context, queueName string) (*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.peek_lowest", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().ZRange(ctx, key, -1, -1).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "peek lowest failed")
	}

	if len(results) == 0 {
		return nil, nil
	}

	return s.deserializeTask(results[0])
}

func (s *priorityQueueService) PeekN(ctx context.Context, queueName string, count int) ([]*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.peek_n", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().ZRange(ctx, key, 0, int64(count-1)).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "peek n failed")
	}

	tasks := make([]*PriorityTask, 0, len(results))
	for _, data := range results {
		if task, err := s.deserializeTask(data); err == nil {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (s *priorityQueueService) PopBatch(ctx context.Context, queueName string, count int) ([]*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.pop_batch", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	tasks := make([]*PriorityTask, 0, count)
	for i := 0; i < count; i++ {
		task, err := s.PopHighestPriority(ctx, queueName)
		if err != nil {
			return tasks, err
		}
		if task == nil {
			break
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (s *priorityQueueService) GetCount(ctx context.Context, queueName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.get_count", time.Since(start), true)
	}()

	if queueName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	count, err := s.client.GetClient().ZCard(ctx, key).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count failed")
	}

	return count, nil
}

func (s *priorityQueueService) GetCountByPriority(ctx context.Context, queueName string, minPriority, maxPriority float64) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.get_count_by_priority", time.Since(start), true)
	}()

	if queueName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	// 注意：因为我们存储时使用了负数，所以这里也要转换
	count, err := s.client.GetClient().ZCount(ctx, key,
		fmt.Sprintf("%f", -maxPriority),
		fmt.Sprintf("%f", -minPriority)).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count by priority failed")
	}

	return count, nil
}

func (s *priorityQueueService) UpdatePriority(ctx context.Context, queueName string, taskID string, newPriority float64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.update_priority", time.Since(start), true)
	}()

	if queueName == "" || taskID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task ID cannot be empty")
	}

	s.logger.Warn("UpdatePriority 需要遍历查找任务，性能较低",
		logger.String("queue", queueName),
		logger.String("task_id", taskID))

	return rediserr.New(rediserr.ErrCodeInternal, "not implemented - requires task index for efficiency")
}

func (s *priorityQueueService) RemoveTask(ctx context.Context, queueName string, taskID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.remove_task", time.Since(start), true)
	}()

	if queueName == "" || taskID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task ID cannot be empty")
	}

	key := s.buildKey(queueName)

	// 使用 Lua 脚本查找并删除
	script := `
		local members = redis.call('ZRANGE', KEYS[1], 0, -1)
		for i, member in ipairs(members) do
			if string.find(member, ARGV[1]) then
				redis.call('ZREM', KEYS[1], member)
				return 1
			end
		end
		return 0
	`

	result, err := s.client.GetClient().Eval(ctx, script, []string{key}, fmt.Sprintf("\"id\":\"%s\"", taskID)).Result()
	if err != nil {
		s.logger.Error("移除任务失败",
			logger.String("queue", queueName),
			logger.String("task_id", taskID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove task failed")
	}

	if result.(int64) == 0 {
		return rediserr.New(rediserr.ErrCodeNotFound, "task not found")
	}

	s.logger.Info("移除任务成功",
		logger.String("queue", queueName),
		logger.String("task_id", taskID))

	return nil
}

func (s *priorityQueueService) Clear(ctx context.Context, queueName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.clear", time.Since(start), true)
	}()

	if queueName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear queue failed")
	}

	s.logger.Info("清空优先级队列", logger.String("queue", queueName))
	return nil
}

func (s *priorityQueueService) GetTasksByPriority(ctx context.Context, queueName string, minPriority, maxPriority float64) ([]*PriorityTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.get_tasks_by_priority", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", -maxPriority),
		Max: fmt.Sprintf("%f", -minPriority),
	}).Result()

	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get tasks by priority failed")
	}

	tasks := make([]*PriorityTask, 0, len(results))
	for _, data := range results {
		if task, err := s.deserializeTask(data); err == nil {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (s *priorityQueueService) IncreasePriority(ctx context.Context, queueName string, taskID string, delta float64) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("priority_queue.increase_priority", time.Since(start), true)
	}()

	if queueName == "" || taskID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task ID cannot be empty")
	}

	s.logger.Warn("IncreasePriority 需要遍历查找任务，性能较低",
		logger.String("queue", queueName),
		logger.String("task_id", taskID))

	return 0, rediserr.New(rediserr.ErrCodeInternal, "not implemented - requires task index for efficiency")
}
