package main

import (
    "context"
    "flag"
    "fmt"
    "strings"
    "time"

    "vps-rpc/client"
    "vps-rpc/rpc"
)

// 简单压测与混合探测工具
// 示例：
//   go run cmd/regress/main.go -addr tile0.zeromaps.cn:4242 -mode burst -n 30
//   go run cmd/regress/main.go -addr tile0.zeromaps.cn:4242 -mode mix -n 50 \
//       -domain kh.google.com -path /rt/earth/PlanetoidMetadata \
//       -ip4 142.250.204.46 -ip6 2404:6800:4012:9::200e

func main() {
    var addr string
    var mode string
    var n int
    var domain string
    var path string
    var ip4 string
    var ip6 string
    var random bool
    var rounds int

    flag.StringVar(&addr, "addr", "127.0.0.1:4242", "目标 gRPC 地址 host:port")
    flag.StringVar(&mode, "mode", "burst", "测试模式：burst | mix")
    flag.IntVar(&n, "n", 30, "请求次数")
    flag.StringVar(&domain, "domain", "kh.google.com", "混合模式的目标域名")
    flag.StringVar(&path, "path", "/rt/earth/PlanetoidMetadata", "混合模式的路径")
    flag.StringVar(&ip4, "ip4", "", "混合模式使用的IPv4")
    flag.StringVar(&ip6, "ip6", "", "混合模式使用的IPv6")
    flag.BoolVar(&random, "random", false, "启用随机并发(3-8)的轮询压测，仅对 burst 模式有效")
    flag.IntVar(&rounds, "rounds", 10, "随机并发轮数(仅在 --random 下生效)")
    flag.Parse()

    cli, err := client.NewClient(&client.ClientConfig{
        Address: addr,
        Timeout: 25 * time.Second,
        InsecureSkipVerify: true,
        EnableConnectionPool: true,
        MaxPoolSize: 16,
    })
    if err != nil {
        fmt.Println("创建客户端失败:", err)
        return
    }
    defer cli.Close()

    switch strings.ToLower(mode) {
    case "burst":
        if random {
            runRandomBurst(cli, rounds)
        } else {
            runBurst(cli, n)
        }
    case "mix":
        if ip4 == "" || ip6 == "" {
            fmt.Println("混合模式需要提供 --ip4 与 --ip6")
            return
        }
        runMix(cli, n, domain, path, ip4, ip6)
    default:
        fmt.Println("未知模式:", mode)
    }
}

func runBurst(cli *client.Client, n int) {
    url := "https://kh.google.com/rt/earth/PlanetoidMetadata"
    ok := 0
    for i := 1; i <= n; i++ {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        start := time.Now()
        resp, err := cli.Fetch(ctx, &rpc.FetchRequest{Url: url, TlsClient: rpc.TLSClientType_CHROME})
        cancel()
        d := time.Since(start)
        if err != nil {
            fmt.Printf("[%02d] err=%v %v\n", i, err, d)
        } else if resp.Error != "" {
            fmt.Printf("[%02d] respErr=%s %v\n", i, resp.Error, d)
        } else {
            fmt.Printf("[%02d] 200 %dB %v\n", i, len(resp.Body), d)
            ok++
        }
        time.Sleep(120 * time.Millisecond)
    }
    fmt.Printf("ok=%d/%d\n", ok, n)
}

// runRandomBurst 以随机并发(3-8)进行多轮压测，避免固定并发触发风控
func runRandomBurst(cli *client.Client, rounds int) {
    url := "https://kh.google.com/rt/earth/PlanetoidMetadata"
    if rounds <= 0 { rounds = 1 }
    okTotal := 0
    for r := 1; r <= rounds; r++ {
        // 随机并发 3-8
        c := int(time.Now().UnixNano()%6) + 3 // 简易随机，无需引入额外包
        if c < 3 { c = 3 } else if c > 8 { c = 8 }
        done := make(chan struct{}, c)
        for i := 0; i < c; i++ {
            go func() {
                ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
                _, err := cli.Fetch(ctx, &rpc.FetchRequest{Url: url, TlsClient: rpc.TLSClientType_CHROME})
                cancel()
                if err == nil { okTotal++ }
                done <- struct{}{}
            }()
        }
        for i := 0; i < c; i++ { <-done }
        fmt.Printf("[round %d] concurrency=%d\n", r, c)
        time.Sleep(200 * time.Millisecond)
    }
    fmt.Printf("ok_total=%d\n", okTotal)
}

func runMix(cli *client.Client, n int, domain, pth, ip4, ip6 string) {
    mk := func(host string) string { return fmt.Sprintf("https://%s%s", host, pth) }
    domainURL := "https://" + domain + pth

    ok, fail := 0, 0
    for i := 1; i <= n; i++ {
        var url string
        headers := map[string]string{}
        switch i % 3 {
        case 1:
            url = domainURL
        case 2:
            url = mk(ip4)
            headers["Host"] = domain
        case 0:
            url = mk("[" + ip6 + "]")
            headers["Host"] = domain
        }

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        st := time.Now()
        resp, err := cli.Fetch(ctx, &rpc.FetchRequest{Url: url, Headers: headers, TlsClient: rpc.TLSClientType_CHROME})
        cancel()
        dur := time.Since(st)

        if err != nil {
            fmt.Printf("[%02d] ✗ %v %v url=%s\n", i, err, dur, url)
            fail++
        } else if resp.Error != "" {
            fmt.Printf("[%02d] ✗ %s %v url=%s\n", i, resp.Error, dur, url)
            fail++
        } else {
            fmt.Printf("[%02d] ✓ %d %v url=%s\n", i, resp.StatusCode, dur, url)
            ok++
        }
        time.Sleep(150 * time.Millisecond)
    }
    fmt.Printf("result ok=%d fail=%d\n", ok, fail)
}


