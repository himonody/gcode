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

// 4. 消息去重：流去重窗口 + Msg-Id 头部防止重复消息
// 通过配置去重窗口和消息 ID，确保相同 ID 的消息在窗口期内只存储一次
func main() {
	const (
		url     = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream  = "F_DEDUP"               // JetStream 流名称
		subject = "features.dedup"        // 消息主题
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

	// 确保流存在（配置了 5 分钟的去重窗口）
	if err := ensureStream(js, stream, subject); err != nil {
		log.Fatalf("确保流存在失败: %v", err)
	}

	// 订阅者：观察去重效果
	// 只会收到去重后的消息（相同 Msg-Id 的消息只保留第一条）
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
	}, nats.Durable("F_DEDUP_D"), nats.ManualAck()); err != nil {
		log.Fatalf("订阅失败: %v", err)
	}

	// 发布测试消息
	publishOnce(js, subject, "id-1", []byte("hello"))
	publishOnce(js, subject, "id-1", []byte("hello again")) // 由于相同的 Msg-Id 被丢弃
	publishOnce(js, subject, "id-2", []byte("world"))

	log.Println("消息去重演示运行中；按 Ctrl+C 退出")
	<-ctx.Done()
	log.Println("正在关闭")
}

// ensureStream 确保 JetStream 流存在，如果不存在则创建
// 配置了 5 分钟的去重窗口
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
		Duplicates: 5 * time.Minute, // 去重窗口：5 分钟内相同 Msg-Id 的消息会被去重
	})
	return err
}

// publishOnce 发布带有消息 ID 的消息
// 相同 msgID 的消息在去重窗口内只会被存储一次
func publishOnce(js nats.JetStreamContext, subject, msgID string, data []byte) {
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set(nats.MsgIdHdr, msgID) // 设置消息 ID 用于去重
	if _, err := js.PublishMsg(msg); err != nil {
		log.Printf("发布失败: %v", err)
	}
}
