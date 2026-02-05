// Package consumer 提供了 Kafka 消费者组的通用运行框架
package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"gcode/kafka/options"

	"github.com/IBM/sarama"
)

// Runner 是消费者组的运行器，负责完整的生命周期管理
// 包括：
// 1. 启动消费者组并订阅主题
// 2. 处理分区重平衡（Rebalance）
// 3. 响应系统信号实现优雅退出
// 4. 自动重连和错误恢复
type Runner struct {
	Brokers []string                      // Kafka 集群地址列表
	GroupID string                        // 消费者组 ID，同组消费者共享消费进度
	Topics  []string                      // 订阅的主题列表
	Config  *sarama.Config                // Sarama 配置
	Handler sarama.ConsumerGroupHandler   // 消息处理器，实现具体的业务逻辑
}

// NewRunner 创建一个新的消费者组运行器
// 参数：
//   - brokers: Kafka 集群地址，建议配置多个以提高可用性
//   - groupID: 消费者组 ID，相同 groupID 的消费者会协同消费（负载均衡）
//   - topics: 订阅的主题列表，支持同时消费多个主题
//   - handler: 消息处理器，需要实现 Setup、Cleanup、ConsumeClaim 三个方法
func NewRunner(brokers []string, groupID string, topics []string, handler sarama.ConsumerGroupHandler) *Runner {
	cfg := options.GetConsumerConfig(groupID)
	return &Runner{
		Brokers: brokers,
		GroupID: groupID,
		Topics:  topics,
		Config:  cfg,
		Handler: handler,
	}
}

// Run 启动消费者组并进入消费循环
// 这是一个阻塞方法，会一直运行直到：
// 1. context 被取消
// 2. 接收到 SIGINT/SIGTERM 系统信号
// 3. 发生不可恢复的错误
//
// 消费流程：
// 1. 创建消费者组客户端
// 2. 加入消费者组，触发分区重平衡
// 3. 调用 Handler.Setup() 初始化
// 4. 开始消费消息，调用 Handler.ConsumeClaim()
// 5. 重平衡时调用 Handler.Cleanup()，然后重复步骤 3-4
// 6. 退出时调用 Handler.Cleanup() 并关闭连接
func (r *Runner) Run(ctx context.Context) error {
	// 创建消费者组客户端，建立与 Kafka 集群的连接
	client, err := sarama.NewConsumerGroup(r.Brokers, r.GroupID, r.Config)
	if err != nil {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}
	defer client.Close() // 确保退出时关闭连接

	// 创建可取消的上下文，用于控制消费循环的退出
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 监听系统退出信号（Ctrl+C 或 kill 命令）
	// 确保消费者组能够优雅退出，触发 Cleanup 并提交 offset
	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	// 在后台 goroutine 中运行消费循环
	go func() {
		defer wg.Done()
		for {
			// Consume 是阻塞方法，会：
			// 1. 加入消费者组并参与重平衡
			// 2. 调用 Handler.Setup()
			// 3. 为每个分配的分区启动 Handler.ConsumeClaim()
			// 4. 监听重平衡事件，重平衡时会退出并调用 Handler.Cleanup()
			//
			// 注意：Consume 在重平衡或 Session 过期时会返回，需要循环调用
			if err := client.Consume(ctx, r.Topics, r.Handler); err != nil {
				slog.Error("Error from consumer", "err", err)
				// 可恢复的错误（如网络抖动）会自动重试
				// 不可恢复的错误（如认证失败）应该退出
			}
			// 检查 context 是否已取消（优雅退出）
			if ctx.Err() != nil {
				return
			}
			// 否则继续循环，重新加入消费者组
		}
	}()

	slog.Info("Consumer group started", "groupID", r.GroupID, "topics", r.Topics)

	// 阻塞等待退出信号
	select {
	case <-sigterm:
		slog.Info("Terminating: via signal") // 接收到 SIGINT/SIGTERM
	case <-ctx.Done():
		slog.Info("Terminating: context cancelled") // context 被外部取消
	}

	// 触发消费循环退出
	cancel()
	// 等待后台 goroutine 完全停止（确保 Cleanup 执行完毕）
	wg.Wait()
	return nil
}
