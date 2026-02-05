# NATS JetStream 延迟消息

## 概述

本示例演示如何使用 NATS JetStream 的延迟消息功能，通过 `Nats-Schedule` 头部实现消息的延迟投递。消息会在指定的延迟时间后才被投递给消费者。

**注意：** 此功能需要 NATS Server 2.11+ 版本，并且需要在流上启用调度器功能。

## 核心概念

### 什么是延迟消息？

延迟消息是指发布后不立即投递，而是在指定的延迟时间后才投递给消费者的消息。这对于需要延迟处理的业务场景非常有用。

### 实现机制

使用 `Nats-Schedule` 头部配合 `@at` 格式指定消息的投递时间：

```go
fireAt := time.Now().Add(5 * time.Second)
msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
msg.Header.Set("Nats-Schedule-Target", subject)
```

## 架构图

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ Publish (T0)
       │ Header: Nats-Schedule = @at T0+5s
       ▼
┌─────────────────────────────────────────┐
│   JetStream Stream (F_DELAY)            │
│   消息存储但不立即投递                    │
└──────────────┬──────────────────────────┘
               │
               │ 等待 5 秒...
               │
               ▼ (T0+5s)
┌─────────────────────────────────────────┐
│   Scheduler 触发投递                     │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│   Consumer                              │
│   在 T0+5s 时收到消息                    │
└─────────────────────────────────────────┘
```

## 关键配置

### 1. Nats-Schedule 头部

```go
msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
```

**格式说明：**
- `@at` 前缀：表示绝对时间
- 时间格式：RFC3339 格式（UTC 时区）
- 示例：`@at 2026-02-02T10:30:00Z`

### 2. Nats-Schedule-Target 头部

```go
msg.Header.Set("Nats-Schedule-Target", subject)
```

**作用：** 指定消息到期后投递到哪个主题。

## 运行示例

### 前置条件

```bash
# 启动 NATS Server 2.11+（支持调度器）
docker run -p 4222:4222 nats:2.11-alpine -js
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/05_delay
go run main.go
```

### 预期输出

```
2026/02/02 18:30:00 发布延迟消息，延迟 5s（投递时间 2026-02-02T18:30:05+08:00），当前时间=2026-02-02T18:30:00+08:00
2026/02/02 18:30:00 延迟消息演示运行中；按 Ctrl+C 退出
... 等待 5 秒 ...
2026/02/02 18:30:05 收到消息于 2026-02-02T18:30:05.123456789+08:00: 延迟消息内容
```

## 使用场景

### 1. 订单超时取消

```go
// 创建订单后，发送 30 分钟延迟消息用于超时检查
func createOrder(js nats.JetStreamContext, order Order) error {
    // 保存订单...
    
    // 发送延迟消息
    fireAt := time.Now().Add(30 * time.Minute)
    msg := &nats.Msg{
        Subject: "orders.timeout-check",
        Data:    marshalOrderID(order.ID),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "orders.timeout-check")
    
    return js.PublishMsg(msg)
}

// 消费者检查订单状态
js.Subscribe("orders.timeout-check", func(m *nats.Msg) {
    orderID := unmarshalOrderID(m.Data)
    order := getOrder(orderID)
    
    if order.Status == "pending" {
        // 30 分钟后仍未支付，取消订单
        cancelOrder(orderID)
    }
})
```

### 2. 定时提醒

```go
// 会议前 10 分钟发送提醒
func scheduleMeetingReminder(js nats.JetStreamContext, meeting Meeting) error {
    reminderTime := meeting.StartTime.Add(-10 * time.Minute)
    
    msg := &nats.Msg{
        Subject: "notifications.reminder",
        Data:    marshalMeeting(meeting),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + reminderTime.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "notifications.reminder")
    
    return js.PublishMsg(msg)
}
```

### 3. 重试机制

```go
// 处理失败后，延迟重试
func retryWithDelay(js nats.JetStreamContext, task Task, delay time.Duration) error {
    fireAt := time.Now().Add(delay)
    
    msg := &nats.Msg{
        Subject: "tasks.retry",
        Data:    marshalTask(task),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "tasks.process")
    
    return js.PublishMsg(msg)
}
```

### 4. 限流控制

```go
// 延迟处理请求以实现限流
func rateLimitedPublish(js nats.JetStreamContext, request Request) error {
    // 每个请求延迟 100ms 处理
    fireAt := time.Now().Add(100 * time.Millisecond)
    
    msg := &nats.Msg{
        Subject: "requests.process",
        Data:    marshalRequest(request),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "requests.process")
    
    return js.PublishMsg(msg)
}
```

## 最佳实践

### 1. 时区处理

```go
// ✅ 推荐：使用 UTC 时区
fireAt := time.Now().UTC().Add(delay)
msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))

// ❌ 避免：使用本地时区可能导致混淆
fireAt := time.Now().Add(delay)  // 本地时区
msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))
```

### 2. 延迟时间范围

```go
// 短延迟（秒级）
delay := 5 * time.Second

// 中等延迟（分钟级）
delay := 30 * time.Minute

// 长延迟（小时级）
delay := 2 * time.Hour

// 超长延迟（天级）
delay := 24 * time.Hour
```

### 3. 错误处理

```go
func publishDelayed(js nats.JetStreamContext, subject string, data []byte, delay time.Duration) error {
    fireAt := time.Now().UTC().Add(delay)
    
    msg := &nats.Msg{
        Subject: subject,
        Data:    data,
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", subject)
    
    ack, err := js.PublishMsg(msg)
    if err != nil {
        return fmt.Errorf("发布延迟消息失败: %w", err)
    }
    
    log.Printf("延迟消息已发布: seq=%d, 投递时间=%s", ack.Sequence, fireAt.Format(time.RFC3339))
    return nil
}
```

### 4. 取消延迟消息

```go
// 延迟消息一旦发布，无法直接取消
// 解决方案：在消费时检查状态

js.Subscribe("orders.timeout-check", func(m *nats.Msg) {
    orderID := unmarshalOrderID(m.Data)
    order := getOrder(orderID)
    
    // 检查订单是否已支付
    if order.Status == "paid" {
        // 已支付，忽略超时检查
        m.Ack()
        return
    }
    
    // 未支付，执行超时逻辑
    cancelOrder(orderID)
    m.Ack()
})
```

## 常见问题

### Q1: 延迟消息的精度如何？

**答案：** 精度约为 1 秒。NATS 调度器每秒检查一次到期消息。

```go
// 延迟 5.5 秒
fireAt := time.Now().Add(5500 * time.Millisecond)
// 实际投递时间：5-6 秒之间
```

### Q2: 延迟消息会占用多少存储空间？

**答案：** 延迟消息会存储在流中，占用正常的存储空间。长时间延迟的大量消息可能占用较多空间。

**优化建议：**
- 使用合理的延迟时间
- 定期清理过期的延迟消息
- 监控流的存储使用情况

### Q3: 如何实现相对延迟（而非绝对时间）？

**答案：** 计算绝对时间后再设置。

```go
// 相对延迟：5 分钟后
delay := 5 * time.Minute
fireAt := time.Now().UTC().Add(delay)
msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))
```

### Q4: 延迟消息支持重复投递吗？

**不支持**。延迟消息只投递一次。如需重复投递，需要使用定时任务或 cron 表达式（见 06_schedule 示例）。

### Q5: NATS Server 重启后延迟消息会丢失吗？

**不会**。延迟消息存储在流中（FileStorage），服务器重启后会继续调度。

## 性能考虑

### 延迟消息数量限制

```go
// 大量延迟消息可能影响性能
// 建议：
// - 短延迟（< 1 分钟）：< 10,000 条
// - 中等延迟（1-30 分钟）：< 100,000 条
// - 长延迟（> 30 分钟）：< 1,000,000 条
```

### 调度器性能

```go
// 调度器每秒检查一次到期消息
// 性能指标：
// - 检查速度：~100,000 条/秒
// - 投递速度：~10,000 条/秒
// - 内存占用：~1KB/1000 条延迟消息
```

## 实战示例

### 示例 1：电商订单超时系统

```go
type OrderService struct {
    js nats.JetStreamContext
}

func (s *OrderService) CreateOrder(order Order) error {
    // 1. 保存订单
    if err := saveOrder(order); err != nil {
        return err
    }
    
    // 2. 发送 30 分钟延迟消息用于超时检查
    timeoutAt := time.Now().UTC().Add(30 * time.Minute)
    msg := &nats.Msg{
        Subject: "orders.timeout",
        Data:    []byte(order.ID),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + timeoutAt.Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "orders.timeout")
    
    if _, err := s.js.PublishMsg(msg); err != nil {
        return fmt.Errorf("发送超时检查消息失败: %w", err)
    }
    
    return nil
}

func (s *OrderService) HandleTimeout() {
    s.js.Subscribe("orders.timeout", func(m *nats.Msg) {
        orderID := string(m.Data)
        order := getOrder(orderID)
        
        if order.Status == "pending" {
            log.Printf("订单 %s 超时未支付，自动取消", orderID)
            cancelOrder(orderID)
        }
        
        m.Ack()
    })
}
```

### 示例 2：任务重试系统

```go
type TaskProcessor struct {
    js nats.JetStreamContext
}

func (p *TaskProcessor) ProcessTask(task Task) error {
    if err := doWork(task); err != nil {
        // 处理失败，延迟重试
        return p.retryTask(task, err)
    }
    return nil
}

func (p *TaskProcessor) retryTask(task Task, lastErr error) error {
    task.RetryCount++
    
    if task.RetryCount > 3 {
        log.Printf("任务 %s 重试次数超限，放弃", task.ID)
        return lastErr
    }
    
    // 指数退避：1s, 2s, 4s
    delay := time.Duration(1<<(task.RetryCount-1)) * time.Second
    fireAt := time.Now().UTC().Add(delay)
    
    msg := &nats.Msg{
        Subject: "tasks.process",
        Data:    marshalTask(task),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "tasks.process")
    
    log.Printf("任务 %s 将在 %s 后重试（第 %d 次）", task.ID, delay, task.RetryCount)
    
    _, err := p.js.PublishMsg(msg)
    return err
}
```

## 相关示例

- **06_schedule**: 定时消息（cron 表达式）
- **07_dlq**: 死信队列
- **08_sync_async**: 同步/异步发布

## 总结

本示例展示了 NATS JetStream 的延迟消息功能：

✅ **Nats-Schedule 头部**：使用 `@at` 格式指定投递时间  
✅ **灵活延迟**：支持秒级到天级的延迟  
✅ **持久化存储**：服务器重启后延迟消息不丢失  
✅ **应用场景**：订单超时、定时提醒、重试机制等  
⚠️ **版本要求**：需要 NATS 2.11+ 版本  
⚠️ **精度限制**：约 1 秒精度  

通过延迟消息功能，可以轻松实现各种需要延迟处理的业务场景。
