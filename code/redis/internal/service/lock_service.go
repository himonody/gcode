package service

import (
	"context"
	"fmt"
	"time"

	"gcode/redis/internal/repository"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
)

type LockService interface {
	Lock(ctx context.Context, resource string, ttl time.Duration) (*Lock, error)
	TryLock(ctx context.Context, resource string, ttl time.Duration, retries int, retryDelay time.Duration) (*Lock, error)
	WithLock(ctx context.Context, resource string, ttl time.Duration, fn func(ctx context.Context) error) error
}

type Lock struct {
	key    string
	token  string
	ttl    time.Duration
	repo   repository.LockRepository
	logger logger.Logger
}

func (l *Lock) Release(ctx context.Context) error {
	return l.repo.Release(ctx, l.key, l.token)
}

func (l *Lock) Extend(ctx context.Context, ttl time.Duration) error {
	return l.repo.Extend(ctx, l.key, l.token, ttl)
}

func (l *Lock) Key() string {
	return l.key
}

type lockService struct {
	repo   repository.LockRepository
	logger logger.Logger
	prefix string
}

func NewLockService(repo repository.LockRepository, log logger.Logger, prefix string) LockService {
	if prefix == "" {
		prefix = "lock"
	}

	return &lockService{
		repo:   repo,
		logger: log,
		prefix: prefix,
	}
}

func (s *lockService) buildKey(resource string) string {
	return fmt.Sprintf("%s:%s", s.prefix, resource)
}

func (s *lockService) Lock(ctx context.Context, resource string, ttl time.Duration) (*Lock, error) {
	if resource == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "resource cannot be empty")
	}

	if ttl <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "ttl must be positive")
	}

	key := s.buildKey(resource)

	token, err := s.repo.Acquire(ctx, key, ttl)
	if err != nil {
		return nil, err
	}

	lock := &Lock{
		key:    key,
		token:  token,
		ttl:    ttl,
		repo:   s.repo,
		logger: s.logger,
	}

	s.logger.Info("Lock acquired", logger.String("resource", resource), logger.Duration("ttl", ttl))

	return lock, nil
}

func (s *lockService) TryLock(ctx context.Context, resource string, ttl time.Duration, retries int, retryDelay time.Duration) (*Lock, error) {
	if resource == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "resource cannot be empty")
	}

	if ttl <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "ttl must be positive")
	}

	if retries < 0 {
		retries = 0
	}

	var lastErr error

	for i := 0; i <= retries; i++ {
		lock, err := s.Lock(ctx, resource, ttl)
		if err == nil {
			return lock, nil
		}

		lastErr = err

		if !rediserr.IsNotFound(err) && err.(*rediserr.RedisError).Code != rediserr.ErrCodeLockFailed {
			return nil, err
		}

		if i < retries {
			s.logger.Debug("Lock acquisition failed, retrying",
				logger.String("resource", resource),
				logger.Int("attempt", i+1),
				logger.Int("max_retries", retries))

			select {
			case <-ctx.Done():
				return nil, rediserr.Wrap(ctx.Err(), rediserr.ErrCodeTimeout, "context cancelled while waiting for lock")
			case <-time.After(retryDelay):
			}
		}
	}

	s.logger.Warn("Failed to acquire lock after retries",
		logger.String("resource", resource),
		logger.Int("retries", retries))

	return nil, rediserr.Wrap(lastErr, rediserr.ErrCodeRetryExhausted, "failed to acquire lock after retries")
}

func (s *lockService) WithLock(ctx context.Context, resource string, ttl time.Duration, fn func(ctx context.Context) error) error {
	lock, err := s.Lock(ctx, resource, ttl)
	if err != nil {
		return err
	}

	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := lock.Release(releaseCtx); err != nil {
			s.logger.Error("Failed to release lock", logger.ErrorField(err), logger.String("resource", resource))
		}
	}()

	return fn(ctx)
}
