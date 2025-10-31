package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"vps-rpc/client"
	"vps-rpc/config"
	"vps-rpc/rpc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: diagnose_timeout <server_address>")
		os.Exit(1)
	}

	if err := config.LoadConfig("./config.toml"); err != nil {
		fmt.Println("加载配置失败:", err)
		os.Exit(1)
	}

	serverAddr := os.Args[1]
	url := "https://kh.google.com/rt/earth/PlanetoidMetadata"

	headers := map[string]string{
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br, zstd",
		"Origin":          "https://earth.google.com",
		"Referer":         "https://earth.google.com/",
	}

	fmt.Printf("=== QUIC超时诊断 ===\n")
	fmt.Printf("服务器: %s\n", serverAddr)
	fmt.Printf("配置的MaxIdleTimeout: %v\n", config.AppConfig.GetQuicMaxIdleTimeout())
	fmt.Printf("配置的HandshakeIdleTimeout: %v\n", config.AppConfig.GetQuicHandshakeIdleTimeout())
	fmt.Printf("\n")

	// 测试1: 单个请求（正常情况）
	fmt.Println("【测试1】单个请求（30秒超时）")
	cli1, err := client.NewClient(&client.ClientConfig{
		Address:            serverAddr,
		Timeout:            30 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: false,
		EnableRetry:       false,
	})
	if err != nil {
		fmt.Printf("  创建客户端失败: %v\n", err)
	} else {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err := cli1.Fetch(ctx, &rpc.FetchRequest{
			Url:     url,
			Headers: headers,
			TlsClient: rpc.TLSClientType_CHROME,
		})
		cancel()
		duration := time.Since(start)
		if err != nil {
			fmt.Printf("  失败: %v (耗时: %v)\n", err, duration)
		} else if resp.Error != "" {
			fmt.Printf("  服务器返回错误: %s (耗时: %v)\n", resp.Error, duration)
		} else {
			fmt.Printf("  成功: Status=%d, IP=%s (耗时: %v)\n", resp.StatusCode, resp.Headers["X-VPS-IP"], duration)
		}
		cli1.Close()
	}

	time.Sleep(2 * time.Second)

	// 测试2: 高并发（10个并发）
	fmt.Println("\n【测试2】10个并发请求（35秒超时）")
	cli2, err := client.NewClient(&client.ClientConfig{
		Address:            serverAddr,
		Timeout:            35 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: true,
		MaxPoolSize:       10,
		EnableRetry:       false,
	})
	if err != nil {
		fmt.Printf("  创建客户端失败: %v\n", err)
	} else {
		type result struct {
			idx      int
			success  bool
			err      error
			status   int32
			ip       string
			duration time.Duration
		}
		results := make(chan result, 10)

		start := time.Now()
		for i := 0; i < 10; i++ {
			go func(idx int) {
				reqStart := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
				resp, err := cli2.Fetch(ctx, &rpc.FetchRequest{
					Url:     url,
					Headers: headers,
					TlsClient: rpc.TLSClientType_CHROME,
				})
				cancel()
				reqDuration := time.Since(reqStart)

				success := err == nil && resp != nil && resp.Error == "" && resp.StatusCode == 200
				status := int32(0)
				ip := ""
				if resp != nil {
					status = resp.StatusCode
					ip = resp.Headers["X-VPS-IP"]
				}

				results <- result{
					idx:      idx,
					success:  success,
					err:      err,
					status:   status,
					ip:       ip,
					duration: reqDuration,
				}
			}(i)
		}

		success := 0
		fail := 0
		for i := 0; i < 10; i++ {
			r := <-results
			if r.success {
				success++
				fmt.Printf("  [%d] 成功: IP=%s, Status=%d, 耗时=%v\n", r.idx, r.ip, r.status, r.duration)
			} else {
				fail++
				errStr := "<无错误>"
				if r.err != nil {
					errStr = r.err.Error()
				} else if r.status != 200 {
					errStr = fmt.Sprintf("Status=%d", r.status)
				}
				fmt.Printf("  [%d] 失败: %s, 耗时=%v\n", r.idx, errStr, r.duration)
			}
		}
		totalDuration := time.Since(start)
		fmt.Printf("  总计: 成功=%d 失败=%d, 总耗时=%v, 平均=%v\n", success, fail, totalDuration, totalDuration/10)
		cli2.Close()
	}

	time.Sleep(2 * time.Second)

	// 测试3: 检查连接复用
	fmt.Println("\n【测试3】连接复用测试（连续5个请求，观察连接是否复用）")
	cli3, err := client.NewClient(&client.ClientConfig{
		Address:            serverAddr,
		Timeout:            30 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: true,
		MaxPoolSize:       5,
		EnableRetry:       false,
	})
	if err != nil {
		fmt.Printf("  创建客户端失败: %v\n", err)
	} else {
		for i := 0; i < 5; i++ {
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			resp, err := cli3.Fetch(ctx, &rpc.FetchRequest{
				Url:     url,
				Headers: headers,
				TlsClient: rpc.TLSClientType_CHROME,
			})
			cancel()
			duration := time.Since(start)

			if err != nil {
				fmt.Printf("  [%d] 失败: %v (耗时: %v)\n", i+1, err, duration)
			} else if resp.Error != "" {
				fmt.Printf("  [%d] 服务器错误: %s (耗时: %v)\n", i+1, resp.Error, duration)
			} else {
				fmt.Printf("  [%d] 成功: IP=%s, Status=%d (耗时: %v)\n", i+1, resp.Headers["X-VPS-IP"], resp.StatusCode, duration)
			}
			time.Sleep(500 * time.Millisecond)
		}
		cli3.Close()
	}

	fmt.Println("\n=== 诊断完成 ===")
	fmt.Println("可能的原因：")
	fmt.Println("1. UDP缓冲区过小（建议调整系统参数）")
	fmt.Println("2. MaxIdleTimeout过短（当前30秒），高并发时连接可能空闲")
	fmt.Println("3. 服务端AcceptStream阻塞（无超时）")
	fmt.Println("4. 网络延迟或丢包导致QUIC连接不稳定")
}

