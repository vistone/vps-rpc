package proxy

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "net"
    "os"
    "path/filepath"
    "sync"
    "time"

    "vps-rpc/config"
    "vps-rpc/rpc"
)

// dnsRecord 存储单个域名的解析结果
type dnsRecord struct {
	Domain     string   `json:"domain"`
	IPv4       []string `json:"ipv4"`
	IPv6       []string `json:"ipv6"`
	UpdatedAt  int64    `json:"updated_at"`
	NextIndex4 int      `json:"next_index4"`
	NextIndex6 int      `json:"next_index6"`
	// NextPreferV6 当同时允许v4/v6时用于交替选择的开关
	NextPreferV6 bool `json:"next_prefer_v6"`
	// Blacklist 黑名单：被封禁的 IP 集合（仅以是否存在为准，不使用过期时间）
	Blacklist map[string]bool `json:"blacklist"`
	// Whitelist 白名单：通过复测恢复可用的 IP 集合
	Whitelist map[string]bool `json:"whitelist"`
	// EWMA 时延（毫秒）用于自适应族选择（0 表示未知）
	EwmaLatency4 float64 `json:"ewma_latency4"`
	EwmaLatency6 float64 `json:"ewma_latency6"`
	// IPLatency 每个IP的EWMA延迟（毫秒），用于自动过滤慢IP
	IPLatency map[string]float64 `json:"ip_latency,omitempty"`
}

type DNSPool struct {
    mu       sync.RWMutex
    records  map[string]*dnsRecord
    filename string
    // 持久化节流与热加载
    persistMu    sync.Mutex
    persistTimer *time.Timer
    lastPersist  time.Time
    fileModTime  time.Time
    closed       chan struct{}
    // 并发检测：用于检测短时间内同一域名的并发请求
    concurrentRequests map[string]int64 // domain -> 最近请求时间戳
    concurrentMu       sync.Mutex
    // 并发批次预留IP池：domain -> []预留的IP
    reservedIPPool     map[string][]struct{IP string; IsV6 bool; reservedAt int64}
    reservedMu        sync.Mutex
}

func NewDNSPool(path string) (*DNSPool, error) {
    // Treat provided path as JSON file path. If directory, use dns_pool.json inside it.
    if abs, e := filepath.Abs(path); e == nil { path = abs }
    fi, err := os.Stat(path)
    if err == nil && fi.IsDir() {
        path = filepath.Join(path, "dns_pool.json")
    }
    pool := &DNSPool{
        records:            make(map[string]*dnsRecord),
        filename:           path,
        closed:             make(chan struct{}),
        concurrentRequests: make(map[string]int64),
        reservedIPPool:     make(map[string][]struct{IP string; IsV6 bool; reservedAt int64}),
    }
    if err := pool.loadFromDisk(); err != nil {
        if !errors.Is(err, os.ErrNotExist) {
            return nil, err
        }
        // ensure parent dir exists
        _ = os.MkdirAll(filepath.Dir(path), 0o755)
        // create empty file lazily on first persist
    }
    log.Printf("[dns-pool] using file: %s", path)
    // 启动热加载后台任务（轻量轮询，避免引入外部依赖）
    go pool.backgroundHotReload()
    return pool, nil
}

func (p *DNSPool) Close() error {
    // 停止热加载轮询
    select { case <-p.closed: default: close(p.closed) }
    // 立即落盘（若有尚未触发的节流保存）
    p.persistMu.Lock()
    if p.persistTimer != nil {
        p.persistTimer.Stop()
        p.persistTimer = nil
    }
    p.persistMu.Unlock()
    p.persistNow()
    return nil
}

func (p *DNSPool) loadFromDisk() error {
    data, err := os.ReadFile(p.filename)
    if err != nil {
        return err
    }
    var m map[string]*dnsRecord
    if err := json.Unmarshal(data, &m); err != nil {
        return err
    }
    p.mu.Lock()
    p.records = m
    // backfill maps inside records
    for _, rec := range p.records {
        if rec.Blacklist == nil { rec.Blacklist = map[string]bool{} }
        if rec.Whitelist == nil { rec.Whitelist = map[string]bool{} }
        if rec.IPLatency == nil { rec.IPLatency = map[string]float64{} }
        // 将白名单中的IP并入 IPv4/IPv6 列表，保证可选候选包含白名单
        if len(rec.Whitelist) > 0 {
            present4 := map[string]struct{}{}
            present6 := map[string]struct{}{}
            for _, ip := range rec.IPv4 { present4[ip] = struct{}{} }
            for _, ip := range rec.IPv6 { present6[ip] = struct{}{} }
            for ip := range rec.Whitelist {
                parsed := net.ParseIP(ip)
                if parsed == nil { continue }
                if parsed.To4() != nil {
                    if _, ok := present4[ip]; !ok { rec.IPv4 = append(rec.IPv4, ip); present4[ip] = struct{}{} }
                } else {
                    if _, ok := present6[ip]; !ok { rec.IPv6 = append(rec.IPv6, ip); present6[ip] = struct{}{} }
                }
            }
        }
    }
    p.mu.Unlock()
    if fi, err := os.Stat(p.filename); err == nil {
        p.persistMu.Lock()
        p.fileModTime = fi.ModTime()
        p.persistMu.Unlock()
    }
    return nil
}

func (p *DNSPool) persist() error {
    // 改为节流批量写：延迟合并 500ms 内的多次保存请求
    const throttle = 500 * time.Millisecond
    p.persistMu.Lock()
    defer p.persistMu.Unlock()
    if p.persistTimer != nil {
        if p.persistTimer.Stop() {
            // 重置定时器
        }
    }
    p.persistTimer = time.AfterFunc(throttle, func() {
        p.persistNow()
    })
    return nil
}

func (p *DNSPool) persistNow() {
    p.mu.RLock()
    recordCount := len(p.records)
    if recordCount == 0 {
        p.mu.RUnlock()
        // 空记录不写入，避免覆盖已有数据
        return
    }
    data, err := json.MarshalIndent(p.records, "", "  ")
    p.mu.RUnlock()
    if err != nil { 
        log.Printf("[dns-pool] marshal failed: %v", err)
        return
    }
    
    dir := filepath.Dir(p.filename)
    if err := os.MkdirAll(dir, 0o755); err != nil { 
        log.Printf("[dns-pool] mkdir failed: %v (dir=%s)", err, dir)
        return
    }
    
    tmp := p.filename + ".tmp"
    if err := os.WriteFile(tmp, data, 0o644); err != nil { 
        log.Printf("[dns-pool] write tmp failed: %v (file=%s, size=%d bytes)", err, tmp, len(data))
        return
    }
    if err := os.Rename(tmp, p.filename); err != nil { 
        log.Printf("[dns-pool] rename failed: %v (tmp=%s -> file=%s)", err, tmp, p.filename)
        return
    }
    
    p.persistMu.Lock()
    p.lastPersist = time.Now()
    if fi, err := os.Stat(p.filename); err == nil { 
        p.fileModTime = fi.ModTime()
    }
    p.persistMu.Unlock()
    log.Printf("[dns-pool] persisted %d domains to %s (%d bytes)", recordCount, p.filename, len(data))
}

// 背景热加载：定期检查文件是否被外部修改（如手工或工具写入），若变更则加载
func (p *DNSPool) backgroundHotReload() {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-p.closed:
            return
        case <-ticker.C:
        }
        p.persistMu.Lock()
        lastPersist := p.lastPersist
        lastSeen := p.fileModTime
        p.persistMu.Unlock()
        fi, err := os.Stat(p.filename)
        if err != nil { continue }
        mod := fi.ModTime()
        // 若文件修改时间晚于我们上次持久化，视为外部修改
        if mod.After(lastSeen) && mod.After(lastPersist.Add(100*time.Millisecond)) {
            if err := p.loadFromDisk(); err != nil {
                log.Printf("[dns-pool] hot-reload failed: %v", err)
            } else {
                log.Printf("[dns-pool] hot-reloaded from %s", p.filename)
            }
        }
    }
}

// resolveAndStore 解析域名A/AAAA并存储
func (p *DNSPool) resolveAndStore(ctx context.Context, domain string) (*dnsRecord, error) {
	var rec dnsRecord
	rec.Domain = domain

	// 增强探测：多次系统解析刺探，合并去重（某些CDN会轮转VIP）
	// 增加探测次数和变化间隔，以发现更多IP地址
	uniq4 := map[string]struct{}{}
	uniq6 := map[string]struct{}{}
	
	// 使用多个Resolver实例，避免缓存影响
	attempts := 30 // 增加到30次探测
	baseDelay := 50 * time.Millisecond // 缩短基础延迟
	
	log.Printf("[dns-probe] 开始探测 %s (共%d次)", domain, attempts)
	
	for i := 0; i < attempts; i++ {
		// 每次使用新的Resolver实例，清除可能的缓存
		r := &net.Resolver{}
		
		// 并行查询IPv4和IPv6
		type result struct {
			ipv4 []net.IP
			ipv6 []net.IP
			err4 error
			err6 error
		}
		resChan := make(chan result, 1)
		
		go func() {
			var res result
			res.ipv4, res.err4 = r.LookupIP(ctx, "ip4", domain)
			res.ipv6, res.err6 = r.LookupIP(ctx, "ip6", domain)
			resChan <- res
		}()
		
		select {
		case res := <-resChan:
			if res.err4 == nil {
				for _, ip := range res.ipv4 {
					uniq4[ip.String()] = struct{}{}
				}
			}
			if res.err6 == nil {
				for _, ip := range res.ipv6 {
					uniq6[ip.String()] = struct{}{}
				}
			}
        case <-ctx.Done():
            return nil, ctx.Err()
		}
		
		// 变化的延迟：50-150ms之间，避免被限流
		delay := baseDelay + time.Duration(i%10)*10*time.Millisecond
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	
	for ip := range uniq4 {
		rec.IPv4 = append(rec.IPv4, ip)
	}
	for ip := range uniq6 {
		rec.IPv6 = append(rec.IPv6, ip)
	}
	rec.UpdatedAt = time.Now().Unix()

	if len(rec.IPv4) == 0 && len(rec.IPv6) == 0 {
		return nil, fmt.Errorf("DNS 无记录: %s", domain)
	}

	if err := p.save(&rec); err != nil {
		return nil, err
	}
	log.Printf("[dns] %s A=%d AAAA=%d (探测完成)", domain, len(rec.IPv4), len(rec.IPv6))
	return &rec, nil
}

// resolveAndStoreQuick 快速解析模式：用于请求路径，尽量降低首包等待
// 采用较少次数与更短间隔，避免长时间阻塞请求
func (p *DNSPool) resolveAndStoreQuick(ctx context.Context, domain string) (*dnsRecord, error) {
    var rec dnsRecord
    rec.Domain = domain

    uniq4 := map[string]struct{}{}
    uniq6 := map[string]struct{}{}

    attempts := 3
    baseDelay := 30 * time.Millisecond

    for i := 0; i < attempts; i++ {
        r := &net.Resolver{}
        // 同步查询，减少调度开销
        if addrs4, err := r.LookupIP(ctx, "ip4", domain); err == nil {
            for _, ip := range addrs4 { uniq4[ip.String()] = struct{}{} }
        }
        if addrs6, err := r.LookupIP(ctx, "ip6", domain); err == nil {
            for _, ip := range addrs6 { uniq6[ip.String()] = struct{}{} }
        }
        if i < attempts-1 {
            time.Sleep(baseDelay)
        }
    }

    for ip := range uniq4 { rec.IPv4 = append(rec.IPv4, ip) }
    for ip := range uniq6 { rec.IPv6 = append(rec.IPv6, ip) }
    rec.UpdatedAt = time.Now().Unix()

    if len(rec.IPv4) == 0 && len(rec.IPv6) == 0 {
        return nil, fmt.Errorf("DNS 无记录: %s", domain)
    }
    if err := p.save(&rec); err != nil { return nil, err }
    log.Printf("[dns-quick] %s A=%d AAAA=%d", domain, len(rec.IPv4), len(rec.IPv6))
    return &rec, nil
}

func (p *DNSPool) save(rec *dnsRecord) error {
    p.mu.Lock()
    // keep existing lists if present, only update fields from rec
    existing, ok := p.records[rec.Domain]
    if !ok {
        existing = &dnsRecord{Domain: rec.Domain}
        p.records[rec.Domain] = existing
    }
    // 合并 IPv4：保留已有顺序，追加新发现（去重）
    if len(existing.IPv4) == 0 {
        existing.IPv4 = append([]string(nil), rec.IPv4...)
    } else {
        present := map[string]struct{}{}
        for _, ip := range existing.IPv4 { present[ip] = struct{}{} }
        for _, ip := range rec.IPv4 {
            if _, ok := present[ip]; !ok {
                existing.IPv4 = append(existing.IPv4, ip)
                present[ip] = struct{}{}
            }
        }
    }
    // 合并 IPv6：保留已有顺序，追加新发现（去重）
    if len(existing.IPv6) == 0 {
        existing.IPv6 = append([]string(nil), rec.IPv6...)
    } else {
        present6 := map[string]struct{}{}
        for _, ip := range existing.IPv6 { present6[ip] = struct{}{} }
        for _, ip := range rec.IPv6 {
            if _, ok := present6[ip]; !ok {
                existing.IPv6 = append(existing.IPv6, ip)
                present6[ip] = struct{}{}
            }
        }
    }
    if existing.Blacklist == nil { existing.Blacklist = map[string]bool{} }
    if existing.Whitelist == nil { existing.Whitelist = map[string]bool{} }
    existing.UpdatedAt = rec.UpdatedAt
    // 持久化轮询状态，确保重启/热加载后延续轮询进度
    // 若来者未显式设置索引（为0），则保留已有索引，避免被解析刷新覆盖成0
    if rec.NextIndex4 != 0 {
        existing.NextIndex4 = rec.NextIndex4 % max(1, len(existing.IPv4))
    } else if existing.NextIndex4 >= len(existing.IPv4) {
        existing.NextIndex4 = 0
    }
    if rec.NextIndex6 != 0 {
        existing.NextIndex6 = rec.NextIndex6 % max(1, len(existing.IPv6))
    } else if existing.NextIndex6 >= len(existing.IPv6) {
        existing.NextIndex6 = 0
    }
    // NextPreferV6：仅当调用者显式改变时更新；否则保留
    if rec.NextPreferV6 != existing.NextPreferV6 {
        existing.NextPreferV6 = rec.NextPreferV6
    }
    p.mu.Unlock()
    return p.persist()
}

func max(a, b int) int { if a > b { return a }; return b }

// MergeFromPeer 将对等节点返回的DNS记录合并入本地数据库（仅合并IPv4/IPv6，忽略黑白名单）
func (p *DNSPool) MergeFromPeer(records map[string]*rpc.DNSRecord) error {
    if p == nil || len(records) == 0 { return nil }
    p.mu.Lock()
    for domain, r := range records {
        if r == nil { continue }
        rec, ok := p.records[domain]
        if !ok {
            rec = &dnsRecord{Domain: domain}
            p.records[domain] = rec
        }
        uniq4 := map[string]struct{}{}
        uniq6 := map[string]struct{}{}
        for _, ip := range rec.IPv4 { uniq4[ip] = struct{}{} }
        for _, ip := range rec.IPv6 { uniq6[ip] = struct{}{} }
        for _, ip := range r.Ipv4 { uniq4[ip] = struct{}{} }
        for _, ip := range r.Ipv6 { uniq6[ip] = struct{}{} }
        rec.IPv4 = rec.IPv4[:0]
        rec.IPv6 = rec.IPv6[:0]
        for ip := range uniq4 { rec.IPv4 = append(rec.IPv4, ip) }
        for ip := range uniq6 { rec.IPv6 = append(rec.IPv6, ip) }
        if rec.Whitelist == nil { rec.Whitelist = map[string]bool{} }
        for _, ip := range r.Ipv4 { rec.Whitelist[ip] = true }
        for _, ip := range r.Ipv6 { rec.Whitelist[ip] = true }
        // 白名单 IP 并入候选列表
        seen4 := map[string]struct{}{}
        seen6 := map[string]struct{}{}
        for _, ip := range rec.IPv4 { seen4[ip] = struct{}{} }
        for _, ip := range rec.IPv6 { seen6[ip] = struct{}{} }
        for ip := range rec.Whitelist {
            parsed := net.ParseIP(ip)
            if parsed == nil { continue }
            if parsed.To4() != nil {
                if _, ok := seen4[ip]; !ok { rec.IPv4 = append(rec.IPv4, ip); seen4[ip] = struct{}{} }
            } else {
                if _, ok := seen6[ip]; !ok { rec.IPv6 = append(rec.IPv6, ip); seen6[ip] = struct{}{} }
            }
        }
        rec.UpdatedAt = time.Now().Unix()
    }
    p.mu.Unlock()
    return p.persist()
}

func (p *DNSPool) get(domain string) (*dnsRecord, error) {
    p.mu.RLock()
    rec := p.records[domain]
    p.mu.RUnlock()
    if rec == nil {
        return nil, nil
    }
    // return a shallow copy to avoid external mutation
    c := *rec
    if c.Blacklist == nil { c.Blacklist = map[string]bool{} }
    if c.Whitelist == nil { c.Whitelist = map[string]bool{} }
    return &c, nil
}

// GetIPs 返回可用的IPv4/IPv6列表（必要时刷新）
func (p *DNSPool) GetIPs(ctx context.Context, domain string) (ipv4 []string, ipv6 []string, err error) {
	if p == nil {
		return nil, nil, fmt.Errorf("DNSPool 未初始化")
	}
	rec, _ := p.get(domain)
	needRefresh := rec == nil
	
	// 如果IP数量较少（少于5个），强制触发刷新以丰富IP池
	if !needRefresh && rec != nil {
		totalIPs := len(rec.IPv4) + len(rec.IPv6)
		if totalIPs < 5 {
			log.Printf("[dns] %s IP数量较少(%d)，强制刷新探测", domain, totalIPs)
			needRefresh = true
		}
	}
	
	if !needRefresh && config.AppConfig.DNS.RefreshInterval != "" {
		if d, e := time.ParseDuration(config.AppConfig.DNS.RefreshInterval); e == nil {
			if time.Since(time.Unix(rec.UpdatedAt, 0)) > d {
				needRefresh = true
			}
		}
	}
    if needRefresh {
        // 请求路径走快速解析，减少首请求等待
        var e error
        rec, e = p.resolveAndStoreQuick(ctx, domain)
        if e != nil && rec == nil {
            return nil, nil, e
        }
    }
	if rec != nil {
		return rec.IPv4, rec.IPv6, nil
	}
	return nil, nil, fmt.Errorf("未获取到记录: %s", domain)
}

// GetAllDomainsAndIPs 返回所有域名的所有IP地址（用于预建连接池）
// 返回值: map[domain][]{ip:port}
func (p *DNSPool) GetAllDomainsAndIPs() map[string][]string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string][]string)
	for domain, rec := range p.records {
		if rec == nil {
			continue
		}
		ips := make([]string, 0)
		// 收集所有IPv4和IPv6（排除黑名单）
		for _, ip := range rec.IPv4 {
			if !rec.Blacklist[ip] {
				ips = append(ips, net.JoinHostPort(ip, "443"))
			}
		}
		for _, ip := range rec.IPv6 {
			if !rec.Blacklist[ip] {
				ips = append(ips, net.JoinHostPort(ip, "443"))
			}
		}
		// 合并白名单IP
		for ip := range rec.Whitelist {
			parsed := net.ParseIP(ip)
			if parsed == nil {
				continue
			}
			addr := net.JoinHostPort(ip, "443")
			// 去重
			exists := false
			for _, existing := range ips {
				if existing == addr {
					exists = true
					break
				}
			}
			if !exists {
				ips = append(ips, addr)
			}
		}
		if len(ips) > 0 {
			result[domain] = ips
		}
	}
	return result
}

// NextIP 轮询选择一个 IP（优先IPv6，其次IPv4；可调整策略）
// 优化：请求路径使用快速解析模式，避免阻塞请求
// 增强：自动检测并发，在并发时使用批量分配确保负载分流
func (p *DNSPool) NextIP(ctx context.Context, domain string) (ip string, isV6 bool, err error) {
	const concurrentWindow = 200 * time.Millisecond // 200ms内的请求视为并发批次
	const reservedTTL = 500 * time.Millisecond     // 预留IP池的TTL
	
	now := time.Now().UnixNano()
	
	// 首先检查是否有预留的IP池可用
	p.reservedMu.Lock()
	reserved, hasReserved := p.reservedIPPool[domain]
	if hasReserved && len(reserved) > 0 {
		// 清理过期的预留IP（超过TTL的）
		valid := reserved[:0]
		for _, r := range reserved {
			if now-r.reservedAt < int64(reservedTTL) {
				valid = append(valid, r)
			}
		}
		if len(valid) > 0 {
			// 取出第一个预留IP
			selected := valid[0]
			p.reservedIPPool[domain] = valid[1:] // 移除已使用的IP
			p.reservedMu.Unlock()
			return selected.IP, selected.IsV6, nil
		}
		// 所有预留IP都过期了，清除
		delete(p.reservedIPPool, domain)
	}
	p.reservedMu.Unlock()
	
	// 没有预留IP，检查是否需要创建新的预留池
	p.concurrentMu.Lock()
	lastTime, exists := p.concurrentRequests[domain]
	isConcurrent := exists && (now-lastTime < int64(concurrentWindow))
	
	// 如果是并发请求或首次请求，创建预留池
	if isConcurrent || !exists {
		p.concurrentRequests[domain] = now
		p.concurrentMu.Unlock()
		
		// 批量分配8个IP，放入预留池
		if ips, e := p.NextIPs(ctx, domain, 8); e == nil && len(ips) > 0 {
			// 将所有IP放入预留池
			reservedList := make([]struct{IP string; IsV6 bool; reservedAt int64}, len(ips))
			for i, ipInfo := range ips {
				reservedList[i] = struct{IP string; IsV6 bool; reservedAt int64}{ipInfo.IP, ipInfo.IsV6, now}
			}
			p.reservedMu.Lock()
			p.reservedIPPool[domain] = reservedList
			p.reservedMu.Unlock()
			
			// 返回第一个IP
			return ips[0].IP, ips[0].IsV6, nil
		}
		// 如果批量分配失败，回退到单个分配
	} else {
		// 非并发请求（距离上次请求超过200ms），使用单个分配
		p.concurrentRequests[domain] = now
		p.concurrentMu.Unlock()
	}
	p.mu.Lock()
	rec := p.records[domain]
	needResolve := rec == nil
	p.mu.Unlock()
	
	if needResolve {
		// 请求路径使用快速解析（3次探测，快速返回），避免长时间阻塞
		// 后台ProbeManager会进行深度探测（30次）以发现更多IP
		var e error
		if rec, e = p.resolveAndStoreQuick(ctx, domain); e != nil {
			return "", false, e
		}
		// 解析后重新获取内存中的记录
		p.mu.Lock()
		rec = p.records[domain]
		p.mu.Unlock()
		if rec == nil {
			return "", false, fmt.Errorf("解析后仍无记录: %s", domain)
		}
	}
	
	allowV4 := HasIPv4()
	allowV6 := HasIPv6()
	
    // 直接操作内存中的记录，确保索引更新生效
	p.mu.Lock()
	rec = p.records[domain]
	if rec == nil {
		p.mu.Unlock()
		return "", false, fmt.Errorf("记录不存在: %s", domain)
	}
	if rec.Blacklist == nil { rec.Blacklist = map[string]bool{} }
	if rec.Whitelist == nil { rec.Whitelist = map[string]bool{} }
    // 将白名单并入候选列表（保证可轮询）
    if len(rec.Whitelist) > 0 {
        seen4 := map[string]struct{}{}
        seen6 := map[string]struct{}{}
        for _, ipx := range rec.IPv4 { seen4[ipx] = struct{}{} }
        for _, ipx := range rec.IPv6 { seen6[ipx] = struct{}{} }
        for ipx := range rec.Whitelist {
            parsed := net.ParseIP(ipx)
            if parsed == nil { continue }
            if parsed.To4() != nil {
                if _, ok := seen4[ipx]; !ok { rec.IPv4 = append(rec.IPv4, ipx); seen4[ipx] = struct{}{} }
            } else {
                if _, ok := seen6[ipx]; !ok { rec.IPv6 = append(rec.IPv6, ipx); seen6[ipx] = struct{}{} }
            }
        }
    }
	
    // 合并所有可用IP到单一列表（排除黑名单），不分IPv4/IPv6，统一严格轮询
    allAvailableIPs := make([]struct {
        ip   string
        isV6 bool
    }, 0)
    
    // 根据设备能力和配置决定加入哪些IP
    preferV6 := config.AppConfig.DNS.PreferIPv6
    
    // 如果设备支持IPv6且配置优先，IPv6在前；否则IPv4在前
    if allowV6 && (preferV6 || !allowV4) {
        // IPv6优先：先加IPv6，再加IPv4
        for _, ip := range rec.IPv6 {
            if !rec.Blacklist[ip] {
                allAvailableIPs = append(allAvailableIPs, struct {
                    ip   string
                    isV6 bool
                }{ip, true})
            }
        }
        if allowV4 {
            for _, ip := range rec.IPv4 {
                if !rec.Blacklist[ip] {
                    allAvailableIPs = append(allAvailableIPs, struct {
                        ip   string
                        isV6 bool
                    }{ip, false})
                }
            }
        }
    } else {
        // IPv4优先或仅IPv4：先加IPv4，再加IPv6
        if allowV4 {
            for _, ip := range rec.IPv4 {
                if !rec.Blacklist[ip] {
                    allAvailableIPs = append(allAvailableIPs, struct {
                        ip   string
                        isV6 bool
                    }{ip, false})
                }
            }
        }
        if allowV6 {
            for _, ip := range rec.IPv6 {
                if !rec.Blacklist[ip] {
                    allAvailableIPs = append(allAvailableIPs, struct {
                        ip   string
                        isV6 bool
                    }{ip, true})
                }
            }
        }
    }
    
    if len(allAvailableIPs) == 0 {
        p.mu.Unlock()
        return "", false, fmt.Errorf("无可用IP: %s", domain)
    }
    
    // 严格轮询：所有IP平均分配，使用统一索引
    unifiedIdx := rec.NextIndex4
    idx := unifiedIdx % len(allAvailableIPs)
    rec.NextIndex4 = (unifiedIdx + 1) % len(allAvailableIPs)
    
    selected := allAvailableIPs[idx]
    p.mu.Unlock()
    if err := p.persist(); err != nil { log.Printf("[dns-pool] persist failed: %v", err) }
    return selected.ip, selected.isV6, nil
}

// NextIPs 批量获取多个不同的IP（用于并发请求负载分流）
// count: 需要获取的IP数量
// 返回值: []struct{IP string, IsV6 bool} - IP列表，保证每个IP都不同（在可用IP数量允许的情况下）
func (p *DNSPool) NextIPs(ctx context.Context, domain string, count int) ([]struct{IP string; IsV6 bool}, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be > 0")
	}
	
	p.mu.Lock()
	rec := p.records[domain]
	needResolve := rec == nil
	p.mu.Unlock()
	
	if needResolve {
		var e error
		if rec, e = p.resolveAndStoreQuick(ctx, domain); e != nil {
			return nil, e
		}
		p.mu.Lock()
		rec = p.records[domain]
		p.mu.Unlock()
		if rec == nil {
			return nil, fmt.Errorf("解析后仍无记录: %s", domain)
		}
	}
	
	allowV4 := HasIPv4()
	allowV6 := HasIPv6()
	
	p.mu.Lock()
	rec = p.records[domain]
	if rec == nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("记录不存在: %s", domain)
	}
	if rec.Blacklist == nil { rec.Blacklist = map[string]bool{} }
	if rec.Whitelist == nil { rec.Whitelist = map[string]bool{} }
	
	// 将白名单并入候选列表
	if len(rec.Whitelist) > 0 {
		seen4 := map[string]struct{}{}
		seen6 := map[string]struct{}{}
		for _, ipx := range rec.IPv4 { seen4[ipx] = struct{}{} }
		for _, ipx := range rec.IPv6 { seen6[ipx] = struct{}{} }
		for ipx := range rec.Whitelist {
			parsed := net.ParseIP(ipx)
			if parsed == nil { continue }
			if parsed.To4() != nil {
				if _, ok := seen4[ipx]; !ok { rec.IPv4 = append(rec.IPv4, ipx); seen4[ipx] = struct{}{} }
			} else {
				if _, ok := seen6[ipx]; !ok { rec.IPv6 = append(rec.IPv6, ipx); seen6[ipx] = struct{}{} }
			}
		}
	}
	
	// 合并所有可用IP到单一列表（排除黑名单）
	allAvailableIPs := make([]struct {
		ip   string
		isV6 bool
	}, 0)
	
	preferV6 := config.AppConfig.DNS.PreferIPv6
	
	if allowV6 && (preferV6 || !allowV4) {
		for _, ip := range rec.IPv6 {
			if !rec.Blacklist[ip] {
				allAvailableIPs = append(allAvailableIPs, struct {
					ip   string
					isV6 bool
				}{ip, true})
			}
		}
		if allowV4 {
			for _, ip := range rec.IPv4 {
				if !rec.Blacklist[ip] {
					allAvailableIPs = append(allAvailableIPs, struct {
						ip   string
						isV6 bool
					}{ip, false})
				}
			}
		}
	} else {
		if allowV4 {
			for _, ip := range rec.IPv4 {
				if !rec.Blacklist[ip] {
					allAvailableIPs = append(allAvailableIPs, struct {
						ip   string
						isV6 bool
					}{ip, false})
				}
			}
		}
		if allowV6 {
			for _, ip := range rec.IPv6 {
				if !rec.Blacklist[ip] {
					allAvailableIPs = append(allAvailableIPs, struct {
						ip   string
						isV6 bool
					}{ip, true})
				}
			}
		}
	}
	
	if len(allAvailableIPs) == 0 {
		p.mu.Unlock()
		return nil, fmt.Errorf("无可用IP: %s", domain)
	}
	
	// 批量分配：从当前索引开始，连续分配count个不同的IP
	result := make([]struct{IP string; IsV6 bool}, 0, count)
	unifiedIdx := rec.NextIndex4
	
	for i := 0; i < count; i++ {
		idx := (unifiedIdx + i) % len(allAvailableIPs)
		selected := allAvailableIPs[idx]
		result = append(result, struct{IP string; IsV6 bool}{selected.ip, selected.isV6})
	}
	
	// 更新索引：移动到分配的最后一个IP的下一个
	rec.NextIndex4 = (unifiedIdx + count) % len(allAvailableIPs)
	
	p.mu.Unlock()
	if err := p.persist(); err != nil { log.Printf("[dns-pool] persist failed: %v", err) }
	return result, nil
}

// ReportResult 根据返回码更新黑名单。
func (p *DNSPool) ReportResult(domain, ip string, status int) error {
    if p == nil || ip == "" || domain == "" { return nil }
    p.mu.Lock()
    rec, ok := p.records[domain]
    if !ok {
        rec = &dnsRecord{Domain: domain, Blacklist: map[string]bool{}, Whitelist: map[string]bool{}}
        p.records[domain] = rec
    }
    if rec.Blacklist == nil { rec.Blacklist = map[string]bool{} }
    if rec.Whitelist == nil { rec.Whitelist = map[string]bool{} }
    // 确保观测到的IP被纳入记录
    var ipPresent bool // 标识IP是否已存在于记录中
    if net.ParseIP(ip) != nil && net.ParseIP(ip).To4() == nil {
        // IPv6
        for _, v := range rec.IPv6 { 
            if v == ip { 
                ipPresent = true
                break 
            } 
        }
        if !ipPresent {
            rec.IPv6 = append(rec.IPv6, ip)
            log.Printf("[dns-pool] add %s ip=%s v6=true", domain, ip)
        }
    } else {
        // IPv4 或未知按v4处理
        for _, v := range rec.IPv4 { 
            if v == ip { 
                ipPresent = true
                break 
            } 
        }
        if !ipPresent {
            rec.IPv4 = append(rec.IPv4, ip)
            log.Printf("[dns-pool] add %s ip=%s v6=false", domain, ip)
        }
    }
    if status == 403 {
        rec.Blacklist[ip] = true
        delete(rec.Whitelist, ip)
    } else if status == 200 {
        delete(rec.Blacklist, ip)
        rec.Whitelist[ip] = true
        // 当IP返回200时，异步预热该IP的连接（不阻塞当前请求）
        // 无论是新IP还是已有IP，都尝试预热，确保连接池是热的
        go func(d, i string) {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            PrewarmSingleConnection(ctx, d, net.JoinHostPort(i, "443"))
        }(domain, ip)
    }
    p.mu.Unlock()
    return p.persist()
}

// ReportLatency 上报请求耗时（毫秒），用于更新每个IP的EWMA延迟并自动过滤慢IP
func (p *DNSPool) ReportLatency(domain, ip string, ms int64) error {
    if p == nil || ip == "" || domain == "" || ms <= 0 { return nil }
    const alpha = 0.3
    // 不再使用阈值，改用权重轮询
    p.mu.Lock()
    rec, ok := p.records[domain]
    if !ok {
        rec = &dnsRecord{Domain: domain, IPLatency: make(map[string]float64)}
        p.records[domain] = rec
    }
    if rec.IPLatency == nil {
        rec.IPLatency = make(map[string]float64)
    }
    if rec.Blacklist == nil {
        rec.Blacklist = make(map[string]bool)
    }
    if rec.Whitelist == nil {
        rec.Whitelist = make(map[string]bool)
    }
    val := float64(ms)
    // 更新该IP的EWMA延迟
    if rec.IPLatency[ip] == 0 {
        rec.IPLatency[ip] = val
    } else {
        rec.IPLatency[ip] = alpha*val + (1-alpha)*rec.IPLatency[ip]
    }
    // 不再自动黑名单，保留所有IP用于权重轮询
    // 同时更新族级别的EWMA（兼容旧逻辑）
    if net.ParseIP(ip) != nil && net.ParseIP(ip).To4() == nil {
        if rec.EwmaLatency6 == 0 { rec.EwmaLatency6 = val } else { rec.EwmaLatency6 = alpha*val + (1-alpha)*rec.EwmaLatency6 }
    } else {
        if rec.EwmaLatency4 == 0 { rec.EwmaLatency4 = val } else { rec.EwmaLatency4 = alpha*val + (1-alpha)*rec.EwmaLatency4 }
    }
    p.mu.Unlock()
    return p.persist()
}

// IsBlacklisted 判断IP是否在黑名单
func (p *DNSPool) IsBlacklisted(domain, ip string) (bool, error) {
	rec, err := p.get(domain)
	if err != nil || rec == nil {
		return false, err
	}
	return rec.Blacklist[ip], nil
}

// ListBlacklisted 返回黑名单 IP 列表
func (p *DNSPool) ListBlacklisted(domain string) ([]string, error) {
	rec, err := p.get(domain)
	if err != nil || rec == nil {
		return nil, err
	}
	out := make([]string, 0, len(rec.Blacklist))
	for ip, banned := range rec.Blacklist {
		if banned {
			out = append(out, ip)
		}
	}
	return out, nil
}

// ListWhitelisted 返回白名单 IP 列表
func (p *DNSPool) ListWhitelisted(domain string) ([]string, error) {
	rec, err := p.get(domain)
	if err != nil || rec == nil {
		return nil, err
	}
	out := make([]string, 0, len(rec.Whitelist))
	for ip, ok := range rec.Whitelist {
		if ok {
			out = append(out, ip)
		}
	}
	return out, nil
}

// GetAllRecords 获取所有DNS记录（用于peer同步）
func (p *DNSPool) GetAllRecords(ctx context.Context) map[string]*dnsRecord {
    p.mu.RLock()
    defer p.mu.RUnlock()
    result := make(map[string]*dnsRecord, len(p.records))
    for k, v := range p.records {
        c := *v
        result[k] = &c
    }
    return result
}
