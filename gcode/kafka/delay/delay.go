package delay

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"gcode/kafka/producer"

	"github.com/IBM/sarama"
)

// DelayProducer 发送带延迟时间的消息
// 实现原理：在消息的 Header 中写入预期执行时间，消费者端根据该时间进行延迟处理
//
// 参数：
//   - p: 同步生产者实例
//   - topic: 目标主题
//   - value: 消息内容
//   - delay: 延迟时长
//
// 适用场景：
//   - 订单超时取消（下单后30分钟未支付）
//   - 定时任务触发（定时发送通知）
//   - 限时优惠到期提醒
func DelayProducer(p *producer.SyncProducer, topic string, value string, delay time.Duration) error {
	// 计算消息应该被执行的时间戳（毫秒）
	executeTime := time.Now().Add(delay).UnixNano() / int64(time.Millisecond)

	// 构造消息对象
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(strconv.FormatInt(time.Now().UnixNano(), 10)), // 使用时间戳作为 Key
		Value: sarama.StringEncoder(value),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("execute_at"),                              // Header 键名：执行时间
				Value: []byte(strconv.FormatInt(executeTime, 10)), // Header 值：时间戳（毫秒）
			},
		},
	}

	// 发送消息到 Kafka（使用 SendMessage 以保留 Headers）
	_, _, err := p.SendMessage(msg)
	return err
}

// DelayHandler 实现延迟消息的消费处理逻辑
// 通过检查消息 Header 中的 execute_at 时间戳，决定是立即处理还是延迟等待
//
// 注意事项：
// 1. 延迟等待会占用消费者线程，影响吞吐量
// 2. 不适合大量延迟消息或超长延迟时间的场景
// 3. 生产环境推荐使用专业的延迟队列方案（如 RocketMQ 延迟消息、RabbitMQ 延迟队列）
type DelayHandler struct{}

// Setup 在消费者组会话启动时调用
func (h *DelayHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup 在消费者组会话结束时调用
func (h *DelayHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim 实现延迟消费的核心逻辑
func (h *DelayHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		// 从消息 Header 中获取预期执行时间
		executeAtMs := getExecuteAt(msg)
		if executeAtMs == 0 {
			// 没有设置延迟时间，作为普通消息立即处理
			h.process(msg)
			session.MarkMessage(msg, "")
			continue
		}

		// 计算当前时间与预期执行时间的差值
		nowMs := time.Now().UnixNano() / int64(time.Millisecond)
		if nowMs < executeAtMs {
			// 还没到执行时间，进入等待状态
			waitDuration := time.Duration(executeAtMs-nowMs) * time.Millisecond
			waitCtx, cancel := context.WithTimeout(session.Context(), waitDuration)
			slog.Info("Waiting for delay message", "offset", msg.Offset, "waitMs", executeAtMs-nowMs)

			// 阻塞等待直到：1) 到达执行时间；2) Session 被取消
			<-waitCtx.Done()
			cancel()

			// 检查是否是因为 Session 结束而退出（重平衡或关闭）
			if session.Context().Err() != nil {
				return nil
			}
		}

		// 时间到达，执行业务逻辑
		h.process(msg)
		// 标记消息为已处理
		session.MarkMessage(msg, "")
	}
	return nil
}

// process 执行实际的业务处理逻辑
func (h *DelayHandler) process(msg *sarama.ConsumerMessage) {
	slog.Info("Executing delayed message", "value", string(msg.Value), "offset", msg.Offset)
	// 在这里实现具体的业务逻辑
}

// getExecuteAt 从消息 Header 中提取执行时间戳
// 返回 0 表示没有设置延迟时间
func getExecuteAt(msg *sarama.ConsumerMessage) int64 {
	for _, h := range msg.Headers {
		if string(h.Key) == "execute_at" {
			v, _ := strconv.ParseInt(string(h.Value), 10, 64)
			return v
		}
	}
	return 0 // 未找到 execute_at Header
}
