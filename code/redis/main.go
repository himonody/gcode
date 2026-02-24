package main

import (
	"context"
	"os"

	"gcode/redis/app"
	"gcode/redis/config"
	"gcode/redis/pkg/logger"
)

func main() {
	env := getEnvironment()

	log := logger.NewLogger(logger.INFO)
	log.Info("Starting Redis Application", logger.String("environment", string(env)))

	cfg := config.NewConfig(env)

	if redisHost := os.Getenv("REDIS_HOST"); redisHost != "" {
		cfg.Redis.Host = redisHost
	}
	if redisPassword := os.Getenv("REDIS_PASSWORD"); redisPassword != "" {
		cfg.Redis.Password = redisPassword
	}

	application, err := app.NewApplication(cfg, app.WithLogger(log))
	if err != nil {
		log.Fatal("Failed to create application", logger.ErrorField(err))
	}

	runExample(application)

	if err := application.Run(); err != nil {
		log.Fatal("Application error", logger.ErrorField(err))
	}
}

func getEnvironment() config.Environment {
	env := os.Getenv("APP_ENV")
	switch env {
	case "production":
		return config.EnvProduction
	case "staging":
		return config.EnvStaging
	default:
		return config.EnvDevelopment
	}
}

func runExample(application *app.Application) {
	_ = context.Background()
	log := application.GetLogger()

	log.Info("=== Running Enterprise Redis Examples ===")

	log.Info("\n--- Cache Service Example ---")
	// demonstrateCacheService(ctx, cacheService, log)

	log.Info("\n--- Distributed Lock Example ---")
	// demonstrateLockService(ctx, lockService, log)

	log.Info("\n--- Health Check Example ---")
	healthStatus := application.GetHealthStatus()
	log.Info("Health Status",
		logger.String("status", string(healthStatus.Status)),
		logger.String("message", healthStatus.Message),
		logger.Duration("latency", healthStatus.Latency))

	log.Info("\n--- Metrics Example ---")
	stats := application.GetMetrics()
	log.Info("Metrics Summary",
		logger.Int64("total_operations", stats.TotalOperations),
		logger.Int64("total_success", stats.TotalSuccess),
		logger.Int64("total_errors", stats.TotalErrors))

	for opName, opStats := range stats.Operations {
		log.Info("Operation Stats",
			logger.String("operation", opName),
			logger.Int64("count", opStats.Count),
			logger.Int64("success", opStats.SuccessCount),
			logger.Int64("errors", opStats.ErrorCount),
			logger.Duration("avg_duration", opStats.AvgDuration))
	}
}
