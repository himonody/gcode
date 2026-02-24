# NATS JetStream 消息去重

## 概述

本示例演示如何使用 NATS JetStream 的消息去重功能，通过配置去重窗口和消息 ID 头部，防止重复消息被存储到流中。

## 核心概念

### 什么是消息去重？

消息去重是一种防止重复消息的机制，确保相同的消息在指定时间窗口内只被存储一次。这对于以下场景非常重要：
- 网络重试导致的重复发送
- 生产者故障恢复后的重复发布
- 分布式系统中的幂等性保证

### 去重机制

NATS JetStream 使用两个关键配置实现去重：
1. **Duplicates 窗口**：流配置中的去重时间窗口
2. **Msg-Id 头部**：消息中的唯一标识符

## 架构图

```
┌─────────────────────────────────────────┐
│  Producer                               │
│  发布消息时设置 Msg-Id                    │
└──────────────┬──────────────────────────┘
               │
               │ Msg-Id: "id-1", Data: "hello"
               ▼
┌─────────────────────────────────────────┐
│  JetStream Stream (F_DEDUP)             │
│  Duplicates: 5 minutes                  │
│                                         │
│  去重检查：                              │
│  1. 检查 Msg-Id 是否在窗口内存在          │
│  2. 存在 → 丢弃（返回成功但不存储）       │
│  3. 不存在 → 存储并记录 Msg-Id           │
└──────────────┬──────────────────────────┘
               │
               │ 只有唯一的消息
               ▼
┌─────────────────────────────────────────┐
│  Consumer                               │
│  收到去重后的消息                         │
└─────────────────────────────────────────┘
```

## 工作原理

### 去重流程

```
1. 生产者发布消息（Msg-Id: "id-1"）
   ↓
2. JetStream 检查去重窗口
   - 窗口内是否存在 "id-1"？
   ↓
3a. 不存在 → 存储消息，记录 "id-1"
   ↓
   消费者收到消息

3b. 存在 → 丢弃消息（返回成功）
   ↓
   消费者不会收到重复消息
```

### 示例场景

```go
// 第一次发布
publishOnce(js, subject, "id-1", []byte("hello"))
// 结果：✅ 存储成功，seq=1

// 第二次发布（相同 Msg-Id）
publishOnce(js, subject, "id-1", []byte("hello again"))
// 结果：✅ 返回成功，但消息被丢弃（不存储）

// 第三次发布（不同 Msg-Id）
publishOnce(js, subject, "id-2", []byte("world"))
// 结果：✅ 存储成功，seq=2

// 消费者只收到 2 条消息：seq=1 和 seq=2
```

## 关键配置

### 1. 流配置：Duplicates 窗口

```go
js.AddStream(&nats.StreamConfig{
    Name:       "F_DEDUP",
    Duplicates: 5 * time.Minute,  // 5 分钟去重窗口
})
```

**窗口大小选择：**
- 太小：可能无法覆盖所有重试场景
- 太大：占用更多内存（需要记录更多 Msg-Id）
- 推荐：根据业务重试策略设置（通常 1-10 分钟）

### 2. 消息头部：Msg-Id

```go
msg := &nats.Msg{
    Subject: subject,
    Data:    data,
    Header:  nats.Header{},
}
msg.Header.Set(nats.MsgIdHdr, "unique-id-123")
js.PublishMsg(msg)
```

**Msg-Id 生成策略：**
- UUID：`uuid.New().String()`
- 业务 ID：订单号、交易号等
- 组合 ID：`userId-timestamp-sequence`
- 哈希值：消息内容的哈希

## 运行示例

### 前置条件

```bash
# 启动 NATS Server
docker run -p 4222:4222 nats:latest -js
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/04_dedup
go run main.go
```

### 预期输出

```
2026/02/02 18:20:00 收到消息 seq=1 data=hello
2026/02/02 18:20:00 收到消息 seq=2 data=world
2026/02/02 18:20:00 消息去重演示运行中；按 Ctrl+C 退出
```

**注意：** 只收到 2 条消息，第二次发布的 "hello again" 被去重了。

## 使用场景

### 1. 网络重试保护

```go
func publishWithRetry(js nats.JetStreamContext, subject string, data []byte) error {
    msgID := uuid.New().String()
    msg := &nats.Msg{
        Subject: subject,
        Data:    data,
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, msgID)
    
    // 重试逻辑
    for i := 0; i < 3; i++ {
        if _, err := js.PublishMsg(msg); err == nil {
            return nil
        }
        time.Sleep(time.Second)
    }
    return errors.New("publish failed after retries")
}
```

### 2. 幂等性保证

```go
// 订单创建：使用订单号作为 Msg-Id
func createOrder(js nats.JetStreamContext, order Order) error {
    msg := &nats.Msg{
        Subject: "orders.created",
        Data:    marshalOrder(order),
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, order.ID)  // 订单号作为 Msg-Id
    
    _, err := js.PublishMsg(msg)
    return err
}

// 即使多次调用，相同订单号的消息只会存储一次
```

### 3. 分布式事务

```go
// 使用事务 ID 作为 Msg-Id
func publishTransaction(js nats.JetStreamContext, tx Transaction) error {
    msg := &nats.Msg{
        Subject: "transactions",
        Data:    marshalTx(tx),
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, tx.TransactionID)
    
    _, err := js.PublishMsg(msg)
    return err
}
```

### 4. 数据同步

```go
// 数据变更：使用版本号作为 Msg-Id
func syncDataChange(js nats.JetStreamContext, change DataChange) error {
    msgID := fmt.Sprintf("%s-%d", change.EntityID, change.Version)
    msg := &nats.Msg{
        Subject: "data.changes",
        Data:    marshalChange(change),
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, msgID)
    
    _, err := js.PublishMsg(msg)
    return err
}
```

## 最佳实践

### 1. Msg-Id 生成策略

```go
// ✅ 推荐：使用业务 ID
msg.Header.Set(nats.MsgIdHdr, order.OrderID)

// ✅ 推荐：使用 UUID
msg.Header.Set(nats.MsgIdHdr, uuid.New().String())

// ✅ 推荐：组合 ID
msgID := fmt.Sprintf("%s-%d-%d", userID, timestamp, sequence)
msg.Header.Set(nats.MsgIdHdr, msgID)

// ❌ 避免：使用消息内容哈希（内容相同但业务不同的消息会被误去重）
hash := sha256.Sum256(data)
msg.Header.Set(nats.MsgIdHdr, hex.EncodeToString(hash[:]))
```

### 2. 窗口大小配置

```go
// 短期任务（快速重试）
Duplicates: 1 * time.Minute

// 中期任务（一般业务）
Duplicates: 5 * time.Minute

// 长期任务（慢速重试）
Duplicates: 30 * time.Minute

// 超长期（特殊场景）
Duplicates: 24 * time.Hour
```

### 3. 错误处理

```go
func publishWithDedup(js nats.JetStreamContext, subject, msgID string, data []byte) error {
    msg := &nats.Msg{
        Subject: subject,
        Data:    data,
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, msgID)
    
    ack, err := js.PublishMsg(msg)
    if err != nil {
        return fmt.Errorf("publish failed: %w", err)
    }
    
    // 检查是否是重复消息
    if ack.Duplicate {
        log.Printf("消息已存在（去重）: msgID=%s", msgID)
        return nil  // 不是错误，只是重复
    }
    
    log.Printf("消息发布成功: msgID=%s, seq=%d", msgID, ack.Sequence)
    return nil
}
```

### 4. 监控去重率

```go
var (
    publishCount atomic.Int64
    dedupCount   atomic.Int64
)

func publishWithMetrics(js nats.JetStreamContext, subject, msgID string, data []byte) error {
    publishCount.Add(1)
    
    msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
    msg.Header.Set(nats.MsgIdHdr, msgID)
    
    ack, err := js.PublishMsg(msg)
    if err != nil {
        return err
    }
    
    if ack.Duplicate {
        dedupCount.Add(1)
    }
    
    // 定期输出统计
    if publishCount.Load() % 1000 == 0 {
        dedupRate := float64(dedupCount.Load()) / float64(publishCount.Load()) * 100
        log.Printf("去重率: %.2f%% (%d/%d)", dedupRate, dedupCount.Load(), publishCount.Load())
    }
    
    return nil
}
```

## 常见问题

### Q1: 去重窗口过期后会怎样？

**答案：** 窗口过期后，相同 Msg-Id 的消息可以再次被存储。

```go
// 时间线：
// T0: 发布消息（Msg-Id: "id-1"）→ 存储成功
// T1 (4分钟后): 发布消息（Msg-Id: "id-1"）→ 被去重
// T2 (6分钟后): 发布消息（Msg-Id: "id-1"）→ 存储成功（窗口已过期）
```

### Q2: 不设置 Msg-Id 会怎样？

**答案：** 不会去重，所有消息都会被存储。

```go
// 没有 Msg-Id
js.Publish(subject, []byte("hello"))  // seq=1
js.Publish(subject, []byte("hello"))  // seq=2（重复存储）
js.Publish(subject, []byte("hello"))  // seq=3（重复存储）
```

### Q3: Msg-Id 冲突怎么办？

**答案：** 使用更复杂的 ID 生成策略。

```go
// 方案 1：添加时间戳
msgID := fmt.Sprintf("%s-%d", businessID, time.Now().UnixNano())

// 方案 2：添加随机数
msgID := fmt.Sprintf("%s-%s", businessID, uuid.New().String())

// 方案 3：使用命名空间
msgID := fmt.Sprintf("order:%s:created:%d", orderID, timestamp)
```

### Q4: 去重会影响性能吗？

**性能影响：**
- 内存：需要存储窗口内所有 Msg-Id（约 100 bytes/ID）
- CPU：每次发布需要查找 Msg-Id（O(1) 哈希查找）
- 延迟：增加约 0.1-0.5ms

**优化建议：**
- 合理设置窗口大小
- 使用短的 Msg-Id（UUID 比长字符串好）
- 监控内存使用

### Q5: 如何验证去重是否生效？

```go
// 测试代码
func testDedup(js nats.JetStreamContext) {
    msgID := "test-id-123"
    
    // 第一次发布
    ack1, _ := js.PublishMsg(&nats.Msg{
        Subject: "test",
        Data:    []byte("msg1"),
        Header:  nats.Header{"Nats-Msg-Id": []string{msgID}},
    })
    log.Printf("第一次: Duplicate=%v, Seq=%d", ack1.Duplicate, ack1.Sequence)
    
    // 第二次发布（相同 Msg-Id）
    ack2, _ := js.PublishMsg(&nats.Msg{
        Subject: "test",
        Data:    []byte("msg2"),
        Header:  nats.Header{"Nats-Msg-Id": []string{msgID}},
    })
    log.Printf("第二次: Duplicate=%v, Seq=%d", ack2.Duplicate, ack2.Sequence)
    
    // 预期输出：
    // 第一次: Duplicate=false, Seq=1
    // 第二次: Duplicate=true, Seq=1（注意：seq 相同）
}
```

## 性能测试

### 测试场景

```go
// 测试 1：无去重
for i := 0; i < 10000; i++ {
    js.Publish(subject, []byte(fmt.Sprintf("msg-%d", i)))
}
// 结果：10000 条消息，耗时 ~1s

// 测试 2：有去重（无重复）
for i := 0; i < 10000; i++ {
    msg := &nats.Msg{
        Subject: subject,
        Data:    []byte(fmt.Sprintf("msg-%d", i)),
        Header:  nats.Header{"Nats-Msg-Id": []string{fmt.Sprintf("id-%d", i)}},
    }
    js.PublishMsg(msg)
}
// 结果：10000 条消息，耗时 ~1.1s（增加 10%）

// 测试 3：有去重（50% 重复）
for i := 0; i < 10000; i++ {
    msgID := fmt.Sprintf("id-%d", i/2)  // 50% 重复
    msg := &nats.Msg{
        Subject: subject,
        Data:    []byte(fmt.Sprintf("msg-%d", i)),
        Header:  nats.Header{"Nats-Msg-Id": []string{msgID}},
    }
    js.PublishMsg(msg)
}
// 结果：5000 条消息，耗时 ~1.1s
```

## 实战示例

### 示例 1：订单系统

```go
type OrderService struct {
    js nats.JetStreamContext
}

func (s *OrderService) CreateOrder(order Order) error {
    // 使用订单号作为 Msg-Id，确保幂等性
    msg := &nats.Msg{
        Subject: "orders.created",
        Data:    marshalOrder(order),
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, order.OrderID)
    
    ack, err := s.js.PublishMsg(msg)
    if err != nil {
        return err
    }
    
    if ack.Duplicate {
        log.Printf("订单已存在: %s", order.OrderID)
        return ErrOrderExists
    }
    
    return nil
}
```

### 示例 2：支付回调

```go
func handlePaymentCallback(js nats.JetStreamContext, callback PaymentCallback) error {
    // 使用支付平台的交易号作为 Msg-Id
    msgID := fmt.Sprintf("payment:%s:%s", callback.Platform, callback.TransactionID)
    
    msg := &nats.Msg{
        Subject: "payments.callback",
        Data:    marshalCallback(callback),
        Header:  nats.Header{},
    }
    msg.Header.Set(nats.MsgIdHdr, msgID)
    
    ack, err := js.PublishMsg(msg)
    if err != nil {
        return err
    }
    
    if ack.Duplicate {
        log.Printf("支付回调已处理: %s", msgID)
        return nil  // 返回成功，避免支付平台重试
    }
    
    return nil
}
```

## 相关示例

- **01_reliability**: 可靠性和持久化
- **03_ordering**: 有序消费
- **07_dlq**: 死信队列

## 总结

本示例展示了 NATS JetStream 的消息去重功能：

✅ **Duplicates 窗口**：配置去重时间窗口  
✅ **Msg-Id 头部**：唯一标识消息  
✅ **自动去重**：相同 Msg-Id 在窗口内只存储一次  
✅ **幂等性保证**：防止重复消息  
✅ **网络重试保护**：安全地重试发布  

通过合理配置去重窗口和 Msg-Id 生成策略，可以有效防止重复消息，提高系统的可靠性和幂等性。
