# Redis 生产级服务完整文档

本文档详细介绍 21 个生产级 Redis 服务模块的使用方法、适用场景和最佳实践。

## 📋 目录

- [String 数据结构服务](#string-数据结构服务)
  - [1. 字符串缓存服务 (StringCacheService)](#1-字符串缓存服务)
  - [2. 计数器服务 (CounterService)](#2-计数器服务)
  - [3. 分布式锁服务 (LockService)](#3-分布式锁服务)
  - [4. Session 服务 (SessionService)](#4-session-服务)
- [Hash 数据结构服务](#hash-数据结构服务)
  - [5. 用户信息服务 (UserInfoService)](#5-用户信息服务)
  - [6. 购物车服务 (ShoppingCartService)](#6-购物车服务)
- [List 数据结构服务](#list-数据结构服务)
  - [7. 消息队列服务 (MessageQueueService)](#7-消息队列服务)
  - [8. 最新消息服务 (LatestMessagesService)](#8-最新消息服务)
- [Set 数据结构服务](#set-数据结构服务)
  - [9. 去重服务 (DeduplicationService)](#9-去重服务)
  - [10. 抽奖服务 (LotteryService)](#10-抽奖服务)
  - [11. 共同好友服务 (CommonFriendsService)](#11-共同好友服务)
- [ZSet 数据结构服务](#zset-数据结构服务)
  - [12. 排行榜服务 (LeaderboardService)](#12-排行榜服务)
  - [13. 优先级队列服务 (PriorityQueueService)](#13-优先级队列服务)
  - [14. 延迟队列服务 (DelayQueueService)](#14-延迟队列服务)
- [Bitmap 数据结构服务](#bitmap-数据结构服务)
  - [15. 签到服务 (SignInService)](#15-签到服务)
  - [16. 在线状态服务 (OnlineStatusService)](#16-在线状态服务)
  - [17. 用户活动服务 (UserActivityService)](#17-用户活动服务)
- [Stream 数据结构服务](#stream-数据结构服务)
  - [18. 消息流服务 (MessageStreamService)](#18-消息流服务)
  - [19. 消费者组服务 (ConsumerGroupService)](#19-消费者组服务)

---

## String 数据结构服务

### 1. 字符串缓存服务

**服务名称**: `StringCacheService`  
**文件位置**: `internal/service/string_cache_service.go`

#### 适用场景

- **对象缓存**: 用户信息、商品详情、配置数据
- **页面缓存**: 静态页面、API 响应
- **临时数据**: 验证码、临时令牌
- **缓存穿透保护**: 使用 `GetOrSet` 方法

#### 核心方法

##### Set - 设置缓存

```go
// 设置用户信息缓存，10分钟过期
type User struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Age  int    `json:"age"`
}

user := User{ID: "1001", Name: "张三", Age: 28}
err := cacheService.Set(ctx, "user:1001", user, 10*time.Minute)
```

**参数说明**:
- `key`: 缓存键，会自动添加前缀
- `value`: 任意类型，自动 JSON 序列化
- `ttl`: 过期时间，0 表示永不过期

**最佳实践**:
- 使用有意义的键名，如 `user:1001`、`product:5001`
- 根据数据更新频率设置合理的 TTL
- 热点数据使用较长的 TTL，冷数据使用较短的 TTL

##### Get - 获取缓存

```go
var user User
err := cacheService.Get(ctx, "user:1001", &user)
if err != nil {
    if rediserr.IsNotFound(err) {
        // 缓存未命中，从数据库加载
    }
}
```

**错误处理**:
- `ErrCodeNotFound`: 键不存在
- `ErrCodeSerialization`: 反序列化失败
- `ErrCodeInternal`: Redis 内部错误

##### GetOrSet - 缓存穿透保护

```go
var user User
err := cacheService.GetOrSet(ctx, "user:1001", &user, 10*time.Minute, func() (interface{}, error) {
    // 缓存未命中时，从数据库加载
    return userRepo.FindByID("1001")
})
```

**使用场景**:
- 防止缓存穿透
- 自动缓存加载结果
- 减少重复代码

##### MGet/MSet - 批量操作

```go
// 批量设置
pairs := map[string]interface{}{
    "user:1001": user1,
    "user:1002": user2,
    "user:1003": user3,
}
err := cacheService.MSet(ctx, pairs, 10*time.Minute)

// 批量获取
values, err := cacheService.MGet(ctx, "user:1001", "user:1002", "user:1003")
```

**性能优化**:
- 批量操作减少网络往返
- 适合需要同时获取多个缓存的场景

#### 完整示例

```go
package main

import (
    "context"
    "time"
    
    "gcode/redis/internal/service"
)

func main() {
    // 创建服务
    cacheService := service.NewStringCacheService(repo, logger, "myapp:cache")
    
    ctx := context.Background()
    
    // 1. 基本缓存操作
    type Product struct {
        ID    string  `json:"id"`
        Name  string  `json:"name"`
        Price float64 `json:"price"`
    }
    
    product := Product{ID: "5001", Name: "iPhone 15", Price: 5999.00}
    
    // 设置缓存
    err := cacheService.Set(ctx, "product:5001", product, 1*time.Hour)
    
    // 获取缓存
    var cached Product
    err = cacheService.Get(ctx, "product:5001", &cached)
    
    // 2. 缓存穿透保护
    err = cacheService.GetOrSet(ctx, "product:5002", &cached, 1*time.Hour, func() (interface{}, error) {
        // 从数据库加载
        return db.GetProduct("5002")
    })
    
    // 3. 检查缓存是否存在
    count, _ := cacheService.Exists(ctx, "product:5001", "product:5002")
    fmt.Printf("存在 %d 个缓存\n", count)
    
    // 4. 刷新过期时间
    err = cacheService.Refresh(ctx, "product:5001", 2*time.Hour)
    
    // 5. 删除缓存
    err = cacheService.Delete(ctx, "product:5001")
}
```

#### 性能指标

| 操作 | 时间复杂度 | 推荐场景 |
|------|-----------|---------|
| Set | O(1) | 所有缓存场景 |
| Get | O(1) | 所有读取场景 |
| MGet | O(N) | 批量读取 |
| MSet | O(N) | 批量写入 |
| Delete | O(N) | 缓存失效 |

#### 注意事项

1. **序列化开销**: 大对象序列化会影响性能，考虑压缩或分片
2. **内存管理**: 设置合理的 TTL，避免内存溢出
3. **缓存雪崩**: 避免大量缓存同时过期，使用随机 TTL
4. **缓存击穿**: 热点数据使用 `SetWithNX` 防止并发击穿

---

### 2. 计数器服务

**服务名称**: `CounterService`  
**文件位置**: `internal/service/string_counter_service.go`

#### 适用场景

- **统计计数**: 页面浏览量、文章阅读数、视频播放量
- **限流控制**: API 调用次数限制、用户操作频率限制
- **库存管理**: 商品库存扣减、秒杀活动
- **点赞收藏**: 文章点赞数、用户收藏数
- **分布式 ID**: 生成全局唯一 ID

#### 核心方法

##### Increment - 自增计数

```go
// 页面浏览量 +1
count, err := counterService.Increment(ctx, "page:home:views")
fmt.Printf("当前浏览量: %d\n", count)
```

**特点**:
- 原子操作，线程安全
- 返回自增后的值
- 如果键不存在，从 0 开始

##### IncrementBy - 指定增量

```go
// 批量增加浏览量
count, err := counterService.IncrementBy(ctx, "page:home:views", 10)

// 减少库存（使用负数）
stock, err := counterService.IncrementBy(ctx, "product:5001:stock", -1)
if stock < 0 {
    // 库存不足，回滚
    counterService.IncrementBy(ctx, "product:5001:stock", 1)
}
```

##### IncrementWithExpire - 时间窗口计数

```go
// 每日 API 调用次数统计
today := time.Now().Format("2006-01-02")
key := fmt.Sprintf("api:user:1001:calls:%s", today)

count, err := counterService.IncrementWithExpire(ctx, key, 24*time.Hour)
if count > 1000 {
    // 超过每日限额
    return errors.New("API 调用次数超限")
}
```

**使用场景**:
- 每日/每小时统计
- 限流控制
- 时间窗口内的计数

##### GetAndReset - 原子获取并重置

```go
// 获取今日订单数并重置
orderCount, err := counterService.GetAndReset(ctx, "orders:today")
fmt.Printf("今日订单数: %d\n", orderCount)
// 计数器已重置为 0
```

**使用场景**:
- 周期性统计报表
- 定时任务数据收集

##### IncrementFloat - 浮点数计数

```go
// 累计金额统计
amount, err := counterService.IncrementFloat(ctx, "revenue:today", 99.99)
fmt.Printf("今日收入: %.2f\n", amount)
```

#### 完整示例 - 限流器

```go
package main

import (
    "context"
    "fmt"
    "time"
)

// RateLimiter 基于计数器的限流器
type RateLimiter struct {
    counter counterService
    limit   int64
    window  time.Duration
}

func NewRateLimiter(counter counterService, limit int64, window time.Duration) *RateLimiter {
    return &RateLimiter{
        counter: counter,
        limit:   limit,
        window:  window,
    }
}

// Allow 检查是否允许请求
func (r *RateLimiter) Allow(ctx context.Context, userID string) (bool, error) {
    key := fmt.Sprintf("ratelimit:user:%s", userID)
    
    // 自增并设置过期时间
    count, err := r.counter.IncrementWithExpire(ctx, key, r.window)
    if err != nil {
        return false, err
    }
    
    if count > r.limit {
        return false, fmt.Errorf("超过限流阈值: %d/%d", count, r.limit)
    }
    
    return true, nil
}

func main() {
    // 创建限流器：每分钟最多 100 次请求
    limiter := NewRateLimiter(counterService, 100, 1*time.Minute)
    
    ctx := context.Background()
    
    // 检查用户请求
    allowed, err := limiter.Allow(ctx, "user:1001")
    if !allowed {
        fmt.Println("请求被限流")
        return
    }
    
    // 处理请求
    handleRequest()
}
```

#### 完整示例 - 库存扣减

```go
// DeductStock 扣减库存（带并发控制）
func DeductStock(ctx context.Context, productID string, quantity int64) error {
    key := fmt.Sprintf("product:%s:stock", productID)
    
    // 使用 Lua 脚本保证原子性
    script := `
        local stock = redis.call('GET', KEYS[1])
        if not stock then
            return -1  -- 商品不存在
        end
        
        stock = tonumber(stock)
        local quantity = tonumber(ARGV[1])
        
        if stock < quantity then
            return -2  -- 库存不足
        end
        
        redis.call('DECRBY', KEYS[1], quantity)
        return stock - quantity
    `
    
    result, err := client.Eval(ctx, script, []string{key}, quantity).Result()
    if err != nil {
        return err
    }
    
    remaining := result.(int64)
    if remaining == -1 {
        return errors.New("商品不存在")
    }
    if remaining == -2 {
        return errors.New("库存不足")
    }
    
    logger.Info("库存扣减成功",
        logger.String("product_id", productID),
        logger.Int64("quantity", quantity),
        logger.Int64("remaining", remaining))
    
    return nil
}
```

#### 性能指标

| 操作 | 时间复杂度 | QPS (单机) |
|------|-----------|-----------|
| Increment | O(1) | 100,000+ |
| IncrementBy | O(1) | 100,000+ |
| Get | O(1) | 100,000+ |
| GetMultiple | O(N) | 50,000+ |

#### 最佳实践

1. **键命名规范**
```go
// 推荐格式: 类型:对象:属性:时间
"counter:page:home:views"           // 页面浏览量
"counter:api:user:1001:calls:2024-02-05"  // 每日 API 调用
"counter:product:5001:stock"        // 商品库存
```

2. **时间窗口统计**
```go
// 使用日期作为键的一部分
today := time.Now().Format("2006-01-02")
key := fmt.Sprintf("counter:orders:%s", today)
count, _ := counterService.IncrementWithExpire(ctx, key, 24*time.Hour)
```

3. **防止超卖**
```go
// 使用 Lua 脚本保证原子性
// 或使用分布式锁 + 计数器组合
```

4. **批量统计**
```go
// 使用 GetMultiple 批量获取多个计数器
keys := []string{"page:home:views", "page:about:views", "page:contact:views"}
counts, _ := counterService.GetMultiple(ctx, keys...)
```

---

### 3. 分布式锁服务

**服务名称**: `LockService`  
**文件位置**: `internal/service/lock_service.go`

#### 适用场景

- **防止重复提交**: 订单创建、支付请求
- **资源互斥访问**: 库存扣减、账户余额修改
- **定时任务**: 防止多实例重复执行
- **分布式事务**: 协调多个服务的操作
- **缓存更新**: 防止缓存击穿

#### 核心方法

##### Lock - 获取锁

```go
// 获取订单锁，30秒超时
lock, err := lockService.Lock(ctx, "order:12345", 30*time.Second)
if err != nil {
    // 锁已被其他进程持有
    return err
}
defer lock.Release(ctx)

// 执行临界区代码
processOrder("12345")
```

**特点**:
- 基于 SetNX 实现
- 自动生成唯一 token
- 防止误删其他进程的锁

##### TryLock - 重试获取锁

```go
// 尝试获取锁，最多重试 3 次，每次间隔 100ms
lock, err := lockService.TryLock(ctx, "inventory:product:5001", 30*time.Second, 3, 100*time.Millisecond)
if err != nil {
    if err.(*rediserr.RedisError).Code == rediserr.ErrCodeRetryExhausted {
        return errors.New("系统繁忙，请稍后重试")
    }
    return err
}
defer lock.Release(ctx)
```

**使用场景**:
- 高并发场景
- 允许短暂等待
- 提高锁获取成功率

##### WithLock - 自动管理锁

```go
// 自动获取和释放锁
err := lockService.WithLock(ctx, "user:1001:balance", 10*time.Second, func(ctx context.Context) error {
    // 临界区代码
    balance, err := getBalance("1001")
    if err != nil {
        return err
    }
    
    newBalance := balance - 100
    if newBalance < 0 {
        return errors.New("余额不足")
    }
    
    return updateBalance("1001", newBalance)
})
```

**优势**:
- 自动释放锁
- 异常安全
- 代码简洁

##### Extend - 延长锁时间

```go
lock, err := lockService.Lock(ctx, "long:task", 30*time.Second)
if err != nil {
    return err
}
defer lock.Release(ctx)

// 执行长时间任务
for i := 0; i < 10; i++ {
    processChunk(i)
    
    // 每次处理后延长锁时间
    if err := lock.Extend(ctx, 30*time.Second); err != nil {
        logger.Warn("延长锁失败", logger.Error(err))
    }
}
```

#### 完整示例 - 防止重复下单

```go
package main

import (
    "context"
    "fmt"
    "time"
)

// CreateOrder 创建订单（防重复提交）
func CreateOrder(ctx context.Context, userID string, productID string) error {
    // 使用用户ID作为锁的资源标识
    lockKey := fmt.Sprintf("create_order:%s", userID)
    
    // 尝试获取锁，最多等待 3 秒
    lock, err := lockService.TryLock(ctx, lockKey, 10*time.Second, 30, 100*time.Millisecond)
    if err != nil {
        return fmt.Errorf("请勿重复提交订单")
    }
    defer lock.Release(ctx)
    
    // 检查是否已有未支付订单
    existingOrder, err := orderRepo.FindUnpaidOrder(userID, productID)
    if existingOrder != nil {
        return fmt.Errorf("您有未支付的订单，请先完成支付")
    }
    
    // 创建订单
    order := &Order{
        UserID:    userID,
        ProductID: productID,
        Status:    "PENDING",
        CreatedAt: time.Now(),
    }
    
    if err := orderRepo.Create(order); err != nil {
        return err
    }
    
    logger.Info("订单创建成功",
        logger.String("user_id", userID),
        logger.String("order_id", order.ID))
    
    return nil
}
```

#### 完整示例 - 库存扣减

```go
// DeductInventory 扣减库存（分布式锁保护）
func DeductInventory(ctx context.Context, productID string, quantity int) error {
    lockKey := fmt.Sprintf("inventory:%s", productID)
    
    return lockService.WithLock(ctx, lockKey, 5*time.Second, func(ctx context.Context) error {
        // 获取当前库存
        stock, err := inventoryRepo.GetStock(productID)
        if err != nil {
            return err
        }
        
        // 检查库存是否充足
        if stock < quantity {
            return fmt.Errorf("库存不足: 需要 %d，剩余 %d", quantity, stock)
        }
        
        // 扣减库存
        newStock := stock - quantity
        if err := inventoryRepo.UpdateStock(productID, newStock); err != nil {
            return err
        }
        
        logger.Info("库存扣减成功",
            logger.String("product_id", productID),
            logger.Int("quantity", quantity),
            logger.Int("remaining", newStock))
        
        return nil
    })
}
```

#### 完整示例 - 定时任务防重复

```go
// RunScheduledTask 运行定时任务（防止多实例重复执行）
func RunScheduledTask(ctx context.Context, taskName string) error {
    lockKey := fmt.Sprintf("scheduled_task:%s", taskName)
    
    // 尝试获取锁，不重试
    lock, err := lockService.Lock(ctx, lockKey, 5*time.Minute)
    if err != nil {
        // 其他实例正在执行，跳过
        logger.Info("任务已被其他实例执行，跳过",
            logger.String("task", taskName))
        return nil
    }
    defer lock.Release(ctx)
    
    logger.Info("开始执行定时任务", logger.String("task", taskName))
    
    // 执行任务
    if err := executeTask(taskName); err != nil {
        logger.Error("任务执行失败",
            logger.String("task", taskName),
            logger.Error(err))
        return err
    }
    
    logger.Info("任务执行成功", logger.String("task", taskName))
    return nil
}
```

#### 锁的实现原理

```go
// 获取锁（SetNX + 唯一 token）
SET lock:resource "unique_token" NX EX 30

// 释放锁（Lua 脚本保证原子性）
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end

// 延长锁（Lua 脚本）
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("pexpire", KEYS[1], ARGV[2])
else
    return 0
end
```

#### 性能指标

| 操作 | 时间复杂度 | 平均耗时 |
|------|-----------|---------|
| Lock | O(1) | < 1ms |
| Release | O(1) | < 1ms |
| Extend | O(1) | < 1ms |
| TryLock | O(N) | N * 100ms |

#### 最佳实践

1. **合理设置锁超时时间**
```go
// 根据业务执行时间设置
// 短任务: 5-10 秒
// 长任务: 30-60 秒
// 避免设置过长，防止死锁
```

2. **始终释放锁**
```go
// 使用 defer 确保锁被释放
lock, err := lockService.Lock(ctx, "resource", 30*time.Second)
if err != nil {
    return err
}
defer lock.Release(ctx)
```

3. **处理锁获取失败**
```go
lock, err := lockService.Lock(ctx, "resource", 30*time.Second)
if err != nil {
    if err.(*rediserr.RedisError).Code == rediserr.ErrCodeLockFailed {
        // 锁被占用，返回友好提示
        return errors.New("系统繁忙，请稍后重试")
    }
    return err
}
```

4. **避免死锁**
```go
// 设置合理的超时时间
// 使用 context 超时控制
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

lock, err := lockService.Lock(ctx, "resource", 30*time.Second)
```

5. **锁粒度控制**
```go
// 细粒度锁：锁定具体资源
lockKey := fmt.Sprintf("order:%s", orderID)

// 粗粒度锁：锁定用户所有操作
lockKey := fmt.Sprintf("user:%s", userID)

// 根据业务场景选择合适的粒度
```

---

## 总结

本文档提供了前 3 个 String 数据结构服务的详细说明。每个服务都包含：

- ✅ **适用场景**: 明确的业务场景
- ✅ **核心方法**: 详细的 API 说明
- ✅ **完整示例**: 可直接使用的代码
- ✅ **性能指标**: 时间复杂度和 QPS
- ✅ **最佳实践**: 生产环境经验总结

由于文档篇幅较长，剩余 18 个服务的文档将在后续部分继续提供。

**下一部分将包含**:
- Session 服务
- Hash 数据结构服务（用户信息、购物车）
- List 数据结构服务（消息队列、最新消息）
- 其他服务...
