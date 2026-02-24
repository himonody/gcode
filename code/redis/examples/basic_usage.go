package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"gcode/redis/app"
	"gcode/redis/config"
	"gcode/redis/pkg/logger"
)

// 基础使用示例
// 演示如何使用各种Redis服务

func main() {
	// 1. 初始化应用
	cfg := config.NewConfig(config.Development)
	appLogger := logger.NewLogger(logger.INFO)

	application, err := app.NewApp(cfg, appLogger)
	if err != nil {
		log.Fatal("Failed to create app:", err)
	}
	defer application.Shutdown()

	// 2. 获取服务工厂
	factory := application.GetServiceFactory()
	ctx := context.Background()

	// 3. 使用各种服务
	demonstrateCacheService(ctx, factory, appLogger)
	demonstrateCounterService(ctx, factory, appLogger)
	demonstrateLockService(ctx, factory, appLogger)
	demonstrateLeaderboardService(ctx, factory, appLogger)
	demonstrateUserActivityService(ctx, factory, appLogger)
}

// 演示缓存服务
func demonstrateCacheService(ctx context.Context, factory *service.ServiceFactory, log logger.Logger) {
	fmt.Println("\n=== 缓存服务示例 ===")

	cache := factory.NewStringCacheService("example:cache")

	// 设置缓存
	user := map[string]interface{}{
		"id":    "1001",
		"name":  "张三",
		"email": "zhangsan@example.com",
		"age":   28,
	}

	err := cache.Set(ctx, "user:1001", user, 10*time.Minute)
	if err != nil {
		log.Error("设置缓存失败", logger.ErrorField(err))
		return
	}
	fmt.Println("✓ 缓存设置成功")

	// 获取缓存
	var cachedUser map[string]interface{}
	err = cache.Get(ctx, "user:1001", &cachedUser)
	if err != nil {
		log.Error("获取缓存失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 获取缓存: %v\n", cachedUser)

	// 批量获取
	keys := []string{"user:1001", "user:1002", "user:1003"}
	results, err := cache.MGet(ctx, keys)
	if err != nil {
		log.Error("批量获取失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 批量获取: %d 个结果\n", len(results))
}

// 演示计数器服务
func demonstrateCounterService(ctx context.Context, factory *service.ServiceFactory, log logger.Logger) {
	fmt.Println("\n=== 计数器服务示例 ===")

	counter := factory.NewCounterService("example:counter")

	// 增加计数
	count, err := counter.Increment(ctx, "page:views")
	if err != nil {
		log.Error("增加计数失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 页面访问次数: %d\n", count)

	// 增加指定值
	count, err = counter.IncrementBy(ctx, "page:views", 10)
	if err != nil {
		log.Error("增加计数失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 页面访问次数: %d\n", count)

	// 限流示例
	limit, err := counter.IncrementWithExpire(ctx, "rate:limit:user:1001", 1*time.Minute)
	if err != nil {
		log.Error("限流计数失败", logger.ErrorField(err))
		return
	}

	if limit > 100 {
		fmt.Println("✗ 超过限流阈值")
	} else {
		fmt.Printf("✓ 当前请求数: %d/100\n", limit)
	}
}

// 演示分布式锁服务
func demonstrateLockService(ctx context.Context, factory *service.ServiceFactory, log logger.Logger) {
	fmt.Println("\n=== 分布式锁服务示例 ===")

	lockSvc := factory.NewLockService("example:lock")

	// 使用WithLock模式（推荐）
	err := lockSvc.WithLock(ctx, "resource:order:1001", 30*time.Second, func() error {
		fmt.Println("✓ 获取锁成功，执行业务逻辑...")

		// 模拟业务处理
		time.Sleep(100 * time.Millisecond)

		fmt.Println("✓ 业务逻辑执行完成")
		return nil
	})

	if err != nil {
		log.Error("锁操作失败", logger.ErrorField(err))
		return
	}

	fmt.Println("✓ 锁已自动释放")
}

// 演示排行榜服务
func demonstrateLeaderboardService(ctx context.Context, factory *service.ServiceFactory, log logger.Logger) {
	fmt.Println("\n=== 排行榜服务示例 ===")

	leaderboard := factory.NewLeaderboardService("example:leaderboard")

	// 添加玩家分数
	players := map[string]float64{
		"player:1001": 1500,
		"player:1002": 2000,
		"player:1003": 1800,
		"player:1004": 2200,
		"player:1005": 1600,
	}

	for player, score := range players {
		err := leaderboard.AddScore(ctx, "game:season1", player, score)
		if err != nil {
			log.Error("添加分数失败", logger.ErrorField(err))
			continue
		}
	}
	fmt.Println("✓ 添加5个玩家分数")

	// 获取前3名
	topPlayers, err := leaderboard.GetTopN(ctx, "game:season1", 3)
	if err != nil {
		log.Error("获取排行榜失败", logger.ErrorField(err))
		return
	}

	fmt.Println("✓ 前3名玩家:")
	for _, p := range topPlayers {
		fmt.Printf("  排名 %d: %s (分数: %.0f)\n", p.Rank, p.Player, p.Score)
	}

	// 获取玩家排名
	rank, err := leaderboard.GetRank(ctx, "game:season1", "player:1003")
	if err != nil {
		log.Error("获取排名失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ player:1003 的排名: %d\n", rank)
}

// 演示用户活动统计服务
func demonstrateUserActivityService(ctx context.Context, factory *service.ServiceFactory, log logger.Logger) {
	fmt.Println("\n=== 用户活动统计示例 ===")

	activity := factory.NewUserActivityService("example:activity")

	// 记录用户活动
	userIDs := []int64{1001, 1002, 1003, 1004, 1005}
	err := activity.BatchRecordActivity(ctx, "login", userIDs, time.Now())
	if err != nil {
		log.Error("记录活动失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 记录 %d 个用户登录活动\n", len(userIDs))

	// 获取DAU
	dau, err := activity.GetDAU(ctx, "login", time.Now())
	if err != nil {
		log.Error("获取DAU失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 今日活跃用户数(DAU): %d\n", dau)

	// 获取MAU
	now := time.Now()
	mau, err := activity.GetMAU(ctx, "login", now.Year(), int(now.Month()))
	if err != nil {
		log.Error("获取MAU失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 本月活跃用户数(MAU): %d\n", mau)

	// 获取连续活跃天数
	continuous, err := activity.GetContinuousActiveDays(ctx, "login", 1001, time.Now())
	if err != nil {
		log.Error("获取连续活跃天数失败", logger.ErrorField(err))
		return
	}
	fmt.Printf("✓ 用户1001连续活跃天数: %d\n", continuous)
}

// 注意：需要先导入service包
// import "gcode/redis/internal/service"
