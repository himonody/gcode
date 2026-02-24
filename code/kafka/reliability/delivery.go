// Package reliability 提供可靠性消费的处理器实现
// 确保消息处理成功后才标记为已消费，防止消息丢失
package reliability

import (
	"log/slog"

	"github.com/IBM/sarama"
)

// ReliableConsumerHandler 演示手动提交以确保消息处理后再标记。
type ReliableConsumerHandler struct{}

func (h *ReliableConsumerHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *ReliableConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }
func (h *ReliableConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// 注意：在生产环境中，Reliable 处理通常意味着在当前协程同步处理。
	// 如果开启协程处理，必须确保所有协程处理完后才能进行位移提交，否则可能丢失消息。
	for msg := range claim.Messages() {
		slog.Info("Processing message", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset)

		// 执行业务逻辑...
		if err := processBusiness(msg); err != nil {
			slog.Error("Business processing failed", "err", err)
			// 可选策略：
			// 1. 直接返回 err：会触发消费组重启/重平衡，稍后重试（可能导致死循环）。
			// 2. 发送到重试/死信队列（推荐，见 dlq_retry 模块）。
			// 3. 记录日志并跳过。
			continue
		}

		// 处理成功后手动标记该消息为已消费。
		// Sarama 会根据 AutoCommit 配置定期将已标记的 Offset 提交到 Kafka。
		session.MarkMessage(msg, "")
	}
	return nil
}

// processBusiness 模拟实际业务逻辑
// 生产环境中这里通常包含数据库操作、HTTP 调用等
func processBusiness(msg *sarama.ConsumerMessage) error {
	// 实现具体的业务逻辑
	return nil
}
