# NATS JetStream 消息模式：扇出与队列

## 概述

本示例演示 NATS JetStream 的两种核心消息分发模式：
- **扇出模式（Fan-out）**：广播消息给多个独立订阅者
- **队列模式（Queue）**：负载均衡，消息在队列组成员间分发

## 核心概念

### 扇出模式（Fan-out / Broadcast）

每个订阅者都有**独立的 durable 名称**，因此：
- 每个订阅者维护自己的消费进度
- 所有订阅者都会收到所有消息
- 适用于需要多个服务独立处理同一消息的场景

### 队列模式（Queue / Load Balancing）

多个订阅者共享**同一个 durable 名称**和**队列组**，因此：
- 每条消息只投递给组内一个成员
- 实现负载均衡和水平扩展
- 适用于需要分散处理负载的场景

## 架构图

```
                    ┌─────────────┐
                    │  Producer   │
                    └──────┬──────┘
                           │ Publish
                           ▼
        ┌──────────────────────────────────────┐
        │   JetStream Stream (F_MODES)         │
        └──────────────┬───────────────────────┘
                       │
        ┌──────────────┴──────────────┐
        │                             │
        ▼                             ▼
┌───────────────────┐      ┌──────────────────────┐
│   扇出模式         │      │   队列模式            │
│   (Fan-out)       │      │   (Queue)            │
└───────┬───────────┘      └──────┬───────────────┘
        │                         │
    ┌───┴───┐              ┌──────┴──────┐
    ▼       ▼              ▼             ▼
┌────────┐ ┌────────┐  ┌────────┐  ┌────────┐
│ Sub A  │ │ Sub B  │  │Member 1│  │Member 2│
│(独立)  │ │(独立)  │  │(共享)  │  │(共享)  │
└────────┘ └────────┘  └────────┘  └────────┘
  收到全部   收到全部     收到部分     收到部分
```

## 代码结构

| 组件 | 说明 |
|------|------|
| `subAll()` | 创建扇出模式订阅者，每个有独立的 durable 名称 |
| `QueueSubscribe()` | 创建队列模式订阅者，共享 durable 和队列组 |
| 发布消息 | 发布 4 条测试消息观察分发效果 |

## 消息分发对比

### 发布 4 条消息的分发结果

| 消息 | fanout-A | fanout-B | queue member 1 | queue member 2 |
|------|----------|----------|----------------|----------------|
| Msg 1 | ✅ | ✅ | ✅ | ❌ |
| Msg 2 | ✅ | ✅ | ❌ | ✅ |
| Msg 3 | ✅ | ✅ | ✅ | ❌ |
| Msg 4 | ✅ | ✅ | ❌ | ✅ |
| **总计** | **4** | **4** | **2** | **2** |

### 扇出模式特点

```go
// 每个订阅者有独立的 durable 名称
js.Subscribe(subject, handler, nats.Durable("fanout-A"))
js.Subscribe(subject, handler, nats.Durable("fanout-B"))
```

- ✅ 所有订阅者都收到所有消息
- ✅ 每个订阅者独立维护消费进度
- ✅ 一个订阅者故障不影响其他订阅者
- ❌ 消息处理量翻倍（N 个订阅者 = N 倍处理）

### 队列模式特点

```go
// 多个成员共享同一个 durable 和队列组
js.QueueSubscribe(subject, "workers", handler, nats.Durable("F_MODES_Q"))
js.QueueSubscribe(subject, "workers", handler, nats.Durable("F_MODES_Q"))
```

- ✅ 每条消息只处理一次
- ✅ 自动负载均衡
- ✅ 水平扩展（增加成员提高吞吐量）
- ✅ 高可用（一个成员故障，其他成员接管）

## 运行示例

### 前置条件

```bash
# 启动 NATS Server
docker run -p 4222:4222 nats:latest -js
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/02_modes
go run main.go
```

### 预期输出

```
2026/02/02 18:00:00 fanout-A 收到: 2026-02-02T18:00:00.123456789+08:00
2026/02/02 18:00:00 fanout-B 收到: 2026-02-02T18:00:00.123456789+08:00
2026/02/02 18:00:00 队列成员 1 收到: 2026-02-02T18:00:00.123456789+08:00

2026/02/02 18:00:00 fanout-A 收到: 2026-02-02T18:00:00.234567890+08:00
2026/02/02 18:00:00 fanout-B 收到: 2026-02-02T18:00:00.234567890+08:00
2026/02/02 18:00:00 队列成员 2 收到: 2026-02-02T18:00:00.234567890+08:00

2026/02/02 18:00:00 fanout-A 收到: 2026-02-02T18:00:00.345678901+08:00
2026/02/02 18:00:00 fanout-B 收到: 2026-02-02T18:00:00.345678901+08:00
2026/02/02 18:00:00 队列成员 1 收到: 2026-02-02T18:00:00.345678901+08:00

2026/02/02 18:00:00 fanout-A 收到: 2026-02-02T18:00:00.456789012+08:00
2026/02/02 18:00:00 fanout-B 收到: 2026-02-02T18:00:00.456789012+08:00
2026/02/02 18:00:00 队列成员 2 收到: 2026-02-02T18:00:00.456789012+08:00

2026/02/02 18:00:00 消息模式演示运行中；按 Ctrl+C 退出
```

## 使用场景

### 扇出模式适用场景

1. **多服务协同**
```
订单创建 → 扇出
  ├─ 库存服务（扣减库存）
  ├─ 支付服务（创建支付单）
  ├─ 通知服务（发送通知）
  └─ 日志服务（记录日志）
```

2. **数据同步**
```
数据变更 → 扇出
  ├─ 数据库同步
  ├─ 缓存更新
  ├─ 搜索引擎索引
  └─ 数据仓库
```

3. **监控告警**
```
系统事件 → 扇出
  ├─ 监控系统
  ├─ 告警系统
  ├─ 审计系统
  └─ 分析系统
```

### 队列模式适用场景

1. **任务处理**
```
任务队列 → 队列组
  ├─ Worker 1
  ├─ Worker 2
  ├─ Worker 3
  └─ Worker N
```

2. **负载均衡**
```
HTTP 请求 → 队列组
  ├─ 服务实例 1
  ├─ 服务实例 2
  └─ 服务实例 3
```

3. **批量处理**
```
数据批次 → 队列组
  ├─ 处理器 1
  ├─ 处理器 2
  └─ 处理器 3
```

## 最佳实践

### 1. 选择合适的模式

```go
// 需要多个服务独立处理 → 扇出模式
js.Subscribe(subject, handler, nats.Durable("service-A"))
js.Subscribe(subject, handler, nats.Durable("service-B"))

// 需要负载均衡 → 队列模式
js.QueueSubscribe(subject, "workers", handler, nats.Durable("worker-pool"))
```

### 2. 混合使用两种模式

```go
// 同时使用扇出和队列模式
// 扇出：日志服务独立接收所有消息
js.Subscribe(subject, logHandler, nats.Durable("logger"))

// 队列：业务处理负载均衡
js.QueueSubscribe(subject, "processors", processHandler, nats.Durable("processor-pool"))
```

### 3. 队列组命名规范

```go
// 推荐：使用描述性名称
queueGroup := "order-processors"
queueGroup := "email-workers"
queueGroup := "image-resizers"

// 避免：通用名称
queueGroup := "workers"  // 不够具体
queueGroup := "queue"    // 不够描述性
```

### 4. 动态扩缩容

```go
// 队列模式支持动态增加成员
// 启动更多实例即可自动负载均衡
for i := 0; i < workerCount; i++ {
    js.QueueSubscribe(subject, queueGroup, handler, nats.Durable(durable))
}
```

### 5. 错误处理

```go
js.Subscribe(subject, func(m *nats.Msg) {
    if err := processMessage(m); err != nil {
        // 扇出模式：记录错误但不影响其他订阅者
        log.Printf("处理失败: %v", err)
        m.Nak()  // 重新投递
        return
    }
    m.Ack()
}, nats.Durable("processor"))
```

## 性能考虑

### 扇出模式性能

```
吞吐量 = 单个订阅者吞吐量
延迟 = 最慢订阅者的延迟
资源消耗 = N × 单个订阅者资源
```

**优化建议：**
- 确保所有订阅者性能均衡
- 避免慢订阅者拖累整体性能
- 考虑使用异步处理

### 队列模式性能

```
吞吐量 = N × 单个成员吞吐量
延迟 = 单个成员的平均延迟
资源消耗 = N × 单个成员资源
```

**优化建议：**
- 根据负载动态调整成员数量
- 监控队列积压情况
- 使用 Pull Subscribe 更好地控制流量

## 常见问题

### Q1: 扇出模式下，如何确保所有订阅者都处理成功？

**方案 1：使用事务协调器**
```go
// 每个订阅者处理完成后通知协调器
// 协调器等待所有订阅者完成
```

**方案 2：使用 Saga 模式**
```go
// 每个订阅者独立处理
// 失败时执行补偿操作
```

### Q2: 队列模式下，如何保证消息顺序？

**答案：** 队列模式不保证顺序，因为消息会分发给不同成员。

**解决方案：**
- 使用单个消费者（放弃负载均衡）
- 使用分区（按 key 路由到固定成员）
- 使用有序消费者（见 03_ordering 示例）

### Q3: 如何监控队列组的负载均衡情况？

```go
// 在每个成员中记录处理的消息数
var processedCount atomic.Int64

js.QueueSubscribe(subject, queueGroup, func(m *nats.Msg) {
    processedCount.Add(1)
    // 定期输出统计信息
    if processedCount.Load() % 100 == 0 {
        log.Printf("成员 %s 已处理 %d 条消息", memberID, processedCount.Load())
    }
    // 处理消息
}, nats.Durable(durable))
```

### Q4: 扇出模式下，一个订阅者故障会影响其他订阅者吗？

**不会**。每个订阅者维护独立的消费进度：
- 故障订阅者重启后从上次位置继续
- 其他订阅者不受影响
- 消息不会丢失

### Q5: 队列模式下，如何实现优先级处理？

```go
// 方案 1：使用多个队列组
js.QueueSubscribe("high-priority", "workers", handler, nats.Durable("high"))
js.QueueSubscribe("low-priority", "workers", handler, nats.Durable("low"))

// 方案 2：在消息中添加优先级字段，消费者根据优先级处理
```

## 对比表

| 特性 | 扇出模式 | 队列模式 |
|------|---------|---------|
| Durable 名称 | 每个订阅者独立 | 共享同一个 |
| 消息投递 | 所有订阅者都收到 | 只投递给一个成员 |
| 消费进度 | 每个订阅者独立 | 队列组共享 |
| 负载均衡 | ❌ | ✅ |
| 水平扩展 | ❌ | ✅ |
| 消息处理次数 | N 次（N=订阅者数） | 1 次 |
| 适用场景 | 多服务协同 | 任务分发 |

## 实战示例

### 示例 1：订单处理系统

```go
// 扇出：多个服务独立处理订单
js.Subscribe("orders.created", inventoryHandler, nats.Durable("inventory-service"))
js.Subscribe("orders.created", paymentHandler, nats.Durable("payment-service"))
js.Subscribe("orders.created", notificationHandler, nats.Durable("notification-service"))

// 队列：订单审核任务负载均衡
js.QueueSubscribe("orders.review", "reviewers", reviewHandler, nats.Durable("review-pool"))
```

### 示例 2：日志处理系统

```go
// 扇出：日志同时写入多个目标
js.Subscribe("logs", elasticsearchHandler, nats.Durable("elasticsearch"))
js.Subscribe("logs", s3Handler, nats.Durable("s3-archiver"))
js.Subscribe("logs", alertHandler, nats.Durable("alerting"))

// 队列：日志解析任务负载均衡
js.QueueSubscribe("logs.raw", "parsers", parseHandler, nats.Durable("parser-pool"))
```

## 相关示例

- **01_reliability**: 可靠性和持久化
- **03_ordering**: 有序消费
- **07_dlq**: 死信队列

## 总结

本示例展示了 NATS JetStream 的两种核心消息分发模式：

✅ **扇出模式**：广播消息给多个独立订阅者，适用于多服务协同  
✅ **队列模式**：负载均衡，消息在队列组成员间分发，适用于任务分发  
✅ **灵活组合**：可以同时使用两种模式满足复杂业务需求  
✅ **水平扩展**：队列模式支持动态增加成员提高吞吐量  

根据业务场景选择合适的模式，可以构建高效、可扩展的消息处理系统。
