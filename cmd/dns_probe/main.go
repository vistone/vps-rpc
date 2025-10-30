package main

import (
    "context"
    "crypto/tls"
    "flag"
    "fmt"
    "net"
    "net/http"
    "time"

    "vps-rpc/config"
    "vps-rpc/proxy"
)

func main() {
    var domain string
    var path string
    flag.StringVar(&domain, "domain", "", "目标域名，如 kh.google.com")
    flag.StringVar(&path, "path", "/", "请求路径，如 /rt/earth/PlanetoidMetadata")
    flag.Parse()
    if domain == "" { fmt.Println("必须提供 --domain"); return }

    if err := config.LoadConfig("./config.toml"); err != nil {
        fmt.Println("加载配置失败:", err)
        return
    }

    pool, err := proxy.NewDNSPool(config.AppConfig.DNS.DBPath)
    if err != nil { fmt.Println("打开DNS池失败:", err); return }
    defer pool.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    // 确保有记录
    ipv4, ipv6, _ := pool.GetIPs(ctx, domain)
    // 加上黑名单里的 IP 也需要复测
    bl, _ := pool.ListBlacklisted(domain)

    // 组建待测列表
    candidates := make([]string, 0)
    candidates = append(candidates, ipv6...)
    candidates = append(candidates, ipv4...)
    candidates = append(candidates, bl...)

    // 去重
    seen := map[string]bool{}
    uniq := make([]string, 0, len(candidates))
    for _, ip := range candidates {
        if ip == "" || seen[ip] { continue }
        seen[ip] = true
        uniq = append(uniq, ip)
    }

    fmt.Printf("开始刺探 %s，共 %d 个 IP\n", domain, len(uniq))
    for _, ip := range uniq {
        status := probeIP(ctx, domain, path, ip)
        _ = pool.ReportResult(domain, ip, status)
        fmt.Printf("%s -> %d\n", ip, status)
    }
}

func probeIP(ctx context.Context, domain, path, ip string) int {
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


