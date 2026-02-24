# Redis 服务使用示例

本文档提供所有21个Redis服务的完整使用示例和最佳实践。

## 目录

- [快速开始](#快速开始)
- [String 服务](#string-服务)
- [Hash 服务](#hash-服务)
- [List 服务](#list-服务)
- [Set 服务](#set-服务)
- [ZSet 服务](#zset-服务)
- [Bitmap 服务](#bitmap-服务)
- [Stream 服务](#stream-服务)
- [最佳实践](#最佳实践)

## 快速开始

### 初始化服务工厂

```go
package main

import (
    "context"
    "log"
    
    "gcode/redis/app"
    "gcode/redis/config"
    "gcode/redis/pkg/logger"
)

func main() {
    // 1. 创建配置
    cfg := config.NewConfig(config.Development)
    
    // 2. 创建日志器
    log := logger.NewLogger(logger.INFO)
    
    // 3. 创建应用实例
    application, err := app.NewApp(cfg, log)
    if err != nil {
        log.Fatal("Failed to create app", logger.ErrorField(err))
    }
    defer application.Shutdown()
    
    // 4. 获取服务工厂
    factory := application.GetServiceFactory()
    
    // 5. 使用服务
    ctx := context.Background()
    cache := factory.NewStringCacheService("myapp:cache")
    
    // 示例：缓存用户数据
    user := map[string]interface{}{
        "id": "1001",
        "name": "张三",
        "email": "zhangsan@example.com",
    }
    
    if err := cache.Set(ctx, "user:1001", user, 10*time.Minute); err != nil {
        log.Error("Cache set failed", logger.ErrorField(err))
    }
}
```

## String 服务

### 1. StringCacheService - 对象缓存

**适用场景**：用户信息、配置数据、API响应缓存

```go
cache := factory.NewStringCacheService("myapp:cache")

// 基本操作
type User struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

user := User{ID: "1001", Name: "张三", Email: "zhangsan@example.com"}

// 设置缓存（10分钟过期）
err := cache.Set(ctx, "user:1001", user, 10*time.Minute)

// 获取缓存
var cachedUser User
err = cache.Get(ctx, "user:1001", &cachedUser)

// 批量获取
keys := []string{"user:1001", "user:1002", "user:1003"}
results, err := cache.MGet(ctx, keys)

// 获取或设置（缓存穿透保护）
result, err := cache.GetOrSet(ctx, "expensive:data", 5*time.Minute, func() (interface{}, error) {
    // 从数据库加载数据
    return loadFromDatabase()
})

// 检查存在
exists, err := cache.Exists(ctx, "user:1001")

// 删除
err = cache.Delete(ctx, "user:1001")
```

### 2. CounterService - 计数器

**适用场景**：访问统计、点赞数、库存扣减、限流

```go
counter := factory.NewCounterService("myapp:counter")

// 增加计数
newValue, err := counter.Increment(ctx, "page:views")
newValue, err = counter.IncrementBy(ctx, "page:views", 10)

// 减少计数
newValue, err = counter.Decrement(ctx, "inventory:item:1001")
newValue, err = counter.DecrementBy(ctx, "inventory:item:1001", 5)

// 带过期时间的计数（限流场景）
count, err := counter.IncrementWithExpire(ctx, "rate:limit:user:1001", 1*time.Minute)
if count > 100 {
    // 超过限流阈值
    return errors.New("rate limit exceeded")
}

// 获取并重置（统计场景）
total, err := counter.GetAndReset(ctx, "daily:orders")

// 浮点数计数
price, err := counter.IncrementFloat(ctx, "total:revenue", 99.99)

// 批量获取
keys := []string{"counter:1", "counter:2", "counter:3"}
values, err := counter.GetMultiple(ctx, keys)
```

### 3. LockService - 分布式锁

**适用场景**：防止重复提交、库存扣减、定时任务互斥

```go
lockSvc := factory.NewLockService("myapp:lock")

// 基本加锁（自动重试）
lock, err := lockSvc.Lock(ctx, "order:create:user:1001", 30*time.Second)
if err != nil {
    return err
}
defer lockSvc.Unlock(ctx, lock)

// 执行业务逻辑
err = createOrder(ctx, userID)

// 尝试加锁（不重试）
lock, err := lockSvc.TryLock(ctx, "inventory:deduct:item:1001", 10*time.Second)
if err != nil {
    return errors.New("resource is locked")
}

// 使用 WithLock 模式（推荐）
err = lockSvc.WithLock(ctx, "payment:process:order:1001", 30*time.Second, func() error {
    // 业务逻辑自动在锁保护下执行
    return processPayment(orderID)
})

// 延长锁时间
success, err := lockSvc.Extend(ctx, lock, 30*time.Second)
```

### 4. SessionService - 会话管理

**适用场景**：用户登录状态、临时数据存储

```go
session := factory.NewSessionService("myapp:session")

// 创建会话
sessionData := map[string]interface{}{
    "user_id": "1001",
    "username": "zhangsan",
    "role": "admin",
    "login_time": time.Now().Unix(),
}
err := session.Create(ctx, "session:token:abc123", sessionData, 2*time.Hour)

// 获取会话
var data map[string]interface{}
err = session.Get(ctx, "session:token:abc123", &data)

// 更新会话
updates := map[string]interface{}{
    "last_activity": time.Now().Unix(),
}
err = session.Update(ctx, "session:token:abc123", updates)

// 刷新过期时间
err = session.Refresh(ctx, "session:token:abc123", 2*time.Hour)

// 删除会话（登出）
err = session.Delete(ctx, "session:token:abc123")

// 批量删除（踢出用户所有设备）
keys := []string{"session:token:abc123", "session:token:def456"}
err = session.DeleteBatch(ctx, keys)
```

## Hash 服务

### 5. UserInfoService - 用户信息

**适用场景**：用户资料、用户设置、用户状态

```go
userSvc := factory.NewUserInfoService("myapp:user")

// 保存用户信息
userInfo := map[string]interface{}{
    "name": "张三",
    "email": "zhangsan@example.com",
    "age": 28,
    "city": "北京",
    "balance": 1000.00,
    "points": 500,
}
err := userSvc.Save(ctx, "user:1001", userInfo)

// 获取完整信息
info, err := userSvc.Get(ctx, "user:1001")

// 获取特定字段
fields := []string{"name", "email", "balance"}
data, err := userSvc.GetFields(ctx, "user:1001", fields)

// 更新单个字段
err = userSvc.UpdateField(ctx, "user:1001", "city", "上海")

// 批量更新
updates := map[string]interface{}{
    "phone": "13800138000",
    "verified": true,
}
err = userSvc.UpdateFields(ctx, "user:1001", updates)

// 原子增加余额
newBalance, err := userSvc.IncrementBalance(ctx, "user:1001", 100.50)

// 原子增加积分
newPoints, err := userSvc.IncrementPoints(ctx, "user:1001", 50)

// 删除用户
err = userSvc.Delete(ctx, "user:1001")
```

### 6. ShoppingCartService - 购物车

**适用场景**：电商购物车、临时收藏夹

```go
cart := factory.NewShoppingCartService("myapp:cart")

// 添加商品
item := map[string]interface{}{
    "product_id": "P1001",
    "name": "iPhone 15 Pro",
    "price": 7999.00,
    "quantity": 1,
    "image": "https://example.com/image.jpg",
}
err := cart.AddItem(ctx, "cart:user:1001", "P1001", item)

// 更新数量
err = cart.UpdateQuantity(ctx, "cart:user:1001", "P1001", 3)

// 移除商品
err = cart.RemoveItem(ctx, "cart:user:1001", "P1001")

// 获取购物车
items, err := cart.GetCart(ctx, "cart:user:1001")

// 获取商品数量
count, err := cart.GetItemCount(ctx, "cart:user:1001")

// 计算总价
total, err := cart.GetTotalPrice(ctx, "cart:user:1001")

// 批量添加
items := []map[string]interface{}{
    {"product_id": "P1001", "name": "商品1", "price": 99.00, "quantity": 1},
    {"product_id": "P1002", "name": "商品2", "price": 199.00, "quantity": 2},
}
err = cart.BatchAddItems(ctx, "cart:user:1001", items)

// 合并购物车（用户登录后）
err = cart.MergeCart(ctx, "cart:guest:abc", "cart:user:1001")

// 清空购物车
err = cart.Clear(ctx, "cart:user:1001")
```

## List 服务

### 7. MessageQueueService - 消息队列

**适用场景**：异步任务、消息通知、事件处理

```go
queue := factory.NewMessageQueueService("myapp:queue")

// 推送消息（右侧入队）
message := map[string]interface{}{
    "type": "email",
    "to": "user@example.com",
    "subject": "Welcome",
    "body": "Welcome to our service!",
}
err := queue.Push(ctx, "tasks:email", message)

// 批量推送
messages := []interface{}{msg1, msg2, msg3}
err = queue.PushBatch(ctx, "tasks:email", messages)

// 弹出消息（左侧出队，FIFO）
var msg map[string]interface{}
err = queue.Pop(ctx, "tasks:email", &msg)

// 阻塞弹出（等待新消息）
err = queue.BlockingPop(ctx, "tasks:email", &msg, 30*time.Second)

// 查看队列长度
length, err := queue.GetLength(ctx, "tasks:email")

// 查看消息（不移除）
var peeked map[string]interface{}
err = queue.Peek(ctx, "tasks:email", &peeked)

// 批量弹出
var batch []interface{}
err = queue.PopBatch(ctx, "tasks:email", 10, &batch)

// 清空队列
err = queue.Clear(ctx, "tasks:email")
```

### 8. LatestMessagesService - 最新消息

**适用场景**：时间线、最新动态、消息列表

```go
latest := factory.NewLatestMessagesService("myapp:latest")

// 添加帖子（自动保持最新100条）
post := map[string]interface{}{
    "id": "post:1001",
    "user_id": "user:1001",
    "content": "这是一条动态",
    "created_at": time.Now().Unix(),
}
err := latest.AddPost(ctx, "timeline:user:1001", post, 100)

// 获取最新N条
posts, err := latest.GetLatest(ctx, "timeline:user:1001", 20)

// 分页获取
page1, err := latest.GetPage(ctx, "timeline:user:1001", 1, 20)
page2, err := latest.GetPage(ctx, "timeline:user:1001", 2, 20)

// 获取指定范围
posts, err := latest.GetRange(ctx, "timeline:user:1001", 0, 9) // 前10条

// 获取总数
count, err := latest.GetCount(ctx, "timeline:user:1001")

// 删除指定帖子
err = latest.RemovePost(ctx, "timeline:user:1001", "post:1001")

// 清空
err = latest.Clear(ctx, "timeline:user:1001")
```

## Set 服务

### 9. DeduplicationService - 去重

**适用场景**：唯一性检查、访客统计、标签系统

```go
dedup := factory.NewDeduplicationService("myapp:dedup")

// 添加成员
added, err := dedup.Add(ctx, "visitors:today", "user:1001")

// 批量添加
members := []string{"user:1001", "user:1002", "user:1003"}
count, err := dedup.AddBatch(ctx, "visitors:today", members)

// 检查是否存在
exists, err := dedup.IsMember(ctx, "visitors:today", "user:1001")

// 移除成员
err = dedup.Remove(ctx, "visitors:today", "user:1001")

// 获取所有成员
members, err := dedup.GetAll(ctx, "visitors:today")

// 获取数量
count, err := dedup.GetCount(ctx, "visitors:today")

// 集合运算 - 并集
union, err := dedup.Union(ctx, "set1", "set2", "set3")

// 集合运算 - 交集
intersection, err := dedup.Intersect(ctx, "set1", "set2")

// 集合运算 - 差集
diff, err := dedup.Diff(ctx, "set1", "set2")

// 随机获取
random, err := dedup.RandomMember(ctx, "visitors:today")
randomN, err := dedup.RandomMembers(ctx, "visitors:today", 5)
```

### 10. LotteryService - 抽奖

**适用场景**：活动抽奖、随机分配、A/B测试

```go
lottery := factory.NewLotteryService("myapp:lottery")

// 添加参与者
added, err := lottery.AddParticipant(ctx, "spring_2024", "user:1001")

// 批量添加
userIDs := []string{"user:1001", "user:1002", "user:1003"}
count, err := lottery.AddParticipants(ctx, "spring_2024", userIDs)

// 检查是否参与
participating, err := lottery.IsParticipating(ctx, "spring_2024", "user:1001")

// 获取参与人数
count, err := lottery.GetParticipantCount(ctx, "spring_2024")

// 抽取中奖者（不移除，可重复抽）
winner, err := lottery.DrawWinner(ctx, "spring_2024")

// 抽取多个中奖者
winners, err := lottery.DrawWinners(ctx, "spring_2024", 10, false) // 不允许重复

// 抽取并移除（确保不重复）
winner, err := lottery.DrawAndRemoveWinner(ctx, "spring_2024")
winners, err := lottery.DrawAndRemoveWinners(ctx, "spring_2024", 10)

// 保存中奖名单
err = lottery.SaveWinners(ctx, "spring_2024", winners)

// 获取中奖名单
winnerList, err := lottery.GetWinners(ctx, "spring_2024")

// 检查是否中奖
isWinner, err := lottery.IsWinner(ctx, "spring_2024", "user:1001")

// 清空抽奖池
err = lottery.Clear(ctx, "spring_2024")
```

### 11. SocialGraphService - 社交关系

**适用场景**：好友系统、关注/粉丝、社交推荐

```go
social := factory.NewSocialGraphService("myapp:social")

// 添加好友（双向）
err := social.AddFriend(ctx, "user:1001", "user:1002")

// 添加关注（单向）
err = social.AddFollowing(ctx, "user:1001", "user:1002")

// 删除好友
err = social.RemoveFriend(ctx, "user:1001", "user:1002")

// 取消关注
err = social.RemoveFollowing(ctx, "user:1001", "user:1002")

// 检查好友关系
isFriend, err := social.IsFriend(ctx, "user:1001", "user:1002")

// 检查关注关系
isFollowing, err := social.IsFollowing(ctx, "user:1001", "user:1002")

// 获取好友列表
friends, err := social.GetFriends(ctx, "user:1001")

// 获取关注列表
following, err := social.GetFollowing(ctx, "user:1001")

// 获取粉丝列表
followers, err := social.GetFollowers(ctx, "user:1001")

// 获取共同好友
commonFriends, err := social.GetCommonFriends(ctx, "user:1001", "user:1002")

// 获取互相关注
mutual, err := social.GetMutualFollowing(ctx, "user:1001")

// 可能认识的人（基于共同好友）
recommendations, err := social.MayKnow(ctx, "user:1001", 10)

// 获取二度好友
secondDegree, err := social.GetSecondDegreeFriends(ctx, "user:1001")

// 批量添加好友
friendIDs := []string{"user:1002", "user:1003", "user:1004"}
err = social.BatchAddFriends(ctx, "user:1001", friendIDs)
```

## ZSet 服务

### 12. LeaderboardService - 排行榜

**适用场景**：游戏排行、销售排名、热度排序

```go
leaderboard := factory.NewLeaderboardService("myapp:leaderboard")

// 添加/更新分数
err := leaderboard.AddScore(ctx, "game:season1", "player:1001", 1500)

// 增加分数
newScore, err := leaderboard.IncrementScore(ctx, "game:season1", "player:1001", 100)

// 获取玩家排名（从1开始）
rank, err := leaderboard.GetRank(ctx, "game:season1", "player:1001")

// 获取玩家分数
score, err := leaderboard.GetScore(ctx, "game:season1", "player:1001")

// 获取前N名
topPlayers, err := leaderboard.GetTopN(ctx, "game:season1", 10)
// 返回: []PlayerScore{{Player: "player:1001", Score: 2500, Rank: 1}, ...}

// 获取指定排名范围
players, err := leaderboard.GetRange(ctx, "game:season1", 1, 100)

// 获取周围玩家
around, err := leaderboard.GetAroundPlayers(ctx, "game:season1", "player:1001", 5)

// 获取总人数
count, err := leaderboard.GetCount(ctx, "game:season1")

// 获取分数范围内的玩家数
count, err = leaderboard.GetCountByScore(ctx, "game:season1", 1000, 2000)

// 移除玩家
err = leaderboard.RemovePlayer(ctx, "game:season1", "player:1001")

// 清空排行榜
err = leaderboard.Clear(ctx, "game:season1")
```

### 13. DelayQueueService - 延迟队列

**适用场景**：延迟任务、订单超时、定时提醒

```go
delayQueue := factory.NewDelayQueueService("myapp:delay")

// 添加延迟任务
task := map[string]interface{}{
    "type": "order_timeout",
    "order_id": "ORD123456",
    "user_id": "user:1001",
}

// 30分钟后执行
executeTime := time.Now().Add(30 * time.Minute)
err := delayQueue.AddTask(ctx, "orders", task, executeTime)

// 或使用延迟时间
err = delayQueue.AddTaskWithDelay(ctx, "orders", task, 30*time.Minute)

// 获取到期任务
readyTasks, err := delayQueue.GetReadyTasks(ctx, "orders", 10)

// 弹出到期任务（原子操作）
task, err := delayQueue.PopReadyTask(ctx, "orders")
if task != nil {
    // 处理任务
    processTask(task)
}

// 批量弹出
tasks, err := delayQueue.PopReadyTasks(ctx, "orders", 10)

// 查看下一个任务（不移除）
nextTask, err := delayQueue.PeekNextTask(ctx, "orders")

// 获取队列长度
count, err := delayQueue.GetCount(ctx, "orders")

// 删除任务
err = delayQueue.RemoveTask(ctx, "orders", "task:123")

// 清空队列
err = delayQueue.Clear(ctx, "orders")
```

### 14. PriorityQueueService - 优先级队列

**适用场景**：任务调度、工单处理、紧急事件

```go
priority := factory.NewPriorityQueueService("myapp:priority")

// 添加任务
task := &PriorityTask{
    ID:       "task:1001",
    Type:     "urgent_repair",
    Payload:  map[string]interface{}{"device_id": "DEV001"},
    Priority: 100.0, // 优先级越高越先处理
}
err := priority.AddTask(ctx, "tasks", task)

// 指定优先级添加
err = priority.AddTaskWithPriority(ctx, "tasks", task, 200.0)

// 弹出最高优先级任务
highestTask, err := priority.PopHighestPriority(ctx, "tasks")
if highestTask != nil {
    // 处理任务
    processTask(highestTask)
}

// 查看最高优先级（不移除）
task, err := priority.PeekHighest(ctx, "tasks")

// 查看前N个任务
topTasks, err := priority.PeekN(ctx, "tasks", 10)

// 批量弹出
tasks, err := priority.PopBatch(ctx, "tasks", 5)

// 获取队列长度
count, err := priority.GetCount(ctx, "tasks")

// 获取指定优先级范围的任务数
count, err = priority.GetCountByPriority(ctx, "tasks", 50.0, 100.0)

// 获取指定优先级范围的任务
tasks, err = priority.GetTasksByPriority(ctx, "tasks", 80.0, 100.0)

// 清空队列
err = priority.Clear(ctx, "tasks")
```

## Bitmap 服务

### 15. SignInService - 签到

**适用场景**：用户签到、打卡记录、出勤统计

```go
signIn := factory.NewSignInService("myapp:signin")

// 今日签到
err := signIn.SignIn(ctx, "user:1001", nil)

// 指定日期签到
date := time.Date(2024, 2, 5, 0, 0, 0, 0, time.Local)
err = signIn.SignIn(ctx, "user:1001", &date)

// 检查是否已签到
isSigned, err := signIn.CheckSignIn(ctx, "user:1001", nil)

// 获取本月签到天数
count, err := signIn.GetMonthSignInCount(ctx, "user:1001", 2024, 2)

// 获取本年签到天数
count, err = signIn.GetYearSignInCount(ctx, "user:1001", 2024)

// 获取连续签到天数
continuous, err := signIn.GetContinuousSignInDays(ctx, "user:1001")

// 获取本月签到详情
details, err := signIn.GetMonthSignInDetails(ctx, "user:1001", 2024, 2)
// 返回: map[int]bool{1: true, 2: true, 3: false, ...}

// 获取首次签到日期
firstDate, err := signIn.GetFirstSignInDate(ctx, "user:1001", 2024, 2)

// 获取时间范围内的签到日期
startDate := time.Date(2024, 2, 1, 0, 0, 0, 0, time.Local)
endDate := time.Date(2024, 2, 28, 0, 0, 0, 0, time.Local)
dates, err := signIn.GetSignInDates(ctx, "user:1001", startDate, endDate)

// 获取签到率
rate, err := signIn.GetSignInRate(ctx, "user:1001", 2024, 2)
// 返回: 85.7 (表示85.7%的签到率)

// 取消签到（补签或纠错）
err = signIn.CancelSignIn(ctx, "user:1001", &date)
```

### 16. OnlineStatusService - 在线状态

**适用场景**：用户在线、设备状态、实时统计

```go
online := factory.NewOnlineStatusService("myapp:online")

// 设置用户在线
err := online.SetOnline(ctx, 1001)

// 设置用户离线
err = online.SetOffline(ctx, 1001)

// 检查是否在线
isOnline, err := online.IsOnline(ctx, 1001)

// 获取在线人数
count, err := online.GetOnlineCount(ctx)

// 批量设置在线
userIDs := []int64{1001, 1002, 1003}
err = online.BatchSetOnline(ctx, userIDs)

// 批量检查在线状态
statuses, err := online.BatchCheckOnline(ctx, userIDs)
// 返回: map[int64]bool{1001: true, 1002: false, 1003: true}

// 获取两天都在线的用户数
bothCount, err := online.GetBothOnlineCount(ctx, "2024-02-05", "2024-02-06")

// 获取任一天在线的用户数
eitherCount, err := online.GetEitherOnlineCount(ctx, "2024-02-05", "2024-02-06")

// 设置在线状态并自动过期
err = online.SetOnlineWithExpire(ctx, 1001, 5*time.Minute)

// 获取在线用户列表（小规模场景）
users, err := online.GetOnlineUsers(ctx, 10000)

// 清空所有在线状态
err = online.ClearAll(ctx)
```

### 17. UserActivityService - 用户活动

**适用场景**：DAU/MAU统计、活跃度分析、留存率

```go
activity := factory.NewUserActivityService("myapp:activity")

// 记录用户活动
err := activity.RecordActivity(ctx, "login", 1001, time.Now())

// 检查是否活跃
isActive, err := activity.IsActive(ctx, "login", 1001, time.Now())

// 获取日活跃用户数（DAU）
dau, err := activity.GetDAU(ctx, "login", time.Now())

// 获取月活跃用户数（MAU）
mau, err := activity.GetMAU(ctx, "login", 2024, 2)

// 获取连续活跃天数
continuous, err := activity.GetContinuousActiveDays(ctx, "login", 1001, time.Now())

// 获取时间范围内的活跃用户数
startDate := time.Date(2024, 2, 1, 0, 0, 0, 0, time.Local)
endDate := time.Date(2024, 2, 7, 0, 0, 0, 0, time.Local)
count, err := activity.GetActiveUsersInRange(ctx, "login", startDate, endDate)

// 计算留存率（7日留存）
baseDate := time.Date(2024, 2, 1, 0, 0, 0, 0, time.Local)
targetDate := time.Date(2024, 2, 8, 0, 0, 0, 0, time.Local)
retentionRate, err := activity.GetRetentionRate(ctx, "login", baseDate, targetDate)
// 返回: 45.5 (表示45.5%的留存率)

// 批量记录活动
userIDs := []int64{1001, 1002, 1003}
err = activity.BatchRecordActivity(ctx, "login", userIDs, time.Now())

// 获取用户活跃天数
days, err := activity.GetActivityDays(ctx, "login", 1001, startDate, endDate)

// 对比两天的活跃情况
date1 := time.Date(2024, 2, 5, 0, 0, 0, 0, time.Local)
date2 := time.Date(2024, 2, 6, 0, 0, 0, 0, time.Local)
onlyDate1, onlyDate2, both, err := activity.CompareActivity(ctx, "login", date1, date2)

// 清除活动记录
err = activity.ClearActivity(ctx, "login", time.Now())
```

## Stream 服务

### 18. MessageStreamService - 消息流

**适用场景**：事件流、日志收集、消息持久化

```go
stream := factory.NewMessageStreamService("myapp:stream")

// 添加消息
message := &StreamMessage{
    Type: "order_created",
    Payload: map[string]interface{}{
        "order_id": "ORD123456",
        "user_id": "user:1001",
        "amount": 299.00,
    },
}
messageID, err := stream.Add(ctx, "events", message)

// 批量添加
messages := []*StreamMessage{msg1, msg2, msg3}
messageIDs, err := stream.BatchAdd(ctx, "events", messages)

// 读取消息（从头开始）
messages, err := stream.Read(ctx, "events", "0", 100)

// 读取新消息（阻塞等待）
messages, err = stream.ReadNew(ctx, "events", 30*time.Second)

// 读取指定范围
messages, err = stream.ReadRange(ctx, "events", "-", "+", 100)

// 获取流长度
length, err := stream.GetLength(ctx, "events")

// 获取流信息
info, err := stream.GetInfo(ctx, "events")

// 修剪流（保留最新10000条）
deleted, err := stream.Trim(ctx, "events", 10000, true)

// 按时间修剪（删除1小时前的消息）
minID := fmt.Sprintf("%d-0", time.Now().Add(-1*time.Hour).UnixMilli())
deleted, err = stream.TrimByTime(ctx, "events", minID)

// 删除指定消息
deleted, err = stream.Delete(ctx, "events", messageID1, messageID2)

// 获取最后一条消息ID
lastID, err := stream.GetLastID(ctx, "events")
```

### 19. ConsumerGroupService - 消费者组

**适用场景**：分布式消息处理、任务分配、负载均衡

```go
consumer := factory.NewConsumerGroupService("myapp:stream")

// 创建消费者组
err := consumer.CreateGroup(ctx, "events", "group1", "0")

// 从消费者组读取消息
messages, err := consumer.ReadGroup(ctx, "events", "group1", "consumer1", 10, 5*time.Second)

// 处理消息
for _, msg := range messages {
    // 处理业务逻辑
    err := processMessage(msg)
    if err != nil {
        continue
    }
    
    // 确认消息
    consumer.Ack(ctx, "events", "group1", msg.ID)
}

// 读取待处理消息（重试失败的消息）
pending, err := consumer.ReadPending(ctx, "events", "group1", "consumer1", 10)

// 认领超时消息（从其他消费者）
claimed, err := consumer.Claim(ctx, "events", "group1", "consumer1", 
    5*time.Minute, messageID1, messageID2)

// 自动认领超时消息
messages, nextStart, err := consumer.AutoClaim(ctx, "events", "group1", "consumer1",
    5*time.Minute, "0-0", 10)

// 获取待处理消息信息
pendingInfo, err := consumer.GetPendingInfo(ctx, "events", "group1")
// 返回: &PendingInfo{Count: 5, Lower: "xxx", Higher: "yyy", Consumers: {...}}

// 获取消费者的待处理消息
pendingMsgs, err := consumer.GetConsumerPendingInfo(ctx, "events", "group1", "consumer1", 10)

// 获取消费者组信息
groups, err := consumer.GetGroupInfo(ctx, "events")

// 获取消费者信息
consumers, err := consumer.GetConsumersInfo(ctx, "events", "group1")

// 删除消费者
deleted, err := consumer.DeleteConsumer(ctx, "events", "group1", "consumer1")

// 设置消费者组的最后交付ID
err = consumer.SetID(ctx, "events", "group1", "1234567890-0")

// 销毁消费者组
err = consumer.DestroyGroup(ctx, "events", "group1")
```

## 最佳实践

### 1. 错误处理

```go
import "gcode/redis/pkg/errors"

// 检查特定错误类型
if err != nil {
    if redisErr, ok := err.(*errors.RedisError); ok {
        switch redisErr.Code {
        case errors.ErrCodeNotFound:
            // 处理未找到的情况
        case errors.ErrCodeTimeout:
            // 处理超时
        case errors.ErrCodeInvalidInput:
            // 处理参数错误
        default:
            // 其他错误
        }
    }
}
```

### 2. 性能监控

```go
// 获取操作统计
stats := application.GetMetrics().GetStats()
for operation, opStats := range stats {
    log.Info("Operation stats",
        logger.String("operation", operation),
        logger.Int64("count", opStats.Count),
        logger.Int64("success", opStats.SuccessCount),
        logger.Int64("errors", opStats.ErrorCount),
        logger.Duration("avg_duration", opStats.AvgDuration))
}
```

### 3. 优雅关闭

```go
func main() {
    app, err := app.NewApp(cfg, log)
    if err != nil {
        log.Fatal("Failed to create app", logger.ErrorField(err))
    }
    
    // 确保资源清理
    defer app.Shutdown()
    
    // 业务逻辑
    // ...
}
```

### 4. 上下文超时

```go
// 设置操作超时
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

err := cache.Set(ctx, "key", value, 10*time.Minute)
if err != nil {
    // 处理超时或其他错误
}
```

### 5. 批量操作

```go
// 优先使用批量操作提升性能
// ❌ 不推荐
for _, key := range keys {
    cache.Get(ctx, key, &result)
}

// ✅ 推荐
results, err := cache.MGet(ctx, keys)
```

### 6. 缓存穿透保护

```go
// 使用 GetOrSet 防止缓存穿透
result, err := cache.GetOrSet(ctx, "expensive:data", 5*time.Minute, func() (interface{}, error) {
    // 只有缓存不存在时才执行
    return loadFromDatabase()
})
```

### 7. 分布式锁最佳实践

```go
// 使用 WithLock 模式，自动处理锁的获取和释放
err := lockSvc.WithLock(ctx, "resource:id", 30*time.Second, func() error {
    // 业务逻辑在锁保护下执行
    // 即使发生panic，锁也会被正确释放
    return processResource()
})
```

### 8. 键命名规范

```go
// 使用清晰的命名空间
// 格式: {app}:{type}:{id}
cache := factory.NewStringCacheService("myapp:cache")
cache.Set(ctx, "user:1001", userData, ttl)

// 使用业务含义明确的键名
counter.Increment(ctx, "page:views:homepage")
counter.Increment(ctx, "api:calls:user:1001")
```

### 9. TTL 设置建议

```go
// 根据数据特性设置合理的过期时间
cache.Set(ctx, "user:session", data, 2*time.Hour)      // 会话数据
cache.Set(ctx, "user:profile", data, 24*time.Hour)     // 用户资料
cache.Set(ctx, "config:app", data, 1*time.Hour)        // 配置数据
cache.Set(ctx, "hot:data", data, 5*time.Minute)        // 热点数据
```

### 10. 日志记录

```go
// 记录关键操作
log.Info("Cache operation",
    logger.String("operation", "set"),
    logger.String("key", key),
    logger.Duration("ttl", ttl))

// 记录错误时包含上下文
log.Error("Operation failed",
    logger.String("operation", "get"),
    logger.String("key", key),
    logger.ErrorField(err))
```

## 总结

本文档提供了所有21个Redis服务的完整使用示例。每个服务都经过生产级验证，包含：

- ✅ 详尽的中文注释
- ✅ 完整的错误处理
- ✅ 性能指标收集
- ✅ 批量操作支持
- ✅ 最佳实践指导

建议根据实际业务场景选择合适的服务，并遵循最佳实践以获得最佳性能和可靠性。
