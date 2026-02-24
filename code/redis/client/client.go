package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/config"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

type Client interface {
	GetClient() redis.UniversalClient
	Ping(ctx context.Context) error
	Close() error
	GetMetrics() metrics.Stats
	HealthCheck(ctx context.Context) error
}

type redisClient struct {
	client  redis.UniversalClient
	config  *config.Config
	logger  logger.Logger
	metrics metrics.Metrics
	mu      sync.RWMutex
	closed  bool
}

type ClientOption func(*redisClient)

func WithLogger(log logger.Logger) ClientOption {
	return func(c *redisClient) {
		c.logger = log
	}
}

func WithMetrics(m metrics.Metrics) ClientOption {
	return func(c *redisClient) {
		c.metrics = m
	}
}

func NewClient(cfg *config.Config, opts ...ClientOption) (Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInvalidInput, "invalid config")
	}

	c := &redisClient{
		config:  cfg,
		logger:  logger.NewLogger(logger.INFO),
		metrics: metrics.NewMetrics(),
	}

	for _, opt := range opts {
		opt(c)
	}

	var universalClient redis.UniversalClient

	switch cfg.Mode {
	case "standalone":
		universalClient = redis.NewClient(&redis.Options{
			Addr:            cfg.Redis.Addr(),
			Password:        cfg.Redis.Password,
			DB:              cfg.Redis.DB,
			MaxRetries:      cfg.Redis.MaxRetries,
			MinRetryBackoff: cfg.Redis.MinRetryBackoff,
			MaxRetryBackoff: cfg.Redis.MaxRetryBackoff,
			DialTimeout:     cfg.Redis.DialTimeout,
			ReadTimeout:     cfg.Redis.ReadTimeout,
			WriteTimeout:    cfg.Redis.WriteTimeout,
			PoolSize:        cfg.Redis.PoolSize,
			MinIdleConns:    cfg.Redis.MinIdleConns,
			ConnMaxIdleTime: cfg.Redis.ConnMaxIdleTime,
			ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
			PoolTimeout:     cfg.Redis.PoolTimeout,
		})

	case "cluster":
		universalClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:           cfg.Cluster.Addrs,
			Password:        cfg.Cluster.Password,
			MaxRetries:      cfg.Cluster.MaxRetries,
			MinRetryBackoff: cfg.Cluster.MinRetryBackoff,
			MaxRetryBackoff: cfg.Cluster.MaxRetryBackoff,
			DialTimeout:     cfg.Cluster.DialTimeout,
			ReadTimeout:     cfg.Cluster.ReadTimeout,
			WriteTimeout:    cfg.Cluster.WriteTimeout,
			PoolSize:        cfg.Cluster.PoolSize,
			MinIdleConns:    cfg.Cluster.MinIdleConns,
			ConnMaxIdleTime: cfg.Cluster.ConnMaxIdleTime,
			ConnMaxLifetime: cfg.Cluster.ConnMaxLifetime,
		})

	case "sentinel":
		universalClient = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:      cfg.Sentinel.MasterName,
			SentinelAddrs:   cfg.Sentinel.SentinelAddrs,
			Password:        cfg.Sentinel.Password,
			DB:              cfg.Sentinel.DB,
			MaxRetries:      cfg.Sentinel.MaxRetries,
			MinRetryBackoff: cfg.Sentinel.MinRetryBackoff,
			MaxRetryBackoff: cfg.Sentinel.MaxRetryBackoff,
			DialTimeout:     cfg.Sentinel.DialTimeout,
			ReadTimeout:     cfg.Sentinel.ReadTimeout,
			WriteTimeout:    cfg.Sentinel.WriteTimeout,
			PoolSize:        cfg.Sentinel.PoolSize,
			MinIdleConns:    cfg.Sentinel.MinIdleConns,
		})

	default:
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, fmt.Sprintf("unsupported mode: %s", cfg.Mode))
	}

	c.client = universalClient

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Ping(ctx); err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeConnection, "failed to connect to redis")
	}

	c.logger.Info("Redis client initialized successfully",
		logger.String("mode", cfg.Mode),
		logger.String("environment", string(cfg.Environment)))

	return c, nil
}

func (c *redisClient) GetClient() redis.UniversalClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		c.logger.Warn("Attempting to use closed client")
		return nil
	}

	return c.client
}

func (c *redisClient) Ping(ctx context.Context) error {
	start := time.Now()

	err := c.client.Ping(ctx).Err()

	duration := time.Since(start)
	c.metrics.RecordOperation("ping", duration, err == nil)

	if err != nil {
		c.logger.Error("Ping failed", logger.ErrorField(err), logger.Duration("duration", duration))
		return rediserr.Wrap(err, rediserr.ErrCodeConnection, "ping failed")
	}

	c.logger.Debug("Ping successful", logger.Duration("duration", duration))
	return nil
}

func (c *redisClient) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return rediserr.New(rediserr.ErrCodeConnection, "client is closed")
	}
	c.mu.RUnlock()

	if err := c.Ping(ctx); err != nil {
		return err
	}

	stats := c.client.PoolStats()
	c.logger.Debug("Connection pool stats",
		logger.Int("hits", int(stats.Hits)),
		logger.Int("misses", int(stats.Misses)),
		logger.Int("timeouts", int(stats.Timeouts)),
		logger.Int("total_conns", int(stats.TotalConns)),
		logger.Int("idle_conns", int(stats.IdleConns)),
		logger.Int("stale_conns", int(stats.StaleConns)))

	if stats.TotalConns == 0 {
		return rediserr.New(rediserr.ErrCodeConnection, "no active connections")
	}

	return nil
}

func (c *redisClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.logger.Info("Closing Redis client")

	if err := c.client.Close(); err != nil {
		c.logger.Error("Error closing Redis client", logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "failed to close client")
	}

	c.closed = true
	c.logger.Info("Redis client closed successfully")

	return nil
}

func (c *redisClient) GetMetrics() metrics.Stats {
	return c.metrics.GetStats()
}
