package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"vps-rpc/config"

	bolt "go.etcd.io/bbolt"
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
}

type DNSPool struct {
	db *bolt.DB
}

func NewDNSPool(dbPath string) (*DNSPool, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists([]byte("dns_records"))
		return e
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &DNSPool{db: db}, nil
}

func (p *DNSPool) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
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
			break
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
	return p.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("dns_records"))
		data, _ := json.Marshal(rec)
		return b.Put([]byte(rec.Domain), data)
	})
}

func (p *DNSPool) get(domain string) (*dnsRecord, error) {
	var out *dnsRecord
	err := p.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("dns_records"))
		v := b.Get([]byte(domain))
		if v == nil {
			return nil
		}
		var rec dnsRecord
		if e := json.Unmarshal(v, &rec); e != nil {
			return e
		}
		if rec.Blacklist == nil {
			rec.Blacklist = map[string]bool{}
		}
		if rec.Whitelist == nil {
			rec.Whitelist = map[string]bool{}
		}
		out = &rec
		return nil
	})
	return out, err
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

// NextIP 轮询选择一个 IP（优先IPv6，其次IPv4；可调整策略）
func (p *DNSPool) NextIP(ctx context.Context, domain string) (ip string, isV6 bool, err error) {
	rec, e := p.get(domain)
	if e != nil {
		return "", false, e
	}
	if rec == nil {
		if rec, e = p.resolveAndStore(ctx, domain); e != nil {
			return "", false, e
		}
	}
	allowV4 := HasIPv4()
	allowV6 := HasIPv6()
	// 严格轮询：两族都可用时按 NextPreferV6 交替；否则按可用族
	tryV6First := allowV6 && (!allowV4 || rec.NextPreferV6)
	// 先尝试V6
	if tryV6First && len(rec.IPv6) > 0 {
		for i := 0; i < len(rec.IPv6); i++ {
			idx := rec.NextIndex6 % len(rec.IPv6)
			rec.NextIndex6 = (rec.NextIndex6 + 1) % len(rec.IPv6)
			candidate := rec.IPv6[idx]
			if !rec.Blacklist[candidate] {
				if allowV4 && allowV6 {
					rec.NextPreferV6 = false
				}
				_ = p.save(rec)
				return candidate, true, nil
			}
		}
	}
	if allowV4 && len(rec.IPv4) > 0 {
		for i := 0; i < len(rec.IPv4); i++ {
			idx := rec.NextIndex4 % len(rec.IPv4)
			rec.NextIndex4 = (rec.NextIndex4 + 1) % len(rec.IPv4)
			candidate := rec.IPv4[idx]
			if !rec.Blacklist[candidate] {
				if allowV4 && allowV6 {
					rec.NextPreferV6 = true
				}
				_ = p.save(rec)
				return candidate, false, nil
			}
		}
	}
	// 如果未尝试V6且允许，最后再试一次V6（交替情况下）
	if !tryV6First && allowV6 && len(rec.IPv6) > 0 {
		for i := 0; i < len(rec.IPv6); i++ {
			idx := rec.NextIndex6 % len(rec.IPv6)
			rec.NextIndex6 = (rec.NextIndex6 + 1) % len(rec.IPv6)
			candidate := rec.IPv6[idx]
			if !rec.Blacklist[candidate] {
				if allowV4 && allowV6 {
					rec.NextPreferV6 = false
				}
				_ = p.save(rec)
				return candidate, true, nil
			}
		}
	}
	return "", false, fmt.Errorf("无可用IP: %s", domain)
}

// ReportResult 根据返回码更新黑名单。
func (p *DNSPool) ReportResult(domain, ip string, status int) error {
	if p == nil || ip == "" || domain == "" {
		return nil
	}
	return p.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("dns_records"))
		v := b.Get([]byte(domain))
		var rec dnsRecord
		if v != nil {
			_ = json.Unmarshal(v, &rec)
		}
		if rec.Domain == "" {
			rec.Domain = domain
		}
		if rec.Blacklist == nil {
			rec.Blacklist = map[string]bool{}
		}
		if rec.Whitelist == nil {
			rec.Whitelist = map[string]bool{}
		}
		// 仅 403 拉黑；200 解封并加入白名单；其他状态码不变
		if status == 403 {
			rec.Blacklist[ip] = true
			delete(rec.Whitelist, ip)
		} else if status == 200 {
			delete(rec.Blacklist, ip)
			rec.Whitelist[ip] = true
		}
		data, _ := json.Marshal(&rec)
		return b.Put([]byte(domain), data)
	})
}

// ReportLatency 上报请求耗时（毫秒），用于更新 EWMA
func (p *DNSPool) ReportLatency(domain, ip string, ms int64) error {
	if p == nil || ip == "" || domain == "" || ms <= 0 {
		return nil
	}
	const alpha = 0.3 // EWMA 平滑系数
	return p.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("dns_records"))
		v := b.Get([]byte(domain))
		var rec dnsRecord
		if v != nil {
			_ = json.Unmarshal(v, &rec)
		}
		if rec.Domain == "" {
			rec.Domain = domain
		}
		// 判定族
		val := float64(ms)
		if net.ParseIP(ip) != nil && net.ParseIP(ip).To4() == nil {
			if rec.EwmaLatency6 == 0 {
				rec.EwmaLatency6 = val
			} else {
				rec.EwmaLatency6 = alpha*val + (1-alpha)*rec.EwmaLatency6
			}
		} else {
			if rec.EwmaLatency4 == 0 {
				rec.EwmaLatency4 = val
			} else {
				rec.EwmaLatency4 = alpha*val + (1-alpha)*rec.EwmaLatency4
			}
		}
		data, _ := json.Marshal(&rec)
		return b.Put([]byte(domain), data)
	})
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
