package proxy

import (
	"net"
	"sync"
	"time"
)

// IPv6AddressStats IPv6地址统计信息
type IPv6AddressStats struct {
	TotalRequests int       // 总请求数
	SuccessCount  int       // 成功次数（200）
	FailureCount  int       // 失败次数（网络错误）
	Error403Count int       // 403错误次数（被拉黑）
	Error429Count int       // 429错误次数（限流）
	LastUsed      time.Time // 最后使用时间
	LastError     time.Time // 最后错误时间
}

// IPv6Pool IPv6地址池（带统计和健康检查）
type IPv6Pool struct {
	addresses     []net.IP
	stats         map[string]*IPv6AddressStats
	currentIndex  int
	mu            sync.RWMutex
	lastRefreshed time.Time
}

var (
	globalIPv6Pool     *IPv6Pool
	globalIPv6PoolOnce sync.Once
	globalIPv6PoolMu   sync.RWMutex
)

// GetGlobalIPv6Pool 获取全局IPv6地址池单例
func GetGlobalIPv6Pool() *IPv6Pool {
	globalIPv6PoolOnce.Do(func() {
		globalIPv6Pool = NewIPv6Pool()
		// 初始化时刷新地址池
		globalIPv6Pool.Refresh()
		// 启动定期刷新（每分钟）
		go func() {
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				globalIPv6Pool.Refresh()
			}
		}()
	})
	globalIPv6PoolMu.RLock()
	defer globalIPv6PoolMu.RUnlock()
	return globalIPv6Pool
}

// NewIPv6Pool 创建新的IPv6地址池
func NewIPv6Pool() *IPv6Pool {
	return &IPv6Pool{
		addresses: make([]net.IP, 0),
		stats:     make(map[string]*IPv6AddressStats),
	}
}

// Refresh 刷新IPv6地址池（从系统重新发现）
func (p *IPv6Pool) Refresh() {
	newAddrs := DiscoverIPv6LocalAddrs()
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// 保留现有统计信息
	oldStats := p.stats
	p.stats = make(map[string]*IPv6AddressStats)
	
	// 合并新旧地址，保留统计信息
	seen := make(map[string]bool)
	p.addresses = p.addresses[:0]
	
	for _, ip := range newAddrs {
		ipStr := ip.String()
		if seen[ipStr] {
			continue
		}
		seen[ipStr] = true
		p.addresses = append(p.addresses, ip)
		
		// 保留原有统计信息，如果没有则创建新的
		if stats, ok := oldStats[ipStr]; ok {
			p.stats[ipStr] = stats
		} else {
			p.stats[ipStr] = &IPv6AddressStats{}
		}
	}
	
	p.lastRefreshed = time.Now()
	
	// 如果当前索引超出范围，重置为0
	if p.currentIndex >= len(p.addresses) {
		p.currentIndex = 0
	}
}

// GetHealthyNext 智能选择下一个健康的IPv6地址
// 参考zeromaps-rpc的实现：过滤掉被拉黑和高失败率的地址
func (p *IPv6Pool) GetHealthyNext() net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.addresses) == 0 {
		return nil
	}
	
	// 过滤健康地址
	healthyAddresses := make([]net.IP, 0)
	for _, ip := range p.addresses {
		ipStr := ip.String()
		stats := p.stats[ipStr]
		if stats == nil {
			stats = &IPv6AddressStats{}
			p.stats[ipStr] = stats
		}
		
		// 1. 被拉黑（403 >= 5次）→ 排除
		if stats.Error403Count >= 5 {
			continue
		}
		
		// 2. 失败率 > 30% 且请求数 > 20 → 排除
		if stats.TotalRequests > 20 {
			failRate := float64(stats.FailureCount) / float64(stats.TotalRequests)
			if failRate > 0.3 {
				continue
			}
		}
		
		healthyAddresses = append(healthyAddresses, ip)
	}
	
	if len(healthyAddresses) == 0 {
		// 如果没有健康地址，回退到所有地址
		healthyAddresses = p.addresses
	}
	
	// 从健康地址中轮询选择
	if len(healthyAddresses) == 0 {
		return nil
	}
	
	ip := healthyAddresses[p.currentIndex%len(healthyAddresses)]
	p.currentIndex = (p.currentIndex + 1) % len(healthyAddresses)
	
	return ip
}

// RecordSuccess 记录成功请求
func (p *IPv6Pool) RecordSuccess(ip net.IP) {
	if ip == nil {
		return
	}
	ipStr := ip.String()
	p.mu.Lock()
	defer p.mu.Unlock()
	
	stats := p.stats[ipStr]
	if stats == nil {
		stats = &IPv6AddressStats{}
		p.stats[ipStr] = stats
	}
	
	stats.TotalRequests++
	stats.SuccessCount++
	stats.LastUsed = time.Now()
}

// RecordFailure 记录失败请求（网络错误）
func (p *IPv6Pool) RecordFailure(ip net.IP) {
	if ip == nil {
		return
	}
	ipStr := ip.String()
	p.mu.Lock()
	defer p.mu.Unlock()
	
	stats := p.stats[ipStr]
	if stats == nil {
		stats = &IPv6AddressStats{}
		p.stats[ipStr] = stats
	}
	
	stats.TotalRequests++
	stats.FailureCount++
	stats.LastError = time.Now()
}

// RecordError403 记录403错误（被拉黑）
func (p *IPv6Pool) RecordError403(ip net.IP) {
	if ip == nil {
		return
	}
	ipStr := ip.String()
	p.mu.Lock()
	defer p.mu.Unlock()
	
	stats := p.stats[ipStr]
	if stats == nil {
		stats = &IPv6AddressStats{}
		p.stats[ipStr] = stats
	}
	
	stats.TotalRequests++
	stats.Error403Count++
	stats.LastError = time.Now()
}

// RecordError429 记录429错误（限流）
func (p *IPv6Pool) RecordError429(ip net.IP) {
	if ip == nil {
		return
	}
	ipStr := ip.String()
	p.mu.Lock()
	defer p.mu.Unlock()
	
	stats := p.stats[ipStr]
	if stats == nil {
		stats = &IPv6AddressStats{}
		p.stats[ipStr] = stats
	}
	
	stats.TotalRequests++
	stats.Error429Count++
	stats.LastError = time.Now()
}

// GetStats 获取统计信息（用于监控）
func (p *IPv6Pool) GetStats() map[string]*IPv6AddressStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	result := make(map[string]*IPv6AddressStats)
	for ipStr, stats := range p.stats {
		// 创建副本以避免外部修改
		s := *stats
		result[ipStr] = &s
	}
	return result
}

// GetHealthyCount 获取健康地址数量
func (p *IPv6Pool) GetHealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	count := 0
	for _, ip := range p.addresses {
		ipStr := ip.String()
		stats := p.stats[ipStr]
		if stats == nil {
			count++
			continue
		}
		
		// 被拉黑
		if stats.Error403Count >= 5 {
			continue
		}
		
		// 失败率过高
		if stats.TotalRequests > 20 {
			failRate := float64(stats.FailureCount) / float64(stats.TotalRequests)
			if failRate > 0.3 {
				continue
			}
		}
		
		count++
	}
	return count
}

