# Redis 生产级服务快速上手指南

## 🚀 5分钟快速开始

### 1. 环境准备

```bash
# 克隆项目
cd /Users/mac/workspace/code/gcode/redis

# 安装依赖
go mod tidy

# 启动 Redis（使用 Docker）
make docker-up

# 验证 Redis 连接
redis-cli ping
```

### 2. 创建第一个应用

```go
package main

import (
    "context"
    "fmt"
    "time"

    "gcode/redis/app"
    "gcode/redis/config"
    "gcode/redis/pkg/logger"
)

func main() {
    // 1. 创建配置
    cfg := config.NewConfig(config.EnvDevelopment)
    
    // 2. 创建应用
    log := logger.NewLogger(logger.INFO)
    application, err := app.NewApplication(cfg, app.WithLogger(log))
    if err != nil {
        panic(err)
    }
    
    // 3. 使用缓存服务
    ctx := context.Background()
    cacheService := application.GetCacheService()
    
    // 设置缓存
    err = cacheService.Set(ctx, "hello", "world", 10*time.Minute)
    if err != nil {
        log.Error("设置缓存失败", logger.Error(err))
        return
    }
    
    // 获取缓存
    var value string
    err = cacheService.Get(ctx, "hello", &value)
    if err != nil {
        log.Error("获取缓存失败", logger.Error(err))
        return
    }
    
    fmt.Printf("缓存值: %s\n", value)
}
```

### 3. 运行应用

```bash
go run main.go
```

---

## 📚 核心服务使用示例

### String 服务 - 缓存

```go
import "gcode/redis/internal/service"

// 创建缓存服务
factory := NewServiceFactory(client, logger, metrics)
cache := factory.NewStringCacheService("myapp:cache")

// 1. 基本操作
type User struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

user := User{ID: "1001", Name: "张三"}
cache.Set(ctx, "user:1001", user, 10*time.Minute)

var cachedUser User
cache.Get(ctx, "user:1001", &cachedUser)

// 2. 缓存穿透保护
cache.GetOrSet(ctx, "user:1001", &cachedUser, 10*time.Minute, func() (interface{}, error) {
    return db.GetUser("1001")
})

// 3. 批量操作
pairs := map[string]interface{}{
    "user:1001": user1,
    "user:1002": user2,
}
cache.MSet(ctx, pairs, 10*time.Minute)
```

### String 服务 - 计数器

```go
counter := factory.NewCounterService("myapp:counter")

// 1. 浏览量统计
count, _ := counter.Increment(ctx, "page:home:views")
fmt.Printf("浏览量: %d\n", count)

// 2. API 限流
count, _ = counter.IncrementWithExpire(ctx, "api:user:1001:calls", 1*time.Minute)
if count > 100 {
    return errors.New("超过限流")
}

// 3. 库存扣减
stock, _ := counter.IncrementBy(ctx, "product:5001:stock", -1)
if stock < 0 {
    counter.IncrementBy(ctx, "product:5001:stock", 1) // 回滚
    return errors.New("库存不足")
}
```

### String 服务 - 分布式锁

```go
lockService := application.GetLockService()

// 1. 基本用法
lock, _ := lockService.Lock(ctx, "order:12345", 30*time.Second)
defer lock.Release(ctx)
// 临界区代码

// 2. 自动管理
lockService.WithLock(ctx, "resource", 30*time.Second, func(ctx context.Context) error {
    // 临界区代码
    return processOrder()
})

// 3. 重试获取
lock, _ = lockService.TryLock(ctx, "resource", 30*time.Second, 3, 100*time.Millisecond)
```

### Hash 服务 - 用户信息

```go
userService := factory.NewUserInfoService("myapp:user")

// 1. 保存用户信息
user := &UserInfo{
    ID:      "1001",
    Name:    "张三",
    Email:   "zhangsan@example.com",
    Balance: 1000.00,
}
userService.Save(ctx, user)

// 2. 获取用户信息
user, _ := userService.Get(ctx, "1001")

// 3. 更新单个字段
userService.UpdateField(ctx, "1001", "name", "李四")

// 4. 余额操作
newBalance, _ := userService.IncrBalance(ctx, "1001", 100.00)
```

### Hash 服务 - 购物车

```go
cartService := factory.NewShoppingCartService("myapp:cart")

// 1. 添加商品
cartService.AddItem(ctx, "user:1001", "product:5001", 2)

// 2. 更新数量
cartService.UpdateQuantity(ctx, "user:1001", "product:5001", 5)

// 3. 获取购物车
items, _ := cartService.GetAll(ctx, "user:1001")
for productID, quantity := range items {
    fmt.Printf("%s: %d件\n", productID, quantity)
}

// 4. 清空购物车
cartService.Clear(ctx, "user:1001")
```

### List 服务 - 消息队列

```go
queueService := factory.NewMessageQueueService("myapp:queue")

// 1. 推送消息
msg := &Message{
    ID:      "msg001",
    Type:    "email",
    Content: "欢迎注册",
}
queueService.Push(ctx, "email", msg)

// 2. 消费消息
msg, _ := queueService.Pop(ctx, "email")

// 3. 阻塞消费
msg, _ = queueService.BlockingPop(ctx, "email", 5*time.Second)

// 4. 批量推送
messages := []*Message{msg1, msg2, msg3}
queueService.PushBatch(ctx, "email", messages)
```

### Set 服务 - 去重

```go
dedupService := factory.NewDeduplicationService("myapp:dedup")

// 1. 添加元素
isNew, _ := dedupService.Add(ctx, "visitors", "user:1001")
if isNew {
    fmt.Println("新访客")
}

// 2. 检查是否存在
exists, _ := dedupService.IsMember(ctx, "visitors", "user:1001")

// 3. 获取所有元素
visitors, _ := dedupService.GetAll(ctx, "visitors")

// 4. 统计数量
count, _ := dedupService.Count(ctx, "visitors")
```

### ZSet 服务 - 排行榜

```go
leaderboard := factory.NewLeaderboardService("myapp:leaderboard")

// 1. 添加分数
leaderboard.AddScore(ctx, "game", "player001", 1500)

// 2. 增加分数
newScore, _ := leaderboard.IncrScore(ctx, "game", "player001", 100)

// 3. 获取排名
rank, _ := leaderboard.GetRank(ctx, "game", "player001")

// 4. 获取 Top N
topPlayers, _ := leaderboard.GetTopN(ctx, "game", 10)
for _, player := range topPlayers {
    fmt.Printf("第%d名: %s - %.0f分\n", player.Rank, player.ID, player.Score)
}
```

### Bitmap 服务 - 签到

```go
signinService := factory.NewSignInService("myapp:signin")

// 1. 签到
signinService.SignIn(ctx, "user:1001", time.Now())

// 2. 检查是否签到
isSigned, _ := signinService.CheckSignIn(ctx, "user:1001", time.Now())

// 3. 获取月度签到天数
count, _ := signinService.GetMonthSignInCount(ctx, "user:1001", 2024, 2)

// 4. 获取连续签到天数
continuous, _ := signinService.GetContinuousSignInDays(ctx, "user:1001")
```

### Stream 服务 - 消息流

```go
streamService := factory.NewMessageStreamService("myapp:stream")

// 1. 添加消息
id, _ := streamService.Add(ctx, "events", map[string]interface{}{
    "event":   "user_login",
    "user_id": "1001",
    "time":    time.Now().Unix(),
})

// 2. 读取消息
messages, _ := streamService.Read(ctx, "events", "-", 10)

// 3. 读取最新消息
messages, _ = streamService.ReadNew(ctx, "events", lastID, 5*time.Second)
```

---

## 🎯 常见业务场景

### 场景1: 用户登录

```go
func UserLogin(ctx context.Context, username, password string) error {
    // 1. 验证用户名密码
    user, err := userRepo.FindByUsername(username)
    if err != nil || !verifyPassword(password, user.Password) {
        return errors.New("用户名或密码错误")
    }
    
    // 2. 创建 Session
    sessionID := generateSessionID()
    sessionData := &SessionData{
        UserID:    user.ID,
        Username:  user.Name,
        LoginTime: time.Now(),
    }
    
    sessionService := factory.NewSessionService("myapp:session")
    err = sessionService.Create(ctx, sessionID, sessionData, 24*time.Hour)
    if err != nil {
        return err
    }
    
    // 3. 缓存用户信息
    cacheService := factory.NewStringCacheService("myapp:cache")
    err = cacheService.Set(ctx, fmt.Sprintf("user:%s", user.ID), user, 1*time.Hour)
    
    // 4. 记录登录日志
    streamService := factory.NewMessageStreamService("myapp:stream")
    streamService.Add(ctx, "login_events", map[string]interface{}{
        "user_id":    user.ID,
        "session_id": sessionID,
        "time":       time.Now().Unix(),
    })
    
    return nil
}
```

### 场景2: 商品秒杀

```go
func FlashSale(ctx context.Context, userID, productID string) error {
    // 1. 获取分布式锁（防止超卖）
    lockKey := fmt.Sprintf("flashsale:%s", productID)
    lockService := application.GetLockService()
    
    return lockService.WithLock(ctx, lockKey, 5*time.Second, func(ctx context.Context) error {
        // 2. 检查库存
        counterService := factory.NewCounterService("myapp:counter")
        stockKey := fmt.Sprintf("product:%s:stock", productID)
        
        stock, err := counterService.Get(ctx, stockKey)
        if err != nil || stock <= 0 {
            return errors.New("商品已售罄")
        }
        
        // 3. 扣减库存
        newStock, err := counterService.IncrementBy(ctx, stockKey, -1)
        if err != nil || newStock < 0 {
            // 回滚
            counterService.IncrementBy(ctx, stockKey, 1)
            return errors.New("库存不足")
        }
        
        // 4. 创建订单
        order := createOrder(userID, productID)
        
        // 5. 记录购买记录
        dedupService := factory.NewDeduplicationService("myapp:dedup")
        dedupService.Add(ctx, fmt.Sprintf("flashsale:%s:buyers", productID), userID)
        
        return nil
    })
}
```

### 场景3: API 限流

```go
func RateLimitMiddleware(limit int64, window time.Duration) func(http.Handler) http.Handler {
    counterService := factory.NewCounterService("myapp:ratelimit")
    
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 获取用户标识（IP 或 UserID）
            identifier := getIdentifier(r)
            key := fmt.Sprintf("api:%s", identifier)
            
            // 计数并检查
            count, err := counterService.IncrementWithExpire(r.Context(), key, window)
            if err != nil {
                http.Error(w, "Internal Server Error", 500)
                return
            }
            
            if count > limit {
                http.Error(w, "Too Many Requests", 429)
                return
            }
            
            // 设置响应头
            w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
            w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-count))
            
            next.ServeHTTP(w, r)
        })
    }
}
```

---

## 🔧 配置说明

### 开发环境

```go
cfg := config.NewConfig(config.EnvDevelopment)
// 默认配置：
// - Host: localhost
// - Port: 6379
// - PoolSize: 10
// - Timeout: 3s
```

### 生产环境

```go
cfg := config.NewConfig(config.EnvProduction)
// 生产配置：
// - PoolSize: 50
// - MaxRetries: 5
// - Timeout: 5s
// - 连接池优化
```

### 环境变量

```bash
export APP_ENV=production
export REDIS_HOST=redis.example.com
export REDIS_PORT=6379
export REDIS_PASSWORD=your_password
export REDIS_POOL_SIZE=100
```

---

## 📊 监控和指标

```go
// 获取性能指标
stats := application.GetMetrics()

fmt.Printf("总操作数: %d\n", stats.TotalOperations)
fmt.Printf("成功率: %.2f%%\n", 
    float64(stats.TotalSuccess)/float64(stats.TotalOperations)*100)

// 查看各操作统计
for opName, opStats := range stats.Operations {
    fmt.Printf("%s: 平均耗时=%v, 成功率=%.2f%%\n",
        opName,
        opStats.AvgDuration,
        float64(opStats.SuccessCount)/float64(opStats.Count)*100)
}

// 健康检查
health := application.GetHealthStatus()
fmt.Printf("状态: %s, 延迟: %v\n", health.Status, health.Latency)
```

---

## 🐛 常见问题

### 1. 连接失败

```bash
# 检查 Redis 是否运行
redis-cli ping

# 检查配置
echo $REDIS_HOST
echo $REDIS_PORT
```

### 2. 性能问题

```go
// 使用批量操作
cache.MSet(ctx, pairs, ttl)  // 而不是多次 Set

// 使用 Pipeline
pipe := client.Pipeline()
pipe.Set(ctx, "key1", "value1", 0)
pipe.Set(ctx, "key2", "value2", 0)
pipe.Exec(ctx)
```

### 3. 内存溢出

```go
// 设置合理的 TTL
cache.Set(ctx, "key", value, 10*time.Minute)

// 使用 LRU 策略
// redis.conf: maxmemory-policy allkeys-lru
```

---

## 📖 下一步

- 阅读 [完整服务文档](./SERVICES_CN.md)
- 查看 [服务索引](./SERVICE_INDEX_CN.md)
- 学习 [最佳实践](../README_ENTERPRISE.md)
- 运行 [示例代码](../examples/)

---

## 💡 提示

1. **始终使用 context**: 所有操作都支持超时控制
2. **合理设置 TTL**: 避免内存溢出
3. **使用前缀隔离**: 不同业务使用不同的键前缀
4. **监控指标**: 定期查看性能指标和健康状态
5. **错误处理**: 区分不同类型的错误，采取相应策略
