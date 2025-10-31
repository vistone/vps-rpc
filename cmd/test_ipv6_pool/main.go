package main

import (
	"fmt"
	"vps-rpc/proxy"
)

func main() {
	fmt.Println("=== 检测系统IPv6地址池 ===\n")
	
	// 检测系统IPv6能力
	hasIPv6 := proxy.HasIPv6()
	fmt.Printf("系统支持IPv6: %v\n\n", hasIPv6)
	
	// 发现所有IPv6地址
	addrs := proxy.DiscoverIPv6LocalAddrs()
	fmt.Printf("检测到的IPv6地址数量: %d\n\n", len(addrs))
	
	if len(addrs) > 0 {
		fmt.Println("IPv6地址列表:")
		for i, addr := range addrs {
			if i < 50 { // 只显示前50个
				fmt.Printf("  [%3d] %s\n", i+1, addr.String())
			} else if i == 50 {
				fmt.Printf("  ... (还有 %d 个地址未显示)\n", len(addrs)-50)
				break
			}
		}
		fmt.Println()
		
		// 测试轮询机制
		fmt.Println("测试IPv6地址轮询（前10次）:")
		for i := 0; i < 10 && i < len(addrs); i++ {
			ip := proxy.NextIPv6LocalAddr()
			fmt.Printf("  轮询 %d: %s\n", i+1, ip.String())
		}
	} else {
		fmt.Println("⚠️  未检测到任何IPv6地址")
		fmt.Println("\n可能的原因:")
		fmt.Println("  1. 系统确实没有IPv6地址")
		fmt.Println("  2. 地址被过滤了（环回地址、链路本地地址等）")
		fmt.Println("  3. 网络接口未启用")
	}
	
	fmt.Println()
}

