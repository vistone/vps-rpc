package proxy

import (
    "bufio"
    "net"
    "os"
    "strings"
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
            if ip == nil || ip.To4() != nil { continue }
            // 只把“全局单播”视为可用IPv6：排除环回与链路本地（fe80::/10）
            if ip.IsLoopback() || ip.IsLinkLocalUnicast() { continue }
            return true
        }
    }
    return false
}

// DiscoverIPv6LocalAddrs 返回可用的本机 IPv6 源地址列表（全局单播）
// 改进：直接从/proc/net/if_inet6读取所有IPv6地址，包括所有接口和别名地址
// 这样可以检测到net.Interfaces()可能遗漏的地址
func DiscoverIPv6LocalAddrs() []net.IP {
    out := []net.IP{}
    seen := make(map[string]bool) // 用于去重
    
    // 方法1：从/proc/net/if_inet6读取所有IPv6地址（最完整）
    // 格式：address scope flags interface
    if file, err := os.Open("/proc/net/if_inet6"); err == nil {
        defer file.Close()
        scanner := bufio.NewScanner(file)
        for scanner.Scan() {
            line := strings.TrimSpace(scanner.Text())
            if line == "" {
                continue
            }
            
            // 解析/proc/net/if_inet6格式
            // 例如: 2408820718e66ce0e21e0cab8a2a6753 03 40 00 01 wlp0s20f3
            fields := strings.Fields(line)
            if len(fields) < 6 {
                continue
            }
            
            // 第一个字段是IPv6地址（32个十六进制字符）
            hexAddr := fields[0]
            if len(hexAddr) != 32 {
                continue
            }
            
            // 转换为net.IP
            ip := parseHexIPv6(hexAddr)
            if ip == nil {
                continue
            }
            
            // 排除链路本地地址 fe80::/10 和环回地址 ::1
            if ip.IsLinkLocalUnicast() || ip.IsLoopback() {
                continue
            }
            
            // 去重
            ipStr := ip.String()
            if seen[ipStr] {
                continue
            }
            seen[ipStr] = true
            
            out = append(out, ip)
        }
    }
    
    // 方法2：如果/proc/net/if_inet6不可用，回退到net.Interfaces()方法
    if len(out) == 0 {
        ifaces, err := net.Interfaces()
        if err == nil {
            for _, iface := range ifaces {
                if iface.Flags&net.FlagUp == 0 { continue }
                
                addrs, err := iface.Addrs()
                if err != nil {
                    continue
                }
                
                for _, a := range addrs {
                    ip, _, err := net.ParseCIDR(a.String())
                    if err != nil || ip == nil {
                        continue
                    }
                    
                    if ip.To4() != nil {
                        continue
                    }
                    
                    if ip.IsLinkLocalUnicast() || ip.IsLoopback() {
                        continue
                    }
                    
                    ipStr := ip.String()
                    if seen[ipStr] {
                        continue
                    }
                    seen[ipStr] = true
                    
                    out = append(out, ip)
                }
            }
        }
    }
    
    return out
}

// parseHexIPv6 将32个十六进制字符转换为net.IP
func parseHexIPv6(hex string) net.IP {
    if len(hex) != 32 {
        return nil
    }
    
    ip := make(net.IP, 16)
    for i := 0; i < 32; i += 2 {
        var b byte
        for j := 0; j < 2; j++ {
            c := hex[i+j]
            var val byte
            if c >= '0' && c <= '9' {
                val = c - '0'
            } else if c >= 'a' && c <= 'f' {
                val = c - 'a' + 10
            } else if c >= 'A' && c <= 'F' {
                val = c - 'A' + 10
            } else {
                return nil
            }
            if j == 0 {
                b = val << 4
            } else {
                b |= val
            }
        }
        ip[i/2] = b
    }
    
    return ip
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


