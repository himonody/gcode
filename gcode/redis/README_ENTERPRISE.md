# Redis 企业级架构实现

这是一个生产级别的 Redis 应用框架，采用分层架构、依赖注入、接口抽象等企业级设计模式。

## 🏗️ 架构设计

### 分层架构

```
┌─────────────────────────────────────────────────────────┐
│                     Application Layer                    │
│                    (app/app.go)                          │
│  - 应用生命周期管理                                        │
│  - 依赖注入容器                                           │
│  - 优雅关闭                                               │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│                     Service Layer                        │
│              (internal/service/*.go)                     │
│  - 业务逻辑封装                                           │
│  - 参数验证                                               │
│  - 业务流程编排                                           │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│                   Repository Layer                       │
│            (internal/repository/*.go)                    │
│  - 数据访问抽象                                           │
│  - Redis 操作封装                                         │
│  - 错误处理                                               │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│                     Client Layer                         │
│                  (client/client.go)                      │
│  - 连接管理                                               │
│  - 连接池配置                                             │
│  - 健康检查                                               │
└─────────────────────────────────────────────────────────┘
                            │
┌─────────────────────────────────────────────────────────┐
│                  Infrastructure Layer                    │
│                    (pkg/*)                               │
│  - Logger (日志)                                          │
│  - Metrics (指标)                                         │
│  - Errors (错误处理)                                      │
│  - Health (健康检查)                                      │
│  - Retry (重试机制)                                       │
└─────────────────────────────────────────────────────────┘
```

## 📁 项目结构

```
redis/
├── app/                          # 应用层
│   └── app.go                   # 应用启动、依赖注入、生命周期管理
├── client/                       # 客户端层
│   └── client.go                # Redis 客户端封装（支持单机/集群/哨兵）
├── config/                       # 配置层
│   └── config.go                # 配置管理（多环境支持）
├── internal/                     # 内部实现
│   ├── repository/              # 数据访问层
│   │   ├── cache_repository.go  # 缓存仓储
│   │   └── lock_repository.go   # 锁仓储
│   └── service/                 # 业务服务层
│       ├── cache_service.go     # 缓存服务
│       └── lock_service.go      # 分布式锁服务
├── pkg/                         # 公共包
│   ├── errors/                  # 错误处理
│   │   └── errors.go           # 统一错误定义
│   ├── health/                  # 健康检查
│   │   └── health.go           # 健康检查器
│   ├── logger/                  # 日志
│   │   └── logger.go           # 结构化日志
│   ├── metrics/                 # 指标
│   │   └── metrics.go          # 性能指标收集
│   └── retry/                   # 重试
│       └── retry.go            # 重试机制
├── main.go                      # 程序入口
├── go.mod                       # Go 模块
└── README_ENTERPRISE.md         # 企业级文档
```

## 🎯 核心特性

### 1. 配置管理
- ✅ 多环境支持（Development/Staging/Production）
- ✅ 环境变量覆盖
- ✅ 配置验证
- ✅ 多种部署模式（单机/集群/哨兵）

### 2. 连接管理
- ✅ 连接池优化
- ✅ 自动重连
- ✅ 超时控制
- ✅ 健康检查

### 3. 错误处理
- ✅ 统一错误码
- ✅ 错误包装
- ✅ 上下文信息
- ✅ 错误分类

### 4. 日志系统
- ✅ 结构化日志
- ✅ 日志级别控制
- ✅ 字段化输出
- ✅ 上下文传递

### 5. 监控指标
- ✅ 操作计数
- ✅ 延迟统计
- ✅ 成功率追踪
- ✅ 错误分类统计

### 6. 可靠性
- ✅ 重试机制
- ✅ 超时控制
- ✅ 优雅关闭
- ✅ 资源清理

### 7. 可测试性
- ✅ 接口抽象
- ✅ 依赖注入
- ✅ Mock 友好
- ✅ 单元测试支持

## 🚀 快速开始

### 安装依赖

```bash
go mod tidy
```

### 基本使用

```go
package main

import (
    "context"
    "time"
    
    "gcode/redis/app"
    "gcode/redis/config"
    "gcode/redis/pkg/logger"
)

func main() {
    // 1. 创建配置
    cfg := config.NewConfig(config.EnvProduction)
    
    // 2. 创建日志
    log := logger.NewLogger(logger.INFO)
    
    // 3. 创建应用
    application, err := app.NewApplication(cfg, app.WithLogger(log))
    if err != nil {
        log.Fatal("Failed to create application", logger.Error(err))
    }
    
    // 4. 使用服务
    ctx := context.Background()
    cacheService := application.GetCacheService()
    
    // 缓存操作
    err = cacheService.Set(ctx, "key", "value", 10*time.Minute)
    
    // 5. 运行应用（带优雅关闭）
    if err := application.Run(); err != nil {
        log.Fatal("Application error", logger.Error(err))
    }
}
```

## 📖 使用示例

### 缓存服务

```go
// 基本操作
cacheService := app.GetCacheService()

// 设置缓存
type User struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

user := User{ID: "1001", Name: "张三"}
err := cacheService.Set(ctx, "user:1001", user, 10*time.Minute)

// 获取缓存
var cachedUser User
err = cacheService.Get(ctx, "user:1001", &cachedUser)

// 缓存穿透保护 - Remember 模式
result, err := cacheService.Remember(ctx, "expensive:key", 5*time.Minute, func() (interface{}, error) {
    // 执行昂贵的计算或数据库查询
    return fetchFromDatabase()
})

// 缓存回退
err = cacheService.GetWithFallback(ctx, "key", &dest, func() (interface{}, error) {
    return fetchFromSource()
}, 10*time.Minute)
```

### 分布式锁

```go
lockService := app.GetLockService()

// 基本锁操作
lock, err := lockService.Lock(ctx, "order:12345", 30*time.Second)
if err != nil {
    // 处理锁获取失败
}
defer lock.Release(ctx)

// 执行临界区代码
processOrder()

// 自动管理锁
err = lockService.WithLock(ctx, "inventory:product:5001", 10*time.Second, func(ctx context.Context) error {
    // 自动获取和释放锁
    return updateInventory()
})

// 重试获取锁
lock, err = lockService.TryLock(ctx, "resource", 30*time.Second, 3, 100*time.Millisecond)
```

## ⚙️ 配置说明

### 环境变量

```bash
# 应用环境
export APP_ENV=production          # development/staging/production

# Redis 配置
export REDIS_HOST=localhost
export REDIS_PORT=6379
export REDIS_PASSWORD=your_password
export REDIS_DB=0
```

### 配置文件

```go
// 开发环境
cfg := config.NewConfig(config.EnvDevelopment)

// 生产环境
cfg := config.NewConfig(config.EnvProduction)

// 自定义配置
cfg.Redis.PoolSize = 100
cfg.Redis.MaxRetries = 5
cfg.Redis.ReadTimeout = 5 * time.Second
```

### 集群模式

```go
cfg := config.NewConfig(config.EnvProduction)
cfg.Mode = "cluster"
cfg.Cluster = &config.ClusterConfig{
    Addrs: []string{
        "node1:6379",
        "node2:6379",
        "node3:6379",
    },
    Password: "password",
    PoolSize: 50,
}
```

### 哨兵模式

```go
cfg := config.NewConfig(config.EnvProduction)
cfg.Mode = "sentinel"
cfg.Sentinel = &config.SentinelConfig{
    MasterName: "mymaster",
    SentinelAddrs: []string{
        "sentinel1:26379",
        "sentinel2:26379",
        "sentinel3:26379",
    },
    Password: "password",
}
```

## 📊 监控与指标

### 获取指标

```go
stats := application.GetMetrics()

fmt.Printf("Total Operations: %d\n", stats.TotalOperations)
fmt.Printf("Success Rate: %.2f%%\n", 
    float64(stats.TotalSuccess)/float64(stats.TotalOperations)*100)

for opName, opStats := range stats.Operations {
    fmt.Printf("%s: count=%d, avg_duration=%v\n", 
        opName, opStats.Count, opStats.AvgDuration)
}
```

### 健康检查

```go
healthStatus := application.GetHealthStatus()

fmt.Printf("Status: %s\n", healthStatus.Status)
fmt.Printf("Message: %s\n", healthStatus.Message)
fmt.Printf("Latency: %v\n", healthStatus.Latency)
```

## 🔒 错误处理

### 错误类型

```go
import rediserr "gcode/redis/pkg/errors"

// 检查错误类型
if rediserr.IsNotFound(err) {
    // 处理键不存在
}

if rediserr.IsTimeout(err) {
    // 处理超时
}

if rediserr.IsConnectionError(err) {
    // 处理连接错误
}

// 自定义错误
err := rediserr.New(rediserr.ErrCodeInvalidInput, "invalid parameter")
err = rediserr.Wrap(originalErr, rediserr.ErrCodeInternal, "operation failed")
```

## 🔄 重试机制

```go
import "gcode/redis/pkg/retry"

// 配置重试
retryConfig := &retry.Config{
    MaxAttempts:     5,
    InitialInterval: 100 * time.Millisecond,
    MaxInterval:     5 * time.Second,
    Multiplier:      2.0,
}

retryer := retry.NewRetryer(retryConfig, log)

// 执行带重试的操作
err := retryer.Do(ctx, "operation_name", func() error {
    return performOperation()
})

// 带返回值的重试
result, err := retryer.DoWithResult(ctx, "fetch_data", func() (interface{}, error) {
    return fetchData()
})
```

## 🏥 健康检查

```go
import "gcode/redis/pkg/health"

// 创建健康检查器
checker := health.NewChecker(client, log, 30*time.Second, 5*time.Second)
checker.Start()
defer checker.Stop()

// 获取健康状态
status := checker.GetStatus()
```

## 📝 日志最佳实践

```go
import "gcode/redis/pkg/logger"

log := logger.NewLogger(logger.INFO)

// 结构化日志
log.Info("User logged in",
    logger.String("user_id", "1001"),
    logger.String("ip", "192.168.1.1"),
    logger.Duration("duration", time.Since(start)))

log.Error("Database query failed",
    logger.Error(err),
    logger.String("query", sql),
    logger.Int("retry_count", retries))

// 带上下文的日志
contextLog := log.With(
    logger.String("request_id", requestID),
    logger.String("user_id", userID))

contextLog.Info("Processing request")
contextLog.Debug("Cache hit")
```

## 🎯 生产环境最佳实践

### 1. 连接池配置

```go
// 生产环境推荐配置
cfg.Redis.PoolSize = 50              // 根据并发量调整
cfg.Redis.MinIdleConns = 10          // 保持最小空闲连接
cfg.Redis.ConnMaxLifetime = 60 * time.Minute
cfg.Redis.ConnMaxIdleTime = 10 * time.Minute
cfg.Redis.PoolTimeout = 5 * time.Second
```

### 2. 超时设置

```go
cfg.Redis.DialTimeout = 10 * time.Second
cfg.Redis.ReadTimeout = 5 * time.Second
cfg.Redis.WriteTimeout = 5 * time.Second
```

### 3. 重试策略

```go
cfg.Redis.MaxRetries = 5
cfg.Redis.MinRetryBackoff = 16 * time.Millisecond
cfg.Redis.MaxRetryBackoff = 1024 * time.Millisecond
```

### 4. 键命名规范

```go
// 使用 CacheKeyBuilder
builder := service.NewCacheKeyBuilder("myapp")

userKey := builder.UserKey("1001")        // myapp:user:1001
sessionKey := builder.SessionKey("abc")   // myapp:session:abc
productKey := builder.ProductKey("5001")  // myapp:product:5001
```

### 5. 优雅关闭

```go
// 应用会自动处理 SIGTERM 和 SIGINT
// 30秒超时等待所有操作完成
application.Run()
```

## 🧪 测试

### 单元测试示例

```go
package service_test

import (
    "context"
    "testing"
    "time"
    
    "gcode/redis/internal/service"
    "gcode/redis/pkg/logger"
)

type mockCacheRepo struct{}

func (m *mockCacheRepo) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
    return nil
}

func TestCacheService(t *testing.T) {
    repo := &mockCacheRepo{}
    log := logger.NewLogger(logger.DEBUG)
    svc := service.NewCacheService(repo, log)
    
    ctx := context.Background()
    err := svc.Set(ctx, "test", "value", time.Minute)
    
    if err != nil {
        t.Errorf("Expected no error, got %v", err)
    }
}
```

## 🔐 安全建议

1. **密码管理**: 使用环境变量或密钥管理服务
2. **TLS/SSL**: 生产环境启用加密连接
3. **访问控制**: 使用 Redis ACL 限制权限
4. **网络隔离**: 限制 Redis 访问来源
5. **审计日志**: 记录所有关键操作

## 📈 性能优化

1. **Pipeline**: 批量操作使用 Pipeline
2. **连接复用**: 合理配置连接池
3. **序列化**: 选择高效的序列化方式
4. **过期策略**: 合理设置 TTL
5. **监控告警**: 实时监控关键指标

## 🐛 故障排查

### 连接问题

```bash
# 检查 Redis 连接
redis-cli -h localhost -p 6379 ping

# 查看连接数
redis-cli info clients
```

### 性能问题

```go
// 查看连接池统计
stats := client.GetClient().PoolStats()
fmt.Printf("Total Conns: %d\n", stats.TotalConns)
fmt.Printf("Idle Conns: %d\n", stats.IdleConns)
fmt.Printf("Timeouts: %d\n", stats.Timeouts)
```

## 📚 参考资料

- [Redis 官方文档](https://redis.io/documentation)
- [go-redis 文档](https://redis.uptrace.dev/)
- [Go 并发模式](https://go.dev/blog/pipelines)
- [微服务设计模式](https://microservices.io/patterns/)

## 📄 License

MIT License
