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

// 8. 同步 vs 异步发布：JetStream 的两种发布模式
// 同步发布等待服务器确认，异步发布立即返回并在后台等待确认
func main() {
	const (
		url     = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream  = "F_SYNC_ASYNC"          // JetStream 流名称
		subject = "features.sync_async"   // 消息主题
		durable = "F_SYNC_ASYNC_D"        // 持久化消费者名称
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

	// 消费者：观察接收到的消息
	if _, err := js.Subscribe(subject, func(m *nats.Msg) {
		meta, err := m.Metadata()
		if err != nil {
			log.Printf("获取元数据错误: %v", err)
			return
		}
		log.Printf("收到消息 seq=%d data=%s", meta.Sequence.Stream, string(m.Data))
		if err := m.Ack(); err != nil {
			log.Printf("ACK 错误: %v", err)
		}
	}, nats.Durable(durable), nats.ManualAck()); err != nil {
		log.Fatalf("订阅失败: %v", err)
	}

	// --- 同步发布（等待服务器确认）---
	// 同步发布会阻塞直到收到服务器的 ACK 响应
	if _, err := js.Publish(subject, []byte("sync-1")); err != nil {
		log.Fatalf("同步发布失败: %v", err)
	}
	log.Println("同步发布完成")

	// --- 异步发布（缓冲模式，后台收集确认）---
	// 异步发布立即返回，不等待服务器确认，适合高吞吐量场景
	pending := js.PublishAsync(subject, []byte("async-1"))
	js.PublishAsync(subject, []byte("async-2"))

	// 等待所有异步发布完成
	select {
	case <-js.PublishAsyncComplete():
		log.Println("所有异步发布已确认")
	case err := <-pending.Err():
		log.Fatalf("异步发布错误: %v", err)
	case <-time.After(3 * time.Second):
		log.Fatal("异步发布超时")
	}

	log.Println("同步/异步发布演示运行中；按 Ctrl+C 退出")
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
