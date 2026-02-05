# NATS JetStream 同步与异步发布

## 概述

本示例演示 NATS JetStream 的两种发布模式：同步发布和异步发布。两种模式在性能、可靠性和使用场景上有不同的权衡。

## 核心概念

### 同步发布（Sync Publish）

同步发布会阻塞等待服务器的 ACK 响应，确保消息已被持久化后才返回。

```go
ack, err := js.Publish(subject, []byte("message"))
// 此时消息已确认存储
```

### 异步发布（Async Publish）

异步发布立即返回，不等待服务器确认，适合高吞吐量场景。

```go
future := js.PublishAsync(subject, []byte("message"))
// 立即返回，后台等待确认
```

## 架构图

### 同步发布流程

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ 1. Publish
       ▼
┌─────────────────────────────────────────┐
│   NATS Server                           │
│   2. 存储消息                            │
│   3. 返回 ACK                            │
└──────────────┬──────────────────────────┘
               │ 4. 收到 ACK
               ▼
┌─────────────────────────────────────────┐
│   Producer                              │
│   5. Publish 返回（阻塞结束）            │
└─────────────────────────────────────────┘

时间线：
T0: 发起 Publish
T1: 服务器存储
T2: 返回 ACK
T3: Publish 返回
总耗时：~1-5ms
```

### 异步发布流程

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ 1. PublishAsync
       ▼
┌─────────────────────────────────────────┐
│   NATS Client Buffer                    │
│   2. 缓冲消息                            │
└──────────────┬──────────────────────────┘
               │ 3. 立即返回 Future
               ▼
┌─────────────────────────────────────────┐
│   Producer                              │
│   4. PublishAsync 返回（不阻塞）         │
│   5. 继续发布更多消息                    │
└─────────────────────────────────────────┘
               │
               │ 后台线程
               ▼
┌─────────────────────────────────────────┐
│   NATS Server                           │
│   6. 批量发送消息                        │
│   7. 返回 ACK                            │
└─────────────────────────────────────────┘

时间线：
T0: 发起 PublishAsync
T1: 立即返回（~0.1ms）
T2: 后台发送到服务器
T3: 收到 ACK
总耗时（发布）：~0.1ms
总耗时（确认）：~1-5ms
```

## 对比表

| 特性 | 同步发布 | 异步发布 |
|------|---------|---------|
| 阻塞 | ✅ 阻塞 | ❌ 不阻塞 |
| 吞吐量 | 较低 (~1k msg/s) | 高 (~10k msg/s) |
| 延迟 | 较高 (~1-5ms) | 低 (~0.1ms) |
| 确认时机 | 立即 | 延迟 |
| 错误处理 | 简单（直接返回） | 复杂（需检查 Future） |
| 内存使用 | 低 | 较高（缓冲区） |
| 适用场景 | 低频、关键消息 | 高频、批量消息 |

## 代码示例

### 同步发布

```go
// 同步发布：等待确认
ack, err := js.Publish(subject, []byte("sync-message"))
if err != nil {
    log.Fatalf("发布失败: %v", err)
}
log.Printf("消息已确认: seq=%d", ack.Sequence)
// 此时消息已安全存储
```

### 异步发布

```go
// 异步发布：立即返回
future1 := js.PublishAsync(subject, []byte("async-1"))
future2 := js.PublishAsync(subject, []byte("async-2"))

// 等待所有异步发布完成
select {
case <-js.PublishAsyncComplete():
    log.Println("所有消息已确认")
case err := <-future1.Err():
    log.Fatalf("发布失败: %v", err)
case <-time.After(3 * time.Second):
    log.Fatal("超时")
}
```

## 运行示例

### 前置条件

```bash
# 启动 NATS Server
docker run -p 4222:4222 nats:latest -js
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/08_sync_async
go run main.go
```

### 预期输出

```
2026/02/02 18:50:00 收到消息 seq=1 data=sync-1
2026/02/02 18:50:00 同步发布完成
2026/02/02 18:50:00 收到消息 seq=2 data=async-1
2026/02/02 18:50:00 收到消息 seq=3 data=async-2
2026/02/02 18:50:00 所有异步发布已确认
2026/02/02 18:50:00 同步/异步发布演示运行中；按 Ctrl+C 退出
```

## 使用场景

### 同步发布适用场景

1. **关键业务消息**
```go
// 订单支付：必须确认存储
func processPayment(js nats.JetStreamContext, payment Payment) error {
    data := marshalPayment(payment)
    ack, err := js.Publish("payments.processed", data)
    if err != nil {
        return fmt.Errorf("支付消息发布失败: %w", err)
    }
    log.Printf("支付已确认: seq=%d", ack.Sequence)
    return nil
}
```

2. **低频消息**
```go
// 系统配置变更：频率低，需要确认
func updateConfig(js nats.JetStreamContext, config Config) error {
    data := marshalConfig(config)
    _, err := js.Publish("config.updated", data)
    return err
}
```

3. **事务性操作**
```go
// 需要确认每一步都成功
func executeTransaction(js nats.JetStreamContext, tx Transaction) error {
    // 步骤 1
    if _, err := js.Publish("tx.step1", tx.Step1Data); err != nil {
        return err
    }
    // 步骤 2
    if _, err := js.Publish("tx.step2", tx.Step2Data); err != nil {
        return err
    }
    return nil
}
```

### 异步发布适用场景

1. **高频日志**
```go
// 日志发送：高频、允许短暂延迟
func logEvent(js nats.JetStreamContext, event Event) {
    data := marshalEvent(event)
    js.PublishAsync("logs.events", data)
    // 不等待确认，继续处理
}
```

2. **批量数据导入**
```go
// 批量导入：高吞吐量
func importData(js nats.JetStreamContext, records []Record) error {
    for _, record := range records {
        data := marshalRecord(record)
        js.PublishAsync("data.import", data)
    }
    
    // 等待所有消息确认
    select {
    case <-js.PublishAsyncComplete():
        return nil
    case <-time.After(30 * time.Second):
        return errors.New("导入超时")
    }
}
```

3. **指标采集**
```go
// 指标上报：高频、非关键
func reportMetrics(js nats.JetStreamContext, metrics Metrics) {
    data := marshalMetrics(metrics)
    js.PublishAsync("metrics.system", data)
    // 不等待确认
}
```

4. **消息转发**
```go
// 消息转发：高吞吐量
func forwardMessages(js nats.JetStreamContext, messages []Message) {
    for _, msg := range messages {
        js.PublishAsync("forwarded.messages", msg.Data)
    }
    
    // 批量等待确认
    <-js.PublishAsyncComplete()
}
```

## 最佳实践

### 1. 选择合适的发布模式

```go
// ✅ 关键消息：使用同步发布
func publishCritical(js nats.JetStreamContext, data []byte) error {
    _, err := js.Publish("critical.messages", data)
    return err
}

// ✅ 高频消息：使用异步发布
func publishHighVolume(js nats.JetStreamContext, data []byte) {
    js.PublishAsync("high.volume", data)
}
```

### 2. 异步发布错误处理

```go
func publishAsyncWithErrorHandling(js nats.JetStreamContext, messages [][]byte) error {
    var futures []nats.PubAckFuture
    
    // 发布所有消息
    for _, msg := range messages {
        future := js.PublishAsync("subject", msg)
        futures = append(futures, future)
    }
    
    // 检查每个消息的结果
    for i, future := range futures {
        select {
        case <-future.Ok():
            log.Printf("消息 %d 已确认", i)
        case err := <-future.Err():
            return fmt.Errorf("消息 %d 发布失败: %w", i, err)
        case <-time.After(5 * time.Second):
            return fmt.Errorf("消息 %d 超时", i)
        }
    }
    
    return nil
}
```

### 3. 批量异步发布

```go
func batchPublishAsync(js nats.JetStreamContext, messages [][]byte) error {
    // 发布所有消息
    for _, msg := range messages {
        js.PublishAsync("batch.messages", msg)
    }
    
    // 等待所有消息确认
    select {
    case <-js.PublishAsyncComplete():
        log.Printf("批量发布完成: %d 条消息", len(messages))
        return nil
    case <-time.After(30 * time.Second):
        return errors.New("批量发布超时")
    }
}
```

### 4. 混合使用

```go
type MessagePublisher struct {
    js nats.JetStreamContext
}

func (p *MessagePublisher) Publish(msg Message) error {
    data := marshalMessage(msg)
    
    // 根据消息优先级选择发布模式
    if msg.Priority == "high" {
        // 高优先级：同步发布
        _, err := p.js.Publish(msg.Subject, data)
        return err
    } else {
        // 低优先级：异步发布
        p.js.PublishAsync(msg.Subject, data)
        return nil
    }
}
```

### 5. 异步发布缓冲区配置

```go
// 配置异步发布缓冲区大小
js, err := nc.JetStream(
    nats.PublishAsyncMaxPending(256), // 最大待确认消息数
)
```

## 性能测试

### 测试场景

```go
func benchmarkSync(js nats.JetStreamContext, count int) time.Duration {
    start := time.Now()
    for i := 0; i < count; i++ {
        js.Publish("test", []byte(fmt.Sprintf("msg-%d", i)))
    }
    return time.Since(start)
}

func benchmarkAsync(js nats.JetStreamContext, count int) time.Duration {
    start := time.Now()
    for i := 0; i < count; i++ {
        js.PublishAsync("test", []byte(fmt.Sprintf("msg-%d", i)))
    }
    <-js.PublishAsyncComplete()
    return time.Since(start)
}
```

### 性能对比

| 消息数量 | 同步发布 | 异步发布 | 性能提升 |
|---------|---------|---------|---------|
| 100 | ~100ms | ~20ms | 5x |
| 1,000 | ~1s | ~150ms | 6.7x |
| 10,000 | ~10s | ~1.2s | 8.3x |
| 100,000 | ~100s | ~10s | 10x |

## 常见问题

### Q1: 异步发布会丢失消息吗？

**不会**。异步发布只是延迟确认，消息仍然会被发送到服务器并持久化。

**但需要注意：**
- 程序崩溃前未确认的消息可能丢失
- 需要等待 `PublishAsyncComplete()` 确保所有消息已发送

### Q2: 如何知道异步发布是否成功？

```go
// 方案 1：等待所有消息完成
<-js.PublishAsyncComplete()

// 方案 2：检查单个消息
future := js.PublishAsync(subject, data)
select {
case <-future.Ok():
    log.Println("成功")
case err := <-future.Err():
    log.Printf("失败: %v", err)
}

// 方案 3：使用错误处理器
js, _ := nc.JetStream(nats.PublishAsyncErrHandler(func(js nats.JetStream, msg *nats.Msg, err error) {
    log.Printf("异步发布失败: %v", err)
}))
```

### Q3: 异步发布的缓冲区满了怎么办？

```go
// 设置缓冲区大小
js, err := nc.JetStream(
    nats.PublishAsyncMaxPending(1000), // 最大 1000 条待确认消息
)

// 缓冲区满时会阻塞
future := js.PublishAsync(subject, data)
// 如果缓冲区满，此调用会阻塞直到有空间
```

### Q4: 如何优雅关闭异步发布？

```go
func gracefulShutdown(js nats.JetStreamContext) {
    // 1. 停止发布新消息
    stopPublishing()
    
    // 2. 等待所有异步发布完成
    select {
    case <-js.PublishAsyncComplete():
        log.Println("所有消息已确认")
    case <-time.After(30 * time.Second):
        log.Println("等待超时，强制退出")
    }
    
    // 3. 关闭连接
    nc.Drain()
}
```

### Q5: 同步和异步发布可以混用吗？

**可以**。但需要注意：
- 同步发布会阻塞当前 goroutine
- 异步发布不会阻塞
- 两者的消息顺序由服务器接收顺序决定

```go
// 混合使用示例
js.Publish("sync-msg", []byte("important"))      // 阻塞
js.PublishAsync("async-msg", []byte("log"))      // 不阻塞
js.Publish("sync-msg-2", []byte("critical"))     // 阻塞
<-js.PublishAsyncComplete()                       // 等待异步消息
```

## 实战示例

### 示例 1：日志收集系统

```go
type LogCollector struct {
    js nats.JetStreamContext
}

func (c *LogCollector) CollectLog(log LogEntry) {
    // 日志使用异步发布（高频、非关键）
    data := marshalLog(log)
    c.js.PublishAsync("logs.collected", data)
}

func (c *LogCollector) Flush() error {
    // 刷新所有待发送的日志
    select {
    case <-c.js.PublishAsyncComplete():
        return nil
    case <-time.After(10 * time.Second):
        return errors.New("刷新超时")
    }
}
```

### 示例 2：订单处理系统

```go
type OrderProcessor struct {
    js nats.JetStreamContext
}

func (p *OrderProcessor) ProcessOrder(order Order) error {
    // 订单使用同步发布（关键业务）
    data := marshalOrder(order)
    ack, err := p.js.Publish("orders.processed", data)
    if err != nil {
        return fmt.Errorf("订单发布失败: %w", err)
    }
    
    log.Printf("订单已确认: id=%s, seq=%d", order.ID, ack.Sequence)
    
    // 发送通知使用异步发布（非关键）
    p.js.PublishAsync("notifications.order", data)
    
    return nil
}
```

### 示例 3：数据同步系统

```go
type DataSyncer struct {
    js nats.JetStreamContext
}

func (s *DataSyncer) SyncBatch(records []Record) error {
    // 批量同步使用异步发布
    for _, record := range records {
        data := marshalRecord(record)
        s.js.PublishAsync("data.sync", data)
    }
    
    // 等待所有记录确认
    select {
    case <-s.js.PublishAsyncComplete():
        log.Printf("同步完成: %d 条记录", len(records))
        return nil
    case <-time.After(60 * time.Second):
        return errors.New("同步超时")
    }
}
```

## 相关示例

- **01_reliability**: 可靠性和持久化
- **04_dedup**: 消息去重
- **07_dlq**: 死信队列

## 总结

本示例展示了 NATS JetStream 的两种发布模式：

✅ **同步发布**：阻塞等待确认，适合关键消息  
✅ **异步发布**：立即返回，适合高吞吐量场景  
✅ **性能差异**：异步发布比同步发布快 5-10 倍  
✅ **灵活选择**：根据业务需求选择合适的模式  
✅ **混合使用**：可以在同一应用中混合使用两种模式  

通过合理选择发布模式，可以在可靠性和性能之间取得最佳平衡。
