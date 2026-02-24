package health

import (
	"context"
	"sync"
	"time"

	"gcode/redis/client"
	"gcode/redis/pkg/logger"
)

type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

type HealthCheck struct {
	Status    Status
	Message   string
	Timestamp time.Time
	Latency   time.Duration
	Details   map[string]interface{}
}

type Checker struct {
	client    client.Client
	logger    logger.Logger
	interval  time.Duration
	timeout   time.Duration
	mu        sync.RWMutex
	lastCheck *HealthCheck
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewChecker(client client.Client, log logger.Logger, interval, timeout time.Duration) *Checker {
	return &Checker{
		client:   client,
		logger:   log,
		interval: interval,
		timeout:  timeout,
		stopCh:   make(chan struct{}),
	}
}

func (c *Checker) Start() {
	c.wg.Add(1)
	go c.run()
}

func (c *Checker) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Checker) run() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.check()

	for {
		select {
		case <-ticker.C:
			c.check()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Checker) check() {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	start := time.Now()
	err := c.client.HealthCheck(ctx)
	latency := time.Since(start)

	check := &HealthCheck{
		Timestamp: time.Now(),
		Latency:   latency,
		Details:   make(map[string]interface{}),
	}

	if err != nil {
		check.Status = StatusUnhealthy
		check.Message = err.Error()
		c.logger.Error("Health check failed", logger.ErrorField(err), logger.Duration("latency", latency))
	} else if latency > c.timeout/2 {
		check.Status = StatusDegraded
		check.Message = "High latency detected"
		c.logger.Warn("Health check degraded", logger.Duration("latency", latency))
	} else {
		check.Status = StatusHealthy
		check.Message = "All systems operational"
		c.logger.Debug("Health check passed", logger.Duration("latency", latency))
	}

	check.Details["latency_ms"] = latency.Milliseconds()

	c.mu.Lock()
	c.lastCheck = check
	c.mu.Unlock()
}

func (c *Checker) GetStatus() *HealthCheck {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.lastCheck == nil {
		return &HealthCheck{
			Status:    StatusUnhealthy,
			Message:   "No health check performed yet",
			Timestamp: time.Now(),
		}
	}

	return c.lastCheck
}
