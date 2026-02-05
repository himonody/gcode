# NATS JetStream 可靠性（零丢失）

## 概述

本示例演示如何使用 NATS JetStream 实现零消息丢失的可靠消息传递。通过持久化流存储和显式 ACK 确认机制，确保消息在任何情况下都不会丢失。

## 核心概念

### 什么是可靠性？

在消息队列系统中，可靠性意味着：
- **消息持久化**：消息写入磁盘，服务器重启后不丢失
- **显式确认**：消费者必须明确确认消息已处理
- **至少一次投递**：消息至少被成功投递一次
- **故障恢复**：网络中断或服务重启后能恢复消费

### 实现机制

1. **FileStorage**：使用文件存储而非内存存储
2. **AckExplicitPolicy**：要求消费者显式调用 `Ack()` 确认
3. **Durable Consumer**：持久化消费者，记录消费进度
4. **Pull Subscribe**：主动拉取模式，更好的流量控制

## 架构图

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ Publish
       ▼
┌─────────────────────────────────────────┐
│    JetStream Stream (F_RELIABLE)        │
│    Storage: FileStorage (持久化)         │
│    Retention: LimitsPolicy              │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│    Durable Pull Consumer                │
│    (F_REL_DURABLE)                      │
│    AckPolicy: Explicit                  │
└──────────────┬──────────────────────────┘
               │ Fetch (Pull)
               ▼
┌─────────────────────────────────────────┐
│    Consumer Application                 │
│    1. Fetch messages                    │
│    2. Process                           │
│    3. Explicit Ack()                    │
└─────────────────────────────────────────┘
```

## 关键特性

### 1. 持久化流存储

```go
Storage: nats.FileStorage  // 文件存储，消息持久化到磁盘
```

**对比：**
- `FileStorage`：消息写入磁盘，服务器重启后仍然存在
- `MemoryStorage`：消息仅存储在内存，服务器重启后丢失

### 2. 显式确认策略

```go
AckPolicy: nats.AckExplicitPolicy  // 必须显式调用 Ack()
```

**确认策略对比：**
- `AckExplicitPolicy`：必须显式调用 `m.Ack()`
- `AckAllPolicy`：确认当前消息及之前的所有消息
- `AckNonePolicy`：无需确认（不可靠）

### 3. 拉取模式（Pull Subscribe）

```go
sub, err := js.PullSubscribe(subject, durable, nats.BindStream(stream))
msgs, err := sub.Fetch(3, nats.MaxWait(2*time.Second))
```

**优势：**
- 消费者控制拉取速率
- 避免消息积压
- 更好的背压（backpressure）控制

### 4. 持久化消费者

```go
Durable: "F_REL_DURABLE"  // 消费者名称持久化
```

**作用：**
- 记录消费进度（offset）
- 消费者重启后从上次位置继续
- 多个实例可以共享同一个 durable consumer

## 代码结构

| 组件 | 说明 |
|------|------|
| `main()` | 主函数，初始化连接、创建流和消费者、发布和消费消息 |
| `ensureStream()` | 确保 JetStream 流存在，配置持久化存储 |
| Pull Subscribe | 使用拉取模式订阅消息 |
| Explicit Ack | 显式确认每条消息已处理 |

## 工作流程

### 消息发布流程

```
1. 连接到 NATS Server
2. 获取 JetStream 上下文
3. 确保流存在（FileStorage）
4. 发布消息到流
5. 消息持久化到磁盘
```

### 消息消费流程

```
1. 创建持久化拉取消费者
2. Fetch 拉取指定数量的消息
3. 处理每条消息
4. 显式调用 Ack() 确认
5. 消费进度被记录
```

### 故障恢复流程

```
场景：消费者处理到一半时崩溃

1. 消费者重启
2. 使用相同的 durable 名称重新订阅
3. 从上次 Ack 的位置继续消费
4. 未 Ack 的消息会被重新投递
```

## 运行示例

### 前置条件

1. 启动 NATS Server（支持 JetStream）：
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
cd /Users/mac/workspace/code/gcode/nats/features/01_reliability
go run main.go
```

### 预期输出

```
2026/02/02 17:45:00 收到消息 seq=1 data=2026-02-02T17:45:00.123456789+08:00
2026/02/02 17:45:00 收到消息 seq=2 data=2026-02-02T17:45:00.234567890+08:00
2026/02/02 17:45:00 收到消息 seq=3 data=2026-02-02T17:45:00.345678901+08:00
2026/02/02 17:45:00 可靠性演示完成；等待信号...
```

## 可靠性验证

### 测试 1：消息持久化

```bash
# 1. 运行程序发布消息
go run main.go

# 2. Ctrl+C 停止程序

# 3. 重启 NATS Server
docker restart <nats-container>

# 4. 再次运行程序
go run main.go

# 结果：消息仍然存在，可以被消费
```

### 测试 2：消费进度保存

```go
// 修改代码：只 Ack 第一条消息
for i, m := range msgs {
    meta, _ := m.Metadata()
    log.Printf("收到消息 seq=%d", meta.Sequence.Stream)
    if i == 0 {
        m.Ack()  // 只确认第一条
    }
}

// 重启程序，会从第二条消息开始消费
```

### 测试 3：网络中断恢复

```bash
# 1. 运行程序
go run main.go

# 2. 断开网络连接
# 观察日志：连接断开: ...

# 3. 恢复网络连接
# 观察日志：已重新连接

# 结果：自动重连，继续正常工作
```

## 最佳实践

### 1. 选择合适的存储类型

```go
// 生产环境：使用文件存储
Storage: nats.FileStorage

// 开发/测试：可以使用内存存储（更快）
Storage: nats.MemoryStorage
```

### 2. 设置合理的重试策略

```go
nats.Connect(url,
    nats.RetryOnFailedConnect(true),
    nats.MaxReconnects(-1),           // 生产环境：无限重连
    nats.ReconnectWait(time.Second),  // 重连间隔
)
```

### 3. 批量拉取消息

```go
// 一次拉取多条消息，提高吞吐量
msgs, err := sub.Fetch(100, nats.MaxWait(5*time.Second))
```

### 4. 错误处理

```go
for _, m := range msgs {
    if err := processMessage(m); err != nil {
        // 处理失败：不 Ack，消息会被重新投递
        log.Printf("处理失败: %v", err)
        continue
    }
    // 处理成功：Ack
    m.Ack()
}
```

### 5. 监控消费进度

```go
meta, err := m.Metadata()
if err == nil {
    log.Printf("Stream Seq: %d, Consumer Seq: %d, Pending: %d",
        meta.Sequence.Stream,
        meta.Sequence.Consumer,
        meta.NumPending)
}
```

## 性能考虑

### 1. 批量大小

```go
// 小批量：低延迟，高开销
msgs, _ := sub.Fetch(1, nats.MaxWait(1*time.Second))

// 大批量：高吞吐，可能增加延迟
msgs, _ := sub.Fetch(1000, nats.MaxWait(10*time.Second))

// 推荐：根据业务场景调整（10-100）
msgs, _ := sub.Fetch(50, nats.MaxWait(5*time.Second))
```

### 2. 并发处理

```go
// Worker Pool 模式
for _, m := range msgs {
    m := m  // 避免闭包问题
    go func() {
        if err := processMessage(m); err == nil {
            m.Ack()
        }
    }()
}
```

### 3. 磁盘 I/O 优化

```bash
# 使用 SSD 存储 JetStream 数据
nats-server -js -sd /path/to/ssd/jetstream
```

## 常见问题

### Q1: 消息会丢失吗？

**不会**，只要满足以下条件：
- 使用 `FileStorage`
- 使用 `AckExplicitPolicy`
- 消息成功发布后（收到 PubAck）
- 在 Ack 之前不会删除消息

### Q2: 消息会重复吗？

**可能会**，在以下场景：
- 消费者处理完消息但在 Ack 之前崩溃
- 网络问题导致 Ack 丢失

**解决方案：**
- 实现幂等性处理
- 使用消息去重（见 04_dedup 示例）

### Q3: Pull vs Push 如何选择？

| 特性 | Pull | Push |
|------|------|------|
| 流量控制 | 消费者控制 | 服务器控制 |
| 背压处理 | 更好 | 可能积压 |
| 延迟 | 稍高 | 更低 |
| 适用场景 | 批处理、高吞吐 | 实时处理、低延迟 |

### Q4: 如何处理慢消费者？

```go
// 方案 1：增加并发
for _, m := range msgs {
    go processMessage(m)
}

// 方案 2：增加消费者实例（使用相同的 durable）
// 多个实例会自动负载均衡

// 方案 3：调整拉取批量大小
msgs, _ := sub.Fetch(200, nats.MaxWait(10*time.Second))
```

### Q5: 如何监控消费延迟？

```go
meta, _ := m.Metadata()
delay := time.Since(meta.Timestamp)
log.Printf("消息延迟: %v", delay)

// 监控 NumPending（待处理消息数）
if meta.NumPending > 1000 {
    log.Printf("警告：积压消息过多: %d", meta.NumPending)
}
```

## 对比其他模式

### vs Core NATS（无 JetStream）

| 特性 | Core NATS | JetStream |
|------|-----------|-----------|
| 持久化 | ❌ | ✅ |
| 消息重放 | ❌ | ✅ |
| 至少一次投递 | ❌ | ✅ |
| 性能 | 更高 | 稍低 |

### vs Kafka

| 特性 | NATS JetStream | Kafka |
|------|----------------|-------|
| 部署复杂度 | 低 | 高 |
| 性能 | 高 | 高 |
| 存储模型 | 流 | 日志 |
| 消费模型 | Pull/Push | Pull |

## 相关示例

- **02_modes**: 扇出和队列模式
- **03_ordering**: 有序消费
- **04_dedup**: 消息去重
- **07_dlq**: 死信队列

## 总结

本示例展示了 NATS JetStream 的核心可靠性特性：

✅ **持久化存储**：FileStorage 确保消息不丢失  
✅ **显式确认**：AckExplicitPolicy 确保消息被正确处理  
✅ **拉取模式**：Pull Subscribe 提供更好的流量控制  
✅ **持久化消费者**：Durable Consumer 记录消费进度  
✅ **自动重连**：网络中断后自动恢复  

通过这些机制，NATS JetStream 提供了企业级的消息可靠性保证。
