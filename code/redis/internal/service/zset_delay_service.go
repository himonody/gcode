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

// DelayTask 延迟任务结构
type DelayTask struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Payload   interface{}            `json:"payload"`
	ExecuteAt int64                  `json:"execute_at"`
	Retry     int                    `json:"retry"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

// DelayQueueService 延迟队列服务接口
// 基于 Redis ZSet 实现的延迟队列
// 适用场景：
// - 订单超时自动取消
// - 延迟通知
// - 定时提醒
// - 延迟重试
// - 定时任务调度
type DelayQueueService interface {
	// AddTask 添加延迟任务
	// task: 任务对象
	AddTask(ctx context.Context, queueName string, task *DelayTask) error

	// AddTaskWithDelay 添加延迟任务（相对时间）
	// delay: 延迟时长
	AddTaskWithDelay(ctx context.Context, queueName string, task *DelayTask, delay time.Duration) error

	// GetReadyTasks 获取所有到期的任务
	// 返回所有 execute_at <= 当前时间的任务
	GetReadyTasks(ctx context.Context, queueName string) ([]*DelayTask, error)

	// PopReadyTask 弹出一个到期的任务
	// 原子操作，保证任务不会被重复消费
	PopReadyTask(ctx context.Context, queueName string) (*DelayTask, error)

	// PopReadyTasks 批量弹出到期的任务
	// count: 最多弹出的任务数
	PopReadyTasks(ctx context.Context, queueName string, count int) ([]*DelayTask, error)

	// PeekNextTask 查看下一个将要执行的任务
	// 返回任务和距离执行的时间
	PeekNextTask(ctx context.Context, queueName string) (*DelayTask, time.Duration, error)

	// GetCount 获取队列中任务总数
	GetCount(ctx context.Context, queueName string) (int64, error)

	// GetReadyCount 获取已到期的任务数
	GetReadyCount(ctx context.Context, queueName string) (int64, error)

	// RemoveTask 移除指定任务
	RemoveTask(ctx context.Context, queueName string, taskID string) error

	// Clear 清空队列
	Clear(ctx context.Context, queueName string) error

	// GetTasksByTimeRange 获取指定时间范围的任务
	// startTime, endTime: 执行时间范围（Unix时间戳）
	GetTasksByTimeRange(ctx context.Context, queueName string, startTime, endTime int64) ([]*DelayTask, error)

	// UpdateTaskTime 更新任务执行时间
	UpdateTaskTime(ctx context.Context, queueName string, taskID string, newExecuteAt int64) error
}

type delayQueueService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewDelayQueueService 创建延迟队列服务
func NewDelayQueueService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) DelayQueueService {
	if prefix == "" {
		prefix = "delay_queue"
	}
	return &delayQueueService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *delayQueueService) buildKey(queueName string) string {
	return fmt.Sprintf("%s:%s", s.prefix, queueName)
}

func (s *delayQueueService) serializeTask(task *DelayTask) (string, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return "", rediserr.Wrap(err, rediserr.ErrCodeSerialization, "serialize task failed")
	}
	return string(data), nil
}

func (s *delayQueueService) deserializeTask(data string) (*DelayTask, error) {
	var task DelayTask
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeSerialization, "deserialize task failed")
	}
	return &task, nil
}

func (s *delayQueueService) AddTask(ctx context.Context, queueName string, task *DelayTask) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.add_task", time.Since(start), true)
	}()

	if queueName == "" || task == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task cannot be empty")
	}

	if task.ExecuteAt == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "execute_at cannot be zero")
	}

	key := s.buildKey(queueName)

	data, err := s.serializeTask(task)
	if err != nil {
		return err
	}

	// 使用执行时间作为分数
	err = s.client.GetClient().ZAdd(ctx, key, redis.Z{
		Score:  float64(task.ExecuteAt),
		Member: data,
	}).Err()

	if err != nil {
		s.logger.Error("添加延迟任务失败",
			logger.String("queue", queueName),
			logger.String("task_id", task.ID),
			logger.Int64("execute_at", task.ExecuteAt),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add task failed")
	}

	s.logger.Info("添加延迟任务成功",
		logger.String("queue", queueName),
		logger.String("task_id", task.ID),
		logger.String("type", task.Type),
		logger.Int64("execute_at", task.ExecuteAt))

	return nil
}

func (s *delayQueueService) AddTaskWithDelay(ctx context.Context, queueName string, task *DelayTask, delay time.Duration) error {
	task.ExecuteAt = time.Now().Add(delay).Unix()
	return s.AddTask(ctx, queueName, task)
}

func (s *delayQueueService) GetReadyTasks(ctx context.Context, queueName string) ([]*DelayTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.get_ready_tasks", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)
	now := float64(time.Now().Unix())

	// 获取所有到期的任务
	results, err := s.client.GetClient().ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%f", now),
	}).Result()

	if err != nil {
		s.logger.Error("获取到期任务失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get ready tasks failed")
	}

	tasks := make([]*DelayTask, 0, len(results))
	for _, z := range results {
		if task, err := s.deserializeTask(z.Member.(string)); err == nil {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (s *delayQueueService) PopReadyTask(ctx context.Context, queueName string) (*DelayTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.pop_ready_task", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)
	now := float64(time.Now().Unix())

	// 使用 Lua 脚本保证原子性
	script := `
		local tasks = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, 1)
		if #tasks > 0 then
			redis.call('ZREM', KEYS[1], tasks[1])
			return tasks[1]
		else
			return nil
		end
	`

	result, err := s.client.GetClient().Eval(ctx, script, []string{key}, now).Result()
	if err != nil {
		s.logger.Error("弹出到期任务失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "pop ready task failed")
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

	s.logger.Info("弹出到期任务",
		logger.String("queue", queueName),
		logger.String("task_id", task.ID),
		logger.String("type", task.Type))

	return task, nil
}

func (s *delayQueueService) PopReadyTasks(ctx context.Context, queueName string, count int) ([]*DelayTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.pop_ready_tasks", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	tasks := make([]*DelayTask, 0, count)
	for i := 0; i < count; i++ {
		task, err := s.PopReadyTask(ctx, queueName)
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

func (s *delayQueueService) PeekNextTask(ctx context.Context, queueName string) (*DelayTask, time.Duration, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.peek_next_task", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	// 获取分数最小的任务（最早执行）
	results, err := s.client.GetClient().ZRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		s.logger.Error("查看下一个任务失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return nil, 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "peek next task failed")
	}

	if len(results) == 0 {
		return nil, 0, nil
	}

	task, err := s.deserializeTask(results[0].Member.(string))
	if err != nil {
		return nil, 0, err
	}

	// 计算距离执行的时间
	executeAt := time.Unix(task.ExecuteAt, 0)
	waitTime := time.Until(executeAt)
	if waitTime < 0 {
		waitTime = 0
	}

	return task, waitTime, nil
}

func (s *delayQueueService) GetCount(ctx context.Context, queueName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.get_count", time.Since(start), true)
	}()

	if queueName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	count, err := s.client.GetClient().ZCard(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取队列长度失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count failed")
	}

	return count, nil
}

func (s *delayQueueService) GetReadyCount(ctx context.Context, queueName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.get_ready_count", time.Since(start), true)
	}()

	if queueName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)
	now := float64(time.Now().Unix())

	count, err := s.client.GetClient().ZCount(ctx, key, "-inf", fmt.Sprintf("%f", now)).Result()
	if err != nil {
		s.logger.Error("获取到期任务数失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get ready count failed")
	}

	return count, nil
}

func (s *delayQueueService) RemoveTask(ctx context.Context, queueName string, taskID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.remove_task", time.Since(start), true)
	}()

	if queueName == "" || taskID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task ID cannot be empty")
	}

	key := s.buildKey(queueName)

	// 需要遍历查找包含该 taskID 的成员
	// 这是一个相对低效的操作，建议使用额外的索引
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

func (s *delayQueueService) Clear(ctx context.Context, queueName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.clear", time.Since(start), true)
	}()

	if queueName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空队列失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear queue failed")
	}

	s.logger.Info("清空队列成功", logger.String("queue", queueName))
	return nil
}

func (s *delayQueueService) GetTasksByTimeRange(ctx context.Context, queueName string, startTime, endTime int64) ([]*DelayTask, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.get_tasks_by_time_range", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", startTime),
		Max: fmt.Sprintf("%d", endTime),
	}).Result()

	if err != nil {
		s.logger.Error("获取时间范围任务失败",
			logger.String("queue", queueName),
			logger.Int64("start_time", startTime),
			logger.Int64("end_time", endTime),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get tasks by time range failed")
	}

	tasks := make([]*DelayTask, 0, len(results))
	for _, z := range results {
		if task, err := s.deserializeTask(z.Member.(string)); err == nil {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (s *delayQueueService) UpdateTaskTime(ctx context.Context, queueName string, taskID string, newExecuteAt int64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("delay_queue.update_task_time", time.Since(start), true)
	}()

	if queueName == "" || taskID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and task ID cannot be empty")
	}

	// 先移除旧任务，再添加新任务
	// 这需要先查找任务，然后更新
	// 建议使用额外的索引来优化此操作

	s.logger.Warn("UpdateTaskTime 需要额外的索引支持以提高性能",
		logger.String("queue", queueName),
		logger.String("task_id", taskID))

	return rediserr.New(rediserr.ErrCodeInternal, "not implemented - requires task index for efficiency")
}
