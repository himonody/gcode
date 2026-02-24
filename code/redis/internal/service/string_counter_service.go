package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// CounterService 计数器服务接口
// 基于 Redis String 的原子计数功能
// 适用场景：
// - 页面浏览量统计
// - API 调用次数限制（限流）
// - 点赞/收藏计数
// - 库存扣减
// - 分布式 ID 生成
type CounterService interface {
	// Increment 自增计数器
	// 返回自增后的值
	Increment(ctx context.Context, key string) (int64, error)

	// IncrementBy 按指定值增加计数器
	// delta: 增量值，可以为负数（相当于减少）
	IncrementBy(ctx context.Context, key string, delta int64) (int64, error)

	// Decrement 自减计数器
	// 返回自减后的值
	Decrement(ctx context.Context, key string) (int64, error)

	// DecrementBy 按指定值减少计数器
	// delta: 减量值
	DecrementBy(ctx context.Context, key string, delta int64) (int64, error)

	// Get 获取计数器当前值
	Get(ctx context.Context, key string) (int64, error)

	// Set 设置计数器值
	Set(ctx context.Context, key string, value int64) error

	// Reset 重置计数器为 0
	Reset(ctx context.Context, key string) error

	// IncrementWithExpire 自增并设置过期时间
	// 如果键不存在，创建后设置过期时间
	// 如果键已存在，只自增不修改过期时间
	// 常用于时间窗口内的计数（如每日/每小时统计）
	IncrementWithExpire(ctx context.Context, key string, ttl time.Duration) (int64, error)

	// GetAndReset 获取当前值并重置为 0
	// 原子操作，常用于周期性统计
	GetAndReset(ctx context.Context, key string) (int64, error)

	// IncrementFloat 浮点数自增
	// 用于需要小数精度的计数场景
	IncrementFloat(ctx context.Context, key string, delta float64) (float64, error)

	// GetMultiple 批量获取多个计数器的值
	GetMultiple(ctx context.Context, keys ...string) (map[string]int64, error)

	// Delete 删除计数器
	Delete(ctx context.Context, keys ...string) error
}

type counterService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewCounterService 创建计数器服务
// prefix: 键前缀，建议使用 "counter" 或 "cnt"
func NewCounterService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) CounterService {
	if prefix == "" {
		prefix = "counter"
	}
	return &counterService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *counterService) buildKey(key string) string {
	return fmt.Sprintf("%s:%s", s.prefix, key)
}

func (s *counterService) Increment(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.increment", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.client.GetClient().Incr(ctx, fullKey).Result()
	if err != nil {
		s.logger.Error("计数器自增失败",
			logger.String("key", key),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "increment failed")
	}

	s.logger.Debug("计数器自增成功",
		logger.String("key", key),
		logger.Int64("value", value))

	return value, nil
}

func (s *counterService) IncrementBy(ctx context.Context, key string, delta int64) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.increment_by", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.client.GetClient().IncrBy(ctx, fullKey, delta).Result()
	if err != nil {
		s.logger.Error("计数器增加失败",
			logger.String("key", key),
			logger.Int64("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "increment by failed")
	}

	s.logger.Debug("计数器增加成功",
		logger.String("key", key),
		logger.Int64("delta", delta),
		logger.Int64("value", value))

	return value, nil
}

func (s *counterService) Decrement(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.decrement", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.client.GetClient().Decr(ctx, fullKey).Result()
	if err != nil {
		s.logger.Error("计数器自减失败",
			logger.String("key", key),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "decrement failed")
	}

	s.logger.Debug("计数器自减成功",
		logger.String("key", key),
		logger.Int64("value", value))

	return value, nil
}

func (s *counterService) DecrementBy(ctx context.Context, key string, delta int64) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.decrement_by", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.client.GetClient().DecrBy(ctx, fullKey, delta).Result()
	if err != nil {
		s.logger.Error("计数器减少失败",
			logger.String("key", key),
			logger.Int64("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "decrement by failed")
	}

	s.logger.Debug("计数器减少成功",
		logger.String("key", key),
		logger.Int64("delta", delta),
		logger.Int64("value", value))

	return value, nil
}

func (s *counterService) Get(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.get", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.client.GetClient().Get(ctx, fullKey).Int64()
	if err == redis.Nil {
		s.logger.Debug("计数器不存在，返回 0", logger.String("key", key))
		return 0, nil
	}

	if err != nil {
		s.logger.Error("获取计数器失败",
			logger.String("key", key),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get counter failed")
	}

	return value, nil
}

func (s *counterService) Set(ctx context.Context, key string, value int64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.set", time.Since(start), true)
	}()

	if key == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	err := s.client.GetClient().Set(ctx, fullKey, value, 0).Err()
	if err != nil {
		s.logger.Error("设置计数器失败",
			logger.String("key", key),
			logger.Int64("value", value),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set counter failed")
	}

	s.logger.Debug("设置计数器成功",
		logger.String("key", key),
		logger.Int64("value", value))

	return nil
}

func (s *counterService) Reset(ctx context.Context, key string) error {
	return s.Set(ctx, key, 0)
}

func (s *counterService) IncrementWithExpire(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.increment_with_expire", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	if ttl <= 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "ttl must be positive")
	}

	fullKey := s.buildKey(key)

	// 使用 Pipeline 保证原子性
	pipe := s.client.GetClient().Pipeline()
	incrCmd := pipe.Incr(ctx, fullKey)
	pipe.Expire(ctx, fullKey, ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("带过期时间的自增失败",
			logger.String("key", key),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "increment with expire failed")
	}

	value := incrCmd.Val()

	s.logger.Debug("带过期时间的自增成功",
		logger.String("key", key),
		logger.Int64("value", value),
		logger.Duration("ttl", ttl))

	return value, nil
}

func (s *counterService) GetAndReset(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.get_and_reset", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	// 使用 Lua 脚本保证原子性
	script := `
		local value = redis.call('GET', KEYS[1])
		if value then
			redis.call('SET', KEYS[1], 0)
			return value
		else
			return 0
		end
	`

	result, err := s.client.GetClient().Eval(ctx, script, []string{fullKey}).Result()
	if err != nil {
		s.logger.Error("获取并重置计数器失败",
			logger.String("key", key),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get and reset failed")
	}

	var value int64
	switch v := result.(type) {
	case int64:
		value = v
	case string:
		// Redis 可能返回字符串
		fmt.Sscanf(v, "%d", &value)
	default:
		value = 0
	}

	s.logger.Debug("获取并重置计数器成功",
		logger.String("key", key),
		logger.Int64("value", value))

	return value, nil
}

func (s *counterService) IncrementFloat(ctx context.Context, key string, delta float64) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.increment_float", time.Since(start), true)
	}()

	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.client.GetClient().IncrByFloat(ctx, fullKey, delta).Result()
	if err != nil {
		s.logger.Error("浮点计数器增加失败",
			logger.String("key", key),
			logger.Float64("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "increment float failed")
	}

	s.logger.Debug("浮点计数器增加成功",
		logger.String("key", key),
		logger.Float64("delta", delta),
		logger.Float64("value", value))

	return value, nil
}

func (s *counterService) GetMultiple(ctx context.Context, keys ...string) (map[string]int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.get_multiple", time.Since(start), true)
	}()

	if len(keys) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = s.buildKey(key)
	}

	values, err := s.client.GetClient().MGet(ctx, fullKeys...).Result()
	if err != nil {
		s.logger.Error("批量获取计数器失败",
			logger.Int("key_count", len(keys)),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get multiple failed")
	}

	result := make(map[string]int64, len(keys))
	for i, key := range keys {
		if values[i] == nil {
			result[key] = 0
			continue
		}

		var value int64
		switch v := values[i].(type) {
		case string:
			fmt.Sscanf(v, "%d", &value)
		case int64:
			value = v
		default:
			value = 0
		}
		result[key] = value
	}

	s.logger.Debug("批量获取计数器成功", logger.Int("key_count", len(keys)))
	return result, nil
}

func (s *counterService) Delete(ctx context.Context, keys ...string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("counter.delete", time.Since(start), true)
	}()

	if len(keys) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = s.buildKey(key)
	}

	err := s.client.GetClient().Del(ctx, fullKeys...).Err()
	if err != nil {
		s.logger.Error("删除计数器失败",
			logger.Int("key_count", len(keys)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "delete counters failed")
	}

	s.logger.Debug("删除计数器成功", logger.Int("key_count", len(keys)))
	return nil
}
