# NATS JetStream 死信队列（DLQ）实现

## 概述

本示例演示如何在 NATS JetStream 中实现死信队列（Dead Letter Queue, DLQ）功能。当消息处理失败并达到最大重试次数后，消息会自动被转发到死信队列，避免阻塞正常消息处理流程。

## 核心概念

### 什么是死信队列？

死信队列是一种消息队列模式，用于处理无法成功消费的消息。当消息满足以下条件之一时，会被发送到死信队列：
- 达到最大重试次数（MaxDeliver）
- 消息过期
- 消费者拒绝消息且不重新入队

### 实现原理

本示例使用以下机制实现 DLQ：

1. **MaxDeliver**：设置消息的最大投递次数（本例为 3 次）
2. **BackOff**：配置重试退避策略（指数退避：1s, 2s, 4s）
3. **JetStream Advisory**：监听 `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES` 事件
4. **消息重新发布**：当收到 advisory 事件时，从流中获取原始消息并发布到 DLQ 主题

## 架构图

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ Publish
       ▼
┌─────────────────────────────────────────┐
│         JetStream Stream                │
│         (features.dlq)                  │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│         Consumer (F_DLQ_D)              │
│  - MaxDeliver: 3                        │
│  - BackOff: [1s, 2s, 4s]                │
└──────────────┬──────────────────────────┘
               │ Push to deliver.dlq
               ▼
┌─────────────────────────────────────────┐
│         Worker                          │
│  - 处理成功 → ACK                        │
│  - 处理失败 → NAK (触发重试)              │
└──────────────┬──────────────────────────┘
               │
               │ 达到 MaxDeliver
               ▼
┌─────────────────────────────────────────┐
│    Advisory Event Publisher             │
│  ($JS.EVENT.ADVISORY.CONSUMER.          │
│   MAX_DELIVERIES.F_DLQ.F_DLQ_D)         │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│    DLQ Advisory Listener                │
│  1. 接收 advisory 事件                   │
│  2. 获取原始消息 (js.GetMsg)             │
│  3. 重新发布到 DLQ.features.dlq          │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│         DLQ Consumer                    │
│  (监听 DLQ.features.dlq)                 │
└─────────────────────────────────────────┘
```

## 代码结构

### 主要组件

| 组件 | 说明 |
|------|------|
| `main()` | 主函数，初始化连接、创建流和消费者、启动工作协程和 DLQ 监听器 |
| `ensureStream()` | 确保 JetStream 流存在，配置存储和去重策略 |
| `ensureDlqConsumer()` | 创建带有 MaxDeliver 和 BackOff 配置的消费者 |
| `subscribeDlqAdvisory()` | 订阅 MaxDeliveries advisory 事件并处理 DLQ 逻辑 |
| `AdvisoryMaxDeliveries` | Advisory 事件的数据结构 |

### 配置参数

```go
const (
    url       = "nats://127.0.0.1:4222"  // NATS 服务器地址
    stream    = "F_DLQ"                  // JetStream 流名称
    subject   = "features.dlq"           // 业务消息主题
    durable   = "F_DLQ_D"                // 持久化消费者名称
    deliverTo = "deliver.dlq"            // 消息投递目标主题
    dlqSubj   = "DLQ.features.dlq"       // 死信队列主题
)
```

## 工作流程

### 1. 正常消息处理流程

```
发布消息 → 流存储 → 消费者投递 → Worker 处理 → ACK 确认 → 完成
```

### 2. 失败消息处理流程

```
发布消息 → 流存储 → 消费者投递 → Worker 处理失败 → NAK
                                    ↓
                              等待 1s (BackOff[0])
                                    ↓
                              第 2 次投递 → NAK
                                    ↓
                              等待 2s (BackOff[1])
                                    ↓
                              第 3 次投递 → NAK
                                    ↓
                              等待 4s (BackOff[2])
                                    ↓
                              达到 MaxDeliver (3)
                                    ↓
                              发布 Advisory 事件
                                    ↓
                              DLQ Listener 接收
                                    ↓
                              获取原始消息 (js.GetMsg)
                                    ↓
                              重新发布到 DLQ.features.dlq
                                    ↓
                              DLQ Consumer 处理
```

## 关键特性

### 1. 指数退避（Exponential BackOff）

```go
BackOff: []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
```

- 第 1 次重试：等待 1 秒
- 第 2 次重试：等待 2 秒
- 第 3 次重试：等待 4 秒

这种策略可以避免在短时间内频繁重试，给系统恢复时间。

### 2. JetStream Advisory 事件

Advisory 主题格式：
```
$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.<stream>.<consumer>
```

本例中的完整主题：
```
$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.F_DLQ.F_DLQ_D
```

### 3. 消息追踪

DLQ 消息会添加额外的 Header 用于追踪：

```go
dlqMsg.Header.Set("X-Original-Subject", rawMsg.Subject)  // 原始主题
dlqMsg.Header.Set("X-Stream-Seq", fmt.Sprintf("%d", adv.StreamSeq))  // 流序列号
```

这些信息可以帮助：
- 追踪消息的来源
- 定位问题消息
- 实现消息重放功能

## 运行示例

### 前置条件

1. 安装并启动 NATS Server（支持 JetStream）：
```bash
# 使用 Docker
docker run -p 4222:4222 nats:latest -js

# 或使用本地安装
nats-server -js
```

2. 安装依赖：
```bash
go get github.com/nats-io/nats.go
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/07_dlq
go run main.go
```

### 预期输出

```
2026/02/02 17:40:00 工作协程收到消息: ok
2026/02/02 17:40:00 工作协程收到消息: fail
2026/02/02 17:40:01 工作协程收到消息: fail
2026/02/02 17:40:03 工作协程收到消息: fail
2026/02/02 17:40:07 收到 MaxDeliveries advisory: stream=F_DLQ consumer=F_DLQ_D seq=2
2026/02/02 17:40:07 已将 seq 2 重新发布到死信队列 DLQ.features.dlq
2026/02/02 17:40:07 死信队列收到消息（已达最大投递次数）: fail
2026/02/02 17:40:07 死信队列演示运行中；按 Ctrl+C 退出
```

### 输出解析

1. **"ok" 消息**：被成功处理并 ACK
2. **"fail" 消息**：
   - 第 1 次投递：NAK，等待 1s
   - 第 2 次投递：NAK，等待 2s
   - 第 3 次投递：NAK，等待 4s
   - 达到 MaxDeliver，触发 advisory 事件
   - DLQ listener 接收并重新发布到死信队列

## 最佳实践

### 1. MaxDeliver 配置

```go
MaxDeliver: 3  // 建议值：3-5 次
```

- 太少：可能导致临时故障的消息过早进入 DLQ
- 太多：可能导致问题消息长时间阻塞队列

### 2. BackOff 策略

```go
// 指数退避
BackOff: []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// 固定间隔
BackOff: []time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second}

// 快速重试 + 长时间等待
BackOff: []time.Duration{100 * time.Millisecond, 1 * time.Second, 10 * time.Second}
```

根据业务场景选择合适的策略。

### 3. DLQ 消息处理

```go
// DLQ 消息处理建议：
// 1. 记录到数据库或日志系统
// 2. 发送告警通知
// 3. 提供手动重试接口
// 4. 定期清理过期的 DLQ 消息
```

### 4. 监控指标

建议监控以下指标：
- DLQ 消息数量
- DLQ 消息增长速率
- 消息重试次数分布
- Advisory 事件频率

### 5. 错误处理

```go
// 在 subscribeDlqAdvisory 中添加更完善的错误处理
if err := nc.PublishMsg(dlqMsg); err != nil {
    // 1. 记录错误日志
    log.Printf("发布到死信队列失败: %v", err)
    
    // 2. 可选：写入备份存储（数据库、文件等）
    // saveToBackupStorage(dlqMsg)
    
    // 3. 可选：发送告警
    // sendAlert("DLQ publish failed", err)
    
    return
}
```

## 常见问题

### Q1: Advisory 事件没有触发？

**可能原因：**
- NATS Server 版本过低（需要 2.2.0+）
- 消费者配置错误
- MaxDeliver 设置为 -1（无限重试）

**解决方案：**
```bash
# 检查 NATS Server 版本
nats-server --version

# 检查消费者配置
nats consumer info F_DLQ F_DLQ_D
```

### Q2: 如何重放 DLQ 中的消息？

**方案 1：手动重新发布**
```go
// 从 DLQ 读取消息
nc.Subscribe(dlqSubj, func(m *nats.Msg) {
    originalSubj := m.Header.Get("X-Original-Subject")
    // 重新发布到原始主题
    js.Publish(originalSubj, m.Data)
})
```

**方案 2：创建专门的重放工具**
```go
func replayDlqMessage(js nats.JetStreamContext, dlqMsg *nats.Msg) error {
    originalSubj := dlqMsg.Header.Get("X-Original-Subject")
    if originalSubj == "" {
        return errors.New("missing X-Original-Subject header")
    }
    _, err := js.Publish(originalSubj, dlqMsg.Data)
    return err
}
```

### Q3: DLQ 消息会占用多少存储空间？

DLQ 消息使用 Core NATS（非 JetStream），默认不持久化。如果需要持久化 DLQ 消息，可以：

**方案 1：创建专门的 DLQ 流**
```go
js.AddStream(&nats.StreamConfig{
    Name:     "DLQ_STREAM",
    Subjects: []string{"DLQ.>"},
    Storage:  nats.FileStorage,
    MaxAge:   7 * 24 * time.Hour,  // 保留 7 天
})
```

**方案 2：写入外部存储**
```go
// 写入数据库、Elasticsearch 等
```

### Q4: 如何避免 DLQ 消息无限增长？

**策略 1：设置过期时间**
```go
js.AddStream(&nats.StreamConfig{
    Name:   "DLQ_STREAM",
    MaxAge: 7 * 24 * time.Hour,  // 7 天后自动删除
})
```

**策略 2：设置最大消息数**
```go
js.AddStream(&nats.StreamConfig{
    Name:        "DLQ_STREAM",
    MaxMsgs:     10000,          // 最多保留 10000 条消息
    Discard:     nats.DiscardOld, // 超过限制时丢弃旧消息
})
```

**策略 3：定期清理**
```go
// 定期任务清理过期的 DLQ 消息
func cleanupDlq(js nats.JetStreamContext) {
    // 删除 7 天前的消息
    cutoff := time.Now().Add(-7 * 24 * time.Hour)
    // 实现清理逻辑
}
```

## 扩展功能

### 1. 多级 DLQ

```go
// 第一级 DLQ：快速失败的消息
// 第二级 DLQ：第一级 DLQ 处理失败的消息

const (
    dlqSubj1 = "DLQ.L1.features.dlq"
    dlqSubj2 = "DLQ.L2.features.dlq"
)
```

### 2. DLQ 消息分类

```go
// 根据错误类型将消息发送到不同的 DLQ
func routeDlqMessage(err error, msg *nats.Msg) string {
    switch {
    case errors.Is(err, ErrTimeout):
        return "DLQ.timeout"
    case errors.Is(err, ErrValidation):
        return "DLQ.validation"
    default:
        return "DLQ.unknown"
    }
}
```

### 3. 自动重试机制

```go
// 定期从 DLQ 中取出消息重试
func autoRetryDlq(js nats.JetStreamContext) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    
    for range ticker.C {
        // 从 DLQ 读取消息并重试
        // 如果仍然失败，放回 DLQ
    }
}
```

## 性能考虑

### 1. Advisory 订阅开销

每个 advisory 订阅都会占用一定的资源。如果有大量消费者，建议：
- 使用通配符订阅：`$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>`
- 集中处理 advisory 事件

### 2. GetMsg 性能

`js.GetMsg()` 会从流中读取消息，频繁调用可能影响性能。优化方案：
- 批量处理 advisory 事件
- 使用缓存减少重复读取

### 3. 并发处理

```go
// 使用 worker pool 处理 DLQ 消息
type DlqWorkerPool struct {
    workers   int
    taskQueue chan *nats.Msg
}

func (p *DlqWorkerPool) Start() {
    for i := 0; i < p.workers; i++ {
        go p.worker()
    }
}
```

## 相关资源

- [NATS JetStream 官方文档](https://docs.nats.io/nats-concepts/jetstream)
- [NATS Advisory Events](https://docs.nats.io/nats-concepts/jetstream/advisories)
- [NATS Go Client](https://github.com/nats-io/nats.go)

## 总结

本示例展示了如何在 NATS JetStream 中实现完整的死信队列功能，包括：

✅ 消息重试机制（MaxDeliver + BackOff）  
✅ Advisory 事件监听  
✅ 自动 DLQ 转发  
✅ 消息追踪和调试  
✅ 优雅关闭和错误处理  

通过合理配置和使用 DLQ，可以有效提高系统的可靠性和可维护性。
