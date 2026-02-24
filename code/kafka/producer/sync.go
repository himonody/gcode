package producer

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"gcode/kafka/options"

	"github.com/IBM/sarama"
)

// SyncProducer 包装了 sarama.SyncProducer，提供同步发送能力
// 每次发送都会阻塞等待 Broker 确认，适用于对可靠性要求高的场景
type SyncProducer struct {
	internal     sarama.SyncProducer
	sendCount    int64 // 发送次数
	successCount int64 // 成功次数
	failureCount int64 // 失败次数
	closed       int32 // 关闭标志，使用 CAS 保证只关闭一次
}

// ProducerStats 生产者统计信息
type ProducerStats struct {
	SendCount    int64   // 总发送次数
	SuccessCount int64   // 成功次数
	FailureCount int64   // 失败次数
	SuccessRate  float64 // 成功率
}

// NewSyncProducer 创建一个新的同步生产者
// reliable=true 时开启幂等性和 acks=-1，确保消息不丢失
func NewSyncProducer(brokers []string, reliable bool) (*SyncProducer, error) {
	config := options.GetProducerConfig(reliable)
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync producer: %w", err)
	}

	return &SyncProducer{
		internal: producer,
	}, nil
}

// Send 发送单条消息到指定主题
// 返回分区号和偏移量，发生错误时返回 -1, -1, error
func (p *SyncProducer) Send(topic, key, value string) (partition int32, offset int64, err error) {
	// 参数验证
	if topic == "" {
		return -1, -1, fmt.Errorf("topic cannot be empty")
	}

	// 构造消息
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(value),
	}
	if key != "" {
		msg.Key = sarama.StringEncoder(key)
	}

	return p.SendMessage(msg)
}

// SendMessage 发送 ProducerMessage 消息
// 支持自定义 Headers、Key、Value 等完整配置
func (p *SyncProducer) SendMessage(msg *sarama.ProducerMessage) (partition int32, offset int64, err error) {
	// 参数验证
	if msg == nil || msg.Topic == "" {
		return -1, -1, fmt.Errorf("message or topic cannot be empty")
	}

	// 发送消息
	atomic.AddInt64(&p.sendCount, 1)
	partition, offset, err = p.internal.SendMessage(msg)

	if err != nil {
		atomic.AddInt64(&p.failureCount, 1)
		slog.Error("Failed to send message",
			"topic", msg.Topic,
			"error", err,
			"error_type", classifyError(err),
		)
		return -1, -1, fmt.Errorf("send message failed: %w", err)
	}

	atomic.AddInt64(&p.successCount, 1)
	slog.Debug("Message sent successfully",
		"topic", msg.Topic,
		"partition", partition,
		"offset", offset,
	)

	return partition, offset, nil
}

// SendWithContext 带超时控制的发送
func (p *SyncProducer) SendWithContext(ctx context.Context, topic, key, value string) (partition int32, offset int64, err error) {
	// 使用 channel 实现超时控制
	type result struct {
		partition int32
		offset    int64
		err       error
	}

	resultChan := make(chan result, 1)

	go func() {
		partition, offset, err := p.Send(topic, key, value)
		resultChan <- result{partition, offset, err}
	}()

	select {
	case res := <-resultChan:
		return res.partition, res.offset, res.err
	case <-ctx.Done():
		atomic.AddInt64(&p.failureCount, 1)
		return -1, -1, fmt.Errorf("send timeout: %w", ctx.Err())
	}
}

// SendResult 批量发送的结果
type SendResult struct {
	Index     int   // 消息索引
	Partition int32 // 分区号
	Offset    int64 // 偏移量
	Error     error // 错误信息
}

// SendBatch 批量发送消息
func (p *SyncProducer) SendBatch(messages []*sarama.ProducerMessage) ([]SendResult, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages cannot be empty")
	}

	results := make([]SendResult, len(messages))

	for i, msg := range messages {
		atomic.AddInt64(&p.sendCount, 1)
		partition, offset, err := p.internal.SendMessage(msg)

		if err != nil {
			atomic.AddInt64(&p.failureCount, 1)
			results[i] = SendResult{Index: i, Partition: -1, Offset: -1, Error: err}
		} else {
			atomic.AddInt64(&p.successCount, 1)
			results[i] = SendResult{Index: i, Partition: partition, Offset: offset, Error: nil}
		}
	}

	return results, nil
}

// GetStats 获取统计信息
func (p *SyncProducer) GetStats() ProducerStats {
	sendCount := atomic.LoadInt64(&p.sendCount)
	successCount := atomic.LoadInt64(&p.successCount)
	failureCount := atomic.LoadInt64(&p.failureCount)

	var successRate float64
	if sendCount > 0 {
		successRate = float64(successCount) / float64(sendCount) * 100
	}

	return ProducerStats{
		SendCount:    sendCount,
		SuccessCount: successCount,
		FailureCount: failureCount,
		SuccessRate:  successRate,
	}
}

// Close 关闭生产者，释放资源
func (p *SyncProducer) Close() error {
	// 使用 CAS 保证只关闭一次
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return fmt.Errorf("producer already closed")
	}

	// 输出最终统计信息
	stats := p.GetStats()
	slog.Info("Producer closing",
		"total_sent", stats.SendCount,
		"success", stats.SuccessCount,
		"failed", stats.FailureCount,
		"success_rate", fmt.Sprintf("%.2f%%", stats.SuccessRate),
	)

	return p.internal.Close()
}

// classifyError 对错误进行分类
func classifyError(err error) string {
	if err == nil {
		return "none"
	}

	// Kafka 特定错误
	switch err {
	case sarama.ErrOutOfBrokers:
		return "no_available_brokers"
	case sarama.ErrNotConnected:
		return "not_connected"
	case sarama.ErrInsufficientData:
		return "insufficient_data"
	case sarama.ErrShuttingDown:
		return "shutting_down"
	case sarama.ErrMessageSizeTooLarge:
		return "message_too_large"
	case sarama.ErrNotLeaderForPartition:
		return "not_leader"
	case sarama.ErrRequestTimedOut:
		return "timeout"
	case sarama.ErrBrokerNotAvailable:
		return "broker_unavailable"
	default:
		return "unknown"
	}
}
