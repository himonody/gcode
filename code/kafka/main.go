// Package main 演示 Kafka 在生产环境中的完整使用示例
// 涵盖了消息队列系统的各种常见场景和最佳实践
//
// 主要模块：
// 1. producer: 同步/异步生产者实现
// 2. consumer: 通用消费者运行框架
// 3. reliability: 高可靠性投递（不丢消息）
// 4. idempotency: 幂等性消费（不重复处理）
// 5. order: 顺序消费（保证顺序性）
// 6. backlog: 积压处理（并发消费）
// 7. dlq_retry: 重试与死信队列
// 8. delay: 延迟消息
//
// 生产环境部署建议：
// 1. Kafka 集群至少 3 个 Broker，开启副本机制（replication-factor >= 2）
// 2. 生产者开启幂等性和 acks=-1，确保消息不丢失
// 3. 消费者合理设置超时参数，避免频繁重平衡
// 4. 使用监控系统（Prometheus + Grafana）监控消息积压、消费延迟等指标
// 5. 为关键业务配置死信队列和告警机制
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gcode/kafka/backlog"
	"gcode/kafka/consumer"
	"gcode/kafka/delay"
	"gcode/kafka/dlq_retry"
	"gcode/kafka/idempotency"
	"gcode/kafka/order"
	"gcode/kafka/producer"
	"gcode/kafka/reliability"

	"github.com/IBM/sarama"
)

// main 函数演示了 Kafka 在生产环境中的多种使用模式
// 包含：同步/异步生产者、可靠性消费、幂等性消费、顺序消费、积压处理、重试与死信队列、延迟消息等
func main() {
	// 初始化结构化日志，使用 Debug 级别以便观察详细的执行流程
	// 生产环境建议使用 JSON 格式并调整为 Info 级别
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	// Kafka 集群地址配置，生产环境中通常配置多个 Broker 以提高可用性
	// 建议从环境变量或配置文件读取
	brokers := []string{"localhost:9092"}
	// 主题名称，用于消息的分类和路由
	topic := "production-topic"

	slog.Info("Starting Kafka Production-Level Example", "brokers", brokers)

	// 创建应用级别的上下文，用于控制所有组件的生命周期
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建资源管理器，用于追踪和清理所有需要关闭的资源
	resources := &resourceManager{
		closers: make([]io.Closer, 0),
	}
	defer func() {
		if err := resources.CloseAll(); err != nil {
			slog.Error("Failed to close resources", "err", err)
		}
	}()

	// ========== 1. 生产者示例 ==========
	// 创建同步生产者，开启高可靠模式（acks=-1, 幂等性）
	// 注意：生产者对象应该在应用生命周期内复用，避免频繁创建和销毁
	syncProd, err := producer.NewSyncProducer(brokers, true)
	if err != nil {
		slog.Error("Failed to create sync producer", "err", err)
		return
	}
	resources.Add(syncProd)

	// 创建异步生产者，适用于高吞吐量场景
	// 异步模式不会阻塞主流程，但需要监听成功和失败通道
	asyncProd, err := producer.NewAsyncProducer(brokers, false)
	if err != nil {
		slog.Error("Failed to create async producer", "err", err)
		return
	}
	resources.Add(asyncProd)

	// 发送同步消息：阻塞直到收到 Broker 确认
	// 返回分区号和偏移量，适用于需要立即确认的场景
	_, _, _ = syncProd.Send(topic, "order-123", "Payload for order 123")

	// 发送异步消息：将消息发送到 Input 通道，立即返回
	// 发送结果将通过 Successes/Errors 通道异步通知
	asyncProd.Input() <- &sarama.ProducerMessage{Topic: topic, Value: sarama.StringEncoder("Async payload")}

	// ========== 2. 可靠性消费示例 ==========
	// 使用通用 Runner 启动消费者组
	// ReliableConsumerHandler 确保消息处理成功后才标记为已消费
	reliableHandler := &reliability.ReliableConsumerHandler{}
	runner := consumer.NewRunner(brokers, "group-reliable", []string{topic}, reliableHandler)

	// 在独立的 goroutine 中运行消费者，避免阻塞主流程
	go func() {
		if err := runner.Run(ctx); err != nil {
			slog.Error("Consumer runner exited", "err", err)
		}
	}()

	// ========== 3. 幂等性消费示例 ==========
	// 防止消息重复消费导致的业务数据错误
	// 生产环境使用 Redis 实现分布式幂等性校验
	redisAddr := getEnvOrDefault("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnvOrDefault("REDIS_PASSWORD", "")
	redisDB := getEnvIntOrDefault("REDIS_DB", 0)

	// 方案一：Redis String + SET NX（推荐，简单高效）
	// 适用场景：普通规模（百万~千万级消息/天）
	// 优点：准确率 100%，无误判
	// 缺点：内存占用较高（每条消息约 100 bytes）
	/*
		redisStore, err := idempotency.NewRedisStore(redisAddr, redisPassword, redisDB, 24*time.Hour, true)
		if err != nil {
			slog.Error("Failed to create Redis store", "err", err)
			return
		}
		resources.Add(redisStore)
		idempotentHandler := idempotency.NewIdempotentHandler(redisStore)
	*/

	// 方案二：Redis + 布隆过滤器（超大规模推荐）
	// 适用场景：超大规模（千万~亿级消息/天）
	// 优点：内存占用极低（1000万消息仅需 12MB，误判率 1%）
	// 缺点：存在误判率（可配置，默认 1%）
	bloomStore, err := idempotency.NewBloomStore(idempotency.BloomStoreConfig{
		Addr:          redisAddr,
		Password:      redisPassword,
		DB:            redisDB,
		KeyPrefix:     "bloom:",
		ExpectedItems: 10000000, // 预期 1000 万消息/天
		FalsePositive: 0.01,     // 1% 误判率
		TTL:           24 * time.Hour,
		EnableDegrade: true,
	})
	if err != nil {
		slog.Error("Failed to create Bloom store", "err", err)
		return
	}
	resources.Add(bloomStore)
	idempotentHandler := idempotency.NewIdempotentHandler(bloomStore)

	idempotentRunner := consumer.NewRunner(brokers, "group-idempotent", []string{topic}, idempotentHandler)
	go func() {
		if err := idempotentRunner.Run(ctx); err != nil {
			slog.Error("Idempotent consumer exited", "err", err)
		}
	}()

	// ========== 4. 顺序消费示例 ==========
	// 保证同一 Key 的消息按照生产顺序被消费
	// 注意：顺序性仅在单个分区内保证，且不能使用并发处理
	orderHandler := &order.OrderHandler{}
	orderRunner := consumer.NewRunner(brokers, "group-order", []string{topic}, orderHandler)
	go orderRunner.Run(ctx)

	// ========== 5. 积压处理（并发消费）示例 ==========
	// 当消息积压严重时，使用 worker pool 并行处理以提高吞吐量
	// 注意：并发处理会破坏消息顺序，且需要注意 offset 提交的安全性
	backlogHandler := backlog.NewBacklogHandler(10, 30*time.Second) // 并发度为 10，超时 30 秒
	backlogRunner := consumer.NewRunner(brokers, "group-backlog", []string{topic}, backlogHandler)
	go func() {
		if err := backlogRunner.Run(ctx); err != nil {
			slog.Error("Backlog consumer exited", "err", err)
		}
	}()

	// ========== 6. 重试与死信队列示例 ==========
	// 处理失败的消息会自动重试，达到最大重试次数后发送到死信队列
	// 避免单条坏消息阻塞整个消费流程
	dlqHandler := dlq_retry.NewBusinessHandler(syncProd)
	dlqRunner := consumer.NewRunner(brokers, "group-dlq", []string{topic}, dlqHandler)
	go dlqRunner.Run(ctx)

	// ========== 7. 延迟消息生产示例 ==========
	// 通过在消息 Header 中设置执行时间来实现延迟消费
	// 适用于定时任务、订单超时取消等场景
	err = delay.DelayProducer(syncProd, "delay-topic", "This is a delayed message", 5*time.Second)
	if err != nil {
		slog.Error("Failed to send delay message", "err", err)
	}

	slog.Info("All examples are running. Press Ctrl+C to exit...")

	// 监听系统信号，实现优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 阻塞等待退出信号
	select {
	case sig := <-sigChan:
		slog.Info("Received shutdown signal", "signal", sig.String())
		cancel() // 取消上下文，通知所有组件停止
	case <-ctx.Done():
		slog.Info("Context cancelled")
	}

	// 等待所有消费者优雅退出
	slog.Info("Waiting for all consumers to shutdown gracefully...")
	time.Sleep(2 * time.Second) // 给消费者时间完成当前消息的处理

	slog.Info("Example finished. All resources cleaned up.")
}

// resourceManager 管理需要关闭的资源，确保优雅退出
type resourceManager struct {
	mu      sync.Mutex
	closers []io.Closer
}

// Add 添加一个需要管理的资源
func (rm *resourceManager) Add(closer io.Closer) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.closers = append(rm.closers, closer)
}

// CloseAll 关闭所有资源，按照添加的逆序关闭
func (rm *resourceManager) CloseAll() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	var errs []error
	// 逆序关闭，先关闭后创建的资源
	for i := len(rm.closers) - 1; i >= 0; i-- {
		if err := rm.closers[i].Close(); err != nil {
			errs = append(errs, err)
			slog.Error("Failed to close resource", "err", err, "index", i)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to close %d resources", len(errs))
	}
	return nil
}

// getEnvOrDefault 从环境变量读取配置，如果不存在则使用默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault 从环境变量读取整型配置，如果不存在或解析失败则使用默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
