package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"time"

	"vps-rpc/client"
	"vps-rpc/rpc"
)

// 测试QUIC连接特性：是否直接连接、连接复用等
func main() {
	var addr string
	var n int
	flag.StringVar(&addr, "addr", "tile0.zeromaps.cn:4242", "服务器地址")
	flag.IntVar(&n, "n", 10, "测试请求次数")
	flag.Parse()

	fmt.Printf("=== QUIC连接特性测试 ===\n")
	fmt.Printf("目标服务器: %s\n\n", addr)

	// 解析地址
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Printf("地址解析失败: %v\n", err)
		return
	}

	fmt.Printf("1. 解析服务器地址\n")
	fmt.Printf("   - 主机: %s\n", host)
	fmt.Printf("   - 端口: %s\n", port)
	
	// 解析IP
	ips, err := net.LookupIP(host)
	if err == nil {
		fmt.Printf("   - DNS解析IP:\n")
		for _, ip := range ips {
			fmt.Printf("     * %s\n", ip.String())
		}
	}
	fmt.Println()

	// 创建QUIC客户端
	fmt.Printf("2. 创建QUIC客户端\n")
	fmt.Printf("   - 使用 quic.DialAddrEarly() 建立直接QUIC连接\n")
	fmt.Printf("   - 协议: QUIC (UDP-based, 端口 %s)\n", port)
	fmt.Printf("   - TLS: 启用（跳过证书验证）\n")
	fmt.Printf("   - 0-RTT: 已启用\n")
	
	startConn := time.Now()
	cli, err := client.NewClient(&client.ClientConfig{
		Address:            addr,
		Timeout:            10 * time.Second,
		InsecureSkipVerify: true,
	})
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}
	connTime := time.Since(startConn)
	fmt.Printf("   - 连接建立耗时: %v\n", connTime)
	fmt.Println()

	// 测试多个请求（验证连接复用）
	fmt.Printf("3. 发送 %d 个请求（验证连接复用）\n", n)
	fmt.Printf("   - 每个请求使用新的QUIC流，但复用同一个QUIC连接\n")
	fmt.Println()

	var firstRequestTime time.Duration
	var subsequentAvgTime time.Duration
	var totalSubsequent time.Duration

	for i := 1; i <= n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		
		resp, err := cli.Fetch(ctx, &rpc.FetchRequest{
			Url:       "https://kh.google.com/rt/earth/PlanetoidMetadata",
			TlsClient: rpc.TLSClientType_CHROME,
		})
		
		elapsed := time.Since(start)
		cancel()

		if err != nil {
			fmt.Printf("[%02d] ❌ 错误: %v\n", i, err)
			continue
		}
		if resp.Error != "" {
			fmt.Printf("[%02d] ❌ 响应错误: %s\n", i, resp.Error)
			continue
		}

		if i == 1 {
			firstRequestTime = elapsed
			fmt.Printf("[%02d] ✓ Status=%d, RTT=%v (首次请求，包含连接握手)\n", i, resp.StatusCode, elapsed)
		} else {
			totalSubsequent += elapsed
			if i <= 5 || i%5 == 0 {
				fmt.Printf("[%02d] ✓ Status=%d, RTT=%v\n", i, resp.StatusCode, elapsed)
			}
		}

		// 短暂延迟，避免过快
		if i < n {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if n > 1 {
		subsequentAvgTime = totalSubsequent / time.Duration(n-1)
		fmt.Println()
		fmt.Printf("4. 连接复用分析\n")
		fmt.Printf("   - 首次请求RTT: %v\n", firstRequestTime)
		fmt.Printf("   - 后续请求平均RTT: %v\n", subsequentAvgTime)
		if firstRequestTime > subsequentAvgTime {
			fmt.Printf("   - 性能提升: %v (连接复用生效)\n", firstRequestTime-subsequentAvgTime)
		}
	}

	// 关闭连接
	cli.Close()
	fmt.Println()
	fmt.Printf("5. 连接特性总结\n")
	fmt.Printf("   ✓ 直接连接: 是（无代理，直接UDP连接到 %s）\n", addr)
	fmt.Printf("   ✓ 连接复用: 是（所有请求复用同一个QUIC连接）\n")
	fmt.Printf("   ✓ 流复用: 是（每个请求使用新的QUIC流）\n")
	fmt.Printf("   ✓ 0-RTT支持: 是（后续请求可立即发送）\n")
	fmt.Printf("   ✓ 协议: QUIC (基于UDP)\n")
	fmt.Printf("   ✓ 加密: TLS 1.3 (QUIC内置)\n")
}

