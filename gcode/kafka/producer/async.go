package producer

import (
	"fmt"
	"log/slog"
	"sync"

	"gcode/kafka/options"

	"github.com/IBM/sarama"
)

// AsyncProducer 异步生产者，适用于高吞吐量场景
// 通过后台协程监听发送结果，避免阻塞主流程
//
// 使用方式：
// 1. 创建生产者：producer.NewAsyncProducer(brokers, reliable)
// 2. 发送消息：producer.Input() <- &sarama.ProducerMessage{...}
// 3. 关闭生产者：producer.Close() // 会等待所有消息处理完成
type AsyncProducer struct {
	internal sarama.AsyncProducer // 底层 Sarama 异步生产者
	wg       sync.WaitGroup        // 用于等待后台协程退出
}

// NewAsyncProducer 创建一个新的异步生产者
// 参数：
//   - brokers: Kafka 集群地址列表
//   - reliable: 是否开启高可靠模式（acks=-1, 幂等性）
func NewAsyncProducer(brokers []string, reliable bool) (*AsyncProducer, error) {
	config := options.GetProducerConfig(reliable)
	producer, err := sarama.NewAsyncProducer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create async producer: %w", err)
	}

	p := &AsyncProducer{internal: producer}

	// 启动后台协程监听发送结果
	// 注意：必须消费 Successes 和 Errors 通道，否则 Input 通道会阻塞
	p.wg.Add(2)
	go p.listenSuccesses()
	go p.listenErrors()

	return p, nil
}

// listenSuccesses 监听成功发送的消息
// 必须持续消费 Successes 通道，否则 Input 通道会阻塞
func (p *AsyncProducer) listenSuccesses() {
	defer p.wg.Done()
	for msg := range p.internal.Successes() {
		slog.Debug("Async message sent successfully",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset)
		// 生产环境建议：
		// 1. 更新指标统计（成功率、延迟等）
		// 2. 记录消息轨迹用于链路追踪
	}
}

// listenErrors 监听发送失败的消息
// 必须持续消费 Errors 通道，否则 Input 通道会阻塞
func (p *AsyncProducer) listenErrors() {
	defer p.wg.Done()
	for err := range p.internal.Errors() {
		slog.Error("Async message sent failed",
			"topic", err.Msg.Topic,
			"error", err.Err)
		// 生产环境必须：
		// 1. 记录详细错误信息和消息内容
		// 2. 更新失败指标并触发告警
		// 3. 持久化失败消息或发送到备份队列
	}
}

// Input 返回消息输入通道
// 使用示例：producer.Input() <- &sarama.ProducerMessage{...}
func (p *AsyncProducer) Input() chan<- *sarama.ProducerMessage {
	return p.internal.Input()
}

// Close 优雅关闭异步生产者
// 会先关闭输入通道，然后等待所有消息处理完成（包括成功和失败回调）
func (p *AsyncProducer) Close() error {
	p.internal.AsyncClose() // 关闭输入通道，不再接受新消息
	p.wg.Wait()             // 等待所有后台协程退出
	return nil
}
