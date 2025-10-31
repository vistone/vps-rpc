package proxy

import (
    "context"
    "crypto/tls"
    "log"
    "net"
    "net/http"
    "sync"
    "time"
)

// ObservationReport 外部/本地观测报告（仅共享报告，不共享黑名单状态）
type ObservationReport struct {
    Domain    string
    IP        string
    Status    int    // 200 or 403
    SourceID  string // 上报节点ID（预留）
    ObservedAt int64
}

// ProbeManager 管理待验证队列与异步复测，以及定期DNS探测
type ProbeManager struct {
    dns            *DNSPool
    queue          chan ObservationReport
    closed         chan struct{}
    wg             sync.WaitGroup
    probeInterval  time.Duration // 定期探测间隔（默认5分钟）
    activeDomains  map[string]bool // 需要探测的域名集合
    domainsMu      sync.RWMutex
}

func NewProbeManager(dns *DNSPool) *ProbeManager {
    pm := &ProbeManager{
        dns:           dns,
        queue:         make(chan ObservationReport, 1024),
        closed:        make(chan struct{}),
        probeInterval: 5 * time.Minute, // 默认5分钟探测一次
        activeDomains: make(map[string]bool),
    }
    pm.wg.Add(2) // worker + periodic probe
    go pm.worker()
    go pm.startPeriodicProbe()
    return pm
}

// RegisterDomain 注册需要定期探测的域名
func (p *ProbeManager) RegisterDomain(domain string) {
    p.domainsMu.Lock()
    defer p.domainsMu.Unlock()
    p.activeDomains[domain] = true
    log.Printf("[probe] 注册域名探测: %s", domain)
}

func (p *ProbeManager) Close() {
    close(p.closed)
    p.wg.Wait()
}

// Submit 外部报告进入本地待验证队列
func (p *ProbeManager) Submit(r ObservationReport) {
    select { case p.queue <- r: default: /* 丢弃过载报告，防止阻塞 */ }
}

func (p *ProbeManager) worker() {
    defer p.wg.Done()
    for {
        select {
        case <-p.closed:
            return
        case r := <-p.queue:
            p.verifyOnce(r)
        }
    }
}

func (p *ProbeManager) verifyOnce(r ObservationReport) {
    if p.dns == nil || r.Domain == "" || r.IP == "" { return }
    // 直连指定 IP 进行复测：HTTPS GET，Host/SNI 为域名
    ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
    defer cancel()
    status := doProbeIP(ctx, r.Domain, r.IP, "/")
    _ = p.dns.ReportResult(r.Domain, r.IP, status)
}

func doProbeIP(ctx context.Context, domain, ip, path string) int {
    url := "https://" + net.JoinHostPort(ip, "443") + path
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Host = domain
    tr := &http.Transport{ TLSClientConfig: &tls.Config{ ServerName: domain, InsecureSkipVerify: true } }
    client := &http.Client{ Timeout: 10 * time.Second, Transport: tr }
    resp, err := client.Do(req)
    if err != nil { return 0 }
    defer resp.Body.Close()
    return resp.StatusCode
}

// startPeriodicProbe 定期DNS探测任务（每5分钟）
func (p *ProbeManager) startPeriodicProbe() {
    defer p.wg.Done()
    ticker := time.NewTicker(p.probeInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-p.closed:
            return
        case <-ticker.C:
            p.probeAllDomains()
        }
    }
}

// probeAllDomains 探测所有注册的域名
func (p *ProbeManager) probeAllDomains() {
    p.domainsMu.RLock()
    domains := make([]string, 0, len(p.activeDomains))
    for domain := range p.activeDomains {
        domains = append(domains, domain)
    }
    p.domainsMu.RUnlock()
    
    if len(domains) == 0 {
        return
    }
    
    log.Printf("[probe] 开始定期DNS探测，共 %d 个域名", len(domains))
    
    for _, domain := range domains {
        p.probeDomain(domain)
    }
}

// probeDomain 探测单个域名：重新解析DNS并测试新发现的IP
func (p *ProbeManager) probeDomain(domain string) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // 1. 获取当前已有的IP列表
    currentIPv4, currentIPv6, _ := p.dns.GetIPs(ctx, domain)
    currentIPs := make(map[string]bool)
    for _, ip := range currentIPv4 {
        currentIPs[ip] = true
    }
    for _, ip := range currentIPv6 {
        currentIPs[ip] = true
    }
    
    // 2. 强制重新解析DNS（忽略缓存，进行新的探测）
    rec, err := p.dns.resolveAndStore(ctx, domain)
    if err != nil {
        log.Printf("[probe] DNS解析失败 %s: %v", domain, err)
        return
    }
    
    // 3. 找出新发现的IP
    newIPs := make([]string, 0)
    for _, ip := range rec.IPv4 {
        if !currentIPs[ip] {
            newIPs = append(newIPs, ip)
        }
    }
    for _, ip := range rec.IPv6 {
        if !currentIPs[ip] {
            newIPs = append(newIPs, ip)
        }
    }
    
    if len(newIPs) > 0 {
        log.Printf("[probe] %s 发现 %d 个新IP: %v", domain, len(newIPs), newIPs)
        
        // 4. 测试新IP的连通性（使用常见路径）
        testPaths := []string{"/", "/rt/earth/PlanetoidMetadata"}
        validIPs := 0
        
        for _, ip := range newIPs {
            for _, path := range testPaths {
                ctx2, cancel2 := context.WithTimeout(context.Background(), 8*time.Second)
                status := doProbeIP(ctx2, domain, ip, path)
                cancel2()
                
                // 200或404都算可用
                if status == 200 || status == 404 {
                    validIPs++
                    // 上报成功结果，加入白名单
                    _ = p.dns.ReportResult(domain, ip, status)
                    log.Printf("[probe] ✅ 新IP可用: %s (status=%d)", ip, status)
                    break // 找到可用路径即可
                }
            }
        }
        
        log.Printf("[probe] %s 验证完成: %d/%d 个新IP可用", domain, validIPs, len(newIPs))
    } else {
        log.Printf("[probe] %s 未发现新IP (当前: IPv4=%d, IPv6=%d)", domain, len(rec.IPv4), len(rec.IPv6))
    }
}


