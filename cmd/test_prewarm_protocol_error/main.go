package main

import (
	"context"
	"log"
	"os"
	"time"

	"vps-rpc/config"
	"vps-rpc/proxy"
)

func main() {
	// 加载配置
	if err := config.LoadConfig("./config.toml"); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化DNS池
	if config.AppConfig.DNS.Enabled && config.AppConfig.DNS.DBPath != "" {
		dnsPool, err := proxy.NewDNSPool(config.AppConfig.DNS.DBPath)
		if err != nil {
			log.Fatalf("创建DNS池失败: %v", err)
		}
		defer dnsPool.Close()
		proxy.SetGlobalDNSPool(dnsPool)

		// 获取所有域名的IP
		domainIPs := dnsPool.GetAllDomainsAndIPs()
		if len(domainIPs) == 0 {
			log.Printf("DNS池中没有记录，退出")
			os.Exit(0)
		}

		// 测试预热前几个IP
		testCount := 0
		maxTest := 10 // 最多测试10个IP

		log.Printf("\n=== 开始测试连接预热（HTTP/1.1，应无协议错误） ===\n")

		for domain, ips := range domainIPs {
			if testCount >= maxTest {
				break
			}

			for _, ip := range ips {
				if testCount >= maxTest {
					break
				}

				log.Printf("\n[测试 %d/%d] 预热: %s -> %s", testCount+1, maxTest, domain, ip)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				
				start := time.Now()
				proxy.PrewarmSingleConnection(ctx, domain, ip)
				duration := time.Since(start)
				
				cancel()
				log.Printf("[测试 %d/%d] 预热完成，耗时: %v", testCount+1, maxTest, duration)
				
				// 短暂延迟，避免过快
				time.Sleep(100 * time.Millisecond)
				testCount++
			}
		}

		log.Printf("\n=== 测试完成：如果上方没有 'protocol error: received DATA on a HEAD request' 错误，则修复成功 ===\n")
	} else {
		log.Fatalf("DNS池未启用")
	}
}

