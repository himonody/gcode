package idempotency

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState 熔断器状态
type CircuitState int32

const (
	// StateClosed 关闭状态：正常运行，允许请求通过
	StateClosed CircuitState = iota
	// StateOpen 打开状态：熔断开启，拒绝请求
	StateOpen
	// StateHalfOpen 半开状态：尝试恢复，允许部分请求通过
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker 熔断器实现
// 用于保护 Redis 等外部依赖，避免故障扩散
//
// 工作原理：
// 1. 关闭状态（Closed）：正常运行，记录失败次数
// 2. 失败次数达到阈值 -> 打开状态（Open）
// 3. 打开状态：拒绝所有请求，超时后进入半开状态
// 4. 半开状态（Half-Open）：允许部分请求测试服务是否恢复
// 5. 测试成功次数达到阈值 -> 关闭状态
// 6. 测试失败 -> 重新打开
type CircuitBreaker struct {
	state            int32         // 当前状态（原子操作）
	failureCount     int64         // 失败计数
	successCount     int64         // 成功计数（半开状态使用）
	lastFailureTime  time.Time     // 最后失败时间
	lastStateChange  time.Time     // 最后状态变更时间
	failureThreshold int           // 失败阈值
	successThreshold int           // 成功阈值（恢复）
	timeout          time.Duration // 超时时间
	mutex            sync.RWMutex  // 保护非原子字段
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	FailureThreshold int           // 失败阈值，达到后进入打开状态
	SuccessThreshold int           // 成功阈值，达到后从半开恢复到关闭
	Timeout          time.Duration // 打开状态持续时间，之后进入半开状态
}

// NewCircuitBreaker 创建一个新的熔断器
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:            int32(StateClosed),
		failureThreshold: config.FailureThreshold,
		successThreshold: config.SuccessThreshold,
		timeout:          config.Timeout,
		lastStateChange:  time.Now(),
	}
}

// CanExecute 检查是否允许执行请求
func (cb *CircuitBreaker) CanExecute() bool {
	state := CircuitState(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		// 关闭状态：允许执行
		return true

	case StateOpen:
		// 打开状态：检查是否超时，可以尝试恢复
		cb.mutex.RLock()
		elapsed := time.Since(cb.lastStateChange)
		cb.mutex.RUnlock()

		if elapsed > cb.timeout {
			// 超时了，尝试进入半开状态
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateOpen), int32(StateHalfOpen)) {
				cb.mutex.Lock()
				cb.lastStateChange = time.Now()
				atomic.StoreInt64(&cb.successCount, 0)
				cb.mutex.Unlock()

				slog.Info("Circuit breaker entering HALF_OPEN state")
				return true
			}
		}
		// 仍在打开状态，拒绝请求
		return false

	case StateHalfOpen:
		// 半开状态：允许部分请求通过（简化实现：允许所有）
		return true

	default:
		return false
	}
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess() {
	state := CircuitState(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		// 关闭状态：重置失败计数
		atomic.StoreInt64(&cb.failureCount, 0)

	case StateHalfOpen:
		// 半开状态：增加成功计数
		successCount := atomic.AddInt64(&cb.successCount, 1)

		// 检查是否达到成功阈值，可以恢复到关闭状态
		if int(successCount) >= cb.successThreshold {
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateHalfOpen), int32(StateClosed)) {
				cb.mutex.Lock()
				cb.lastStateChange = time.Now()
				atomic.StoreInt64(&cb.failureCount, 0)
				atomic.StoreInt64(&cb.successCount, 0)
				cb.mutex.Unlock()

				slog.Info("Circuit breaker recovered to CLOSED state",
					"successCount", successCount)
			}
		}
	}
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure() {
	state := CircuitState(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		// 关闭状态：增加失败计数
		failureCount := atomic.AddInt64(&cb.failureCount, 1)

		cb.mutex.Lock()
		cb.lastFailureTime = time.Now()
		cb.mutex.Unlock()

		// 检查是否达到失败阈值，需要打开熔断器
		if int(failureCount) >= cb.failureThreshold {
			if atomic.CompareAndSwapInt32(&cb.state, int32(StateClosed), int32(StateOpen)) {
				cb.mutex.Lock()
				cb.lastStateChange = time.Now()
				cb.mutex.Unlock()

				slog.Warn("Circuit breaker opened due to failures",
					"failureCount", failureCount,
					"threshold", cb.failureThreshold)
			}
		}

	case StateHalfOpen:
		// 半开状态：失败了，重新打开
		if atomic.CompareAndSwapInt32(&cb.state, int32(StateHalfOpen), int32(StateOpen)) {
			cb.mutex.Lock()
			cb.lastStateChange = time.Now()
			cb.lastFailureTime = time.Now()
			atomic.StoreInt64(&cb.successCount, 0)
			cb.mutex.Unlock()

			slog.Warn("Circuit breaker re-opened from HALF_OPEN state")
		}
	}
}

// GetState 获取当前状态
func (cb *CircuitBreaker) GetState() CircuitState {
	return CircuitState(atomic.LoadInt32(&cb.state))
}

// GetStats 获取统计信息
func (cb *CircuitBreaker) GetStats() (state string, failures, successes int64) {
	return cb.GetState().String(),
		atomic.LoadInt64(&cb.failureCount),
		atomic.LoadInt64(&cb.successCount)
}
