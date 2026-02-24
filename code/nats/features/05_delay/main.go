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

// 5. 延迟消息：使用 Nats-Schedule 头部发送延迟消息
// 消息在指定的延迟时间后才会被投递给消费者
func main() {
	const (
		url     = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream  = "F_DELAY"               // JetStream 流名称
		subject = "features.delay"        // 消息主题
		durable = "F_DELAY_D"             // 持久化消费者名称
		delay   = 5 * time.Second         // 延迟时间：5 秒
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

	// 订阅者：接收延迟后的消息
	if _, err := js.Subscribe(subject, func(m *nats.Msg) {
		log.Printf("收到消息于 %s: %s", time.Now().Format(time.RFC3339Nano), string(m.Data))
		if err := m.Ack(); err != nil {
			log.Printf("ACK 错误: %v", err)
		}
	}, nats.Durable(durable), nats.ManualAck()); err != nil {
		log.Fatalf("订阅失败: %v", err)
	}

	// 发布延迟消息（使用 Nats-Schedule 头部，@at 格式指定投递时间）
	// 注意：需要 NATS 2.11+ 版本，并且流上启用了调度器
	fireAt := time.Now().Add(delay)
	msg := &nats.Msg{Subject: subject, Data: []byte("延迟消息内容"), Header: nats.Header{}}
	msg.Header.Set("Nats-Schedule", "@at "+fireAt.UTC().Format(time.RFC3339)) // 设置投递时间
	msg.Header.Set("Nats-Schedule-Target", subject)                           // 设置目标主题
	log.Printf("发布延迟消息，延迟 %s（投递时间 %s），当前时间=%s", delay, fireAt.Format(time.RFC3339), time.Now().Format(time.RFC3339))
	if _, err := js.PublishMsg(msg); err != nil {
		log.Printf("发布失败（可能未启用调度器）: %v", err)
	}

	log.Println("延迟消息演示运行中；按 Ctrl+C 退出")
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
