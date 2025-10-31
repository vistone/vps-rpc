package main

import (
	"context"
	"log"
	"time"

	"vps-rpc/client"
	"vps-rpc/config"
	"vps-rpc/rpc"
)

func main() {
	// 加载配置
	if err := config.LoadConfig("./config.toml"); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 连接到tile0
	addr := "tile0.zeromaps.cn:4242"
	log.Printf("[测试] 连接到 %s", addr)

	// 创建客户端
	cli, err := client.NewClient(&client.ClientConfig{
		Address:            addr,
		Timeout:            15 * time.Second,
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer cli.Close()

	log.Printf("[测试] ✅ 客户端连接成功")

	// 创建一个PeerClient来测试ExchangeDNS
	log.Printf("\n[测试] === 测试ExchangeDNS（空的请求，带类型标记0x01） ===")
	
	// 创建PeerClient
	peerClient, err := client.NewPeerClient(addr, true)
	if err != nil {
		log.Fatalf("创建PeerClient失败: %v", err)
	}
	
	// 创建空的ExchangeDNSRequest
	req := &rpc.ExchangeDNSRequest{
		Records: make(map[string]*rpc.DNSRecord),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := peerClient.ExchangeDNS(ctx, req)
	duration := time.Since(start)

	if err != nil {
		log.Printf("[测试] ❌ ExchangeDNS失败: %v (耗时: %v)", err, duration)
		log.Printf("[测试] 错误详情: %+v", err)
		return
	}

	log.Printf("[测试] ✅ ExchangeDNS成功 (耗时: %v)", duration)
	log.Printf("[测试] 收到 %d 个域的DNS记录:", len(resp.Records))

	// 显示前10个域
	count := 0
	for domain, record := range resp.Records {
		if count >= 10 {
			log.Printf("[测试] ... 还有 %d 个域", len(resp.Records)-10)
			break
		}
		ipv4Count := len(record.Ipv4)
		ipv6Count := len(record.Ipv6)
		log.Printf("[测试]   - %s: IPv4=%d个, IPv6=%d个", domain, ipv4Count, ipv6Count)
		if ipv4Count > 0 && count < 3 {
			log.Printf("[测试]       IPv4示例: %v", record.Ipv4[:min(3, ipv4Count)])
		}
		if ipv6Count > 0 && count < 3 {
			log.Printf("[测试]       IPv6示例: %v", record.Ipv6[:min(3, ipv6Count)])
		}
		count++
	}

	log.Printf("\n[测试] === 测试完成 ===")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

