# Redis 服务最佳实践指南

本文档提供使用Redis服务的最佳实践、性能优化建议和常见问题解决方案。

## 目录

- [架构设计](#架构设计)
- [性能优化](#性能优化)
- [可靠性保障](#可靠性保障)
- [安全性](#安全性)
- [监控告警](#监控告警)
- [故障排查](#故障排查)
- [生产环境部署](#生产环境部署)

## 架构设计

### 1. 分层架构

```
┌─────────────────────────────────────┐
│         Application Layer           │  业务逻辑层
├─────────────────────────────────────┤
│         Service Layer               │  服务层（21个服务）
├─────────────────────────────────────┤
│         Repository Layer            │  数据访问层
├─────────────────────────────────────┤
│         Client Layer                │  Redis客户端层
├─────────────────────────────────────┤
│    Infrastructure (Logger/Metrics)  │  基础设施层
└─────────────────────────────────────┘
```

**优势**：
- 职责分离，易于维护
- 便于单元测试
- 支持依赖注入
- 可扩展性强

### 2. 服务选择指南

| 场景 | 推荐服务 | 数据结构 |
|------|---------|---------|
| 对象缓存 | StringCacheService | String |
| 计数统计 | CounterService | String |
| 分布式锁 | LockService | String |
| 用户会话 | SessionService | String |
| 用户资料 | UserInfoService | Hash |
| 购物车 | ShoppingCartService | Hash |
| 消息队列 | MessageQueueService | List |
| 时间线 | LatestMessagesService | List |
| 去重检查 | DeduplicationService | Set |
| 抽奖系统 | LotteryService | Set |
| 社交关系 | SocialGraphService | Set |
| 排行榜 | LeaderboardService | ZSet |
| 延迟任务 | DelayQueueService | ZSet |
| 任务调度 | PriorityQueueService | ZSet |
| 签到打卡 | SignInService | Bitmap |
| 在线状态 | OnlineStatusService | Bitmap |
| 活跃统计 | UserActivityService | Bitmap |
| 事件流 | MessageStreamService | Stream |
| 消息消费 | ConsumerGroupService | Stream |

### 3. 键命名规范

```go
// 推荐的命名格式
{namespace}:{type}:{id}:{field}

// 示例
myapp:user:1001                    // 用户信息
myapp:cache:product:1001           // 商品缓存
myapp:counter:page:views           // 页面访问计数
myapp:lock:order:create:1001       // 订单创建锁
myapp:session:token:abc123         // 会话token
myapp:cart:user:1001               // 购物车
myapp:queue:email                  // 邮件队列
myapp:leaderboard:game:season1     // 游戏排行榜
```

**命名原则**：
- 使用冒号分隔层级
- 保持一致的命名风格
- 避免使用特殊字符
- 长度适中，易于理解

## 性能优化

### 1. 批量操作

```go
// ❌ 避免：循环单次操作
for _, key := range keys {
    cache.Get(ctx, key, &result)
}

// ✅ 推荐：使用批量操作
results, err := cache.MGet(ctx, keys)
```

**性能提升**：批量操作可减少网络往返次数，提升10-100倍性能。

### 2. Pipeline 使用

```go
// 对于大量独立操作，使用 Pipeline
pipe := client.GetClient().Pipeline()

for _, item := range items {
    pipe.Set(ctx, item.Key, item.Value, ttl)
}

_, err := pipe.Exec(ctx)
```

### 3. 合理设置 TTL

```go
// 根据数据特性设置过期时间
cache.Set(ctx, "hot:data", data, 5*time.Minute)      // 热点数据，短TTL
cache.Set(ctx, "user:profile", data, 1*time.Hour)    // 用户资料，中等TTL
cache.Set(ctx, "config:static", data, 24*time.Hour)  // 静态配置，长TTL
```

**建议**：
- 热点数据：5-15分钟
- 用户数据：1-2小时
- 配置数据：12-24小时
- 避免永不过期（可能导致内存泄漏）

### 4. 缓存预热

```go
func warmupCache(ctx context.Context, cache service.StringCacheService) error {
    // 预加载热点数据
    hotProducts, err := loadHotProducts()
    if err != nil {
        return err
    }
    
    for _, product := range hotProducts {
        cache.Set(ctx, fmt.Sprintf("product:%s", product.ID), product, 1*time.Hour)
    }
    
    return nil
}
```

### 5. 避免大Key

```go
// ❌ 避免：单个Key存储大量数据
cache.Set(ctx, "all:users", allUsers, ttl) // 可能包含数万条记录

// ✅ 推荐：分片存储
for i, chunk := range chunkUsers(allUsers, 1000) {
    cache.Set(ctx, fmt.Sprintf("users:chunk:%d", i), chunk, ttl)
}
```

**大Key标准**：
- String：> 10KB
- Hash：> 5000个field
- List：> 10000个元素
- Set：> 10000个成员
- ZSet：> 10000个成员

### 6. 连接池配置

```go
// config/config.go
type RedisConfig struct {
    PoolSize:     100,  // 连接池大小
    MinIdleConns: 10,   // 最小空闲连接
    MaxRetries:   3,    // 最大重试次数
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
}
```

## 可靠性保障

### 1. 错误处理

```go
import "gcode/redis/pkg/errors"

func handleRedisError(err error) {
    if err == nil {
        return
    }
    
    // 类型断言获取详细错误信息
    if redisErr, ok := err.(*errors.RedisError); ok {
        switch redisErr.Code {
        case errors.ErrCodeNotFound:
            // 缓存未命中，从数据库加载
            loadFromDB()
        case errors.ErrCodeTimeout:
            // 超时，记录日志并降级
            log.Warn("Redis timeout, using fallback")
            useFallback()
        case errors.ErrCodeLockFailed:
            // 获取锁失败，返回繁忙状态
            return errors.New("resource busy")
        default:
            // 其他错误，记录并告警
            log.Error("Redis error", logger.ErrorField(err))
            alertOps(err)
        }
    }
}
```

### 2. 降级策略

```go
type CacheService struct {
    redis  service.StringCacheService
    local  *sync.Map // 本地缓存作为降级
}

func (s *CacheService) Get(ctx context.Context, key string) (interface{}, error) {
    // 尝试从Redis获取
    var result interface{}
    err := s.redis.Get(ctx, key, &result)
    
    if err == nil {
        return result, nil
    }
    
    // Redis失败，使用本地缓存
    if val, ok := s.local.Load(key); ok {
        log.Warn("Using local cache fallback", logger.String("key", key))
        return val, nil
    }
    
    // 本地缓存也没有，从数据库加载
    result, err = s.loadFromDB(key)
    if err != nil {
        return nil, err
    }
    
    // 更新本地缓存
    s.local.Store(key, result)
    
    return result, nil
}
```

### 3. 重试机制

```go
// 使用内置的重试器
retryer := retry.NewRetryer(retry.Config{
    MaxAttempts:     3,
    InitialInterval: 100 * time.Millisecond,
    MaxInterval:     1 * time.Second,
    Multiplier:      2.0,
})

err := retryer.Do(ctx, "cache_set", func() error {
    return cache.Set(ctx, key, value, ttl)
})
```

### 4. 熔断器

```go
type CircuitBreaker struct {
    failureCount    int
    successCount    int
    lastFailureTime time.Time
    state           string // "closed", "open", "half-open"
    threshold       int
    timeout         time.Duration
    mu              sync.Mutex
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    cb.mu.Lock()
    
    if cb.state == "open" {
        if time.Since(cb.lastFailureTime) > cb.timeout {
            cb.state = "half-open"
        } else {
            cb.mu.Unlock()
            return errors.New("circuit breaker is open")
        }
    }
    
    cb.mu.Unlock()
    
    err := fn()
    
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    if err != nil {
        cb.failureCount++
        cb.lastFailureTime = time.Now()
        
        if cb.failureCount >= cb.threshold {
            cb.state = "open"
        }
        return err
    }
    
    cb.successCount++
    if cb.state == "half-open" && cb.successCount >= 3 {
        cb.state = "closed"
        cb.failureCount = 0
    }
    
    return nil
}
```

### 5. 健康检查

```go
// 定期检查Redis健康状态
func startHealthCheck(app *app.App) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        status := app.GetHealthChecker().GetStatus()
        
        if status.Status != "healthy" {
            log.Error("Redis unhealthy",
                logger.String("status", status.Status),
                logger.Duration("latency", status.Latency))
            
            // 触发告警
            alertOps("Redis health check failed")
        }
    }
}
```

## 安全性

### 1. 访问控制

```go
// 使用Redis ACL（Redis 6.0+）
// redis.conf
user default on nopass ~* &* +@all
user app_user on >strong_password ~myapp:* +@read +@write -@dangerous
```

### 2. 数据加密

```go
import "crypto/aes"
import "encoding/base64"

type EncryptedCache struct {
    cache service.StringCacheService
    key   []byte
}

func (e *EncryptedCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
    // 序列化
    data, err := json.Marshal(value)
    if err != nil {
        return err
    }
    
    // 加密
    encrypted, err := e.encrypt(data)
    if err != nil {
        return err
    }
    
    // 存储
    return e.cache.Set(ctx, key, encrypted, ttl)
}

func (e *EncryptedCache) encrypt(data []byte) (string, error) {
    // AES加密实现
    // ...
    return base64.StdEncoding.EncodeToString(encrypted), nil
}
```

### 3. 敏感信息脱敏

```go
func sanitizeLog(data map[string]interface{}) map[string]interface{} {
    sensitive := []string{"password", "token", "credit_card", "ssn"}
    
    result := make(map[string]interface{})
    for k, v := range data {
        if contains(sensitive, k) {
            result[k] = "***"
        } else {
            result[k] = v
        }
    }
    
    return result
}
```

### 4. 防止注入攻击

```go
import "regexp"

func validateKey(key string) error {
    // 只允许字母、数字、冒号、下划线、连字符
    pattern := regexp.MustCompile(`^[a-zA-Z0-9:_-]+$`)
    
    if !pattern.MatchString(key) {
        return errors.New("invalid key format")
    }
    
    if len(key) > 256 {
        return errors.New("key too long")
    }
    
    return nil
}
```

## 监控告警

### 1. 关键指标

```go
type RedisMetrics struct {
    // 性能指标
    OperationCount    int64
    SuccessRate       float64
    AverageDuration   time.Duration
    P95Duration       time.Duration
    P99Duration       time.Duration
    
    // 连接指标
    ActiveConnections int
    IdleConnections   int
    WaitingClients    int
    
    // 内存指标
    UsedMemory        int64
    MaxMemory         int64
    MemoryUsageRate   float64
    
    // 命中率
    CacheHitRate      float64
    CacheMissRate     float64
}

func collectMetrics(app *app.App) *RedisMetrics {
    stats := app.GetMetrics().GetStats()
    
    metrics := &RedisMetrics{}
    
    for _, opStats := range stats {
        metrics.OperationCount += opStats.Count
        metrics.SuccessRate = float64(opStats.SuccessCount) / float64(opStats.Count)
        metrics.AverageDuration = opStats.AvgDuration
    }
    
    return metrics
}
```

### 2. 告警规则

```yaml
alerts:
  - name: HighErrorRate
    condition: error_rate > 0.05
    duration: 5m
    severity: critical
    message: "Redis error rate exceeds 5%"
    
  - name: SlowOperations
    condition: p99_duration > 1s
    duration: 5m
    severity: warning
    message: "Redis P99 latency exceeds 1 second"
    
  - name: LowHitRate
    condition: cache_hit_rate < 0.8
    duration: 10m
    severity: warning
    message: "Cache hit rate below 80%"
    
  - name: HighMemoryUsage
    condition: memory_usage > 0.9
    duration: 5m
    severity: critical
    message: "Redis memory usage exceeds 90%"
    
  - name: ConnectionPoolExhausted
    condition: waiting_clients > 10
    duration: 1m
    severity: critical
    message: "Redis connection pool exhausted"
```

### 3. 日志记录

```go
// 结构化日志
log.Info("Cache operation",
    logger.String("operation", "get"),
    logger.String("key", key),
    logger.Duration("duration", duration),
    logger.Bool("hit", hit),
    logger.String("trace_id", traceID))

// 慢查询日志
if duration > 100*time.Millisecond {
    log.Warn("Slow Redis operation",
        logger.String("operation", operation),
        logger.String("key", key),
        logger.Duration("duration", duration))
}

// 错误日志
log.Error("Redis operation failed",
    logger.String("operation", operation),
    logger.String("key", key),
    logger.ErrorField(err),
    logger.String("stack", string(debug.Stack())))
```

### 4. 监控面板

推荐使用 Grafana + Prometheus 构建监控面板：

```
┌─────────────────────────────────────────┐
│  Redis 性能监控                          │
├─────────────────────────────────────────┤
│  QPS: 10,000/s    ↑ 5%                  │
│  延迟: P99 50ms   ↓ 10%                 │
│  错误率: 0.01%    ↔ 0%                  │
│  命中率: 95%      ↑ 2%                  │
├─────────────────────────────────────────┤
│  [QPS趋势图]                            │
│  [延迟分布图]                           │
│  [错误率趋势图]                         │
│  [内存使用图]                           │
└─────────────────────────────────────────┘
```

## 故障排查

### 1. 常见问题

#### 问题1：缓存穿透

**现象**：大量请求查询不存在的数据，导致数据库压力大

**解决方案**：
```go
// 方案1：缓存空值
result, err := cache.Get(ctx, key, &data)
if err == errors.ErrCodeNotFound {
    // 从数据库查询
    data, err = db.Query(key)
    if err != nil {
        // 缓存空值，防止穿透
        cache.Set(ctx, key, nil, 5*time.Minute)
        return nil, err
    }
}

// 方案2：布隆过滤器
if !bloomFilter.MightContain(key) {
    return nil, errors.New("key not exists")
}
```

#### 问题2：缓存雪崩

**现象**：大量缓存同时过期，导致数据库压力激增

**解决方案**：
```go
// 方案1：随机过期时间
baseT TL := 1 * time.Hour
randomTTL := baseTTL + time.Duration(rand.Intn(300))*time.Second
cache.Set(ctx, key, value, randomTTL)

// 方案2：永不过期 + 异步更新
go func() {
    for {
        time.Sleep(50 * time.Minute)
        refreshCache(key)
    }
}()
```

#### 问题3：热Key问题

**现象**：单个Key访问量过大，导致单点压力

**解决方案**：
```go
// 方案1：本地缓存
localCache := &sync.Map{}

func getWithLocalCache(key string) (interface{}, error) {
    // 先查本地缓存
    if val, ok := localCache.Load(key); ok {
        return val, nil
    }
    
    // 再查Redis
    var result interface{}
    err := cache.Get(ctx, key, &result)
    if err == nil {
        localCache.Store(key, result)
    }
    
    return result, err
}

// 方案2：Key分片
func getShardedKey(key string, shardCount int) string {
    shard := hash(key) % shardCount
    return fmt.Sprintf("%s:shard:%d", key, shard)
}
```

#### 问题4：大Key问题

**现象**：单个Key占用内存过大，影响性能

**解决方案**：
```go
// 方案1：分片存储
func saveInChunks(key string, data []byte, chunkSize int) error {
    chunks := splitIntoChunks(data, chunkSize)
    
    for i, chunk := range chunks {
        chunkKey := fmt.Sprintf("%s:chunk:%d", key, i)
        if err := cache.Set(ctx, chunkKey, chunk, ttl); err != nil {
            return err
        }
    }
    
    // 保存元数据
    meta := map[string]interface{}{
        "total_chunks": len(chunks),
        "chunk_size": chunkSize,
    }
    return cache.Set(ctx, key+":meta", meta, ttl)
}

// 方案2：压缩存储
import "compress/gzip"

func compressAndSave(key string, data []byte) error {
    compressed := compress(data)
    return cache.Set(ctx, key, compressed, ttl)
}
```

### 2. 性能分析

```go
// 使用 pprof 分析性能
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()

// 访问 http://localhost:6060/debug/pprof/
// 分析 CPU、内存、goroutine 等
```

### 3. 慢查询分析

```go
// 记录慢操作
type SlowLogger struct {
    threshold time.Duration
}

func (s *SlowLogger) LogIfSlow(operation string, duration time.Duration) {
    if duration > s.threshold {
        log.Warn("Slow operation detected",
            logger.String("operation", operation),
            logger.Duration("duration", duration),
            logger.Duration("threshold", s.threshold))
    }
}
```

## 生产环境部署

### 1. 部署架构

```
┌──────────────────────────────────────────┐
│  Application Servers (多实例)             │
├──────────────────────────────────────────┤
│  Load Balancer                           │
├──────────────────────────────────────────┤
│  Redis Cluster / Sentinel                │
│  ┌────────┐  ┌────────┐  ┌────────┐     │
│  │Master  │  │Master  │  │Master  │     │
│  │Slave   │  │Slave   │  │Slave   │     │
│  └────────┘  └────────┘  └────────┘     │
└──────────────────────────────────────────┘
```

### 2. 配置建议

```yaml
# production.yaml
redis:
  mode: cluster  # standalone, cluster, sentinel
  addresses:
    - redis-node1:6379
    - redis-node2:6379
    - redis-node3:6379
  password: ${REDIS_PASSWORD}
  db: 0
  pool_size: 100
  min_idle_conns: 20
  max_retries: 3
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
  
logging:
  level: INFO
  format: json
  output: /var/log/app/redis.log
  
metrics:
  enabled: true
  port: 9090
  path: /metrics
```

### 3. 容量规划

```go
// 估算内存需求
type CapacityPlanner struct {
    avgKeySize      int64  // 平均Key大小
    avgValueSize    int64  // 平均Value大小
    totalKeys       int64  // 预期Key数量
    replicationFactor int  // 复制因子
}

func (cp *CapacityPlanner) EstimateMemory() int64 {
    // 单个Key-Value占用内存
    perKeyMemory := cp.avgKeySize + cp.avgValueSize + 96 // 96字节开销
    
    // 总内存需求
    totalMemory := perKeyMemory * cp.totalKeys
    
    // 考虑复制
    totalMemory *= int64(cp.replicationFactor)
    
    // 预留20%缓冲
    totalMemory = int64(float64(totalMemory) * 1.2)
    
    return totalMemory
}
```

### 4. 备份策略

```bash
# RDB备份（定时快照）
save 900 1      # 900秒内至少1个key变化
save 300 10     # 300秒内至少10个key变化
save 60 10000   # 60秒内至少10000个key变化

# AOF备份（实时持久化）
appendonly yes
appendfsync everysec  # 每秒同步一次

# 备份脚本
#!/bin/bash
DATE=$(date +%Y%m%d_%H%M%S)
redis-cli --rdb /backup/dump_${DATE}.rdb
find /backup -name "dump_*.rdb" -mtime +7 -delete
```

### 5. 灾难恢复

```go
// 主从切换
func failover(ctx context.Context, sentinel *redis.SentinelClient) error {
    // 触发主从切换
    err := sentinel.Failover(ctx, "mymaster").Err()
    if err != nil {
        return err
    }
    
    // 等待切换完成
    time.Sleep(5 * time.Second)
    
    // 验证新主节点
    master, err := sentinel.GetMasterAddrByName(ctx, "mymaster").Result()
    if err != nil {
        return err
    }
    
    log.Info("Failover completed", logger.Strings("new_master", master))
    return nil
}
```

## 总结

本文档涵盖了Redis服务的核心最佳实践：

1. **架构设计**：分层架构、服务选择、命名规范
2. **性能优化**：批量操作、Pipeline、TTL设置、连接池
3. **可靠性**：错误处理、降级策略、重试机制、熔断器
4. **安全性**：访问控制、数据加密、防注入
5. **监控告警**：关键指标、告警规则、日志记录
6. **故障排查**：常见问题、性能分析、慢查询
7. **生产部署**：部署架构、配置建议、容量规划、备份恢复

遵循这些最佳实践，可以构建高性能、高可用、安全可靠的Redis服务系统。
