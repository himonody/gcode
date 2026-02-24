package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	nats "github.com/nats-io/nats.go"
)

// 1. 可靠性（零丢失）：持久化流 + 显式确认消费者
// 通过文件存储和显式 ACK 机制确保消息不会丢失
func main() {
	const (
		url     = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream  = "F_RELIABLE"            // JetStream 流名称
		subject = "features.reliable"     // 消息主题
		durable = "F_REL_DURABLE"         // 持久化消费者名称
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

	// 创建持久化拉取消费者，使用显式确认策略
	sub, err := js.PullSubscribe(subject, durable, nats.BindStream(stream))
	if err != nil {
		// 如果消费者不存在，则创建
		if _, cErr := js.AddConsumer(stream, &nats.ConsumerConfig{
			Durable:       durable,                // 持久化消费者名称
			AckPolicy:     nats.AckExplicitPolicy, // 显式确认策略：必须手动 ACK
			FilterSubject: subject,                // 只消费匹配此主题的消息
		}); cErr != nil && !errors.Is(cErr, nats.ErrConsumerNameAlreadyInUse) {
			log.Fatalf("添加消费者失败: %v", cErr)
		}
		sub, err = js.PullSubscribe(subject, durable, nats.BindStream(stream))
		if err != nil {
			log.Fatalf("拉取订阅失败: %v", err)
		}
	}

	// 发布几条消息（持久化到磁盘）
	for i := 1; i <= 10; i++ {
		msg := fmt.Sprintf("序号:%d->%s", i, time.Now().Format(time.RFC3339Nano))
		if _, err := js.Publish(subject, []byte(msg)); err != nil {
			log.Fatalf("发布失败: %v", err)
		} else {
			log.Printf("发送消息 seq=%d data=%s", i, msg)
		}

	}

	// 使用显式确认方式消费消息
	msgs, err := sub.Fetch(10, nats.MaxWait(2*time.Second))
	if err != nil && !errors.Is(err, nats.ErrTimeout) {
		log.Fatalf("拉取消息失败: %v", err)
	}
	for _, m := range msgs {
		// 获取消息元数据
		meta, err := m.Metadata()
		if err != nil {
			log.Printf("获取元数据错误: %v", err)
			continue
		}
		log.Printf("收到消息 seq=%d data=%s", meta.Sequence.Stream, string(m.Data))
		// 显式确认消息已处理
		if err := m.Ack(); err != nil {
			log.Printf("ACK 错误: %v", err)
		}
	}

	log.Println("可靠性演示完成；等待信号...")
	<-ctx.Done()
	log.Println("正在关闭")
}

// ensureStream 确保 JetStream 流存在，如果不存在则创建
// 参数：
//   - js: JetStream 上下文
//   - name: 流名称
//   - subject: 流监听的主题
func ensureStream(js nats.JetStreamContext, name, subject string) error {
	// 检查流是否已存在
	if _, err := js.StreamInfo(name); err == nil {
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return err
	}
	// 创建新流
	_, err := js.AddStream(&nats.StreamConfig{
		Name:       name,              // 流名称
		Subjects:   []string{subject}, // 监听的主题列表
		Storage:    nats.FileStorage,  // 使用文件存储（持久化）
		Retention:  nats.LimitsPolicy, // 保留策略：基于限制
		Discard:    nats.DiscardOld,   // 丢弃策略：丢弃旧消息
		Duplicates: 5 * time.Minute,   // 消息去重窗口：5分钟
	})
	return err
}
