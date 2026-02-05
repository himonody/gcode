package dlq_retry

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync/atomic"
	"time"

	"gcode/kafka/producer"

	"github.com/IBM/sarama"
)

const (
	MaxRetries        = 3                       // 最大重试次数
	RetryTopic        = "topic_retry"           // 重试队列主题名称
	DLQTopic          = "topic_dlq"             // 死信队列主题名称
	InitialRetryDelay = 1 * time.Second         // 初始重试延迟
	MaxRetryDelay     = 5 * time.Minute         // 最大重试延迟
	DLQAlertThreshold = 100                     // 死信队列告警阈值
)

// BusinessHandler 实现带重试和死信队列的消费者处理器
// 当消息处理失败时，会自动将消息发送到重试队列
// 达到最大重试次数后，将消息发送到死信队列
//
// 生产级特性：
// - 指数退避重试策略
// - 死信队列计数和告警
// - 详细的错误追踪
// - 统计信息收集
type BusinessHandler struct {
	producer      *producer.SyncProducer // 用于发送重试和死信消息
	maxRetries    int                    // 最大重试次数（可配置）
	retryTopic    string                 // 重试队列主题（可配置）
	dlqTopic      string                 // 死信队列主题（可配置）
	dlqCount      int64                  // 发送到死信队列的消息数（原子操作）
	retryCount    int64                  // 重试的消息数（原子操作）
	processedCount int64                 // 成功处理的消息数（原子操作）
}

// NewBusinessHandler 创建一个新的业务处理器
// p: 生产者实例，用于发送重试和死信消息
func NewBusinessHandler(p *producer.SyncProducer) *BusinessHandler {
	return &BusinessHandler{
		producer:   p,
		maxRetries: MaxRetries,
		retryTopic: RetryTopic,
		dlqTopic:   DLQTopic,
	}
}

// NewBusinessHandlerWithConfig 创建一个可配置的业务处理器
func NewBusinessHandlerWithConfig(p *producer.SyncProducer, maxRetries int, retryTopic, dlqTopic string) *BusinessHandler {
	if maxRetries <= 0 {
		maxRetries = MaxRetries
	}
	if retryTopic == "" {
		retryTopic = RetryTopic
	}
	if dlqTopic == "" {
		dlqTopic = DLQTopic
	}

	return &BusinessHandler{
		producer:   p,
		maxRetries: maxRetries,
		retryTopic: retryTopic,
		dlqTopic:   dlqTopic,
	}
}

// Setup 在消费者组会话启动时调用
func (h *BusinessHandler) Setup(sarama.ConsumerGroupSession) error {
	slog.Info("BusinessHandler setup",
		"maxRetries", h.maxRetries,
		"retryTopic", h.retryTopic,
		"dlqTopic", h.dlqTopic)
	return nil
}

// Cleanup 在消费者组会话结束时调用
func (h *BusinessHandler) Cleanup(sarama.ConsumerGroupSession) error {
	processed := atomic.LoadInt64(&h.processedCount)
	retried := atomic.LoadInt64(&h.retryCount)
	dlqed := atomic.LoadInt64(&h.dlqCount)

	slog.Info("BusinessHandler cleanup",
		"processed", processed,
		"retried", retried,
		"dlqed", dlqed)

	// 检查死信队列是否达到告警阈值
	if dlqed > DLQAlertThreshold {
		slog.Warn("DLQ alert: too many messages in dead letter queue",
			"count", dlqed,
			"threshold", DLQAlertThreshold)
		// 生产环境应该触发告警：发送到监控系统、Slack、邮件等
	}

	return nil
}

// ConsumeClaim 实现消息消费的主流程
// 失败的消息会被自动发送到重试队列或死信队列
func (h *BusinessHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		// 尝试处理消息
		err := h.process(msg)
		if err != nil {
			slog.Error("Process failed, moving to retry/dlq",
				"partition", msg.Partition,
				"offset", msg.Offset,
				"key", string(msg.Key),
				"err", err)

			// 处理失败，根据重试次数决定发送到重试队列还是死信队列
			if err := h.handleFailure(msg, err); err != nil {
				slog.Error("Failed to send message to retry/dlq", "err", err)
				// 发送失败时的策略：
				// 1. 不标记 offset：可能导致无限重试
				// 2. 标记 offset：消息丢失
				// 3. 返回 error：触发重平衡
				// 此处记录日志并标记 offset，避免阻塞
			}
		} else {
			// 处理成功
			atomic.AddInt64(&h.processedCount, 1)
		}

		// 无论成功还是进入重试队列，都标记原始消息为已处理
		// 这样可以避免单条失败消息阻塞整个消费流程
		session.MarkMessage(msg, "")
	}
	return nil
}

// process 执行实际的业务处理逻辑
// 返回 error 表示处理失败，消息将进入重试流程
func (h *BusinessHandler) process(msg *sarama.ConsumerMessage) error {
	slog.Debug("Processing message",
		"partition", msg.Partition,
		"offset", msg.Offset,
		"key", string(msg.Key))

	// 在这里实现具体的业务逻辑
	// 例如：数据库操作、HTTP 调用、数据处理等
	
	// 模拟业务处理
	// 可以根据消息内容决定是否失败
	// if shouldFail(msg) {
	//     return fmt.Errorf("business logic failed")
	// }

	return nil
}

// handleFailure 处理失败的消息，根据重试次数决定发送到重试队列还是死信队列
// 使用指数退避策略计算重试延迟时间
func (h *BusinessHandler) handleFailure(msg *sarama.ConsumerMessage, originalErr error) error {
	// 从消息 Header 中获取当前重试次数
	retries := getRetryCount(msg)

	if retries >= h.maxRetries {
		// 达到最大重试次数，发送到死信队列
		atomic.AddInt64(&h.dlqCount, 1)
		
		slog.Warn("Max retries reached, sending to DLQ",
			"topic", h.dlqTopic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"retries", retries,
			"originalErr", originalErr)

		return h.sendToTopic(msg, h.dlqTopic, retries, originalErr)
	}

	// 未达到最大重试次数，发送到重试队列
	retries++
	atomic.AddInt64(&h.retryCount, 1)

	// 计算指数退避延迟时间：delay = min(initialDelay * 2^retries, maxDelay)
	delay := calculateRetryDelay(retries)

	slog.Info("Retrying message with exponential backoff",
		"count", retries,
		"topic", h.retryTopic,
		"partition", msg.Partition,
		"offset", msg.Offset,
		"delay", delay,
		"originalErr", originalErr)

	return h.sendToTopicWithDelay(msg, h.retryTopic, retries, delay, originalErr)
}

// calculateRetryDelay 计算指数退避的重试延迟时间
func calculateRetryDelay(retries int) time.Duration {
	// 指数退避公式：delay = initialDelay * 2^(retries-1)
	delay := InitialRetryDelay * time.Duration(math.Pow(2, float64(retries-1)))
	
	// 限制最大延迟时间
	if delay > MaxRetryDelay {
		delay = MaxRetryDelay
	}
	
	return delay
}

// sendToTopic 将消息发送到指定的主题（重试队列或死信队列）
// 保留原始消息的所有内容，并更新重试次数
func (h *BusinessHandler) sendToTopic(msg *sarama.ConsumerMessage, targetTopic string, retries int, originalErr error) error {
	return h.sendToTopicWithDelay(msg, targetTopic, retries, 0, originalErr)
}

// sendToTopicWithDelay 将消息发送到指定主题，并设置延迟处理时间
func (h *BusinessHandler) sendToTopicWithDelay(msg *sarama.ConsumerMessage, targetTopic string, retries int, delay time.Duration, originalErr error) error {
	// 构造新消息，保留原始的 Key 和 Value
	newMsg := &sarama.ProducerMessage{
		Topic: targetTopic,
		Key:   sarama.ByteEncoder(msg.Key),   // 保留原始 Key，确保分区路由一致
		Value: sarama.ByteEncoder(msg.Value), // 保留原始消息内容
	}

	// 复制原始 Headers 并更新/添加元数据
	headers := make([]sarama.RecordHeader, 0, len(msg.Headers)+5)
	foundRetryCount := false
	foundOriginalTopic := false

	now := time.Now()

	// 遍历原始 Headers
	for _, hdr := range msg.Headers {
		key := string(hdr.Key)
		if key == "retry_count" {
			foundRetryCount = true
			headers = append(headers, sarama.RecordHeader{
				Key:   []byte("retry_count"),
				Value: []byte(strconv.Itoa(retries)),
			})
		} else if key == "original_topic" {
			foundOriginalTopic = true
			headers = append(headers, *hdr)
		} else {
			// 保留其他 Header
			headers = append(headers, *hdr)
		}
	}

	// 添加缺失的 Header
	if !foundRetryCount {
		headers = append(headers, sarama.RecordHeader{
			Key:   []byte("retry_count"),
			Value: []byte(strconv.Itoa(retries)),
		})
	}
	if !foundOriginalTopic {
		headers = append(headers, sarama.RecordHeader{
			Key:   []byte("original_topic"),
			Value: []byte(msg.Topic),
		})
	}

	// 添加额外的追踪信息
	headers = append(headers, sarama.RecordHeader{
		Key:   []byte("failed_at"),
		Value: []byte(now.Format(time.RFC3339)),
	})
	headers = append(headers, sarama.RecordHeader{
		Key:   []byte("original_partition"),
		Value: []byte(strconv.Itoa(int(msg.Partition))),
	})
	headers = append(headers, sarama.RecordHeader{
		Key:   []byte("original_offset"),
		Value: []byte(strconv.FormatInt(msg.Offset, 10)),
	})

	// 记录原始错误信息
	if originalErr != nil {
		headers = append(headers, sarama.RecordHeader{
			Key:   []byte("error_message"),
			Value: []byte(originalErr.Error()),
		})
	}

	// 如果有延迟，添加延迟执行时间
	if delay > 0 {
		executeAt := now.Add(delay)
		headers = append(headers, sarama.RecordHeader{
			Key:   []byte("execute_at"),
			Value: []byte(strconv.FormatInt(executeAt.UnixNano()/int64(time.Millisecond), 10)),
		})
	}

	newMsg.Headers = headers

	// 发送消息到目标主题
	partition, offset, err := h.producer.SendMessage(newMsg)
	if err != nil {
		// 发送失败会导致消息丢失，生产环境需要监控此类错误
		// 可以考虑：1) 本地持久化；2) 发送到备份主题；3) 触发告警
		slog.Error("Failed to send message to retry/dlq topic",
			"target", targetTopic,
			"err", err,
			"originalPartition", msg.Partition,
			"originalOffset", msg.Offset)
		return fmt.Errorf("failed to send to %s: %w", targetTopic, err)
	}

	slog.Debug("Message sent to retry/dlq topic",
		"target", targetTopic,
		"partition", partition,
		"offset", offset,
		"retries", retries)

	return nil
}

// getRetryCount 从消息 Header 中提取重试次数
// 返回 0 表示首次处理
func getRetryCount(msg *sarama.ConsumerMessage) int {
	for _, h := range msg.Headers {
		if string(h.Key) == "retry_count" {
			v, err := strconv.Atoi(string(h.Value))
			if err != nil {
				slog.Warn("Invalid retry_count in message header", "value", string(h.Value))
				return 0
			}
			return v
		}
	}
	return 0 // 未找到 retry_count，说明是首次处理
}

// GetStats 获取处理统计信息（用于监控和告警）
func (h *BusinessHandler) GetStats() (processed, retried, dlqed int64) {
	return atomic.LoadInt64(&h.processedCount),
		atomic.LoadInt64(&h.retryCount),
		atomic.LoadInt64(&h.dlqCount)
}
