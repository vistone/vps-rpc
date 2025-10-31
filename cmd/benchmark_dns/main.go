package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	"vps-rpc/client"
)

// 基准测试：对比DNS解析开销
func main() {
	var addr string
	var iterations int
	flag.StringVar(&addr, "addr", "tile0.zeromaps.cn:4242", "服务器地址")
	flag.IntVar(&iterations, "iterations", 20, "测试迭代次数")
	flag.Parse()

	host, _, _ := net.SplitHostPort(addr)
	isIP := net.ParseIP(host) != nil

	fmt.Printf("=== DNS vs IP 连接性能对比 ===\n")
	fmt.Printf("目标: %s (%s)\n", addr, map[bool]string{true: "IP地址", false: "域名"}[isIP])
	fmt.Printf("迭代次数: %d\n\n", iterations)

	if !isIP {
		// 测试DNS解析时间
		fmt.Printf("1. DNS解析时间测试\n")
		var dnsTotal time.Duration
		for i := 0; i < 10; i++ {
			start := time.Now()
			_, err := net.LookupIP(host)
			elapsed := time.Since(start)
			if err == nil {
				dnsTotal += elapsed
				if i < 3 {
					fmt.Printf("   [%d] DNS解析耗时: %v\n", i+1, elapsed)
				}
			}
		}
		avgDNS := dnsTotal / 10
		fmt.Printf("   平均DNS解析时间: %v\n\n", avgDNS)
	} else {
		fmt.Printf("1. 使用IP地址（跳过DNS解析）\n\n")
	}

	// 测试连接建立时间
	fmt.Printf("2. QUIC连接建立时间测试\n")
	var connTotal time.Duration
	var successCount int

	for i := 0; i < iterations; i++ {
		start := time.Now()
		cli, err := client.NewClient(&client.ClientConfig{
			Address:            addr,
			Timeout:            5 * time.Second,
			InsecureSkipVerify: true,
		})
		connTime := time.Since(start)

		if err == nil {
			successCount++
			connTotal += connTime
			if i < 5 || i%5 == 0 {
				fmt.Printf("   [%02d] 连接建立: %v\n", i+1, connTime)
			}
			cli.Close()
		} else {
			fmt.Printf("   [%02d] 连接失败: %v\n", i+1, err)
		}

		// 短暂延迟避免过快
		if i < iterations-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	if successCount > 0 {
		avgConn := connTotal / time.Duration(successCount)
		fmt.Printf("\n   成功: %d/%d\n", successCount, iterations)
		fmt.Printf("   平均连接建立时间: %v\n", avgConn)
		if !isIP {
			fmt.Printf("\n   时间分解估算:\n")
			fmt.Printf("   - DNS解析: ~%v (已缓存)\n", time.Millisecond*1)
			fmt.Printf("   - QUIC握手: ~%v\n", avgConn-time.Millisecond*1)
			fmt.Printf("   - 网络往返: ~%v\n", avgConn/2)
		}
	}
}

