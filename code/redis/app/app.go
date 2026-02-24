package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gcode/redis/client"
	"gcode/redis/config"
	"gcode/redis/internal/repository"
	"gcode/redis/internal/service"
	"gcode/redis/pkg/health"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
	"gcode/redis/pkg/retry"
)

type Application struct {
	config      *config.Config
	client      client.Client
	logger      logger.Logger
	metrics     metrics.Metrics
	healthCheck *health.Checker
	retryer     *retry.Retryer

	cacheRepo repository.CacheRepository
	lockRepo  repository.LockRepository

	cacheService service.CacheService
	lockService  service.LockService

	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

type Option func(*Application)

func WithLogger(log logger.Logger) Option {
	return func(a *Application) {
		a.logger = log
	}
}

func WithMetrics(m metrics.Metrics) Option {
	return func(a *Application) {
		a.metrics = m
	}
}

func NewApplication(cfg *config.Config, opts ...Option) (*Application, error) {
	app := &Application{
		config:     cfg,
		logger:     logger.NewLogger(logger.INFO),
		metrics:    metrics.NewMetrics(),
		shutdownCh: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(app)
	}

	if err := app.initialize(); err != nil {
		return nil, err
	}

	return app, nil
}

func (a *Application) initialize() error {
	a.logger.Info("Initializing application",
		logger.String("environment", string(a.config.Environment)),
		logger.String("mode", a.config.Mode))

	redisClient, err := client.NewClient(
		a.config,
		client.WithLogger(a.logger),
		client.WithMetrics(a.metrics),
	)
	if err != nil {
		return fmt.Errorf("failed to create redis client: %w", err)
	}
	a.client = redisClient

	a.healthCheck = health.NewChecker(a.client, a.logger, 30*time.Second, 5*time.Second)

	retryConfig := retry.DefaultConfig()
	if a.config.Environment == config.EnvProduction {
		retryConfig.MaxAttempts = 5
		retryConfig.MaxInterval = 10 * time.Second
	}
	a.retryer = retry.NewRetryer(retryConfig, a.logger)

	a.cacheRepo = repository.NewCacheRepository(a.client, a.logger, a.metrics)
	a.lockRepo = repository.NewLockRepository(a.client, a.logger, a.metrics)

	a.cacheService = service.NewCacheService(a.cacheRepo, a.logger)
	a.lockService = service.NewLockService(a.lockRepo, a.logger, "app:lock")

	a.logger.Info("Application initialized successfully")

	return nil
}

func (a *Application) Start() error {
	a.logger.Info("Starting application")

	a.healthCheck.Start()
	a.logger.Info("Health checker started")

	a.logger.Info("Application started successfully")

	return nil
}

func (a *Application) Stop(ctx context.Context) error {
	a.logger.Info("Stopping application")

	close(a.shutdownCh)

	a.healthCheck.Stop()
	a.logger.Info("Health checker stopped")

	if err := a.client.Close(); err != nil {
		a.logger.Error("Error closing Redis client", logger.ErrorField(err))
		return err
	}

	a.wg.Wait()

	a.logger.Info("Application stopped successfully")

	return nil
}

func (a *Application) Run() error {
	if err := a.Start(); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	a.logger.Info("Shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return a.Stop(ctx)
}

func (a *Application) GetCacheService() service.CacheService {
	return a.cacheService
}

func (a *Application) GetLockService() service.LockService {
	return a.lockService
}

func (a *Application) GetMetrics() metrics.Stats {
	return a.metrics.GetStats()
}

func (a *Application) GetHealthStatus() *health.HealthCheck {
	return a.healthCheck.GetStatus()
}

func (a *Application) GetLogger() logger.Logger {
	return a.logger
}
