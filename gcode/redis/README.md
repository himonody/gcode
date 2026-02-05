# Redis 数据结构应用场景实现

本项目展示了 Redis 各种数据结构在实际业务场景中的应用实现。

## 项目结构

```
redis/
├── main.go                    # 主程序入口
├── string/                    # String 数据结构
│   ├── cache.go              # 普通缓存
│   ├── counter.go            # 计数器
│   ├── distributed_lock.go   # 分布式锁
│   └── session.go            # Session管理
├── hash/                      # Hash 数据结构
│   ├── user_info.go          # 用户信息存储
│   └── shopping_cart.go      # 购物车
├── list/                      # List 数据结构
│   ├── message_queue.go      # 消息队列
│   └── latest_messages.go    # 最新消息列表
├── set/                       # Set 数据结构
│   ├── deduplication.go      # 去重
│   ├── lottery.go            # 抽奖
│   └── common_friends.go     # 共同好友
├── zset/                      # ZSet 数据结构
│   ├── leaderboard.go        # 排行榜
│   ├── weighted_queue.go     # 带权重的任务队列
│   └── delay_queue.go        # 延迟队列
├── bitmap/                    # Bitmap 数据结构
│   ├── sign_in.go            # 签到统计
│   └── online_status.go      # 用户在线状态
└── stream/                    # Stream 数据结构
    ├── message_stream.go     # 消息持久化
    └── consumer_group.go     # 消费者组(多播)
```

## 数据结构与应用场景

### 1. String

**适用场景**: 普通缓存、计数器、分布式锁、Session

#### 缓存 (Cache)
- 基本的 Get/Set 操作
- 带过期时间的缓存
- SetNX 原子操作
- 获取缓存及剩余 TTL

#### 计数器 (Counter)
- 自增/自减操作
- 页面浏览量统计
- API 调用次数限制
- 点赞/取消点赞

#### 分布式锁 (Distributed Lock)
- 基于 SetNX 的锁实现
- 唯一标识防止误删
- 重试机制
- Lua 脚本保证原子性

#### Session
- 用户会话存储
- JSON 序列化
- 会话刷新
- 会话过期管理

### 2. Hash

**适用场景**: 存储对象(如用户信息)、购物车

#### 用户信息 (User Info)
- 存储用户详细信息
- 单字段/多字段更新
- 字段级别的原子操作
- 余额增减

#### 购物车 (Shopping Cart)
- 添加/删除商品
- 更新商品数量
- 购物车合并
- 商品数量统计

### 3. List

**适用场景**: 消息队列、最新消息列表

#### 消息队列 (Message Queue)
- FIFO 队列实现
- 阻塞式弹出
- 批量推送
- 队列长度查询

#### 最新消息列表 (Latest Messages)
- 保持固定长度的列表
- 最新消息优先
- 分页查询
- 范围查询

### 4. Set

**适用场景**: 去重、抽奖、共同好友

#### 去重 (Deduplication)
- 独立访客统计
- 文章浏览人数
- 自动去重特性

#### 抽奖 (Lottery)
- 参与者管理
- 随机抽奖(可重复)
- 抽奖并移除(不可重复)
- 批量抽奖

#### 共同好友 (Common Friends)
- 好友关系管理
- 共同好友查询
- 好友推荐(可能认识的人)
- 集合运算(交集/并集/差集)

### 5. ZSet

**适用场景**: 排行榜、带权重的任务队列、延迟队列

#### 排行榜 (Leaderboard)
- 分数管理
- 排名查询
- Top N 查询
- 分数段查询

#### 带权重的任务队列 (Weighted Queue)
- 优先级队列
- 按优先级弹出
- 阻塞式消费
- 查看队列状态

#### 延迟队列 (Delay Queue)
- 延迟任务调度
- 基于时间戳的排序
- 到期任务查询
- 任务认领机制

### 6. Bitmap

**适用场景**: 签到统计、布隆过滤器、用户在线状态

#### 签到统计 (Sign In)
- 每日签到记录
- 月度签到统计
- 连续签到天数
- 签到详情查询

#### 用户在线状态 (Online Status)
- 在线/离线状态
- 在线人数统计
- 批量状态设置
- 位运算(交集/并集/差集)
- 活跃用户统计

#### 用户活动统计 (User Activity)
- 多种活动类型记录
- 活动参与统计
- 多活动交叉分析

### 7. Stream

**适用场景**: 消息持久化、多播消息队列

#### 消息流 (Message Stream)
- 消息持久化存储
- 消息 ID 自动生成
- 范围查询
- 流长度限制
- 消息删除和修剪

#### 消费者组 (Consumer Group)
- 多消费者协作
- 消息确认机制
- 待处理消息查询
- 消息认领(Claim)
- 消费者管理

## 安装依赖

```bash
go get github.com/redis/go-redis/v9
```

## 配置 Redis

确保 Redis 服务已启动:

```bash
redis-server
```

默认配置:
- 地址: localhost:6379
- 密码: 无
- 数据库: 0

如需修改配置，请编辑 `main.go` 中的 Redis 连接参数。

## 运行示例

```bash
# 运行所有演示
go run main.go

# 或者构建后运行
go build -o redis-demo
./redis-demo
```

## 模块化使用

每个功能模块都可以独立使用:

```go
package main

import (
    "context"
    "github.com/redis/go-redis/v9"
    string_ops "gcode/redis/string"
)

func main() {
    client := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    
    ctx := context.Background()
    
    // 使用缓存服务
    cache := string_ops.NewCacheService(client)
    cache.Set(ctx, "key", "value", 10*time.Minute)
    
    // 使用计数器服务
    counter := string_ops.NewCounterService(client)
    count, _ := counter.Incr(ctx, "page_views")
    
    // 使用分布式锁
    lock := string_ops.NewDistributedLock(client)
    lockValue, _ := lock.Lock(ctx, "resource_lock", 30*time.Second)
    defer lock.Unlock(ctx, "resource_lock", lockValue)
}
```

## 最佳实践

### 1. 连接管理
- 使用连接池，避免频繁创建连接
- 合理设置超时时间
- 生产环境使用哨兵或集群模式

### 2. 键命名规范
- 使用冒号分隔命名空间: `user:1001:profile`
- 避免过长的键名
- 使用有意义的前缀

### 3. 过期时间
- 为缓存数据设置合理的过期时间
- 避免大量键同时过期
- 使用随机过期时间分散压力

### 4. 性能优化
- 使用 Pipeline 批量操作
- 避免大 Key 问题
- 合理使用数据结构

### 5. 错误处理
- 处理 Redis 连接失败
- 处理 Key 不存在的情况
- 实现降级策略

## 应用场景总结

| 数据结构 | 核心特性 | 典型应用 |
|---------|---------|---------|
| String | 简单的 KV 存储 | 缓存、计数、锁、Session |
| Hash | 字段级别操作 | 对象存储、购物车 |
| List | 有序列表 | 队列、最新列表 |
| Set | 无序去重集合 | 去重、抽奖、社交关系 |
| ZSet | 有序集合 | 排行榜、优先级队列、延迟队列 |
| Bitmap | 位操作 | 签到、在线状态、布隆过滤器 |
| Stream | 消息流 | 消息队列、事件溯源 |

## 参考资料

- [Redis 官方文档](https://redis.io/documentation)
- [go-redis 文档](https://redis.uptrace.dev/)
- [Redis 设计与实现](http://redisbook.com/)

## License

MIT
