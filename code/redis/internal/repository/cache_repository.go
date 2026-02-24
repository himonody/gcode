package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

type CacheRepository interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string, dest interface{}) error
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (int64, error)
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	Expire(ctx context.Context, key string, expiration time.Duration) error
	TTL(ctx context.Context, key string) (time.Duration, error)
	MGet(ctx context.Context, keys ...string) ([]interface{}, error)
	MSet(ctx context.Context, pairs ...interface{}) error
}

type cacheRepository struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
}

func NewCacheRepository(client client.Client, log logger.Logger, m metrics.Metrics) CacheRepository {
	return &cacheRepository{
		client:  client,
		logger:  log,
		metrics: m,
	}
}

func (r *cacheRepository) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.set", time.Since(start), true)
	}()

	var data interface{}

	switch v := value.(type) {
	case string, int, int64, float64, bool:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			r.logger.Error("Failed to marshal value", logger.ErrorField(err), logger.String("key", key))
			return rediserr.Wrap(err, rediserr.ErrCodeSerialization, "failed to marshal value")
		}
		data = jsonData
	}

	err := r.client.GetClient().Set(ctx, key, data, expiration).Err()
	if err != nil {
		r.logger.Error("Failed to set cache", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to set cache")
	}

	r.logger.Debug("Cache set successfully", logger.String("key", key), logger.Duration("expiration", expiration))
	return nil
}

func (r *cacheRepository) Get(ctx context.Context, key string, dest interface{}) error {
	start := time.Now()

	result, err := r.client.GetClient().Get(ctx, key).Result()

	duration := time.Since(start)
	r.metrics.RecordOperation("cache.get", duration, err == nil)

	if err == redis.Nil {
		r.logger.Debug("Cache miss", logger.String("key", key))
		return rediserr.New(rediserr.ErrCodeNotFound, "key not found")
	}

	if err != nil {
		r.logger.Error("Failed to get cache", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to get cache")
	}

	if err := json.Unmarshal([]byte(result), dest); err != nil {
		r.logger.Error("Failed to unmarshal value", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeSerialization, "failed to unmarshal value")
	}

	r.logger.Debug("Cache hit", logger.String("key", key))
	return nil
}

func (r *cacheRepository) GetString(ctx context.Context, key string) (string, error) {
	start := time.Now()

	result, err := r.client.GetClient().Get(ctx, key).Result()

	duration := time.Since(start)
	r.metrics.RecordOperation("cache.get_string", duration, err == nil)

	if err == redis.Nil {
		r.logger.Debug("Cache miss", logger.String("key", key))
		return "", rediserr.New(rediserr.ErrCodeNotFound, "key not found")
	}

	if err != nil {
		r.logger.Error("Failed to get cache", logger.ErrorField(err), logger.String("key", key))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to get cache")
	}

	return result, nil
}

func (r *cacheRepository) Delete(ctx context.Context, keys ...string) error {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.delete", time.Since(start), true)
	}()

	err := r.client.GetClient().Del(ctx, keys...).Err()
	if err != nil {
		r.logger.Error("Failed to delete cache", logger.ErrorField(err), logger.Int("key_count", len(keys)))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to delete cache")
	}

	r.logger.Debug("Cache deleted successfully", logger.Int("key_count", len(keys)))
	return nil
}

func (r *cacheRepository) Exists(ctx context.Context, keys ...string) (int64, error) {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.exists", time.Since(start), true)
	}()

	count, err := r.client.GetClient().Exists(ctx, keys...).Result()
	if err != nil {
		r.logger.Error("Failed to check existence", logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to check existence")
	}

	return count, nil
}

func (r *cacheRepository) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.setnx", time.Since(start), true)
	}()

	var data interface{}

	switch v := value.(type) {
	case string, int, int64, float64, bool:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return false, rediserr.Wrap(err, rediserr.ErrCodeSerialization, "failed to marshal value")
		}
		data = jsonData
	}

	success, err := r.client.GetClient().SetNX(ctx, key, data, expiration).Result()
	if err != nil {
		r.logger.Error("Failed to set NX", logger.ErrorField(err), logger.String("key", key))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to set NX")
	}

	return success, nil
}

func (r *cacheRepository) Expire(ctx context.Context, key string, expiration time.Duration) error {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.expire", time.Since(start), true)
	}()

	err := r.client.GetClient().Expire(ctx, key, expiration).Err()
	if err != nil {
		r.logger.Error("Failed to set expiration", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to set expiration")
	}

	return nil
}

func (r *cacheRepository) TTL(ctx context.Context, key string) (time.Duration, error) {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.ttl", time.Since(start), true)
	}()

	ttl, err := r.client.GetClient().TTL(ctx, key).Result()
	if err != nil {
		r.logger.Error("Failed to get TTL", logger.ErrorField(err), logger.String("key", key))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to get TTL")
	}

	return ttl, nil
}

func (r *cacheRepository) MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.mget", time.Since(start), true)
	}()

	values, err := r.client.GetClient().MGet(ctx, keys...).Result()
	if err != nil {
		r.logger.Error("Failed to mget", logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to mget")
	}

	return values, nil
}

func (r *cacheRepository) MSet(ctx context.Context, pairs ...interface{}) error {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("cache.mset", time.Since(start), true)
	}()

	err := r.client.GetClient().MSet(ctx, pairs...).Err()
	if err != nil {
		r.logger.Error("Failed to mset", logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to mset")
	}

	return nil
}
