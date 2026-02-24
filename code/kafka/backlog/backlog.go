package backlog

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
)

// BacklogHandler 实现积压处理的并发消费模式
// 当消息积压严重时，通过 worker pool 并行处理消息以提高吞吐量
//
// 生产级警告：由于并发处理，消息完成顺序可能与 Offset 顺序不一致
// 例如：Offset 100 先完成并 MarkMessage，此时程序崩溃，未完成的 Offset 99 将丢失
//
// 解决方案：
// 1. 幂等性处理（推荐）：允许重复处理 99，通过业务层幂等保证数据一致性
// 2. 滑窗位移管理（复杂）：维护一个滑动窗口，只提交连续完成的最小 Offset
//
// 生产级特性：
// - 支持消息处理超时控制
// - 统计处理成功/失败/超时数量
// - panic 恢复机制
// - 优雅退出保证
type BacklogHandler struct {
	concurrency    int            // 并发处理的 worker 数量
	workerPool     chan struct{}  // 用于控制并发数的信号量
	wg             sync.WaitGroup // 用于等待所有 worker 完成
	timeout        time.Duration  // 单条消息处理超时时间
	processedCount int64          // 成功处理的消息数（原子操作）
	failedCount    int64          // 失败的消息数（原子操作）
	timeoutCount   int64          // 超时的消息数（原子操作）
}

// NewBacklogHandler 创建一个新的积压处理器
// concurrency: 并发处理的 worker 数量，建议根据 CPU 核心数和业务耗时调整
// timeout: 单条消息处理超时时间，0 表示不限制
func NewBacklogHandler(concurrency int, timeout time.Duration) *BacklogHandler {
	if concurrency <= 0 {
		concurrency = 10 // 默认并发数
	}
	if timeout == 0 {
		timeout = 30 * time.Second // 默认超时 30 秒
	}

	return &BacklogHandler{
		concurrency: concurrency,
		workerPool:  make(chan struct{}, concurrency), // 创建带缓冲的信号量通道
		timeout:     timeout,
	}
}

// Setup 在消费者组会话启动时调用，用于初始化资源
func (h *BacklogHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup 在消费者组会话结束时调用，确保资源正确释放
// 必须等待所有正在处理的协程完成，避免数据丢失
func (h *BacklogHandler) Cleanup(sarama.ConsumerGroupSession) error {
	// 阻塞等待所有 worker 完成任务
	h.wg.Wait()

	// 输出统计信息
	slog.Info("Backlog handler cleanup",
		"processed", atomic.LoadInt64(&h.processedCount),
		"failed", atomic.LoadInt64(&h.failedCount),
		"timeout", atomic.LoadInt64(&h.timeoutCount))

	return nil
}

// ConsumeClaim 实现消息的并发消费逻辑
// 通过 worker pool 控制并发数，避免创建过多 goroutine 导致资源耗尽
func (h *BacklogHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// 遍历分配给当前消费者的所有消息
	for msg := range claim.Messages() {
		select {
		case h.workerPool <- struct{}{}: // 获取一个 worker 槽位（如果池满则阻塞）
			h.wg.Add(1)               // 增加等待计数
			go h.handle(session, msg) // 启动 goroutine 处理消息
		case <-session.Context().Done(): // 会话被取消（重平衡或关闭）
			return nil
		}
	}
	return nil
}

// handle 在独立的 goroutine 中处理单条消息
// 包含超时控制、panic 恢复、统计信息等生产级特性
func (h *BacklogHandler) handle(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage) {
	defer func() {
		<-h.workerPool // 释放 worker 槽位，允许新消息进入处理
		h.wg.Done()    // 减少等待计数

		// 恢复 panic，防止单个消息的 panic 导致整个进程崩溃
		if r := recover(); r != nil {
			atomic.AddInt64(&h.failedCount, 1)
			slog.Error("Panic recovered in message handler",
				"panic", r,
				"partition", msg.Partition,
				"offset", msg.Offset)
			// panic 情况下也标记消息，避免重复处理导致反复 panic
			session.MarkMessage(msg, "")
		}
	}()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(session.Context(), h.timeout)
	defer cancel()

	// 使用通道接收处理结果，支持超时控制
	errChan := make(chan error, 1)
	go func() {
		// 执行业务逻辑
		slog.Debug("Concurrent processing", "partition", msg.Partition, "offset", msg.Offset)
		errChan <- doBusiness(ctx, msg)
	}()

	// 等待处理完成或超时
	select {
	case err := <-errChan:
		if err != nil {
			atomic.AddInt64(&h.failedCount, 1)
			slog.Error("Process failed", "offset", msg.Offset, "err", err)
			// 处理失败时的策略选择：
			// 1. 不标记 offset：可能导致消息重复消费（需要业务幂等）
			// 2. 标记 offset：跳过失败消息（需要配合死信队列）
			// 3. 返回 error：触发消费组重平衡（可能导致抖动）
			// 此处选择策略 2，配合死信队列使用
			session.MarkMessage(msg, "")
			return
		}
		// 处理成功
		atomic.AddInt64(&h.processedCount, 1)
		session.MarkMessage(msg, "")

	case <-ctx.Done():
		// 处理超时
		atomic.AddInt64(&h.timeoutCount, 1)
		slog.Warn("Process timeout",
			"partition", msg.Partition,
			"offset", msg.Offset,
			"timeout", h.timeout)
		// 超时也标记消息，避免阻塞消费流程
		// 建议配合监控告警，人工介入处理超时原因
		session.MarkMessage(msg, "")
	}
}

// doBusiness 执行实际的业务处理逻辑
// 生产环境中这里通常包含：数据库操作、HTTP 调用、复杂计算等
//
// 生产级要求：
// 1. 必须支持 context 取消，避免超时后继续执行浪费资源
// 2. 应该实现重试逻辑（针对可恢复错误）
// 3. 记录详细的错误日志，便于问题排查
func doBusiness(ctx context.Context, msg *sarama.ConsumerMessage) error {
	// 模拟耗时任务
	// time.Sleep(100 * time.Millisecond)

	// 检查 context 是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 实际业务逻辑
	// 例如：
	// 1. 解析消息内容
	// 2. 调用数据库或外部服务
	// 3. 更新缓存
	// 4. 发送通知

	return nil
}

// GetStats 获取处理统计信息（用于监控和告警）
func (h *BacklogHandler) GetStats() (processed, failed, timeout int64) {
	return atomic.LoadInt64(&h.processedCount),
		atomic.LoadInt64(&h.failedCount),
		atomic.LoadInt64(&h.timeoutCount)
}
