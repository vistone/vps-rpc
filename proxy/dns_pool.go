package proxy

import (
	"context"
	"encoding/json"
	"fmt"
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

	r := net.Resolver{}
	// A
	if addrs4, err := r.LookupIP(ctx, "ip4", domain); err == nil {
		for _, ip := range addrs4 {
			rec.IPv4 = append(rec.IPv4, ip.String())
		}
	}
	// AAAA
	if addrs6, err := r.LookupIP(ctx, "ip6", domain); err == nil {
		for _, ip := range addrs6 {
			rec.IPv6 = append(rec.IPv6, ip.String())
		}
	}
	rec.UpdatedAt = time.Now().Unix()

	if len(rec.IPv4) == 0 && len(rec.IPv6) == 0 {
		return nil, fmt.Errorf("DNS 无记录: %s", domain)
	}

	if err := p.save(&rec); err != nil {
		return nil, err
	}
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
	if !needRefresh && config.AppConfig.DNS.RefreshInterval != "" {
		if d, e := time.ParseDuration(config.AppConfig.DNS.RefreshInterval); e == nil {
			if time.Since(time.Unix(rec.UpdatedAt, 0)) > d {
				needRefresh = true
			}
		}
	}
	if needRefresh {
		var e error
		rec, e = p.resolveAndStore(ctx, domain)
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
