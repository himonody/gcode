package retry

import (
	"context"
	"fmt"
	"time"

	"gcode/redis/pkg/logger"
)

type Config struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

func DefaultConfig() *Config {
	return &Config{
		MaxAttempts:     3,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
	}
}

type Retryer struct {
	config *Config
	logger logger.Logger
}

func NewRetryer(config *Config, log logger.Logger) *Retryer {
	if config == nil {
		config = DefaultConfig()
	}

	return &Retryer{
		config: config,
		logger: log,
	}
}

func (r *Retryer) Do(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	interval := r.config.InitialInterval

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			if attempt > 1 {
				r.logger.Info("Operation succeeded after retry",
					logger.String("operation", operation),
					logger.Int("attempt", attempt))
			}
			return nil
		}

		lastErr = err

		if attempt == r.config.MaxAttempts {
			break
		}

		r.logger.Warn("Operation failed, retrying",
			logger.String("operation", operation),
			logger.Int("attempt", attempt),
			logger.Int("max_attempts", r.config.MaxAttempts),
			logger.Duration("next_retry_in", interval),
			logger.ErrorField(err))

		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(interval):
		}

		interval = time.Duration(float64(interval) * r.config.Multiplier)
		if interval > r.config.MaxInterval {
			interval = r.config.MaxInterval
		}
	}

	r.logger.Error("Operation failed after all retries",
		logger.String("operation", operation),
		logger.Int("attempts", r.config.MaxAttempts),
		logger.ErrorField(lastErr))

	return fmt.Errorf("operation failed after %d attempts: %w", r.config.MaxAttempts, lastErr)
}

func (r *Retryer) DoWithResult(ctx context.Context, operation string, fn func() (interface{}, error)) (interface{}, error) {
	var result interface{}
	var lastErr error
	interval := r.config.InitialInterval

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		res, err := fn()
		if err == nil {
			if attempt > 1 {
				r.logger.Info("Operation succeeded after retry",
					logger.String("operation", operation),
					logger.Int("attempt", attempt))
			}
			return res, nil
		}

		lastErr = err

		if attempt == r.config.MaxAttempts {
			break
		}

		r.logger.Warn("Operation failed, retrying",
			logger.String("operation", operation),
			logger.Int("attempt", attempt),
			logger.Int("max_attempts", r.config.MaxAttempts),
			logger.Duration("next_retry_in", interval),
			logger.ErrorField(err))

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(interval):
		}

		interval = time.Duration(float64(interval) * r.config.Multiplier)
		if interval > r.config.MaxInterval {
			interval = r.config.MaxInterval
		}
	}

	r.logger.Error("Operation failed after all retries",
		logger.String("operation", operation),
		logger.Int("attempts", r.config.MaxAttempts),
		logger.ErrorField(lastErr))

	return result, fmt.Errorf("operation failed after %d attempts: %w", r.config.MaxAttempts, lastErr)
}
