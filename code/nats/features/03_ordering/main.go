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

// 3. 有序消费：自动修复间隙；使用有序消费者
// 有序消费者确保消息按序列号顺序投递，自动检测和修复消息间隙
func main() {
	const (
		url     = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream  = "F_ORDERED"             // JetStream 流名称
		subject = "features.ordered"      // 消息主题
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

	// 有序消费者：检测到消息间隙时自动重启并重新消费
	// OrderedConsumer 确保消息按序列号顺序投递，不需要手动 ACK
	if _, err := js.Subscribe(subject, func(m *nats.Msg) {
		// 获取消息元数据
		meta, err := m.Metadata()
		if err != nil {
			log.Printf("获取元数据错误: %v", err)
			return
		}
		log.Printf("有序消费 seq=%d data=%s", meta.Sequence.Consumer, string(m.Data))
		// 有序消费者会自动 ACK，但显式 ACK 也是允许的
		if err := m.Ack(); err != nil {
			log.Printf("ACK 错误: %v", err)
		}
	}, nats.OrderedConsumer()); err != nil {
		log.Fatalf("有序订阅失败: %v", err)
	}

	// 发布顺序消息
	for i := 1; i <= 5; i++ {
		body := []byte(time.Now().Format(time.RFC3339Nano))
		if _, err := js.Publish(subject, body); err != nil {
			log.Fatalf("发布失败: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	log.Println("有序消费演示运行中；按 Ctrl+C 退出")
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
