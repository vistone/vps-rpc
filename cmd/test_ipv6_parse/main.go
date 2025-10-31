package main

import (
	"fmt"
	"net"
	"vps-rpc/proxy"
)

func main() {
	fmt.Println("=== 测试IPv6地址检测和解析 ===\n")
	
	// 从/proc/net/if_inet6读取
	fmt.Println("1. 从/proc/net/if_inet6读取的IPv6地址:")
	addrs := proxy.DiscoverIPv6LocalAddrs()
	fmt.Printf("   检测到 %d 个IPv6地址\n\n", len(addrs))
	
	// 测试手动解析一个地址
	testHex := "2408820718e66ce0e21e0cab8a2a6753"
	fmt.Printf("2. 测试手动解析: %s\n", testHex)
	
	// 使用net.ParseIP验证
	expectedIP := net.ParseIP("2408:8207:18e6:6ce0:e21e:0cab:8a2a:6753")
	if expectedIP != nil {
		fmt.Printf("   期望: %s\n", expectedIP.String())
	}
	
	fmt.Println("\n3. 实际检测到的地址列表:")
	for i, addr := range addrs {
		if i < 20 {
			fmt.Printf("   [%3d] %s\n", i+1, addr.String())
		} else if i == 20 {
			fmt.Printf("   ... (还有 %d 个地址未显示)\n", len(addrs)-20)
			break
		}
	}
	
	fmt.Println("\n4. 测试轮询（前20次）:")
	for i := 0; i < 20 && i < len(addrs)*2; i++ {
		ip := proxy.NextIPv6LocalAddr()
		if ip != nil {
			fmt.Printf("   轮询 %2d: %s\n", i+1, ip.String())
		}
	}
	
	fmt.Println("\n=== 完成 ===")
}

