package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"vps-rpc/admin"
)

// CLI工具实现
func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]
	serverAddr := "localhost:4242"

	// 如果提供了服务器地址，使用它
	if len(os.Args) > 2 && os.Args[2] != "--reset" {
		serverAddr = os.Args[2]
	}

	// 创建管理客户端
	config := &admin.ClientConfig{
		Address:            serverAddr,
		InsecureSkipVerify: true,
		Timeout:            10 * time.Second,
	}

	client, err := admin.NewClient(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建客户端失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch command {
	case "status":
		handleStatus(ctx, client)
	case "stats":
		reset := len(os.Args) > 2 && os.Args[2] == "--reset"
		handleStats(ctx, client, reset)
	case "health":
		handleHealth(ctx, client)
	case "logs":
		level := ""
		maxLines := int32(100)
		if len(os.Args) > 2 {
			level = os.Args[2]
		}
		if len(os.Args) > 3 {
			fmt.Sscanf(os.Args[3], "%d", &maxLines)
		}
		handleLogs(ctx, client, level, maxLines)
	case "connections":
		handleConnections(ctx, client)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("用法: vps-rpc-admin <command> [options] [server_address]")
	fmt.Println("\n命令:")
	fmt.Println("  status              - 获取服务器状态")
	fmt.Println("  stats [--reset]     - 获取统计信息")
	fmt.Println("  health              - 健康检查")
	fmt.Println("  logs [level] [lines] - 获取日志")
	fmt.Println("  connections         - 获取连接信息")
	fmt.Println("\n示例:")
	fmt.Println("  vps-rpc-admin status")
	fmt.Println("  vps-rpc-admin stats --reset")
	fmt.Println("  vps-rpc-admin logs error 50")
	fmt.Println("  vps-rpc-admin connections localhost:4242")
}

func handleStatus(ctx context.Context, client *admin.Client) {
	resp, err := client.GetStatus(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取状态失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("服务器状态:")
	fmt.Printf("  运行中: %v\n", resp.Running)
	fmt.Printf("  启动时间: %s\n", time.Unix(resp.StartTime, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("  运行时长: %d秒\n", resp.UptimeSeconds)
	fmt.Printf("  版本: %s\n", resp.Version)
	fmt.Printf("  监听地址: %s\n", resp.ListenAddress)
}

func handleStats(ctx context.Context, client *admin.Client, reset bool) {
	resp, err := client.GetStats(ctx, reset)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取统计信息失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("统计信息:")
	fmt.Printf("  总请求数: %d\n", resp.TotalRequests)
	fmt.Printf("  成功请求数: %d\n", resp.SuccessRequests)
	fmt.Printf("  失败请求数: %d\n", resp.ErrorRequests)
	fmt.Printf("  平均延迟: %dms\n", resp.AverageLatencyMs)
	fmt.Printf("  最小延迟: %dms\n", resp.MinLatencyMs)
	fmt.Printf("  最大延迟: %dms\n", resp.MaxLatencyMs)
	fmt.Printf("  当前连接数: %d\n", resp.CurrentConnections)

	if len(resp.ErrorStats) > 0 {
		fmt.Println("  错误统计:")
		for errType, count := range resp.ErrorStats {
			fmt.Printf("    %s: %d\n", errType, count)
		}
	}

	if reset {
		fmt.Println("\n统计信息已重置")
	}
}

func handleHealth(ctx context.Context, client *admin.Client) {
	resp, err := client.HealthCheck(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "健康检查失败: %v\n", err)
		os.Exit(1)
	}

	if resp.Healthy {
		fmt.Println("✓ 服务器健康")
		fmt.Printf("  消息: %s\n", resp.Message)
	} else {
		fmt.Println("✗ 服务器不健康")
		fmt.Printf("  消息: %s\n", resp.Message)
		os.Exit(1)
	}
}

func handleLogs(ctx context.Context, client *admin.Client, level string, maxLines int32) {
	resp, err := client.GetLogs(ctx, level, maxLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取日志失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("日志条目 (%d条):\n", len(resp.Entries))
	for _, entry := range resp.Entries {
		timestamp := time.Unix(entry.Timestamp, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("[%s] [%s] %s", timestamp, entry.Level, entry.Message)
		if entry.Fields != "" {
			var fields map[string]interface{}
			if json.Unmarshal([]byte(entry.Fields), &fields) == nil {
				fmt.Printf(" %v", fields)
			}
		}
		fmt.Println()
	}
}

func handleConnections(ctx context.Context, client *admin.Client) {
	resp, err := client.GetConnections(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取连接信息失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("连接信息 (总数: %d):\n", resp.TotalConnections)
	for i, conn := range resp.Connections {
		fmt.Printf("  连接[%d]:\n", i+1)
		fmt.Printf("    客户端地址: %s\n", conn.ClientAddress)
		fmt.Printf("    状态: %s\n", conn.State)
		fmt.Printf("    连接时间: %s\n", time.Unix(conn.ConnectTime, 0).Format("2006-01-02 15:04:05"))
		fmt.Printf("    最后活动: %s\n", time.Unix(conn.LastActivityTime, 0).Format("2006-01-02 15:04:05"))
	}
}
