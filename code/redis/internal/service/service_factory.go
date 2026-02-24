package service

import (
	"gcode/redis/client"
	"gcode/redis/internal/repository"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// ServiceFactory 服务工厂
// 统一管理所有 Redis 服务的创建
// 使用依赖注入模式，便于测试和维护
type ServiceFactory struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
}

// NewServiceFactory 创建服务工厂
func NewServiceFactory(c client.Client, l logger.Logger, m metrics.Metrics) *ServiceFactory {
	return &ServiceFactory{
		client:  c,
		logger:  l,
		metrics: m,
	}
}

// ==================== String 数据结构服务 ====================

// NewStringCacheService 创建字符串缓存服务
// prefix: 键前缀，如 "myapp:cache"
func (f *ServiceFactory) NewStringCacheService(prefix string) StringCacheService {
	repo := repository.NewCacheRepository(f.client, f.logger, f.metrics)
	return NewStringCacheService(repo, f.logger, prefix)
}

// NewCounterService 创建计数器服务
// prefix: 键前缀，如 "myapp:counter"
func (f *ServiceFactory) NewCounterService(prefix string) CounterService {
	return NewCounterService(f.client, f.logger, f.metrics, prefix)
}

// NewLockService 创建分布式锁服务
// prefix: 键前缀，如 "myapp:lock"
func (f *ServiceFactory) NewLockService(prefix string) LockService {
	repo := repository.NewLockRepository(f.client, f.logger, f.metrics)
	return NewLockService(repo, f.logger, prefix)
}

// NewSessionService 创建 Session 服务
// prefix: 键前缀，如 "myapp:session"
func (f *ServiceFactory) NewSessionService(prefix string) SessionService {
	repo := repository.NewCacheRepository(f.client, f.logger, f.metrics)
	return NewSessionService(repo, f.logger, prefix)
}

// ==================== Hash 数据结构服务 ====================

// NewUserInfoService 创建用户信息服务
// prefix: 键前缀，如 "myapp:user"
func (f *ServiceFactory) NewUserInfoService(prefix string) UserInfoService {
	return NewUserInfoService(f.client, f.logger, f.metrics, prefix)
}

// NewShoppingCartService 创建购物车服务
// prefix: 键前缀，如 "myapp:cart"
func (f *ServiceFactory) NewShoppingCartService(prefix string) ShoppingCartService {
	return NewShoppingCartService(f.client, f.logger, f.metrics, prefix)
}

// ==================== List 数据结构服务 ====================

// NewMessageQueueService 创建消息队列服务
// prefix: 键前缀，如 "myapp:queue"
func (f *ServiceFactory) NewMessageQueueService(prefix string) MessageQueueService {
	return NewMessageQueueService(f.client, f.logger, f.metrics, prefix)
}

// NewLatestMessagesService 创建最新消息服务
// prefix: 键前缀，如 "myapp:latest"
func (f *ServiceFactory) NewLatestMessagesService(prefix string) LatestMessagesService {
	return NewLatestMessagesService(f.client, f.logger, f.metrics, prefix)
}

// ==================== Set 数据结构服务 ====================

// NewDeduplicationService 创建去重服务
// prefix: 键前缀，如 "myapp:dedup"
func (f *ServiceFactory) NewDeduplicationService(prefix string) DeduplicationService {
	return NewDeduplicationService(f.client, f.logger, f.metrics, prefix)
}

// NewLotteryService 创建抽奖服务
// prefix: 键前缀，如 "myapp:lottery"
func (f *ServiceFactory) NewLotteryService(prefix string) LotteryService {
	return NewLotteryService(f.client, f.logger, f.metrics, prefix)
}

// NewSocialGraphService 创建社交关系服务
// prefix: 键前缀，如 "myapp:social"
func (f *ServiceFactory) NewSocialGraphService(prefix string) SocialGraphService {
	return NewSocialGraphService(f.client, f.logger, f.metrics, prefix)
}

// ==================== ZSet 数据结构服务 ====================

// NewLeaderboardService 创建排行榜服务
// prefix: 键前缀，如 "myapp:leaderboard"
func (f *ServiceFactory) NewLeaderboardService(prefix string) LeaderboardService {
	return NewLeaderboardService(f.client, f.logger, f.metrics, prefix)
}

// NewPriorityQueueService 创建优先级队列服务
// prefix: 键前缀，如 "myapp:priority"
func (f *ServiceFactory) NewPriorityQueueService(prefix string) PriorityQueueService {
	return NewPriorityQueueService(f.client, f.logger, f.metrics, prefix)
}

// NewDelayQueueService 创建延迟队列服务
// prefix: 键前缀，如 "myapp:delay"
func (f *ServiceFactory) NewDelayQueueService(prefix string) DelayQueueService {
	return NewDelayQueueService(f.client, f.logger, f.metrics, prefix)
}

// ==================== Bitmap 数据结构服务 ====================

// NewSignInService 创建签到服务
// prefix: 键前缀，如 "myapp:signin"
func (f *ServiceFactory) NewSignInService(prefix string) SignInService {
	return NewSignInService(f.client, f.logger, f.metrics, prefix)
}

// NewOnlineStatusService 创建在线状态服务
// prefix: 键前缀，如 "myapp:online"
func (f *ServiceFactory) NewOnlineStatusService(prefix string) OnlineStatusService {
	return NewOnlineStatusService(f.client, f.logger, f.metrics, prefix)
}

// NewUserActivityService 创建用户活动服务
// prefix: 键前缀，如 "myapp:activity"
func (f *ServiceFactory) NewUserActivityService(prefix string) UserActivityService {
	return NewUserActivityService(f.client, f.logger, f.metrics, prefix)
}

// ==================== Stream 数据结构服务 ====================

// NewMessageStreamService 创建消息流服务
// prefix: 键前缀，如 "myapp:stream"
func (f *ServiceFactory) NewMessageStreamService(prefix string) MessageStreamService {
	return NewMessageStreamService(f.client, f.logger, f.metrics, prefix)
}

// NewConsumerGroupService 创建消费者组服务
// prefix: 键前缀，如 "myapp:consumer"
func (f *ServiceFactory) NewConsumerGroupService(prefix string) ConsumerGroupService {
	return NewConsumerGroupService(f.client, f.logger, f.metrics, prefix)
}

// ==================== 辅助服务 ====================
