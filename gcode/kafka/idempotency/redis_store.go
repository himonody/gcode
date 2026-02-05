// Package idempotency 提供基于 Redis 的幂等性存储实现
package idempotency

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 实现基于 Redis 的幂等性存储
// 使用 Redis 的 SET NX（Set if Not eXists）命令实现原子性的检查和设置
//
// 优势：
// 1. 支持分布式部署，多个消费者实例共享状态
// 2. 支持数据过期自动清理（TTL）
// 3. 高性能（内存操作）
// 4. 支持持久化（可选）
//
// 生产级特性：
// - 连接池管理
// - 超时控制
// - 熔断器保护
// - 降级策略
// - 指标统计
type RedisStore struct {
	client         *redis.Client   // Redis 客户端
	ttl            time.Duration   // 幂等键的过期时间
	keyPrefix      string          // 键前缀，用于命名空间隔离
	timeout        time.Duration   // 操作超时时间
	hitCount       int64           // 命中次数（已处理过的消息）
	missCount      int64           // 未命中次数（首次处理的消息）
	errorCount     int64           // 错误次数
	degradeMode    int32           // 降级模式标志（0=正常，1=降级）
	circuitBreaker *CircuitBreaker // 熔断器
	enableDegrade  bool            // 是否启用降级
}

// NewRedisStore 创建一个新的 Redis 存储实例（简化版）
// 参数：
//   - addr: Redis 地址，例如 "localhost:6379"
//   - password: Redis 密码
//   - db: Redis 数据库编号（0-15）
//   - ttl: 幂等键的过期时间
//   - enableDegrade: 是否启用降级（Redis 故障时允许继续处理）
func NewRedisStore(addr, password string, db int, ttl time.Duration, enableDegrade bool) (*RedisStore, error) {
	// 创建 Redis 客户端
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		MaxRetries:   3,
		PoolSize:     10,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// 创建熔断器
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          10 * time.Second,
	})

	slog.Info("Redis store initialized",
		"addr", addr,
		"db", db,
		"ttl", ttl,
		"enableDegrade", enableDegrade)

	return &RedisStore{
		client:         client,
		ttl:            ttl,
		keyPrefix:      "idempotent:",
		timeout:        time.Second,
		enableDegrade:  enableDegrade,
		circuitBreaker: cb,
	}, nil
}

// CheckAndSet 实现幂等性检查和设置的原子操作
// 使用 Redis 的 SET NX EX 命令：
// - NX: 只在键不存在时设置
// - EX: 设置过期时间（秒）
//
// 返回值：
// - true: 首次处理，键已设置
// - false: 已处理过，键已存在
// - error: 操作失败
//
// 生产级特性：
// - 熔断器保护：避免 Redis 故障影响整体
// - 降级策略：Redis 不可用时允许继续处理（可配置）
// - 自动重连：连接断开自动重试
func (s *RedisStore) CheckAndSet(ctx context.Context, key string) (bool, error) {
	// 检查熔断器状态
	if !s.circuitBreaker.CanExecute() {
		atomic.AddInt64(&s.errorCount, 1)

		if s.enableDegrade {
			// 降级模式：允许消息继续处理（可能导致重复）
			slog.Warn("Redis circuit breaker open, degrading to allow processing",
				"key", key,
				"state", s.circuitBreaker.GetState())
			atomic.AddInt64(&s.missCount, 1)
			return true, nil // 降级：假设首次处理
		}

		// 不启用降级：返回错误，消息会重试
		return false, fmt.Errorf("redis circuit breaker is open")
	}

	// 添加键前缀，避免与其他业务冲突
	fullKey := s.keyPrefix + key

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// 使用 SET NX EX 命令原子性地检查和设置
	// value 设置为当前时间戳，便于调试和排查
	value := time.Now().Format(time.RFC3339)
	result, err := s.client.SetNX(ctx, fullKey, value, s.ttl).Result()

	if err != nil {
		// Redis 操作失败
		atomic.AddInt64(&s.errorCount, 1)
		s.circuitBreaker.RecordFailure()

		slog.Error("Redis CheckAndSet failed",
			"key", key,
			"err", err,
			"circuitState", s.circuitBreaker.GetState())

		if s.enableDegrade {
			// 降级模式：允许消息继续处理
			slog.Warn("Redis error, degrading to allow processing", "key", key)
			atomic.AddInt64(&s.missCount, 1)
			return true, nil // 降级：假设首次处理
		}

		return false, fmt.Errorf("redis operation failed: %w", err)
	}

	// 记录成功
	s.circuitBreaker.RecordSuccess()

	if result {
		// 键不存在，设置成功，首次处理
		atomic.AddInt64(&s.missCount, 1)
		slog.Debug("Idempotency check: first time processing", "key", key)
		return true, nil
	}

	// 键已存在，已处理过
	atomic.AddInt64(&s.hitCount, 1)
	slog.Debug("Idempotency check: already processed", "key", key)
	return false, nil
}

// Close 关闭 Redis 连接
func (s *RedisStore) Close() error {
	// 输出统计信息
	hit, miss, errors := s.GetStats()
	slog.Info("Redis store closing",
		"hit", hit,
		"miss", miss,
		"errors", errors,
		"circuitState", s.circuitBreaker.GetState())

	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// GetStats 获取统计信息
func (s *RedisStore) GetStats() (hit, miss, errors int64) {
	return atomic.LoadInt64(&s.hitCount),
		atomic.LoadInt64(&s.missCount),
		atomic.LoadInt64(&s.errorCount)
}
