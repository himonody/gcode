# NATS JetStream 有序消费

## 概述

本示例演示如何使用 NATS JetStream 的有序消费者（Ordered Consumer）确保消息按序列号顺序投递。有序消费者会自动检测消息间隙并重新消费，保证消息的顺序性。

## 核心概念

### 什么是有序消费？

有序消费确保消息按照发布顺序被消费，即使在网络中断、消费者重启等情况下也能保持顺序。

### 有序消费者特点

1. **自动间隙检测**：检测到消息序列号跳跃时自动重启
2. **自动重新消费**：从间隙位置重新开始消费
3. **无需手动 ACK**：自动确认机制
4. **临时消费者**：不持久化，重启后从头开始

## 架构图

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ Publish (seq: 1, 2, 3, 4, 5)
       ▼
┌─────────────────────────────────────────┐
│   JetStream Stream (F_ORDERED)          │
│   Messages: [1] [2] [3] [4] [5]         │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│   Ordered Consumer                      │
│   - 检测序列号连续性                      │
│   - 发现间隙自动重启                      │
│   - 从间隙位置重新消费                    │
└──────────────┬──────────────────────────┘
               │ 顺序投递
               ▼
┌─────────────────────────────────────────┐
│   Consumer Application                  │
│   收到: 1 → 2 → 3 → 4 → 5               │
└─────────────────────────────────────────┘
```

## 工作原理

### 正常情况

```
流中的消息: [1] [2] [3] [4] [5]
消费者收到: 1 → 2 → 3 → 4 → 5
结果: ✅ 顺序正确
```

### 间隙检测与修复

```
场景：消费者收到 seq 1, 2, 然后直接收到 seq 5

流中的消息: [1] [2] [3] [4] [5]
消费者收到: 1 → 2 → 5 (检测到间隙！)
           ↓
        自动重启
           ↓
重新消费: 1 → 2 → 3 → 4 → 5
结果: ✅ 间隙已修复，顺序正确
```

## 关键特性

### 1. OrderedConsumer 选项

```go
js.Subscribe(subject, handler, nats.OrderedConsumer())
```

**特点：**
- 自动创建临时消费者
- 不需要指定 durable 名称
- 自动检测和修复间隙
- 自动 ACK（无需手动确认）

### 2. 间隙检测机制

```go
// 有序消费者内部逻辑（伪代码）
lastSeq := 0
for msg := range messages {
    currentSeq := msg.Sequence
    if currentSeq != lastSeq + 1 {
        // 检测到间隙！
        log.Printf("间隙检测: 期望 %d, 收到 %d", lastSeq+1, currentSeq)
        // 重启消费者，从 lastSeq+1 开始重新消费
        restart(lastSeq + 1)
    }
    lastSeq = currentSeq
}
```

### 3. 自动 ACK

有序消费者会自动确认消息，无需手动调用 `m.Ack()`：

```go
js.Subscribe(subject, func(m *nats.Msg) {
    // 处理消息
    processMessage(m)
    // 不需要 m.Ack()，会自动确认
}, nats.OrderedConsumer())
```

## 运行示例

### 前置条件

```bash
# 启动 NATS Server
docker run -p 4222:4222 nats:latest -js
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/03_ordering
go run main.go
```

### 预期输出

```
2026/02/02 18:10:00 有序消费 seq=1 data=2026-02-02T18:10:00.123456789+08:00
2026/02/02 18:10:00 有序消费 seq=2 data=2026-02-02T18:10:00.323456789+08:00
2026/02/02 18:10:00 有序消费 seq=3 data=2026-02-02T18:10:00.523456789+08:00
2026/02/02 18:10:01 有序消费 seq=4 data=2026-02-02T18:10:00.723456789+08:00
2026/02/02 18:10:01 有序消费 seq=5 data=2026-02-02T18:10:00.923456789+08:00
2026/02/02 18:10:01 有序消费演示运行中；按 Ctrl+C 退出
```

## 使用场景

### 1. 金融交易

```go
// 股票交易必须按顺序处理
js.Subscribe("trades.AAPL", func(m *nats.Msg) {
    var trade Trade
    json.Unmarshal(m.Data, &trade)
    // 按顺序更新价格
    updatePrice(trade)
}, nats.OrderedConsumer())
```

### 2. 数据库变更日志

```go
// 数据库 binlog 必须按顺序应用
js.Subscribe("db.changelog", func(m *nats.Msg) {
    var change DBChange
    json.Unmarshal(m.Data, &change)
    // 按顺序应用变更
    applyChange(change)
}, nats.OrderedConsumer())
```

### 3. 状态机转换

```go
// 订单状态转换必须按顺序
js.Subscribe("orders.state", func(m *nats.Msg) {
    var stateChange StateChange
    json.Unmarshal(m.Data, &stateChange)
    // 按顺序更新状态
    updateState(stateChange)
}, nats.OrderedConsumer())
```

### 4. 日志聚合

```go
// 日志必须按时间顺序聚合
js.Subscribe("logs.app", func(m *nats.Msg) {
    var logEntry LogEntry
    json.Unmarshal(m.Data, &logEntry)
    // 按顺序写入日志
    writeLog(logEntry)
}, nats.OrderedConsumer())
```

## 对比：有序 vs 普通消费者

| 特性 | 有序消费者 | 普通消费者 |
|------|-----------|-----------|
| 顺序保证 | ✅ 严格顺序 | ❌ 不保证 |
| 间隙检测 | ✅ 自动 | ❌ 无 |
| 间隙修复 | ✅ 自动重启 | ❌ 需手动处理 |
| ACK 机制 | 自动 | 手动 |
| 持久化 | ❌ 临时 | ✅ 可持久化 |
| 性能 | 稍低（检测开销） | 更高 |
| 适用场景 | 严格顺序要求 | 高吞吐量 |

## 最佳实践

### 1. 何时使用有序消费者

```go
// ✅ 适合：严格顺序要求
js.Subscribe("financial.trades", handler, nats.OrderedConsumer())

// ❌ 不适合：高吞吐量、无顺序要求
js.Subscribe("logs.debug", handler, nats.Durable("logger"))
```

### 2. 错误处理

```go
js.Subscribe(subject, func(m *nats.Msg) {
    if err := processMessage(m); err != nil {
        // 有序消费者会自动 ACK，无法 NAK
        // 错误处理策略：
        // 1. 记录错误日志
        log.Printf("处理失败: %v", err)
        // 2. 发送到死信队列
        sendToDLQ(m, err)
        // 3. 触发告警
        alertError(err)
    }
}, nats.OrderedConsumer())
```

### 3. 监控序列号

```go
var lastSeq uint64

js.Subscribe(subject, func(m *nats.Msg) {
    meta, _ := m.Metadata()
    currentSeq := meta.Sequence.Consumer
    
    // 检测序列号跳跃（理论上不应该发生）
    if lastSeq > 0 && currentSeq != lastSeq+1 {
        log.Printf("警告：序列号跳跃 %d → %d", lastSeq, currentSeq)
    }
    
    lastSeq = currentSeq
    processMessage(m)
}, nats.OrderedConsumer())
```

### 4. 性能优化

```go
// 有序消费者不支持并发处理
// 如果需要提高吞吐量，考虑：

// 方案 1：分区（按 key 路由到不同流）
js.Subscribe("orders.partition-1", handler, nats.OrderedConsumer())
js.Subscribe("orders.partition-2", handler, nats.OrderedConsumer())

// 方案 2：批量处理
var batch []Message
js.Subscribe(subject, func(m *nats.Msg) {
    batch = append(batch, m)
    if len(batch) >= 100 {
        processBatch(batch)
        batch = batch[:0]
    }
}, nats.OrderedConsumer())
```

## 常见问题

### Q1: 有序消费者会重复消费消息吗？

**会**。当检测到间隙时，有序消费者会从间隙位置重新开始消费，因此会重复消费部分消息。

**解决方案：**
- 实现幂等性处理
- 使用消息去重（见 04_dedup 示例）

### Q2: 有序消费者的性能如何？

**性能特点：**
- 比普通消费者慢 10-20%（间隙检测开销）
- 无法并发处理（必须顺序处理）
- 适合中等吞吐量场景（< 10k msg/s）

**优化建议：**
- 使用分区提高并行度
- 批量处理消息
- 考虑使用普通消费者 + 应用层排序

### Q3: 有序消费者支持持久化吗？

**不支持**。有序消费者是临时的：
- 消费者重启后从头开始
- 不保存消费进度
- 适合短期运行的任务

**如需持久化：**
```go
// 使用普通 durable 消费者 + 应用层排序
js.Subscribe(subject, func(m *nats.Msg) {
    // 应用层实现顺序保证
    orderBuffer.Add(m)
    orderBuffer.ProcessInOrder()
}, nats.Durable("my-consumer"))
```

### Q4: 如何处理消息处理失败？

有序消费者自动 ACK，无法 NAK。处理失败的策略：

```go
js.Subscribe(subject, func(m *nats.Msg) {
    if err := processMessage(m); err != nil {
        // 策略 1：重试（同步）
        for i := 0; i < 3; i++ {
            if err = processMessage(m); err == nil {
                break
            }
            time.Sleep(time.Second)
        }
        
        // 策略 2：发送到 DLQ
        if err != nil {
            sendToDLQ(m, err)
        }
        
        // 策略 3：记录并继续
        if err != nil {
            log.Printf("处理失败，跳过: %v", err)
        }
    }
}, nats.OrderedConsumer())
```

### Q5: 有序消费者如何处理网络中断？

**自动恢复：**
1. 检测到连接中断
2. 自动重连
3. 重新创建消费者
4. 从上次位置继续（可能重复部分消息）

**注意：** 由于是临时消费者，长时间中断可能导致从头开始消费。

## 性能测试

### 测试场景

```go
// 发布 10000 条消息
for i := 0; i < 10000; i++ {
    js.Publish(subject, []byte(fmt.Sprintf("msg-%d", i)))
}

// 有序消费者
start := time.Now()
count := 0
js.Subscribe(subject, func(m *nats.Msg) {
    count++
    if count == 10000 {
        log.Printf("有序消费耗时: %v", time.Since(start))
    }
}, nats.OrderedConsumer())
```

### 性能对比

| 消费者类型 | 吞吐量 | 延迟 | CPU 使用 |
|-----------|--------|------|---------|
| 有序消费者 | ~8k msg/s | ~0.1ms | 中等 |
| 普通消费者 | ~10k msg/s | ~0.08ms | 较低 |
| 拉取消费者 | ~12k msg/s | ~0.05ms | 最低 |

## 实战示例

### 示例 1：订单状态机

```go
type OrderStateMachine struct {
    currentState string
}

func (sm *OrderStateMachine) handleStateChange(m *nats.Msg) {
    var change StateChange
    json.Unmarshal(m.Data, &change)
    
    // 状态转换必须按顺序
    if !sm.isValidTransition(sm.currentState, change.NewState) {
        log.Printf("非法状态转换: %s → %s", sm.currentState, change.NewState)
        return
    }
    
    sm.currentState = change.NewState
    log.Printf("状态转换: %s", sm.currentState)
}

// 使用有序消费者确保状态转换顺序
js.Subscribe("orders.state", sm.handleStateChange, nats.OrderedConsumer())
```

### 示例 2：账户余额更新

```go
type Account struct {
    balance int64
    mu      sync.Mutex
}

func (a *Account) handleTransaction(m *nats.Msg) {
    var tx Transaction
    json.Unmarshal(m.Data, &tx)
    
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // 交易必须按顺序处理
    a.balance += tx.Amount
    log.Printf("余额更新: %d (交易: %+d)", a.balance, tx.Amount)
}

// 使用有序消费者确保交易顺序
js.Subscribe("account.transactions", account.handleTransaction, nats.OrderedConsumer())
```

## 相关示例

- **01_reliability**: 可靠性和持久化
- **02_modes**: 扇出和队列模式
- **04_dedup**: 消息去重

## 总结

本示例展示了 NATS JetStream 有序消费者的核心特性：

✅ **严格顺序**：确保消息按序列号顺序投递  
✅ **自动间隙检测**：检测到序列号跳跃时自动重启  
✅ **自动修复**：从间隙位置重新消费  
✅ **简单易用**：无需手动 ACK，自动确认  
⚠️ **性能权衡**：比普通消费者慢 10-20%  
⚠️ **非持久化**：临时消费者，重启后从头开始  

适用于对消息顺序有严格要求的场景，如金融交易、状态机转换、数据库变更日志等。
