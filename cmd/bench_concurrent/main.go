package main

import (
    "context"
    "flag"
    "fmt"
    "sort"
    "sync"
    "time"

    "vps-rpc/client"
    "vps-rpc/rpc"
)

func main() {
    var addr string
    var n int
    var conc int
    var url string
    flag.StringVar(&addr, "addr", "127.0.0.1:4242", "服务器地址")
    flag.IntVar(&n, "n", 200, "总请求数")
    flag.IntVar(&conc, "c", 50, "并发数")
    flag.StringVar(&url, "url", "https://kh.google.com/rt/earth/PlanetoidMetadata", "抓取URL")
    flag.Parse()

    cli, err := client.NewClient(&client.ClientConfig{
        Address:            addr,
        Timeout:            20 * time.Second,
        InsecureSkipVerify: true,
        EnableConnectionPool: false,
    })
    if err != nil {
        fmt.Println("创建客户端失败:", err)
        return
    }
    defer cli.Close()

    fmt.Printf("并发压测: addr=%s, n=%d, c=%d, url=%s\n", addr, n, conc, url)

    type res struct{ ok bool; ms float64 }
    results := make([]res, n)

    wg := sync.WaitGroup{}
    sem := make(chan struct{}, conc)
    startAll := time.Now()
    for i := 0; i < n; i++ {
        wg.Add(1)
        sem <- struct{}{}
        go func(idx int) {
            defer wg.Done()
            defer func(){ <-sem }()
            ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
            t0 := time.Now()
            _, err := cli.Fetch(ctx, &rpc.FetchRequest{ Url: url, TlsClient: rpc.TLSClientType_CHROME })
            cancel()
            elapsed := time.Since(t0).Seconds() * 1000
            results[idx] = res{ ok: err == nil, ms: elapsed }
        }(i)
    }
    wg.Wait()
    totalElapsed := time.Since(startAll)

    // stats
    var lat []float64
    okCount := 0
    for _, r := range results {
        if r.ok {
            okCount++
            lat = append(lat, r.ms)
        }
    }
    sort.Float64s(lat)
    p := func(p float64) float64 { if len(lat)==0 {return 0}; idx := int(float64(len(lat)-1)*p); return lat[idx] }
    avg := 0.0
    for _, v := range lat { avg += v }
    if len(lat) > 0 { avg /= float64(len(lat)) }

    fmt.Printf("成功: %d/%d, 总耗时: %v\n", okCount, n, totalElapsed)
    if len(lat) > 0 {
        fmt.Printf("p50=%.1fms p90=%.1fms p95=%.1fms p99=%.1fms avg=%.1fms min=%.1fms max=%.1fms\n",
            p(0.50), p(0.90), p(0.95), p(0.99), avg, lat[0], lat[len(lat)-1])
        rps := float64(n) / totalElapsed.Seconds()
        fmt.Printf("吞吐: %.1f req/s, 并发=%d\n", rps, conc)
    }
}


