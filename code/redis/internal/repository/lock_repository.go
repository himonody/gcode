package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

type LockRepository interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (string, error)
	Release(ctx context.Context, key string, token string) error
	Extend(ctx context.Context, key string, token string, ttl time.Duration) error
	IsLocked(ctx context.Context, key string) (bool, error)
}

type lockRepository struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
}

func NewLockRepository(client client.Client, log logger.Logger, m metrics.Metrics) LockRepository {
	return &lockRepository{
		client:  client,
		logger:  log,
		metrics: m,
	}
}

func (r *lockRepository) generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *lockRepository) Acquire(ctx context.Context, key string, ttl time.Duration) (string, error) {
	start := time.Now()

	token, err := r.generateToken()
	if err != nil {
		r.logger.Error("Failed to generate lock token", logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to generate token")
	}

	success, err := r.client.GetClient().SetNX(ctx, key, token, ttl).Result()

	duration := time.Since(start)
	r.metrics.RecordOperation("lock.acquire", duration, err == nil && success)

	if err != nil {
		r.logger.Error("Failed to acquire lock", logger.ErrorField(err), logger.String("key", key))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to acquire lock")
	}

	if !success {
		r.logger.Debug("Lock already held", logger.String("key", key))
		return "", rediserr.New(rediserr.ErrCodeLockFailed, "lock already held by another process")
	}

	r.logger.Debug("Lock acquired", logger.String("key", key), logger.Duration("ttl", ttl))
	return token, nil
}

func (r *lockRepository) Release(ctx context.Context, key string, token string) error {
	start := time.Now()

	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := r.client.GetClient().Eval(ctx, script, []string{key}, token).Result()

	duration := time.Since(start)
	r.metrics.RecordOperation("lock.release", duration, err == nil)

	if err != nil {
		r.logger.Error("Failed to release lock", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to release lock")
	}

	if result.(int64) == 0 {
		r.logger.Warn("Lock not owned or already released", logger.String("key", key))
		return rediserr.New(rediserr.ErrCodeLockFailed, "lock not owned or already released")
	}

	r.logger.Debug("Lock released", logger.String("key", key))
	return nil
}

func (r *lockRepository) Extend(ctx context.Context, key string, token string, ttl time.Duration) error {
	start := time.Now()

	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := r.client.GetClient().Eval(ctx, script, []string{key}, token, int64(ttl/time.Millisecond)).Result()

	duration := time.Since(start)
	r.metrics.RecordOperation("lock.extend", duration, err == nil)

	if err != nil {
		r.logger.Error("Failed to extend lock", logger.ErrorField(err), logger.String("key", key))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to extend lock")
	}

	if result.(int64) == 0 {
		r.logger.Warn("Lock not owned", logger.String("key", key))
		return rediserr.New(rediserr.ErrCodeLockFailed, "lock not owned")
	}

	r.logger.Debug("Lock extended", logger.String("key", key), logger.Duration("ttl", ttl))
	return nil
}

func (r *lockRepository) IsLocked(ctx context.Context, key string) (bool, error) {
	start := time.Now()
	defer func() {
		r.metrics.RecordOperation("lock.is_locked", time.Since(start), true)
	}()

	exists, err := r.client.GetClient().Exists(ctx, key).Result()
	if err != nil {
		r.logger.Error("Failed to check lock", logger.ErrorField(err), logger.String("key", key))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to check lock")
	}

	return exists > 0, nil
}
