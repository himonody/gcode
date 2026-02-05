package config

import (
	"fmt"
	"time"
)

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

type RedisConfig struct {
	Host            string
	Port            int
	Password        string
	DB              int
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	PoolSize        int
	MinIdleConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
	PoolTimeout     time.Duration
}

type ClusterConfig struct {
	Addrs           []string
	Password        string
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	PoolSize        int
	MinIdleConns    int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
}

type SentinelConfig struct {
	MasterName      string
	SentinelAddrs   []string
	Password        string
	DB              int
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	PoolSize        int
	MinIdleConns    int
}

type Config struct {
	Environment Environment
	Redis       *RedisConfig
	Cluster     *ClusterConfig
	Sentinel    *SentinelConfig
	Mode        string
}

func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Host:            "localhost",
		Port:            6379,
		Password:        "",
		DB:              0,
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        10,
		MinIdleConns:    5,
		MaxIdleConns:    10,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,
		PoolTimeout:     4 * time.Second,
	}
}

func ProductionRedisConfig() *RedisConfig {
	return &RedisConfig{
		Host:            "localhost",
		Port:            6379,
		Password:        "",
		DB:              0,
		MaxRetries:      5,
		MinRetryBackoff: 16 * time.Millisecond,
		MaxRetryBackoff: 1024 * time.Millisecond,
		DialTimeout:     10 * time.Second,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		PoolSize:        50,
		MinIdleConns:    10,
		MaxIdleConns:    20,
		ConnMaxIdleTime: 10 * time.Minute,
		ConnMaxLifetime: 60 * time.Minute,
		PoolTimeout:     5 * time.Second,
	}
}

func DefaultClusterConfig() *ClusterConfig {
	return &ClusterConfig{
		Addrs:           []string{"localhost:7000", "localhost:7001", "localhost:7002"},
		Password:        "",
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        10,
		MinIdleConns:    5,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,
	}
}

func DefaultSentinelConfig() *SentinelConfig {
	return &SentinelConfig{
		MasterName:      "mymaster",
		SentinelAddrs:   []string{"localhost:26379", "localhost:26380", "localhost:26381"},
		Password:        "",
		DB:              0,
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        10,
		MinIdleConns:    5,
	}
}

func NewConfig(env Environment) *Config {
	cfg := &Config{
		Environment: env,
		Mode:        "standalone",
	}

	switch env {
	case EnvProduction:
		cfg.Redis = ProductionRedisConfig()
	default:
		cfg.Redis = DefaultRedisConfig()
	}

	return cfg
}

func (c *Config) Validate() error {
	if c.Mode == "" {
		return fmt.Errorf("mode cannot be empty")
	}

	switch c.Mode {
	case "standalone":
		if c.Redis == nil {
			return fmt.Errorf("redis config is required for standalone mode")
		}
	case "cluster":
		if c.Cluster == nil {
			return fmt.Errorf("cluster config is required for cluster mode")
		}
		if len(c.Cluster.Addrs) == 0 {
			return fmt.Errorf("cluster addresses cannot be empty")
		}
	case "sentinel":
		if c.Sentinel == nil {
			return fmt.Errorf("sentinel config is required for sentinel mode")
		}
		if c.Sentinel.MasterName == "" {
			return fmt.Errorf("sentinel master name cannot be empty")
		}
		if len(c.Sentinel.SentinelAddrs) == 0 {
			return fmt.Errorf("sentinel addresses cannot be empty")
		}
	default:
		return fmt.Errorf("invalid mode: %s", c.Mode)
	}

	return nil
}
