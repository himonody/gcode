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

// QueueMessage 队列消息结构
type QueueMessage struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Payload   interface{}            `json:"payload"`
	Timestamp int64                  `json:"timestamp"`
	Retry     int                    `json:"retry"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

// MessageQueueService 消息队列服务接口
// 基于 Redis List 实现的 FIFO 队列
// 适用场景：
// - 异步任务队列
// - 邮件发送队列
// - 消息通知队列
// - 日志收集队列
type MessageQueueService interface {
	// Push 推送消息到队列尾部
	// 返回队列长度
	Push(ctx context.Context, queueName string, message *QueueMessage) (int64, error)

	// Pop 从队列头部弹出消息
	// 非阻塞，如果队列为空返回 nil
	Pop(ctx context.Context, queueName string) (*QueueMessage, error)

	// BlockingPop 阻塞式弹出消息
	// timeout: 最长等待时间，0 表示无限等待
	BlockingPop(ctx context.Context, queueName string, timeout time.Duration) (*QueueMessage, error)

	// Peek 查看队列头部消息但不弹出
	Peek(ctx context.Context, queueName string) (*QueueMessage, error)

	// PeekN 查看队列前 N 条消息
	PeekN(ctx context.Context, queueName string, count int) ([]*QueueMessage, error)

	// Length 获取队列长度
	Length(ctx context.Context, queueName string) (int64, error)

	// Clear 清空队列
	Clear(ctx context.Context, queueName string) error

	// PushBatch 批量推送消息
	PushBatch(ctx context.Context, queueName string, messages []*QueueMessage) (int64, error)

	// PopBatch 批量弹出消息
	// count: 弹出数量
	PopBatch(ctx context.Context, queueName string, count int) ([]*QueueMessage, error)

	// Trim 修剪队列，保留指定范围的消息
	// start, end: 保留的索引范围（0 开始）
	Trim(ctx context.Context, queueName string, start, end int64) error

	// GetRange 获取指定范围的消息
	GetRange(ctx context.Context, queueName string, start, end int64) ([]*QueueMessage, error)
}

type messageQueueService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewMessageQueueService 创建消息队列服务
func NewMessageQueueService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) MessageQueueService {
	if prefix == "" {
		prefix = "queue"
	}
	return &messageQueueService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *messageQueueService) buildKey(queueName string) string {
	return fmt.Sprintf("%s:%s", s.prefix, queueName)
}

func (s *messageQueueService) serializeMessage(msg *QueueMessage) (string, error) {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().Unix()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return "", rediserr.Wrap(err, rediserr.ErrCodeSerialization, "serialize message failed")
	}
	return string(data), nil
}

func (s *messageQueueService) deserializeMessage(data string) (*QueueMessage, error) {
	var msg QueueMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeSerialization, "deserialize message failed")
	}
	return &msg, nil
}

func (s *messageQueueService) Push(ctx context.Context, queueName string, message *QueueMessage) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.push", time.Since(start), true)
	}()

	if queueName == "" || message == nil {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and message cannot be empty")
	}

	key := s.buildKey(queueName)

	data, err := s.serializeMessage(message)
	if err != nil {
		return 0, err
	}

	length, err := s.client.GetClient().RPush(ctx, key, data).Result()
	if err != nil {
		s.logger.Error("推送消息失败",
			logger.String("queue", queueName),
			logger.String("message_id", message.ID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "push message failed")
	}

	s.logger.Debug("推送消息成功",
		logger.String("queue", queueName),
		logger.String("message_id", message.ID),
		logger.String("type", message.Type),
		logger.Int64("queue_length", length))

	return length, nil
}

func (s *messageQueueService) Pop(ctx context.Context, queueName string) (*QueueMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.pop", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	data, err := s.client.GetClient().LPop(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		s.logger.Error("弹出消息失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "pop message failed")
	}

	msg, err := s.deserializeMessage(data)
	if err != nil {
		return nil, err
	}

	s.logger.Debug("弹出消息成功",
		logger.String("queue", queueName),
		logger.String("message_id", msg.ID),
		logger.String("type", msg.Type))

	return msg, nil
}

func (s *messageQueueService) BlockingPop(ctx context.Context, queueName string, timeout time.Duration) (*QueueMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.blocking_pop", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	result, err := s.client.GetClient().BLPop(ctx, timeout, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		s.logger.Error("阻塞弹出消息失败",
			logger.String("queue", queueName),
			logger.Duration("timeout", timeout),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "blocking pop failed")
	}

	if len(result) < 2 {
		return nil, nil
	}

	msg, err := s.deserializeMessage(result[1])
	if err != nil {
		return nil, err
	}

	s.logger.Debug("阻塞弹出消息成功",
		logger.String("queue", queueName),
		logger.String("message_id", msg.ID))

	return msg, nil
}

func (s *messageQueueService) Peek(ctx context.Context, queueName string) (*QueueMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.peek", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	data, err := s.client.GetClient().LIndex(ctx, key, 0).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		s.logger.Error("查看消息失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "peek message failed")
	}

	return s.deserializeMessage(data)
}

func (s *messageQueueService) PeekN(ctx context.Context, queueName string, count int) ([]*QueueMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.peek_n", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().LRange(ctx, key, 0, int64(count-1)).Result()
	if err != nil {
		s.logger.Error("查看多条消息失败",
			logger.String("queue", queueName),
			logger.Int("count", count),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "peek messages failed")
	}

	messages := make([]*QueueMessage, 0, len(results))
	for _, data := range results {
		if msg, err := s.deserializeMessage(data); err == nil {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

func (s *messageQueueService) Length(ctx context.Context, queueName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.length", time.Since(start), true)
	}()

	if queueName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	length, err := s.client.GetClient().LLen(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取队列长度失败",
			logger.String("queue", queueName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get length failed")
	}

	return length, nil
}

func (s *messageQueueService) Clear(ctx context.Context, queueName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.clear", time.Since(start), true)
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

func (s *messageQueueService) PushBatch(ctx context.Context, queueName string, messages []*QueueMessage) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.push_batch", time.Since(start), true)
	}()

	if queueName == "" || len(messages) == 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name and messages cannot be empty")
	}

	key := s.buildKey(queueName)

	// 序列化所有消息
	values := make([]interface{}, len(messages))
	for i, msg := range messages {
		data, err := s.serializeMessage(msg)
		if err != nil {
			return 0, err
		}
		values[i] = data
	}

	length, err := s.client.GetClient().RPush(ctx, key, values...).Result()
	if err != nil {
		s.logger.Error("批量推送消息失败",
			logger.String("queue", queueName),
			logger.Int("count", len(messages)),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "push batch failed")
	}

	s.logger.Info("批量推送消息成功",
		logger.String("queue", queueName),
		logger.Int("count", len(messages)),
		logger.Int64("queue_length", length))

	return length, nil
}

func (s *messageQueueService) PopBatch(ctx context.Context, queueName string, count int) ([]*QueueMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.pop_batch", time.Since(start), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	messages := make([]*QueueMessage, 0, count)
	for i := 0; i < count; i++ {
		msg, err := s.Pop(ctx, queueName)
		if err != nil {
			return messages, err
		}
		if msg == nil {
			break
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (s *messageQueueService) Trim(ctx context.Context, queueName string, start, end int64) error {
	start_time := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.trim", time.Since(start_time), true)
	}()

	if queueName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	err := s.client.GetClient().LTrim(ctx, key, start, end).Err()
	if err != nil {
		s.logger.Error("修剪队列失败",
			logger.String("queue", queueName),
			logger.Int64("start", start),
			logger.Int64("end", end),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "trim queue failed")
	}

	s.logger.Debug("修剪队列成功",
		logger.String("queue", queueName),
		logger.Int64("start", start),
		logger.Int64("end", end))

	return nil
}

func (s *messageQueueService) GetRange(ctx context.Context, queueName string, start, end int64) ([]*QueueMessage, error) {
	start_time := time.Now()
	defer func() {
		s.metrics.RecordOperation("queue.get_range", time.Since(start_time), true)
	}()

	if queueName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "queue name cannot be empty")
	}

	key := s.buildKey(queueName)

	results, err := s.client.GetClient().LRange(ctx, key, start, end).Result()
	if err != nil {
		s.logger.Error("获取范围消息失败",
			logger.String("queue", queueName),
			logger.Int64("start", start),
			logger.Int64("end", end),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get range failed")
	}

	messages := make([]*QueueMessage, 0, len(results))
	for _, data := range results {
		if msg, err := s.deserializeMessage(data); err == nil {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}
