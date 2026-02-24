// Package idempotency 提供基于 Redis + 布隆过滤器的幂等性存储实现
package idempotency

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// BloomStore 基于 Redis 布隆过滤器的幂等性存储
// 使用 Redis Bitmap 实现布隆过滤器，通过多个哈希函数降低误判率
//
// 优势：
// 1. 内存占用极低：1000万条记录仅需约 12MB（误判率 1%）
// 2. 查询速度快：O(k) 时间复杂度，k 为哈希函数个数
// 3. 支持分布式：多个消费者实例共享状态
// 4. 支持按天轮转：自动清理过期数据
//
// 劣势：
// 1. 存在误判率：可能将未处理的消息误判为已处理（可通过降低误判率缓解）
// 2. 无法删除：布隆过滤器不支持删除操作
// 3. 需要预估数据量：需要提前确定预期消息数量
//
// 适用场景：
// - 超大规模消息量（千万级/亿级）
// - 对内存成本敏感
// - 可容忍极低的误判率（< 1%）
type BloomStore struct {
	client         *redis.Client   // Redis 客户端
	keyPrefix      string          // 布隆过滤器键前缀
	bitSize        uint64          // 位数组大小（bit）
	hashCount      uint            // 哈希函数个数
	expectedItems  uint64          // 预期元素数量
	falsePositive  float64         // 目标误判率
	timeout        time.Duration   // 操作超时时间
	ttl            time.Duration   // 布隆过滤器过期时间
	checkCount     int64           // 检查次数
	hitCount       int64           // 命中次数（疑似重复）
	missCount      int64           // 未命中次数（首次处理）
	circuitBreaker *CircuitBreaker // 熔断器
	enableDegrade  bool            // 是否启用降级
}

// BloomStoreConfig 布隆过滤器配置
type BloomStoreConfig struct {
	Addr          string        // Redis 地址
	Password      string        // Redis 密码
	DB            int           // Redis 数据库编号
	KeyPrefix     string        // 键前缀，默认 "bloom:"
	ExpectedItems uint64        // 预期元素数量，例如 10000000 (1000万)
	FalsePositive float64       // 目标误判率，例如 0.01 (1%)
	TTL           time.Duration // 过期时间，例如 24*time.Hour
	EnableDegrade bool          // 是否启用降级
}

// NewBloomStore 创建布隆过滤器存储实例
// 参数：
//   - config: 布隆过滤器配置
//
// 注意：
//   - 位数组大小和哈希函数个数会根据预期元素数量和误判率自动计算
//   - 误判率越低，内存占用越大
func NewBloomStore(config BloomStoreConfig) (*BloomStore, error) {
	// 设置默认值
	if config.KeyPrefix == "" {
		config.KeyPrefix = "bloom:"
	}
	if config.ExpectedItems == 0 {
		config.ExpectedItems = 10000000 // 默认 1000 万
	}
	if config.FalsePositive == 0 {
		config.FalsePositive = 0.01 // 默认 1% 误判率
	}
	if config.TTL == 0 {
		config.TTL = 24 * time.Hour // 默认 24 小时
	}

	// 创建 Redis 客户端
	client := redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		MaxRetries:   3,
		PoolSize:     10,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// 计算最优的位数组大小和哈希函数个数
	bitSize, hashCount := calculateOptimalParams(config.ExpectedItems, config.FalsePositive)

	// 创建熔断器
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          10 * time.Second,
	})

	slog.Info("Bloom filter store initialized",
		"addr", config.Addr,
		"expectedItems", config.ExpectedItems,
		"falsePositive", config.FalsePositive,
		"bitSize", bitSize,
		"hashCount", hashCount,
		"memoryMB", float64(bitSize)/(8*1024*1024))

	return &BloomStore{
		client:         client,
		keyPrefix:      config.KeyPrefix,
		bitSize:        bitSize,
		hashCount:      hashCount,
		expectedItems:  config.ExpectedItems,
		falsePositive:  config.FalsePositive,
		timeout:        time.Second,
		ttl:            config.TTL,
		circuitBreaker: cb,
		enableDegrade:  config.EnableDegrade,
	}, nil
}

// calculateOptimalParams 计算最优的布隆过滤器参数
// 公式：
//
//	m = -n * ln(p) / (ln(2)^2)  // 位数组大小
//	k = m / n * ln(2)           // 哈希函数个数
//
// 参数：
//   - n: 预期元素数量
//   - p: 目标误判率
//
// 返回：
//   - m: 位数组大小（bit）
//   - k: 哈希函数个数
func calculateOptimalParams(n uint64, p float64) (m uint64, k uint) {
	// 计算位数组大小
	m = uint64(math.Ceil(-float64(n) * math.Log(p) / math.Pow(math.Log(2), 2)))

	// 计算哈希函数个数
	k = uint(math.Ceil(float64(m) / float64(n) * math.Log(2)))

	// 确保至少有 1 个哈希函数
	if k == 0 {
		k = 1
	}

	return m, k
}

// CheckAndSet 检查元素是否存在，如果不存在则添加
// 使用布隆过滤器进行快速检查，然后添加到过滤器中
//
// 返回值：
//   - true: 首次处理（布隆过滤器判断不存在）
//   - false: 可能已处理（布隆过滤器判断存在，存在误判可能）
//   - error: 操作失败
//
// 注意：
//   - 布隆过滤器可能将未处理的消息误判为已处理（误判率可配置）
//   - 如果需要 100% 准确，建议在业务层做二次校验
func (s *BloomStore) CheckAndSet(ctx context.Context, key string) (bool, error) {
	atomic.AddInt64(&s.checkCount, 1)

	// 检查熔断器状态
	if !s.circuitBreaker.CanExecute() {
		if s.enableDegrade {
			slog.Warn("Bloom filter circuit breaker open, degrading",
				"key", key,
				"state", s.circuitBreaker.GetState())
			atomic.AddInt64(&s.missCount, 1)
			return true, nil // 降级：假设首次处理
		}
		return false, fmt.Errorf("bloom filter circuit breaker is open")
	}

	// 获取当天的布隆过滤器键（按天轮转）
	bloomKey := s.getCurrentBloomKey()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(ctx, s.timeout*time.Duration(s.hashCount+1))
	defer cancel()

	// 1. 先检查元素是否存在
	exists, err := s.bloomCheck(ctx, bloomKey, key)
	if err != nil {
		s.circuitBreaker.RecordFailure()
		if s.enableDegrade {
			slog.Warn("Bloom check failed, degrading", "key", key, "err", err)
			atomic.AddInt64(&s.missCount, 1)
			return true, nil
		}
		return false, fmt.Errorf("bloom check failed: %w", err)
	}

	if exists {
		// 布隆过滤器判断已存在（可能误判）
		s.circuitBreaker.RecordSuccess()
		atomic.AddInt64(&s.hitCount, 1)
		slog.Debug("Bloom filter hit (possible false positive)", "key", key)
		return false, nil
	}

	// 2. 元素不存在，添加到布隆过滤器
	if err := s.bloomAdd(ctx, bloomKey, key); err != nil {
		s.circuitBreaker.RecordFailure()
		if s.enableDegrade {
			slog.Warn("Bloom add failed, degrading", "key", key, "err", err)
			atomic.AddInt64(&s.missCount, 1)
			return true, nil
		}
		return false, fmt.Errorf("bloom add failed: %w", err)
	}

	// 3. 设置布隆过滤器过期时间（仅在首次创建时）
	s.client.Expire(ctx, bloomKey, s.ttl)

	s.circuitBreaker.RecordSuccess()
	atomic.AddInt64(&s.missCount, 1)
	slog.Debug("Bloom filter miss, element added", "key", key)
	return true, nil
}

// bloomCheck 检查元素是否在布隆过滤器中
// 使用多个哈希函数计算位置，检查所有位是否都为 1
func (s *BloomStore) bloomCheck(ctx context.Context, bloomKey, key string) (bool, error) {
	// 使用 Pipeline 批量检查所有哈希位置
	pipe := s.client.Pipeline()
	cmds := make([]*redis.IntCmd, s.hashCount)

	for i := uint(0); i < s.hashCount; i++ {
		offset := s.hash(key, i)
		cmds[i] = pipe.GetBit(ctx, bloomKey, int64(offset))
	}

	// 执行 Pipeline
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return false, fmt.Errorf("pipeline exec failed: %w", err)
	}

	// 检查所有位是否都为 1
	for i := uint(0); i < s.hashCount; i++ {
		bit, err := cmds[i].Result()
		if err != nil && err != redis.Nil {
			return false, fmt.Errorf("getbit failed: %w", err)
		}
		if bit == 0 {
			// 只要有一个位为 0，元素肯定不存在
			return false, nil
		}
	}

	// 所有位都为 1，元素可能存在（可能误判）
	return true, nil
}

// bloomAdd 将元素添加到布隆过滤器
// 使用多个哈希函数计算位置，将所有位设置为 1
func (s *BloomStore) bloomAdd(ctx context.Context, bloomKey, key string) error {
	// 使用 Pipeline 批量设置所有哈希位置
	pipe := s.client.Pipeline()

	for i := uint(0); i < s.hashCount; i++ {
		offset := s.hash(key, i)
		pipe.SetBit(ctx, bloomKey, int64(offset), 1)
	}

	// 执行 Pipeline
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pipeline exec failed: %w", err)
	}

	return nil
}

// hash 计算第 i 个哈希函数的值
// 使用双重哈希技巧：h(i) = (h1 + i*h2) mod m
// 参考：https://en.wikipedia.org/wiki/Double_hashing
func (s *BloomStore) hash(key string, i uint) uint64 {
	// 使用 FNV-1a 算法计算两个哈希值
	h1 := fnv1a64(key)
	h2 := fnv1a64(key + "_salt")

	// 双重哈希
	hash := (h1 + uint64(i)*h2) % s.bitSize
	return hash
}

// fnv1a64 FNV-1a 64位哈希算法
func fnv1a64(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)

	hash := uint64(offset64)
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime64
	}
	return hash
}

// getCurrentBloomKey 获取当天的布隆过滤器键
// 按天轮转，每天使用不同的布隆过滤器，自动清理过期数据
func (s *BloomStore) getCurrentBloomKey() string {
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s%s", s.keyPrefix, date)
}

// Close 关闭 Redis 连接
func (s *BloomStore) Close() error {
	// 输出统计信息
	check, hit, miss := s.GetStats()
	falsePositiveRate := 0.0
	if check > 0 {
		falsePositiveRate = float64(hit) / float64(check) * 100
	}

	slog.Info("Bloom filter store closing",
		"totalChecks", check,
		"hits", hit,
		"misses", miss,
		"actualFalsePositiveRate", fmt.Sprintf("%.4f%%", falsePositiveRate),
		"targetFalsePositiveRate", fmt.Sprintf("%.4f%%", s.falsePositive*100),
		"circuitState", s.circuitBreaker.GetState())

	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// GetStats 获取统计信息
func (s *BloomStore) GetStats() (check, hit, miss int64) {
	return atomic.LoadInt64(&s.checkCount),
		atomic.LoadInt64(&s.hitCount),
		atomic.LoadInt64(&s.missCount)
}

// GetMemoryUsage 获取内存占用估算（字节）
func (s *BloomStore) GetMemoryUsage() uint64 {
	return s.bitSize / 8 // bit 转 byte
}

// GetMemoryUsageMB 获取内存占用估算（MB）
func (s *BloomStore) GetMemoryUsageMB() float64 {
	return float64(s.bitSize) / (8 * 1024 * 1024)
}
