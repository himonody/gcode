package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	nats "github.com/nats-io/nats.go"
)

// 7. 死信队列（DLQ）实现：通过 JetStream Advisory + MaxDeliver + BackOff 机制
// 当消息处理失败达到最大重试次数后，自动将消息转发到死信队列
func main() {
	const (
		url       = "nats://127.0.0.1:4222" // NATS 服务器地址
		stream    = "F_DLQ"                 // JetStream 流名称
		subject   = "features.dlq"          // 业务消息主题
		durable   = "F_DLQ_D"               // 持久化消费者名称
		deliverTo = "deliver.dlq"           // 消息投递目标主题
		dlqSubj   = "DLQ.features.dlq"      // 死信队列主题
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
	// 确保消费者存在（配置了 MaxDeliver 和 BackOff）
	if err := ensureDlqConsumer(js, stream, subject, durable, deliverTo); err != nil {
		log.Fatalf("确保消费者存在失败: %v", err)
	}

	// 订阅 MaxDeliveries advisory 事件，实现死信队列功能
	if err := subscribeDlqAdvisory(nc, js, stream, durable, dlqSubj); err != nil {
		log.Fatalf("订阅死信队列 advisory 失败: %v", err)
	}

	// 工作协程：处理消息，对 "fail" 消息进行 NAK 以触发重试和最终的死信队列
	if _, err := nc.Subscribe(deliverTo, func(m *nats.Msg) {
		log.Printf("工作协程收到消息: %s", string(m.Data))
		if string(m.Data) == "fail" {
			// 拒绝消息，触发重新投递
			if err := m.Nak(); err != nil {
				log.Printf("NAK 错误: %v", err)
			}
			return
		}
		// 确认消息处理成功
		if err := m.Ack(); err != nil {
			log.Printf("ACK 错误: %v", err)
		}
	}); err != nil {
		log.Fatalf("工作协程订阅失败: %v", err)
	}

	// 死信队列监听器（使用 Core NATS 订阅，非 JetStream）
	if _, err := nc.Subscribe(dlqSubj, func(m *nats.Msg) {
		log.Printf("死信队列收到消息（已达最大投递次数）: %s", string(m.Data))
	}); err != nil {
		log.Fatalf("死信队列订阅失败: %v", err)
	}

	// 发布测试消息：一个成功消息和一个失败消息
	if _, err := js.Publish(subject, []byte("ok")); err != nil {
		log.Printf("发布 ok 消息失败: %v", err)
	}
	if _, err := js.Publish(subject, []byte("fail")); err != nil {
		log.Printf("发布 fail 消息失败: %v", err)
	}

	log.Println("死信队列演示运行中；按 Ctrl+C 退出")
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

// ensureDlqConsumer 确保带有死信队列配置的消费者存在，如果不存在则创建
// 参数：
//   - js: JetStream 上下文
//   - stream: 流名称
//   - subject: 过滤主题
//   - durable: 持久化消费者名称
//   - deliverTo: 消息投递目标主题
func ensureDlqConsumer(js nats.JetStreamContext, stream, subject, durable, deliverTo string) error {
	// 检查消费者是否已存在
	if _, err := js.ConsumerInfo(stream, durable); err == nil {
		return nil
	} else if !errors.Is(err, nats.ErrConsumerNotFound) {
		return err
	}
	// 创建新消费者，配置死信队列相关参数
	_, err := js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable:        durable,                                                            // 持久化消费者名称
		AckPolicy:      nats.AckExplicitPolicy,                                             // 显式确认策略：必须手动 ACK
		FilterSubject:  subject,                                                            // 只消费匹配此主题的消息
		DeliverSubject: deliverTo,                                                          // 消息推送到此主题
		MaxDeliver:     3,                                                                  // 最大投递次数：3次后触发死信队列
		BackOff:        []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}, // 重试退避策略：指数退避
	})
	return err
}

// AdvisoryMaxDeliveries 是 MaxDeliveries advisory 事件的数据结构
// 当消息达到最大投递次数时，NATS 会发布此类型的 advisory 事件
type AdvisoryMaxDeliveries struct {
	Stream    string `json:"stream"`     // 流名称
	Consumer  string `json:"consumer"`   // 消费者名称
	StreamSeq uint64 `json:"stream_seq"` // 消息在流中的序列号
}

// subscribeDlqAdvisory 订阅 MaxDeliveries advisory 事件，并将失败的消息重新发布到死信队列
// 参数：
//   - nc: NATS 连接
//   - js: JetStream 上下文
//   - stream: 流名称
//   - consumer: 消费者名称
//   - dlqSubj: 死信队列主题
func subscribeDlqAdvisory(nc *nats.Conn, js nats.JetStreamContext, stream, consumer, dlqSubj string) error {
	// 构造 advisory 主题：$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.<stream>.<consumer>
	advisorySubj := fmt.Sprintf("$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.%s.%s", stream, consumer)
	_, err := nc.Subscribe(advisorySubj, func(m *nats.Msg) {
		// 解析 advisory 事件数据
		var adv AdvisoryMaxDeliveries
		if err := json.Unmarshal(m.Data, &adv); err != nil {
			log.Printf("解析 advisory 失败: %v", err)
			return
		}
		log.Printf("收到 MaxDeliveries advisory: stream=%s consumer=%s seq=%d", adv.Stream, adv.Consumer, adv.StreamSeq)

		// 根据序列号从流中获取原始消息
		rawMsg, err := js.GetMsg(stream, adv.StreamSeq)
		if err != nil {
			log.Printf("获取消息 seq %d 失败: %v", adv.StreamSeq, err)
			return
		}

		// 重新发布到死信队列主题
		dlqMsg := &nats.Msg{
			Subject: dlqSubj,       // 死信队列主题
			Data:    rawMsg.Data,   // 原始消息数据
			Header:  rawMsg.Header, // 原始消息头
		}
		// 添加追踪信息到消息头
		dlqMsg.Header.Set("X-Original-Subject", rawMsg.Subject)             // 原始主题
		dlqMsg.Header.Set("X-Stream-Seq", fmt.Sprintf("%d", adv.StreamSeq)) // 流序列号
		if err := nc.PublishMsg(dlqMsg); err != nil {
			log.Printf("发布到死信队列失败: %v", err)
			return
		}
		log.Printf("已将 seq %d 重新发布到死信队列 %s", adv.StreamSeq, dlqSubj)
	})
	return err
}
