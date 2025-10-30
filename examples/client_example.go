package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"vps-rpc/admin"
	"vps-rpc/client"
	"vps-rpc/rpc"
)

// 客户端使用示例
func main() {
	// 配置服务器地址
	serverAddr := "localhost:4242"

	// ========== 爬虫客户端示例 ==========
	fmt.Println("=== 爬虫客户端示例 ===")

	// 创建爬虫客户端配置
	crawlerConfig := &client.ClientConfig{
		Address:            serverAddr,
		InsecureSkipVerify: true, // 跳过TLS验证（因为使用自签名证书）
		Timeout:            10 * time.Second,
		EnableConnectionPool: true, // 启用连接池
		MaxPoolSize:        10,
		EnableRetry:        true, // 启用重试
		RetryConfig:        client.DefaultRetryConfig(),
	}

	// 创建爬虫客户端
	crawlerClient, err := client.NewClient(crawlerConfig)
	if err != nil {
		log.Fatalf("创建爬虫客户端失败: %v", err)
	}
	defer crawlerClient.Close()

	fmt.Printf("爬虫客户端已连接到: %s\n", crawlerClient.GetAddress())

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 示例1: 单个URL抓取
	fmt.Println("\n--- 示例1: 单个URL抓取 ---")
	fetchReq := &rpc.FetchRequest{
		Url:       "https://www.example.com",
		TlsClient: rpc.TLSClientType_CHROME,
	}

	fetchResp, err := crawlerClient.Fetch(ctx, fetchReq)
	if err != nil {
		log.Printf("抓取失败: %v", err)
	} else {
		if fetchResp.Error != "" {
			fmt.Printf("响应包含错误: %s\n", fetchResp.Error)
		} else {
			fmt.Printf("抓取成功: URL=%s, StatusCode=%d, BodyLength=%d\n",
				fetchResp.Url, fetchResp.StatusCode, len(fetchResp.Body))
		}
	}

	// 示例2: 批量URL抓取
	fmt.Println("\n--- 示例2: 批量URL抓取 ---")
	batchReq := &rpc.BatchFetchRequest{
		Requests: []*rpc.FetchRequest{
			{
				Url:       "https://www.example.com",
				TlsClient: rpc.TLSClientType_CHROME,
			},
			{
				Url:       "https://www.google.com",
				TlsClient: rpc.TLSClientType_CHROME,
			},
		},
		MaxConcurrent: 2,
	}

	batchResp, err := crawlerClient.BatchFetch(ctx, batchReq)
	if err != nil {
		log.Printf("批量抓取失败: %v", err)
	} else {
		fmt.Printf("批量抓取完成，共 %d 个响应\n", len(batchResp.Responses))
		for i, resp := range batchResp.Responses {
			if resp.Error != "" {
				fmt.Printf("  响应[%d]: URL=%s, Error=%s\n", i, resp.Url, resp.Error)
			} else {
				fmt.Printf("  响应[%d]: URL=%s, StatusCode=%d\n", i, resp.Url, resp.StatusCode)
			}
		}
	}

	// ========== 管理客户端示例 ==========
	fmt.Println("\n=== 管理客户端示例 ===")

	// 创建管理客户端配置
	adminConfig := &admin.ClientConfig{
		Address:            serverAddr,
		InsecureSkipVerify: true,
		Timeout:            10 * time.Second,
	}

	// 创建管理客户端
	adminClient, err := admin.NewClient(adminConfig)
	if err != nil {
		log.Fatalf("创建管理客户端失败: %v", err)
	}
	defer adminClient.Close()

	fmt.Printf("管理客户端已连接到: %s\n", adminClient.GetAddress())

	// 示例3: 获取服务器状态
	fmt.Println("\n--- 示例3: 获取服务器状态 ---")
	statusResp, err := adminClient.GetStatus(ctx)
	if err != nil {
		log.Printf("获取状态失败: %v", err)
	} else {
		fmt.Printf("服务器状态: 运行中=%v, 运行时长=%d秒, 版本=%s, 监听地址=%s\n",
			statusResp.Running, statusResp.UptimeSeconds, statusResp.Version, statusResp.ListenAddress)
	}

	// 示例4: 获取统计信息
	fmt.Println("\n--- 示例4: 获取统计信息 ---")
	statsResp, err := adminClient.GetStats(ctx, false)
	if err != nil {
		log.Printf("获取统计信息失败: %v", err)
	} else {
		fmt.Printf("统计信息:\n")
		fmt.Printf("  总请求数: %d\n", statsResp.TotalRequests)
		fmt.Printf("  成功请求数: %d\n", statsResp.SuccessRequests)
		fmt.Printf("  失败请求数: %d\n", statsResp.ErrorRequests)
		fmt.Printf("  平均延迟: %dms\n", statsResp.AverageLatencyMs)
		fmt.Printf("  最小延迟: %dms\n", statsResp.MinLatencyMs)
		fmt.Printf("  最大延迟: %dms\n", statsResp.MaxLatencyMs)
		fmt.Printf("  当前连接数: %d\n", statsResp.CurrentConnections)
		if len(statsResp.ErrorStats) > 0 {
			fmt.Printf("  错误统计:\n")
			for errType, count := range statsResp.ErrorStats {
				fmt.Printf("    %s: %d\n", errType, count)
			}
		}
	}

	// 示例5: 健康检查
	fmt.Println("\n--- 示例5: 健康检查 ---")
	healthResp, err := adminClient.HealthCheck(ctx)
	if err != nil {
		log.Printf("健康检查失败: %v", err)
	} else {
		fmt.Printf("健康状态: %v, 消息: %s\n", healthResp.Healthy, healthResp.Message)
	}

	// 示例6: 获取日志
	fmt.Println("\n--- 示例6: 获取日志 ---")
	logsResp, err := adminClient.GetLogs(ctx, "", 10)
	if err != nil {
		log.Printf("获取日志失败: %v", err)
	} else {
		fmt.Printf("获取到 %d 条日志:\n", len(logsResp.Entries))
		for i, entry := range logsResp.Entries {
			if i >= 5 { // 只显示前5条
				break
			}
			fmt.Printf("  [%s] %s: %s\n",
				time.Unix(entry.Timestamp, 0).Format("2006-01-02 15:04:05"),
				entry.Level,
				entry.Message)
		}
	}

	// 示例7: 获取连接信息
	fmt.Println("\n--- 示例7: 获取连接信息 ---")
	connsResp, err := adminClient.GetConnections(ctx)
	if err != nil {
		log.Printf("获取连接信息失败: %v", err)
	} else {
		fmt.Printf("总连接数: %d\n", connsResp.TotalConnections)
		for i, conn := range connsResp.Connections {
			if i >= 5 { // 只显示前5个
				break
			}
			fmt.Printf("  连接[%d]: %s, 状态=%s, 连接时间=%s\n",
				i,
				conn.ClientAddress,
				conn.State,
				time.Unix(conn.ConnectTime, 0).Format("2006-01-02 15:04:05"))
		}
	}

	fmt.Println("\n=== 示例完成 ===")
}

