package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"vps-rpc/client"
	"vps-rpc/rpc"
)

// 测试RPC往返延迟（RTT）
// 测量从本地发送请求到收到响应的时间，不包括实际HTTP抓取时间
func main() {
	var addr string
	var n int
	flag.StringVar(&addr, "addr", "127.0.0.1:4242", "目标 gRPC 地址")
	flag.IntVar(&n, "n", 100, "测试次数")
	flag.Parse()

	cli, err := client.NewClient(&client.ClientConfig{
		Address:            addr,
		Timeout:            10 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: true,
		MaxPoolSize:        16,
	})
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}
	defer cli.Close()

	fmt.Printf("开始测试 RPC 往返延迟，目标: %s，测试次数: %d\n\n", addr, n)

	url := "https://www.example.com" // 简单的测试URL，减少实际HTTP请求时间

	var totalLatency time.Duration
	var minLatency time.Duration = time.Hour
	var maxLatency time.Duration
	successCount := 0

	for i := 1; i <= n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		resp, err := cli.Fetch(ctx, &rpc.FetchRequest{
			Url:       url,
			TlsClient: rpc.TLSClientType_CHROME,
		})
		elapsed := time.Since(start)
		cancel()

		if err != nil {
			fmt.Printf("[%03d] ❌ 错误: %v, 耗时: %v\n", i, err, elapsed)
			continue
		}
		if resp.Error != "" {
			fmt.Printf("[%03d] ❌ 响应错误: %s, 耗时: %v\n", i, resp.Error, elapsed)
			continue
		}

		successCount++
		totalLatency += elapsed

		if elapsed < minLatency {
			minLatency = elapsed
		}
		if elapsed > maxLatency {
			maxLatency = elapsed
		}

		if i%10 == 0 || i <= 5 {
			fmt.Printf("[%03d] ✓ Status=%d, RTT=%v\n", i, resp.StatusCode, elapsed)
		}
	}

	fmt.Printf("\n=== RPC 往返延迟统计 ===\n")
	fmt.Printf("成功请求: %d/%d\n", successCount, n)
	if successCount > 0 {
		avgLatency := totalLatency / time.Duration(successCount)
		fmt.Printf("平均 RTT: %v\n", avgLatency)
		fmt.Printf("最小 RTT: %v\n", minLatency)
		fmt.Printf("最大 RTT: %v\n", maxLatency)
		fmt.Printf("\n注意: 此延迟包含:\n")
		fmt.Printf("  - RPC 请求发送时间\n")
		fmt.Printf("  - 网络传输时间\n")
		fmt.Printf("  - 实际 HTTP 抓取时间\n")
		fmt.Printf("  - RPC 响应接收时间\n")
		fmt.Printf("\n纯 RPC 往返时间 ≈ (总延迟 - HTTP抓取时间)\n")
		fmt.Printf("可用 ping 或 TCP 连接测试来估算网络基础延迟\n")
	}
}
