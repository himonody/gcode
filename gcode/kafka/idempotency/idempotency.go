// Package idempotency 实现业务层面的幂等性校验，防止消息重复消费带来的数据问题。
package idempotency

import (
	"context"
	"log/slog"

	"github.com/IBM/sarama"
)

// Store 幂等性存储接口。
// 在分布式生产环境中，建议使用 Redis（SETNX）或 数据库唯一索引（INSERT IGNORE）来实现。
type Store interface {
	// CheckAndSet 检查 key 是否存在。
	// 若不存在，则设置该 key 并返回 true, nil。
	// 若已存在，则返回 false, nil。
	CheckAndSet(ctx context.Context, key string) (bool, error)
}

// IdempotentHandler 带有幂等校验的消费者处理器。
type IdempotentHandler struct {
	store Store
}

// NewIdempotentHandler 创建一个幂等处理器。
func NewIdempotentHandler(store Store) *IdempotentHandler {
	return &IdempotentHandler{store: store}
}

func (h *IdempotentHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *IdempotentHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }
func (h *IdempotentHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	ctx := session.Context()
	for msg := range claim.Messages() {
		// 1. 获取消息的唯一标识。
		// 生产中建议由生产者在 Header 中注入一个全局唯一的 Request-ID。
		msgID := getMessageID(msg)
		if msgID == "" {
			slog.Warn("Message missing ID, skipping idempotency check", "offset", msg.Offset)
			// 如果没有唯一 ID，取决于业务：是可以接受重复，还是直接丢弃。
			_ = doWork(msg)
			session.MarkMessage(msg, "")
			continue
		}

		// 2. 幂等性检查（Check-and-Set）。
		ok, err := h.store.CheckAndSet(ctx, msgID)
		if err != nil {
			slog.Error("Idempotency check storage error", "msgID", msgID, "err", err)
			// 存储层故障时的策略：
			// - 停止消费（返回 err）：保证绝对数据准确，但会导致业务中断。
			// - 跳过此消息（continue）：可能导致丢消息。
			// - 降级处理（继续流程）：可能导致重复消费。
			continue
		}
		if !ok {
			// 说明该消息之前已经处理过（或正在处理中）。
			slog.Warn("Message already processed, skipping", "msgID", msgID)
			session.MarkMessage(msg, "")
			continue
		}

		// 3. 执行核心业务逻辑。
		slog.Info("Processing message idempotently", "msgID", msgID)
		if err := doWork(msg); err != nil {
			slog.Error("Business logic failed", "msgID", msgID, "err", err)
			// 注意：如果业务失败了，且你没有回滚 CheckAndSet 的状态，
			// 那么这条消息下次重试时会被 CheckAndSet 拦截导致跳过（造成“假成功”）。
			// 生产建议：使用数据库事务保证“业务处理”和“幂等记录更新”的原子性。
			continue
		}

		// 4. 处理成功，标记位移。
		session.MarkMessage(msg, "")
	}
	return nil
}

// getMessageID 从消息中提取唯一 ID。
func getMessageID(msg *sarama.ConsumerMessage) string {
	// 方案 A：从自定义 Header 获取（推荐）。
	for _, h := range msg.Headers {
		if string(h.Key) == "X-Request-ID" {
			return string(h.Value)
		}
	}
	// 方案 B：使用消息的 Key（如果能保证 Key 唯一）。
	if len(msg.Key) > 0 {
		return string(msg.Key)
	}
	// 方案 C：兜底使用 Partition-Offset（仅能防止 Kafka 层面的重试重复，不能防止业务重发）。
	return ""
}

// doWork 执行实际的业务逻辑。
func doWork(msg *sarama.ConsumerMessage) error {
	return nil
}
