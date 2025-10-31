package main

import (
    "encoding/json"
    "fmt"
    "net"
    "os"
    "path/filepath"
    "sort"
)

type dnsRecord struct {
    Domain        string            `json:"domain"`
    IPv4          []string          `json:"ipv4"`
    IPv6          []string          `json:"ipv6"`
    UpdatedAt     int64             `json:"updated_at"`
    NextIndex4    int               `json:"next_index4"`
    NextIndex6    int               `json:"next_index6"`
    NextPreferV6  bool              `json:"next_prefer_v6"`
    Blacklist     map[string]bool   `json:"blacklist"`
    Whitelist     map[string]bool   `json:"whitelist"`
    EwmaLatency4  float64           `json:"ewma_latency4"`
    EwmaLatency6  float64           `json:"ewma_latency6"`
}

func uniqAppend(list []string, v string) []string {
    for _, x := range list { if x == v { return list } }
    return append(list, v)
}

func main() {
    if len(os.Args) < 4 {
        fmt.Fprintln(os.Stderr, "usage: update_dns_pool <dns_pool.json> <domain> <ip1,ip2,...>")
        os.Exit(2)
    }
    file := os.Args[1]
    domain := os.Args[2]
    ipsCsv := os.Args[3]
    ips := []string{}
    for _, part := range splitCSV(ipsCsv) {
        if part != "" { ips = append(ips, part) }
    }
    // load
    m := map[string]*dnsRecord{}
    if b, err := os.ReadFile(file); err == nil && len(b) > 0 {
        _ = json.Unmarshal(b, &m)
    }
    rec, ok := m[domain]
    if !ok || rec == nil {
        rec = &dnsRecord{Domain: domain, Blacklist: map[string]bool{}, Whitelist: map[string]bool{}}
        m[domain] = rec
    }
    if rec.Blacklist == nil { rec.Blacklist = map[string]bool{} }
    if rec.Whitelist == nil { rec.Whitelist = map[string]bool{} }
    for _, ip := range ips {
        parsed := net.ParseIP(ip)
        if parsed == nil { continue }
        if parsed.To4() != nil {
            rec.IPv4 = uniqAppend(rec.IPv4, ip)
        } else {
            rec.IPv6 = uniqAppend(rec.IPv6, ip)
        }
        rec.Whitelist[ip] = true
        delete(rec.Blacklist, ip)
    }
    // sort for stability
    sort.Strings(rec.IPv4)
    sort.Strings(rec.IPv6)
    // ensure dir
    _ = os.MkdirAll(filepath.Dir(file), 0o755)
    out, _ := json.MarshalIndent(m, "", "  ")
    if err := os.WriteFile(file, out, 0o644); err != nil {
        fmt.Fprintln(os.Stderr, "write file failed:", err)
        os.Exit(1)
    }
}

func splitCSV(s string) []string {
    out := []string{}
    cur := ""
    for i := 0; i < len(s); i++ {
        if s[i] == ',' { out = append(out, cur); cur = "" } else { cur += string(s[i]) }
    }
    out = append(out, cur)
    return out
}


