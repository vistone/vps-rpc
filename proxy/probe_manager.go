package proxy

import (
    "context"
    "crypto/tls"
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

// ProbeManager 管理待验证队列与异步复测
type ProbeManager struct {
    dns    *DNSPool
    queue  chan ObservationReport
    closed chan struct{}
    wg     sync.WaitGroup
}

func NewProbeManager(dns *DNSPool) *ProbeManager {
    pm := &ProbeManager{
        dns:    dns,
        queue:  make(chan ObservationReport, 1024),
        closed: make(chan struct{}),
    }
    pm.wg.Add(1)
    go pm.worker()
    return pm
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
    tr := &http.Transport{ TLSClientConfig: &tls.Config{ ServerName: domain } }
    client := &http.Client{ Timeout: 10 * time.Second, Transport: tr }
    resp, err := client.Do(req)
    if err != nil { return 0 }
    defer resp.Body.Close()
    return resp.StatusCode
}


