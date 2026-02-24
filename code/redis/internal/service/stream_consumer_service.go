package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// ConsumerGroupService 消费者组服务接口
// 基于 Redis Stream 消费者组实现的分布式消息处理
// 适用场景：
// - 分布式任务处理
// - 消息队列消费
// - 负载均衡
// - 消息确认机制
// - 消费进度追踪
type ConsumerGroupService interface {
	// CreateGroup 创建消费者组
	// startID: 起始消息ID，"0"表示从头开始，"$"表示从最新消息开始
	CreateGroup(ctx context.Context, streamKey string, groupName string, startID string) error

	// DestroyGroup 销毁消费者组
	DestroyGroup(ctx context.Context, streamKey string, groupName string) error

	// ReadGroup 从消费者组读取消息
	// consumerName: 消费者名称
	// count: 读取数量
	// block: 阻塞时间，0表示不阻塞
	ReadGroup(ctx context.Context, streamKey string, groupName string, consumerName string, count int64, block time.Duration) ([]*StreamMessage, error)

	// ReadPending 读取待处理消息
	// 读取已分配但未确认的消息
	ReadPending(ctx context.Context, streamKey string, groupName string, consumerName string, count int64) ([]*StreamMessage, error)

	// Ack 确认消息
	// 标记消息已处理
	Ack(ctx context.Context, streamKey string, groupName string, messageIDs ...string) (int64, error)

	// Claim 认领消息
	// 将其他消费者的超时消息认领到当前消费者
	// minIdleTime: 最小空闲时间，超过此时间的消息可被认领
	Claim(ctx context.Context, streamKey string, groupName string, consumerName string, minIdleTime time.Duration, messageIDs ...string) ([]*StreamMessage, error)

	// AutoClaim 自动认领消息
	// 自动查找并认领超时消息
	AutoClaim(ctx context.Context, streamKey string, groupName string, consumerName string, minIdleTime time.Duration, start string, count int64) ([]*StreamMessage, string, error)

	// GetPendingInfo 获取待处理消息信息
	GetPendingInfo(ctx context.Context, streamKey string, groupName string) (*PendingInfo, error)

	// GetConsumerPendingInfo 获取指定消费者的待处理消息信息
	GetConsumerPendingInfo(ctx context.Context, streamKey string, groupName string, consumerName string, count int64) ([]*PendingMessage, error)

	// DeleteConsumer 删除消费者
	DeleteConsumer(ctx context.Context, streamKey string, groupName string, consumerName string) (int64, error)

	// GetGroupInfo 获取消费者组信息
	GetGroupInfo(ctx context.Context, streamKey string) ([]*GroupInfo, error)

	// GetConsumersInfo 获取消费者信息
	GetConsumersInfo(ctx context.Context, streamKey string, groupName string) ([]*ConsumerInfo, error)

	// SetID 设置消费者组的最后交付ID
	SetID(ctx context.Context, streamKey string, groupName string, messageID string) error
}

// PendingInfo 待处理消息信息
type PendingInfo struct {
	Count     int64
	Lower     string
	Higher    string
	Consumers map[string]int64
}

// PendingMessage 待处理消息详情
type PendingMessage struct {
	ID         string
	Consumer   string
	IdleTime   time.Duration
	RetryCount int64
}

// GroupInfo 消费者组信息
type GroupInfo struct {
	Name            string
	Consumers       int64
	Pending         int64
	LastDeliveredID string
}

// ConsumerInfo 消费者信息
type ConsumerInfo struct {
	Name    string
	Pending int64
	Idle    time.Duration
}

type consumerGroupService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewConsumerGroupService 创建消费者组服务
func NewConsumerGroupService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) ConsumerGroupService {
	if prefix == "" {
		prefix = "stream"
	}
	return &consumerGroupService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *consumerGroupService) buildKey(streamKey string) string {
	return fmt.Sprintf("%s:%s", s.prefix, streamKey)
}

func (s *consumerGroupService) CreateGroup(ctx context.Context, streamKey string, groupName string, startID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.create", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and group name cannot be empty")
	}

	if startID == "" {
		startID = "0"
	}

	key := s.buildKey(streamKey)

	err := s.client.GetClient().XGroupCreateMkStream(ctx, key, groupName, startID).Err()
	if err != nil {
		s.logger.Error("创建消费者组失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.String("start_id", startID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "create group failed")
	}

	s.logger.Info("创建消费者组",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.String("start_id", startID))

	return nil
}

func (s *consumerGroupService) DestroyGroup(ctx context.Context, streamKey string, groupName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.destroy", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and group name cannot be empty")
	}

	key := s.buildKey(streamKey)

	err := s.client.GetClient().XGroupDestroy(ctx, key, groupName).Err()
	if err != nil {
		s.logger.Error("销毁消费者组失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "destroy group failed")
	}

	s.logger.Info("销毁消费者组",
		logger.String("stream", streamKey),
		logger.String("group", groupName))

	return nil
}

func (s *consumerGroupService) ReadGroup(ctx context.Context, streamKey string, groupName string, consumerName string, count int64, block time.Duration) ([]*StreamMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.read", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" || consumerName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key, group name and consumer name cannot be empty")
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    groupName,
		Consumer: consumerName,
		Streams:  []string{key, ">"},
		Count:    count,
		Block:    block,
	}).Result()

	if err == redis.Nil {
		return []*StreamMessage{}, nil
	}

	if err != nil {
		s.logger.Error("从消费者组读取消息失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.String("consumer", consumerName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "read group failed")
	}

	messages := make([]*StreamMessage, 0)
	for _, stream := range xmsgs {
		for _, xmsg := range stream.Messages {
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

			messages = append(messages, msg)
		}
	}

	s.logger.Debug("从消费者组读取消息",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.String("consumer", consumerName),
		logger.Int("count", len(messages)))

	return messages, nil
}

func (s *consumerGroupService) ReadPending(ctx context.Context, streamKey string, groupName string, consumerName string, count int64) ([]*StreamMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.read_pending", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" || consumerName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key, group name and consumer name cannot be empty")
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    groupName,
		Consumer: consumerName,
		Streams:  []string{key, "0"},
		Count:    count,
	}).Result()

	if err == redis.Nil {
		return []*StreamMessage{}, nil
	}

	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "read pending failed")
	}

	messages := make([]*StreamMessage, 0)
	for _, stream := range xmsgs {
		for _, xmsg := range stream.Messages {
			msg := &StreamMessage{
				ID:      xmsg.ID,
				Payload: make(map[string]interface{}),
			}
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

func (s *consumerGroupService) Ack(ctx context.Context, streamKey string, groupName string, messageIDs ...string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.ack", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" || len(messageIDs) == 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key, group name and message IDs cannot be empty")
	}

	key := s.buildKey(streamKey)

	acked, err := s.client.GetClient().XAck(ctx, key, groupName, messageIDs...).Result()
	if err != nil {
		s.logger.Error("确认消息失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.Int("count", len(messageIDs)),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "ack failed")
	}

	s.logger.Debug("确认消息",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.Int64("acked", acked))

	return acked, nil
}

func (s *consumerGroupService) Claim(ctx context.Context, streamKey string, groupName string, consumerName string, minIdleTime time.Duration, messageIDs ...string) ([]*StreamMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.claim", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" || consumerName == "" || len(messageIDs) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "parameters cannot be empty")
	}

	key := s.buildKey(streamKey)

	xmsgs, err := s.client.GetClient().XClaim(ctx, &redis.XClaimArgs{
		Stream:   key,
		Group:    groupName,
		Consumer: consumerName,
		MinIdle:  minIdleTime,
		Messages: messageIDs,
	}).Result()

	if err != nil {
		s.logger.Error("认领消息失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.String("consumer", consumerName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "claim failed")
	}

	messages := make([]*StreamMessage, 0, len(xmsgs))
	for _, xmsg := range xmsgs {
		msg := &StreamMessage{
			ID:      xmsg.ID,
			Payload: make(map[string]interface{}),
		}
		messages = append(messages, msg)
	}

	s.logger.Info("认领消息",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.String("consumer", consumerName),
		logger.Int("count", len(messages)))

	return messages, nil
}

func (s *consumerGroupService) AutoClaim(ctx context.Context, streamKey string, groupName string, consumerName string, minIdleTime time.Duration, start string, count int64) ([]*StreamMessage, string, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.auto_claim", time.Since(startTime), true)
	}()

	if streamKey == "" || groupName == "" || consumerName == "" {
		return nil, "", rediserr.New(rediserr.ErrCodeInvalidInput, "parameters cannot be empty")
	}

	if start == "" {
		start = "0-0"
	}

	key := s.buildKey(streamKey)

	xmsgs, nextStart, err := s.client.GetClient().XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   key,
		Group:    groupName,
		Consumer: consumerName,
		MinIdle:  minIdleTime,
		Start:    start,
		Count:    count,
	}).Result()

	if err != nil {
		s.logger.Error("自动认领消息失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.String("consumer", consumerName),
			logger.ErrorField(err))
		return nil, "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "auto claim failed")
	}

	messages := make([]*StreamMessage, 0, len(xmsgs))
	for _, xmsg := range xmsgs {
		msg := &StreamMessage{
			ID:      xmsg.ID,
			Payload: make(map[string]interface{}),
		}
		messages = append(messages, msg)
	}

	s.logger.Info("自动认领消息",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.String("consumer", consumerName),
		logger.Int("count", len(messages)),
		logger.String("next_start", nextStart))

	return messages, nextStart, nil
}

func (s *consumerGroupService) GetPendingInfo(ctx context.Context, streamKey string, groupName string) (*PendingInfo, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.get_pending_info", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and group name cannot be empty")
	}

	key := s.buildKey(streamKey)

	pending, err := s.client.GetClient().XPending(ctx, key, groupName).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get pending info failed")
	}

	info := &PendingInfo{
		Count:     pending.Count,
		Lower:     pending.Lower,
		Higher:    pending.Higher,
		Consumers: pending.Consumers,
	}

	return info, nil
}

func (s *consumerGroupService) GetConsumerPendingInfo(ctx context.Context, streamKey string, groupName string, consumerName string, count int64) ([]*PendingMessage, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.get_consumer_pending_info", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and group name cannot be empty")
	}

	key := s.buildKey(streamKey)

	args := &redis.XPendingExtArgs{
		Stream: key,
		Group:  groupName,
		Start:  "-",
		End:    "+",
		Count:  count,
	}

	if consumerName != "" {
		args.Consumer = consumerName
	}

	pendings, err := s.client.GetClient().XPendingExt(ctx, args).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get consumer pending info failed")
	}

	messages := make([]*PendingMessage, 0, len(pendings))
	for _, p := range pendings {
		msg := &PendingMessage{
			ID:         p.ID,
			Consumer:   p.Consumer,
			IdleTime:   p.Idle,
			RetryCount: p.RetryCount,
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (s *consumerGroupService) DeleteConsumer(ctx context.Context, streamKey string, groupName string, consumerName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.delete_consumer", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" || consumerName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "parameters cannot be empty")
	}

	key := s.buildKey(streamKey)

	deleted, err := s.client.GetClient().XGroupDelConsumer(ctx, key, groupName, consumerName).Result()
	if err != nil {
		s.logger.Error("删除消费者失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.String("consumer", consumerName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "delete consumer failed")
	}

	s.logger.Info("删除消费者",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.String("consumer", consumerName),
		logger.Int64("pending_deleted", deleted))

	return deleted, nil
}

func (s *consumerGroupService) GetGroupInfo(ctx context.Context, streamKey string) ([]*GroupInfo, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.get_group_info", time.Since(start), true)
	}()

	if streamKey == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key cannot be empty")
	}

	key := s.buildKey(streamKey)

	groups, err := s.client.GetClient().XInfoGroups(ctx, key).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get group info failed")
	}

	infos := make([]*GroupInfo, 0, len(groups))
	for _, g := range groups {
		info := &GroupInfo{
			Name:            g.Name,
			Consumers:       g.Consumers,
			Pending:         g.Pending,
			LastDeliveredID: g.LastDeliveredID,
		}
		infos = append(infos, info)
	}

	return infos, nil
}

func (s *consumerGroupService) GetConsumersInfo(ctx context.Context, streamKey string, groupName string) ([]*ConsumerInfo, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.get_consumers_info", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "stream key and group name cannot be empty")
	}

	key := s.buildKey(streamKey)

	consumers, err := s.client.GetClient().XInfoConsumers(ctx, key, groupName).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get consumers info failed")
	}

	infos := make([]*ConsumerInfo, 0, len(consumers))
	for _, c := range consumers {
		info := &ConsumerInfo{
			Name:    c.Name,
			Pending: c.Pending,
			Idle:    c.Idle,
		}
		infos = append(infos, info)
	}

	return infos, nil
}

func (s *consumerGroupService) SetID(ctx context.Context, streamKey string, groupName string, messageID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("consumer_group.set_id", time.Since(start), true)
	}()

	if streamKey == "" || groupName == "" || messageID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "parameters cannot be empty")
	}

	key := s.buildKey(streamKey)

	err := s.client.GetClient().XGroupSetID(ctx, key, groupName, messageID).Err()
	if err != nil {
		s.logger.Error("设置消费者组ID失败",
			logger.String("stream", streamKey),
			logger.String("group", groupName),
			logger.String("message_id", messageID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set id failed")
	}

	s.logger.Info("设置消费者组ID",
		logger.String("stream", streamKey),
		logger.String("group", groupName),
		logger.String("message_id", messageID))

	return nil
}
