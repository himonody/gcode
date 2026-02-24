package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

type Metrics interface {
	RecordOperation(operation string, duration time.Duration, success bool)
	RecordError(operation string, errorType string)
	IncrementCounter(name string)
	GetStats() Stats
	Reset()
}

type Stats struct {
	Operations      map[string]*OperationStats
	Counters        map[string]int64
	TotalOperations int64
	TotalErrors     int64
	TotalSuccess    int64
}

type OperationStats struct {
	Count         int64
	SuccessCount  int64
	ErrorCount    int64
	TotalDuration time.Duration
	AvgDuration   time.Duration
	MinDuration   time.Duration
	MaxDuration   time.Duration
}

type defaultMetrics struct {
	operations map[string]*operationMetrics
	counters   map[string]*int64
	mu         sync.RWMutex
}

type operationMetrics struct {
	count         int64
	successCount  int64
	errorCount    int64
	totalDuration int64
	minDuration   int64
	maxDuration   int64
}

func NewMetrics() Metrics {
	return &defaultMetrics{
		operations: make(map[string]*operationMetrics),
		counters:   make(map[string]*int64),
	}
}

func (m *defaultMetrics) RecordOperation(operation string, duration time.Duration, success bool) {
	m.mu.Lock()
	if _, exists := m.operations[operation]; !exists {
		m.operations[operation] = &operationMetrics{
			minDuration: int64(duration),
			maxDuration: int64(duration),
		}
	}
	m.mu.Unlock()

	om := m.operations[operation]

	atomic.AddInt64(&om.count, 1)
	atomic.AddInt64(&om.totalDuration, int64(duration))

	if success {
		atomic.AddInt64(&om.successCount, 1)
	} else {
		atomic.AddInt64(&om.errorCount, 1)
	}

	for {
		oldMin := atomic.LoadInt64(&om.minDuration)
		if int64(duration) >= oldMin {
			break
		}
		if atomic.CompareAndSwapInt64(&om.minDuration, oldMin, int64(duration)) {
			break
		}
	}

	for {
		oldMax := atomic.LoadInt64(&om.maxDuration)
		if int64(duration) <= oldMax {
			break
		}
		if atomic.CompareAndSwapInt64(&om.maxDuration, oldMax, int64(duration)) {
			break
		}
	}
}

func (m *defaultMetrics) RecordError(operation string, errorType string) {
	counterName := operation + "_error_" + errorType
	m.IncrementCounter(counterName)
}

func (m *defaultMetrics) IncrementCounter(name string) {
	m.mu.Lock()
	if _, exists := m.counters[name]; !exists {
		var zero int64 = 0
		m.counters[name] = &zero
	}
	m.mu.Unlock()

	atomic.AddInt64(m.counters[name], 1)
}

func (m *defaultMetrics) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		Operations: make(map[string]*OperationStats),
		Counters:   make(map[string]int64),
	}

	for name, om := range m.operations {
		count := atomic.LoadInt64(&om.count)
		successCount := atomic.LoadInt64(&om.successCount)
		errorCount := atomic.LoadInt64(&om.errorCount)
		totalDuration := atomic.LoadInt64(&om.totalDuration)
		minDuration := atomic.LoadInt64(&om.minDuration)
		maxDuration := atomic.LoadInt64(&om.maxDuration)

		avgDuration := time.Duration(0)
		if count > 0 {
			avgDuration = time.Duration(totalDuration / count)
		}

		stats.Operations[name] = &OperationStats{
			Count:         count,
			SuccessCount:  successCount,
			ErrorCount:    errorCount,
			TotalDuration: time.Duration(totalDuration),
			AvgDuration:   avgDuration,
			MinDuration:   time.Duration(minDuration),
			MaxDuration:   time.Duration(maxDuration),
		}

		stats.TotalOperations += count
		stats.TotalSuccess += successCount
		stats.TotalErrors += errorCount
	}

	for name, counter := range m.counters {
		stats.Counters[name] = atomic.LoadInt64(counter)
	}

	return stats
}

func (m *defaultMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.operations = make(map[string]*operationMetrics)
	m.counters = make(map[string]*int64)
}
