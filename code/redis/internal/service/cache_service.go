package service

import (
	"context"
	"fmt"
	"time"

	"gcode/redis/internal/repository"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
)

type CacheService interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string, dest interface{}) error
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (bool, error)
	GetWithFallback(ctx context.Context, key string, dest interface{}, fallback func() (interface{}, error), ttl time.Duration) error
	Remember(ctx context.Context, key string, ttl time.Duration, fn func() (interface{}, error)) (interface{}, error)
	Invalidate(ctx context.Context, pattern string) error
}

type cacheService struct {
	repo   repository.CacheRepository
	logger logger.Logger
}

func NewCacheService(repo repository.CacheRepository, log logger.Logger) CacheService {
	return &cacheService{
		repo:   repo,
		logger: log,
	}
}

func (s *cacheService) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if key == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	if ttl < 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "ttl cannot be negative")
	}

	return s.repo.Set(ctx, key, value, ttl)
}

func (s *cacheService) Get(ctx context.Context, key string, dest interface{}) error {
	if key == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	if dest == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "destination cannot be nil")
	}

	return s.repo.Get(ctx, key, dest)
}

func (s *cacheService) GetString(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	return s.repo.GetString(ctx, key)
}

func (s *cacheService) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	return s.repo.Delete(ctx, keys...)
}

func (s *cacheService) Exists(ctx context.Context, keys ...string) (bool, error) {
	if len(keys) == 0 {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	count, err := s.repo.Exists(ctx, keys...)
	if err != nil {
		return false, err
	}

	return count == int64(len(keys)), nil
}

func (s *cacheService) GetWithFallback(ctx context.Context, key string, dest interface{}, fallback func() (interface{}, error), ttl time.Duration) error {
	err := s.Get(ctx, key, dest)

	if err == nil {
		s.logger.Debug("Cache hit", logger.String("key", key))
		return nil
	}

	if !rediserr.IsNotFound(err) {
		return err
	}

	s.logger.Debug("Cache miss, executing fallback", logger.String("key", key))

	value, err := fallback()
	if err != nil {
		s.logger.Error("Fallback function failed", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "fallback function failed")
	}

	if err := s.Set(ctx, key, value, ttl); err != nil {
		s.logger.Warn("Failed to cache fallback result", logger.ErrorField(err), logger.String("key", key))
	}

	if err := s.Get(ctx, key, dest); err != nil {
		return err
	}

	return nil
}

func (s *cacheService) Remember(ctx context.Context, key string, ttl time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	var result interface{}

	err := s.Get(ctx, key, &result)
	if err == nil {
		s.logger.Debug("Cache hit for remember", logger.String("key", key))
		return result, nil
	}

	if !rediserr.IsNotFound(err) {
		return nil, err
	}

	s.logger.Debug("Cache miss for remember, executing function", logger.String("key", key))

	value, err := fn()
	if err != nil {
		s.logger.Error("Remember function failed", logger.ErrorField(err), logger.String("key", key))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "remember function failed")
	}

	if err := s.Set(ctx, key, value, ttl); err != nil {
		s.logger.Warn("Failed to cache remember result", logger.ErrorField(err), logger.String("key", key))
	}

	return value, nil
}

func (s *cacheService) Invalidate(ctx context.Context, pattern string) error {
	return rediserr.New(rediserr.ErrCodeInternal, "invalidate by pattern not implemented - use specific keys")
}

type CacheKeyBuilder struct {
	prefix string
}

func NewCacheKeyBuilder(prefix string) *CacheKeyBuilder {
	return &CacheKeyBuilder{prefix: prefix}
}

func (b *CacheKeyBuilder) Build(parts ...string) string {
	key := b.prefix
	for _, part := range parts {
		key = fmt.Sprintf("%s:%s", key, part)
	}
	return key
}

func (b *CacheKeyBuilder) UserKey(userID string) string {
	return b.Build("user", userID)
}

func (b *CacheKeyBuilder) SessionKey(sessionID string) string {
	return b.Build("session", sessionID)
}

func (b *CacheKeyBuilder) ProductKey(productID string) string {
	return b.Build("product", productID)
}
