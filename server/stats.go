package server

import (
	"sync"
	"time"
)

// StatsCollector 统计收集器
// 收集和统计服务器的运行指标
type StatsCollector struct {
	// 请求统计
	totalRequests   int64
	successRequests int64
	errorRequests   int64

	// 延迟统计
	totalLatency    int64 // 总延迟（纳秒）
	maxLatency      int64 // 最大延迟（纳秒）
	minLatency      int64 // 最小延迟（纳秒）
	latencyCount    int64 // 延迟统计次数

	// 错误统计
	errorStats map[string]int64 // 错误类型 -> 次数

	// 并发统计
	currentConnections int32

	// 统计开始时间
	startTime time.Time

	// 互斥锁
	mutex sync.RWMutex
}

// NewStatsCollector 创建新的统计收集器
// 返回一个初始化的统计收集器实例
// 返回值:
//
//	*StatsCollector: 新创建的统计收集器实例
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		errorStats: make(map[string]int64),
		startTime:  time.Now(),
		minLatency: -1, // -1表示未初始化
	}
}

// RecordRequest 记录请求
// 记录一个请求的统计信息，包括成功/失败状态、延迟时间和错误类型
// 参数:
//
//	success: 请求是否成功
//	latency: 请求延迟时间
//	errorType: 错误类型（如果请求失败）
func (s *StatsCollector) RecordRequest(success bool, latency time.Duration, errorType string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.totalRequests++

	if success {
		s.successRequests++
	} else {
		s.errorRequests++
		if errorType != "" {
			s.errorStats[errorType]++
		}
	}

	// 记录延迟
	if latency > 0 {
		latencyNs := latency.Nanoseconds()
		s.totalLatency += latencyNs
		s.latencyCount++

		if s.maxLatency < latencyNs {
			s.maxLatency = latencyNs
		}

		if s.minLatency == -1 || s.minLatency > latencyNs {
			s.minLatency = latencyNs
		}
	}
}

// IncrementConnections 增加连接数
// 当前连接数加1（线程安全）
func (s *StatsCollector) IncrementConnections() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.currentConnections++
}

// DecrementConnections 减少连接数
// 当前连接数减1（线程安全，不会小于0）
func (s *StatsCollector) DecrementConnections() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.currentConnections > 0 {
		s.currentConnections--
	}
}

// GetStats 获取统计信息
// 返回当前的所有统计信息
// 返回值:
//
//	Stats: 统计信息结构体，包含所有统计数据
func (s *StatsCollector) GetStats() Stats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := Stats{
		TotalRequests:    s.totalRequests,
		SuccessRequests:  s.successRequests,
		ErrorRequests:    s.errorRequests,
		CurrentConnections: int(s.currentConnections),
		StatsStartTime:   s.startTime.Unix(),
		ErrorStats:      make(map[string]int64),
	}

	// 计算平均延迟
	if s.latencyCount > 0 {
		stats.AverageLatencyMs = int64(time.Duration(s.totalLatency/s.latencyCount).Milliseconds())
		stats.MinLatencyMs = int64(time.Duration(s.minLatency).Milliseconds())
		stats.MaxLatencyMs = int64(time.Duration(s.maxLatency).Milliseconds())
	}

	// 复制错误统计
	for k, v := range s.errorStats {
		stats.ErrorStats[k] = v
	}

	return stats
}

// Reset 重置统计信息
// 清空所有统计数据并重置开始时间
func (s *StatsCollector) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.totalRequests = 0
	s.successRequests = 0
	s.errorRequests = 0
	s.totalLatency = 0
	s.maxLatency = 0
	s.minLatency = -1
	s.latencyCount = 0
	s.errorStats = make(map[string]int64)
	s.startTime = time.Now()
}

// Stats 统计信息结构体
// 存储完整的统计信息数据
type Stats struct {
	// TotalRequests 总请求数
	TotalRequests int64
	// SuccessRequests 成功请求数
	SuccessRequests int64
	// ErrorRequests 失败请求数
	ErrorRequests int64
	// AverageLatencyMs 平均延迟（毫秒）
	AverageLatencyMs int64
	// MinLatencyMs 最小延迟（毫秒）
	MinLatencyMs int64
	// MaxLatencyMs 最大延迟（毫秒）
	MaxLatencyMs int64
	// CurrentConnections 当前连接数
	CurrentConnections int
	// ErrorStats 错误统计（错误类型 -> 次数）
	ErrorStats map[string]int64
	// StatsStartTime 统计开始时间（Unix时间戳）
	StatsStartTime int64
}

// GetUptime 获取运行时长（秒）
// 返回统计收集器开始运行到现在的时长
// 返回值:
//
//	int64: 运行时长（秒）
func (s *StatsCollector) GetUptime() int64 {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return int64(time.Since(s.startTime).Seconds())
}

