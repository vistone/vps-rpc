package proxy

import (
    "net"
    "sync"
    "time"
)

// HasIPv4 返回系统是否存在可用的 IPv4 出口（任意接口具备非回环IPv4）
func HasIPv4() bool {
    ifaces, _ := net.Interfaces()
    for _, iface := range ifaces {
        if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 { continue }
        addrs, _ := iface.Addrs()
        for _, a := range addrs {
            ip, _, _ := net.ParseCIDR(a.String())
            if ip != nil && ip.To4() != nil { return true }
        }
    }
    return false
}

// HasIPv6 返回系统是否存在可用的 IPv6 出口（任意接口具备非回环IPv6）
func HasIPv6() bool {
    ifaces, _ := net.Interfaces()
    for _, iface := range ifaces {
        if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 { continue }
        addrs, _ := iface.Addrs()
        for _, a := range addrs {
            ip, _, _ := net.ParseCIDR(a.String())
            if ip != nil && ip.To4() == nil { return true }
        }
    }
    return false
}

// DiscoverIPv6LocalAddrs 返回可用的本机 IPv6 源地址列表（全局单播）
func DiscoverIPv6LocalAddrs() []net.IP {
    out := []net.IP{}
    ifaces, _ := net.Interfaces()
    for _, iface := range ifaces {
        if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 { continue }
        addrs, _ := iface.Addrs()
        for _, a := range addrs {
            ip, _, _ := net.ParseCIDR(a.String())
            if ip == nil || ip.To4() != nil { continue }
            // 简单过滤：排除链路本地 fe80::/10
            if ip.IsLinkLocalUnicast() { continue }
            out = append(out, ip)
        }
    }
    return out
}

// 全局 IPv6 源地址池与轮询索引
var (
    v6PoolOnce sync.Once
    v6Pool     []net.IP
    v6Idx      int
    v6Mu       sync.Mutex
)

// refreshV6Pool 定期刷新本机 IPv6 源地址池
func refreshV6Pool() {
    v6Mu.Lock()
    defer v6Mu.Unlock()
    v6Pool = DiscoverIPv6LocalAddrs()
    if v6Idx >= len(v6Pool) { v6Idx = 0 }
}

// StartV6PoolRefresher 启动后台刷新任务（1分钟更新一次）
func StartV6PoolRefresher() {
    v6PoolOnce.Do(func() {
        refreshV6Pool()
        go func() {
            ticker := time.NewTicker(1 * time.Minute)
            defer ticker.Stop()
            for range ticker.C { refreshV6Pool() }
        }()
    })
}

// NextIPv6LocalAddr 轮询返回一个本机 IPv6 源地址
func NextIPv6LocalAddr() net.IP {
    StartV6PoolRefresher()
    v6Mu.Lock()
    defer v6Mu.Unlock()
    if len(v6Pool) == 0 { return nil }
    ip := v6Pool[v6Idx]
    v6Idx = (v6Idx + 1) % len(v6Pool)
    return ip
}


