package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"
	"time"

	nats "github.com/nats-io/nats.go"
)

// 2. 消息模式：扇出（多订阅者）和队列（负载均衡）
// 演示两种消息分发模式：广播模式和竞争消费模式
func main() {
	const (
		url          = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream       = "F_MODES"               // JetStream 流名称
		subject      = "features.modes"        // 消息主题
		durableQueue = "F_MODES_Q"             // 队列消费者名称
		queueGroup   = "workers"               // 队列组名称
	)

	// 连接到 NATS 服务器，配置自动重连机制
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true), // 连接失败时自动重试
		nats.MaxReconnects(-1),          // 无限重连次数
		nats.ReconnectWait(time.Second), // 重连等待间隔
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Printf("连接断开: %v", err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.Println("已重新连接")
		}),
	)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	// 优雅关闭：等待所有消息处理完成后再断开连接
	defer func() {
		if err := nc.Drain(); err != nil {
			log.Printf("drain 错误: %v", err)
		}
	}()

	// 创建上下文，监听系统信号以实现优雅退出
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 获取 JetStream 上下文
	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("创建 JetStream 上下文失败: %v", err)
	}

	// 确保流存在
	if err := ensureStream(js, stream, subject); err != nil {
		log.Fatalf("确保流存在失败: %v", err)
	}

	// 扇出模式：两个独立的持久化推送订阅者都会收到所有消息
	// 每个订阅者都有自己的 durable 名称，因此维护独立的消费进度
	subAll := func(name string) {
		if _, err := js.Subscribe(subject, func(m *nats.Msg) {
			log.Printf("%s 收到: %s", name, string(m.Data))
			if err := m.Ack(); err != nil {
				log.Printf("%s ACK 错误: %v", name, err)
			}
		}, nats.Durable(name), nats.ManualAck()); err != nil {
			log.Fatalf("订阅 %s 失败: %v", name, err)
		}
	}
	subAll("fanout-A") // 订阅者 A
	subAll("fanout-B") // 订阅者 B

	// 队列组模式：两个成员共享同一个 durable，每条消息只投递给组内一个成员
	// 实现负载均衡，消息在队列组成员之间分发
	if _, err := js.QueueSubscribe(subject, queueGroup, func(m *nats.Msg) {
		log.Printf("队列成员 1 收到: %s", string(m.Data))
		if err := m.Ack(); err != nil {
			log.Printf("队列成员 1 ACK 错误: %v", err)
		}
	}, nats.Durable(durableQueue), nats.ManualAck()); err != nil {
		log.Fatalf("队列订阅 1 失败: %v", err)
	}
	if _, err := js.QueueSubscribe(subject, queueGroup, func(m *nats.Msg) {
		log.Printf("队列成员 2 收到: %s", string(m.Data))
		if err := m.Ack(); err != nil {
			log.Printf("队列成员 2 ACK 错误: %v", err)
		}
	}, nats.Durable(durableQueue), nats.ManualAck()); err != nil {
		log.Fatalf("队列订阅 2 失败: %v", err)
	}

	// 发布几条测试消息
	for i := 1; i <= 4; i++ {
		body := []byte(time.Now().Format(time.RFC3339Nano))
		if _, err := js.Publish(subject, body); err != nil {
			log.Fatalf("发布失败: %v", err)
		}
	}

	log.Println("消息模式演示运行中；按 Ctrl+C 退出")
	<-ctx.Done()
	log.Println("正在关闭")
}

// ensureStream 确保 JetStream 流存在，如果不存在则创建
func ensureStream(js nats.JetStreamContext, name, subject string) error {
	if _, err := js.StreamInfo(name); err == nil {
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return err
	}
	_, err := js.AddStream(&nats.StreamConfig{
		Name:       name,
		Subjects:   []string{subject},
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		Discard:    nats.DiscardOld,
		Duplicates: 5 * time.Minute,
	})
	return err
}
