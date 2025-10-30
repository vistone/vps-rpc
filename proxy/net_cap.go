package proxy

import (
    "net"
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


