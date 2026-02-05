// Package order 实现 Kafka 分区内的顺序消费逻辑
// Kafka 的顺序性保证：
// 1. 生产者端：同一个 Key 的消息会被路由到同一个分区
// 2. 分区内有序：同一分区内的消息按照写入顺序存储
// 3. 消费者端：单线程顺序处理，不能使用并发
package order

import (
	"log/slog"

	"github.com/IBM/sarama"
)


// OrderHandler 实现顺序消费的处理器
// 严格按照消息在分区中的顺序进行处理
type OrderHandler struct{}

// Setup 在消费者组会话启动时调用
func (h *OrderHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup 在消费者组会话结束时调用
func (h *OrderHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim 实现顺序消费的核心逻辑
// 关键原则：单线程同步处理，严禁并发
func (h *OrderHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// 顺序消费的核心要求：
	// 1. 严禁在循环内开启 goroutine 异步处理（会破坏顺序性）
	// 2. 必须在前一条消息处理完（包括业务逻辑和 MarkMessage）后，再处理下一条
	// 3. 如果处理失败，要么重试直到成功，要么停止消费（不能跳过）
	for msg := range claim.Messages() {
		slog.Info("Ordered processing",
			"partition", msg.Partition, // 同一 Key 的消息在同一分区
			"key", string(msg.Key),     // 业务 ID（如 orderID）
			"value", string(msg.Value), // 消息内容
			"offset", msg.Offset)       // 顺序递增的偏移量

		// 执行业务逻辑（必须同步执行，不能使用 goroutine）
		// 例如：更新订单状态、扣减库存、发送通知等
		// if err := processOrder(msg); err != nil {
		//     // 处理失败的策略：
		//     // 1. 重试直到成功（适用于必须保证顺序的场景）
		//     // 2. 返回 error 停止消费，人工介入处理
		//     // 3. 记录错误并继续（会破坏业务一致性，不推荐）
		//     return err
		// }

		// 标记消息为已处理，Kafka 会记录该 offset
		// 下次重启后从下一条消息开始消费
		session.MarkMessage(msg, "")
	}
	return nil
}
