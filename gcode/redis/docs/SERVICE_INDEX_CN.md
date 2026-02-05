# Redis 21个生产级服务完整索引

## 📚 服务分类总览

### String 数据结构 (4个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **StringCacheService** | `internal/service/string_cache_service.go` | 对象缓存、序列化存储 | 用户信息缓存、商品详情缓存、API响应缓存 |
| **CounterService** | `internal/service/string_counter_service.go` | 原子计数、限流控制 | 浏览量统计、API限流、库存扣减、分布式ID |
| **LockService** | `internal/service/lock_service.go` | 分布式锁、互斥控制 | 防重复提交、库存扣减、定时任务防重 |
| **SessionService** | `internal/service/string_session_service.go` | 会话管理、用户状态 | 用户登录态、SSO单点登录、临时权限 |

### Hash 数据结构 (2个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **UserInfoService** | `internal/service/hash_user_service.go` | 用户信息存储、字段级更新 | 用户资料、账户信息、配置项 |
| **ShoppingCartService** | `internal/service/hash_cart_service.go` | 购物车管理、商品数量 | 电商购物车、临时订单、商品收藏 |

### List 数据结构 (2个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **MessageQueueService** | `internal/service/list_queue_service.go` | FIFO队列、消息传递 | 异步任务、邮件队列、通知推送 |
| **LatestMessagesService** | `internal/service/list_latest_service.go` | 最新列表、固定长度 | 动态时间线、最新评论、操作日志 |

### Set 数据结构 (3个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **DeduplicationService** | `internal/service/set_dedup_service.go` | 去重、唯一性检查 | 独立访客统计、文章浏览人数、标签管理 |
| **LotteryService** | `internal/service/set_lottery_service.go` | 随机抽取、抽奖 | 活动抽奖、随机推荐、A/B测试分组 |
| **SocialGraphService** | `internal/service/set_social_service.go` | 社交关系、集合运算 | 好友关系、共同关注、推荐好友 |

### ZSet 数据结构 (3个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **LeaderboardService** | `internal/service/zset_leaderboard_service.go` | 排行榜、分数排序 | 游戏排行、销量榜、热度排行 |
| **PriorityQueueService** | `internal/service/zset_priority_service.go` | 优先级队列、权重调度 | 任务调度、消息优先级、工单处理 |
| **DelayQueueService** | `internal/service/zset_delay_service.go` | 延迟队列、定时任务 | 延迟通知、订单超时、定时提醒 |

### Bitmap 数据结构 (3个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **SignInService** | `internal/service/bitmap_signin_service.go` | 签到统计、连续天数 | 用户签到、打卡记录、活跃度统计 |
| **OnlineStatusService** | `internal/service/bitmap_online_service.go` | 在线状态、位图运算 | 用户在线、设备状态、实时监控 |
| **UserActivityService** | `internal/service/bitmap_activity_service.go` | 用户行为、活动分析 | 行为分析、漏斗统计、用户画像 |

### Stream 数据结构 (2个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **MessageStreamService** | `internal/service/stream_message_service.go` | 消息流、持久化 | 事件溯源、审计日志、消息存储 |
| **ConsumerGroupService** | `internal/service/stream_consumer_service.go` | 消费者组、多播 | 分布式消费、消息确认、负载均衡 |

### 辅助服务 (2个服务)

| 服务名 | 文件路径 | 核心功能 | 生产场景 |
|-------|---------|---------|---------|
| **GeoService** | `internal/service/geo_service.go` | 地理位置、距离计算 | LBS服务、附近的人、配送范围 |
| **HyperLogLogService** | `internal/service/hll_service.go` | 基数统计、去重计数 | UV统计、独立IP、大数据去重 |

---

## 🎯 按业务场景选择服务

### 用户相关

```
登录认证     → SessionService
用户资料     → UserInfoService
用户行为     → UserActivityService
在线状态     → OnlineStatusService
签到打卡     → SignInService
社交关系     → SocialGraphService
```

### 电商相关

```
购物车       → ShoppingCartService
库存管理     → CounterService + LockService
商品详情缓存 → StringCacheService
订单防重     → LockService
销量排行     → LeaderboardService
秒杀活动     → CounterService + LockService
```

### 内容相关

```
文章缓存     → StringCacheService
浏览量统计   → CounterService
评论列表     → LatestMessagesService
热度排行     → LeaderboardService
标签管理     → DeduplicationService
推荐去重     → DeduplicationService
```

### 系统相关

```
API限流      → CounterService
分布式锁     → LockService
消息队列     → MessageQueueService
延迟任务     → DelayQueueService
事件日志     → MessageStreamService
健康检查     → OnlineStatusService
```

---

## 📖 快速开始

### 1. 初始化应用

```go
package main

import (
    "gcode/redis/app"
    "gcode/redis/config"
    "gcode/redis/pkg/logger"
)

func main() {
    // 创建配置
    cfg := config.NewConfig(config.EnvProduction)
    
    // 创建应用
    application, err := app.NewApplication(cfg)
    if err != nil {
        panic(err)
    }
    
    // 获取服务实例
    cacheService := getStringCacheService(application)
    counterService := getCounterService(application)
    lockService := application.GetLockService()
    
    // 使用服务...
}
```

### 2. 服务使用模板

```go
// 缓存服务使用
func useCacheService(ctx context.Context, cache StringCacheService) {
    // 设置缓存
    cache.Set(ctx, "key", value, 10*time.Minute)
    
    // 获取缓存
    var result Type
    cache.Get(ctx, "key", &result)
    
    // 缓存穿透保护
    cache.GetOrSet(ctx, "key", &result, 10*time.Minute, func() (interface{}, error) {
        return loadFromDB()
    })
}

// 计数器服务使用
func useCounterService(ctx context.Context, counter CounterService) {
    // 自增
    count, _ := counter.Increment(ctx, "page:views")
    
    // 限流
    count, _ = counter.IncrementWithExpire(ctx, "api:calls", 1*time.Minute)
    if count > 100 {
        return errors.New("超过限流")
    }
}

// 分布式锁使用
func useLockService(ctx context.Context, lock LockService) {
    // 方式1: 手动管理
    l, _ := lock.Lock(ctx, "resource", 30*time.Second)
    defer l.Release(ctx)
    // 临界区代码
    
    // 方式2: 自动管理
    lock.WithLock(ctx, "resource", 30*time.Second, func(ctx context.Context) error {
        // 临界区代码
        return nil
    })
}
```

---

## 🔧 服务创建工厂

```go
package factory

import (
    "gcode/redis/client"
    "gcode/redis/internal/repository"
    "gcode/redis/internal/service"
    "gcode/redis/pkg/logger"
    "gcode/redis/pkg/metrics"
)

// ServiceFactory 服务工厂
type ServiceFactory struct {
    client  client.Client
    logger  logger.Logger
    metrics metrics.Metrics
}

func NewServiceFactory(c client.Client, l logger.Logger, m metrics.Metrics) *ServiceFactory {
    return &ServiceFactory{
        client:  c,
        logger:  l,
        metrics: m,
    }
}

// String 服务
func (f *ServiceFactory) NewStringCacheService(prefix string) service.StringCacheService {
    repo := repository.NewCacheRepository(f.client, f.logger, f.metrics)
    return service.NewStringCacheService(repo, f.logger, prefix)
}

func (f *ServiceFactory) NewCounterService(prefix string) service.CounterService {
    return service.NewCounterService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewLockService(prefix string) service.LockService {
    repo := repository.NewLockRepository(f.client, f.logger, f.metrics)
    return service.NewLockService(repo, f.logger, prefix)
}

func (f *ServiceFactory) NewSessionService(prefix string) service.SessionService {
    repo := repository.NewCacheRepository(f.client, f.logger, f.metrics)
    return service.NewSessionService(repo, f.logger, prefix)
}

// Hash 服务
func (f *ServiceFactory) NewUserInfoService(prefix string) service.UserInfoService {
    return service.NewUserInfoService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewShoppingCartService(prefix string) service.ShoppingCartService {
    return service.NewShoppingCartService(f.client, f.logger, f.metrics, prefix)
}

// List 服务
func (f *ServiceFactory) NewMessageQueueService(prefix string) service.MessageQueueService {
    return service.NewMessageQueueService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewLatestMessagesService(prefix string) service.LatestMessagesService {
    return service.NewLatestMessagesService(f.client, f.logger, f.metrics, prefix)
}

// Set 服务
func (f *ServiceFactory) NewDeduplicationService(prefix string) service.DeduplicationService {
    return service.NewDeduplicationService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewLotteryService(prefix string) service.LotteryService {
    return service.NewLotteryService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewSocialGraphService(prefix string) service.SocialGraphService {
    return service.NewSocialGraphService(f.client, f.logger, f.metrics, prefix)
}

// ZSet 服务
func (f *ServiceFactory) NewLeaderboardService(prefix string) service.LeaderboardService {
    return service.NewLeaderboardService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewPriorityQueueService(prefix string) service.PriorityQueueService {
    return service.NewPriorityQueueService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewDelayQueueService(prefix string) service.DelayQueueService {
    return service.NewDelayQueueService(f.client, f.logger, f.metrics, prefix)
}

// Bitmap 服务
func (f *ServiceFactory) NewSignInService(prefix string) service.SignInService {
    return service.NewSignInService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewOnlineStatusService(prefix string) service.OnlineStatusService {
    return service.NewOnlineStatusService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewUserActivityService(prefix string) service.UserActivityService {
    return service.NewUserActivityService(f.client, f.logger, f.metrics, prefix)
}

// Stream 服务
func (f *ServiceFactory) NewMessageStreamService(prefix string) service.MessageStreamService {
    return service.NewMessageStreamService(f.client, f.logger, f.metrics, prefix)
}

func (f *ServiceFactory) NewConsumerGroupService(prefix string) service.ConsumerGroupService {
    return service.NewConsumerGroupService(f.client, f.logger, f.metrics, prefix)
}
```

---

## 📊 性能对比表

| 数据结构 | 读操作 QPS | 写操作 QPS | 内存效率 | 适用数据量 |
|---------|-----------|-----------|---------|-----------|
| String | 100,000+ | 100,000+ | 中 | < 512MB |
| Hash | 80,000+ | 80,000+ | 高 | < 1GB |
| List | 50,000+ | 50,000+ | 中 | < 100万条 |
| Set | 60,000+ | 60,000+ | 中 | < 100万个 |
| ZSet | 40,000+ | 40,000+ | 低 | < 100万个 |
| Bitmap | 100,000+ | 100,000+ | 极高 | < 40亿位 |
| Stream | 30,000+ | 30,000+ | 中 | < 1000万条 |

---

## 🎓 最佳实践

### 1. 键命名规范

```
格式: {业务}:{对象类型}:{对象ID}:{属性}:{时间}

示例:
myapp:cache:user:1001                    # 用户缓存
myapp:counter:page:home:views            # 页面浏览量
myapp:lock:order:12345                   # 订单锁
myapp:session:abc123                     # 用户会话
myapp:cart:user:1001                     # 购物车
myapp:queue:email                        # 邮件队列
myapp:leaderboard:game:score             # 游戏排行榜
myapp:signin:user:1001:2024:02          # 签到记录
```

### 2. 过期时间设置

```go
// 热点数据
cache.Set(ctx, "hot:product:5001", product, 1*time.Hour)

// 普通数据
cache.Set(ctx, "product:5001", product, 10*time.Minute)

// 临时数据
cache.Set(ctx, "verify:code:13800138000", code, 5*time.Minute)

// 永久数据（谨慎使用）
cache.Set(ctx, "config:system", config, 0)
```

### 3. 错误处理

```go
value, err := cache.Get(ctx, "key", &result)
if err != nil {
    if rediserr.IsNotFound(err) {
        // 缓存未命中，从数据库加载
        result = loadFromDB()
    } else if rediserr.IsTimeout(err) {
        // 超时，使用降级策略
        result = getDefaultValue()
    } else {
        // 其他错误，记录日志
        logger.Error("Redis错误", logger.Error(err))
        return err
    }
}
```

### 4. 并发控制

```go
// 使用分布式锁保护临界区
err := lockService.WithLock(ctx, "resource", 30*time.Second, func(ctx context.Context) error {
    // 读取
    value := getValue()
    
    // 修改
    value = modify(value)
    
    // 写入
    return setValue(value)
})
```

---

## 📝 完整使用示例

查看 `docs/SERVICES_CN.md` 获取每个服务的详细文档和完整示例。

---

## 🔗 相关文档

- [企业级架构说明](../README_ENTERPRISE.md)
- [详细服务文档](./SERVICES_CN.md)
- [API 参考](./API_REFERENCE_CN.md)
- [性能优化指南](./PERFORMANCE_CN.md)
- [故障排查手册](./TROUBLESHOOTING_CN.md)
