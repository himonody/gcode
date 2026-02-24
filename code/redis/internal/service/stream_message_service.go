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

// StreamMessage 流消息结构
type StreamMessage struct {
	ID        string                 `json:"id"`
	Timestamp int64                  `json:"timestamp"`
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
}

// MessageStreamService 消息流服务接口
// 基于 Redis Stream 实现的消息流处理
// 适用场景：
// - 消息队列
// - 事件流处理
// - 日志收集
// - 实时数据管道
// - 消息持久化
type MessageStreamService interface {
	// Add 添加消息到流
	// 返回消息ID
	Add(ctx context.Context, streamKey string, message *StreamMessage) (string, error)

	// AddWithID 使用指定ID添加消息
	AddWithID(ctx context.Context, streamKey string, messageID string, message *StreamMessage) error

	// Read 读取消息
	// count: 读取数量，0表示不限制
	// startID: 起始ID，"0"表示从头开始，"$"表示只读新消息
	Read(ctx context.Context, streamKey string, startID string, count int64) ([]*StreamMessage, error)

	// ReadNew 读取新消息（阻塞）
	// block: 阻塞时间，0表示永久阻塞
	ReadNew(ctx context.Context, streamKey string, block time.Duration) ([]*StreamMessage, error)

	// ReadRange 读取指定范围的消息
	// start, end: 消息ID范围，"-"表示最小，"+"表示最大
	ReadRange(ctx context.Context, streamKey string, start, end string, count int64) ([]*StreamMessage, error)

	// GetLength 获取流长度
	GetLength(ctx context.Context, streamKey string) (int64, error)

	// GetInfo 获取流信息
	GetInfo(ctx context.Context, streamKey string) (map[string]interface{}, error)

	// Trim 修剪流
	// maxLen: 保留的最大消息数
	// approximate: 是否使用近似修剪（性能更好）
	Trim(ctx context.Context, streamKey string, maxLen int64, approximate bool) (int64, error)

	// TrimByTime 按时间修剪流
	// minID: 最小消息ID（时间戳格式）
	TrimByTime(ctx context.Context, streamKey string, minID string) (int64, error)

	// Delete 删除指定消息
	Delete(ctx context.Context, streamKey string, messageIDs ...string) (int64, error)

	// GetLastID 获取最后一条消息的ID
	GetLastID(ctx context.Context, streamKey string) (string, error)

	// BatchAdd 批量添加消息
	BatchAdd(ctx context.Context, streamKey string, messages []*StreamMessage) ([]string, error)
}

type messageStreamService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewMessageStreamService 创建消息流服务
func NewMessageStreamService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) MessageStreamService {
	if prefix == "" {
		prefix = "stream"
	}
	return &messageStreamService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *messageStreamService) buildKey(streamKey string) string {
	return fmt.Sprintf("%s:%s", s.prefix, streamKey)
}

func (s *messageStreamService) messageToValues(message *StreamMessage) map[string]interface{} {
	if message.Timestamp == 0 {
		message.Timestamp = time.Now().UnixMilli()
	}

	payloadJSON, _ := json.Marshal(message.Payload)

	return map[string]interface{}{
		"type":      message.Type,
		"timestamp": message.Timestamp,
		"payload":   string(payloadJSON),
	}
}

func (s *messageStreamService) xmessageToStreamMessage(xmsg redis.XMessage) (*StreamMessage, error) {
	msg := &StreamMessage{
		ID:      xmsg.ID,
		Payload: make(map[string]interface{}),
	}

	if msgType, ok := xmsg.Values["type"].(string); ok {
		msg.Type = msgType
	}

	if timestamp, ok := xmsg.Values["timestamp"].(string); ok {
		fmt.Sscanf(timestamp, "%d", &msg.Timestamp)
	}

	if payloadStr, ok := xmsg.Values["payload"].(string); ok {
		json.Unmarshal([]byte(payloadStr), &msg.Payload)
	}

	return msg, nil
}

func (s *messageStreamService) Add(ctx context.Context, streamKey string, message *StreamMessage) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.add", time.Since(start), true)
	}()

	if streamKey == "" || message == nil {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and message cannot be empty")
	}

	key := s.buildKey(streamKey)
	values := s.messageToValues(message)

	messageID, err := s.client.GetClient().XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		Values: values,
	}).Result()

	if err != nil {
		s.logger.Error("添加消息到流失败",
			logger.String("stream", streamKey),
			logger.String("type", message.Type),
			logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "add message failed")
	}

	s.logger.Debug("添加消息到流",
		logger.String("stream", streamKey),
		logger.String("message_id", messageID),
		logger.String("type", message.Type))

	return messageID, nil
}

func (s *messageStreamService) AddWithID(ctx context.Context, streamKey string, messageID string, message *StreamMessage) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.add_with_id", time.Since(start), true)
	}()

	if streamKey == "" || messageID == "" || message == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "stream key, message ID and message cannot be empty")
	}

	key := s.buildKey(streamKey)
	values := s.messageToValues(message)

	_, err := s.client.GetClient().XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		ID:     messageID,
		Values: values,
	}).Result()

	if err != nil {
		s.logger.Error("使用指定ID添加消息失败",
			logger.String("stream", streamKey),
			logger.String("message_id", messageID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add message with id failed")
	}

	s.logger.Debug("使用指定ID添加消息",
		logger.String("stream", streamKey),
		logger.String("message_id", messageID))

	return nil
}

func (s *messageStreamService) Read(ctx context.Context, streamKey string, startID string, count int64) ([]*StreamMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.read", time.Since(start), true)
	}()

	if streamKey == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	if startID == "" {
		startID = "0"
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XRead(ctx, &redis.XReadArgs{
		Streams: []string{key, startID},
		Count:   count,
	}).Result()

	if err == redis.Nil {
		return []*StreamMessage{}, nil
	}

	if err != nil {
		s.logger.Error("读取消息失败",
			logger.String("stream", streamKey),
			logger.String("start_id", startID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "read messages failed")
	}

	messages := make([]*StreamMessage, 0)
	for _, stream := range xmsgs {
		for _, xmsg := range stream.Messages {
			if msg, err := s.xmessageToStreamMessage(xmsg); err == nil {
				messages = append(messages, msg)
			}
		}
	}

	return messages, nil
}

func (s *messageStreamService) ReadNew(ctx context.Context, streamKey string, block time.Duration) ([]*StreamMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.read_new", time.Since(start), true)
	}()

	if streamKey == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XRead(ctx, &redis.XReadArgs{
		Streams: []string{key, "$"},
		Block:   block,
		Count:   0,
	}).Result()

	if err == redis.Nil {
		return []*StreamMessage{}, nil
	}

	if err != nil {
		s.logger.Error("读取新消息失败",
			logger.String("stream", streamKey),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "read new messages failed")
	}

	messages := make([]*StreamMessage, 0)
	for _, stream := range xmsgs {
		for _, xmsg := range stream.Messages {
			if msg, err := s.xmessageToStreamMessage(xmsg); err == nil {
				messages = append(messages, msg)
			}
		}
	}

	return messages, nil
}

func (s *messageStreamService) ReadRange(ctx context.Context, streamKey string, start, end string, count int64) ([]*StreamMessage, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.read_range", time.Since(startTime), true)
	}()

	if streamKey == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	if start == "" {
		start = "-"
	}
	if end == "" {
		end = "+"
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XRange(ctx, key, start, end).Result()
	if err != nil {
		s.logger.Error("读取范围消息失败",
			logger.String("stream", streamKey),
			logger.String("start", start),
			logger.String("end", end),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "read range failed")
	}

	messages := make([]*StreamMessage, 0, len(xmsgs))
	for _, xmsg := range xmsgs {
		if msg, err := s.xmessageToStreamMessage(xmsg); err == nil {
			messages = append(messages, msg)
			if count > 0 && int64(len(messages)) >= count {
				break
			}
		}
	}

	return messages, nil
}

func (s *messageStreamService) GetLength(ctx context.Context, streamKey string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.get_length", time.Since(start), true)
	}()

	if streamKey == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	key := s.buildKey(streamKey)

	length, err := s.client.GetClient().XLen(ctx, key).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get length failed")
	}

	return length, nil
}

func (s *messageStreamService) GetInfo(ctx context.Context, streamKey string) (map[string]interface{}, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.get_info", time.Since(start), true)
	}()

	if streamKey == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	key := s.buildKey(streamKey)

	info, err := s.client.GetClient().XInfoStream(ctx, key).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get info failed")
	}

	result := map[string]interface{}{
		"length":            info.Length,
		"radix_tree_keys":   info.RadixTreeKeys,
		"radix_tree_nodes":  info.RadixTreeNodes,
		"groups":            info.Groups,
		"last_generated_id": info.LastGeneratedID,
	}

	return result, nil
}

func (s *messageStreamService) Trim(ctx context.Context, streamKey string, maxLen int64, approximate bool) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.trim", time.Since(start), true)
	}()

	if streamKey == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	if maxLen <= 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "max length must be positive")
	}

	key := s.buildKey(streamKey)

	deleted, err := s.client.GetClient().XTrimMaxLen(ctx, key, maxLen).Result()
	if err != nil {
		s.logger.Error("修剪流失败",
			logger.String("stream", streamKey),
			logger.Int64("max_len", maxLen),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "trim failed")
	}

	s.logger.Info("修剪流",
		logger.String("stream", streamKey),
		logger.Int64("max_len", maxLen),
		logger.Int64("deleted", deleted))

	return deleted, nil
}

func (s *messageStreamService) TrimByTime(ctx context.Context, streamKey string, minID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.trim_by_time", time.Since(start), true)
	}()

	if streamKey == "" || minID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and min ID cannot be empty")
	}

	key := s.buildKey(streamKey)

	deleted, err := s.client.GetClient().XTrimMinID(ctx, key, minID).Result()
	if err != nil {
		s.logger.Error("按时间修剪流失败",
			logger.String("stream", streamKey),
			logger.String("min_id", minID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "trim by time failed")
	}

	s.logger.Info("按时间修剪流",
		logger.String("stream", streamKey),
		logger.String("min_id", minID),
		logger.Int64("deleted", deleted))

	return deleted, nil
}

func (s *messageStreamService) Delete(ctx context.Context, streamKey string, messageIDs ...string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.delete", time.Since(start), true)
	}()

	if streamKey == "" || len(messageIDs) == 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and message IDs cannot be empty")
	}

	key := s.buildKey(streamKey)

	deleted, err := s.client.GetClient().XDel(ctx, key, messageIDs...).Result()
	if err != nil {
		s.logger.Error("删除消息失败",
			logger.String("stream", streamKey),
			logger.Int("count", len(messageIDs)),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "delete messages failed")
	}

	s.logger.Info("删除消息",
		logger.String("stream", streamKey),
		logger.Int64("deleted", deleted))

	return deleted, nil
}

func (s *messageStreamService) GetLastID(ctx context.Context, streamKey string) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.get_last_id", time.Since(start), true)
	}()

	if streamKey == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XRevRangeN(ctx, key, "+", "-", 1).Result()
	if err != nil {
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "get last id failed")
	}

	if len(xmsgs) == 0 {
		return "", nil
	}

	return xmsgs[0].ID, nil
}

func (s *messageStreamService) BatchAdd(ctx context.Context, streamKey string, messages []*StreamMessage) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("stream.batch_add", time.Since(start), true)
	}()

	if streamKey == "" || len(messages) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and messages cannot be empty")
	}

	key := s.buildKey(streamKey)

	pipe := s.client.GetClient().Pipeline()
	cmds := make([]*redis.StringCmd, len(messages))

	for i, message := range messages {
		values := s.messageToValues(message)
		cmds[i] = pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: key,
			Values: values,
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量添加消息失败",
			logger.String("stream", streamKey),
			logger.Int("count", len(messages)),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch add failed")
	}

	messageIDs := make([]string, len(cmds))
	for i, cmd := range cmds {
		messageIDs[i], _ = cmd.Result()
	}

	s.logger.Info("批量添加消息",
		logger.String("stream", streamKey),
		logger.Int("count", len(messages)))

	return messageIDs, nil
}
