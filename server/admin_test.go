package server_test

import (
	"context"
	"testing"
	"time"

	"vps-rpc/config"
	"vps-rpc/logger"
	"vps-rpc/rpc"
	"vps-rpc/server"
)

// TestStatsCollector 测试统计收集器
func TestStatsCollector(t *testing.T) {
	stats := server.NewStatsCollector()

	// 记录一些请求
	stats.RecordRequest(true, 100*time.Millisecond, "")
	stats.RecordRequest(true, 200*time.Millisecond, "")
	stats.RecordRequest(false, 50*time.Millisecond, "test_error")

	// 获取统计信息
	statsData := stats.GetStats()

	if statsData.TotalRequests != 3 {
		t.Errorf("总请求数不匹配: 期望 3, 实际 %d", statsData.TotalRequests)
	}

	if statsData.SuccessRequests != 2 {
		t.Errorf("成功请求数不匹配: 期望 2, 实际 %d", statsData.SuccessRequests)
	}

	if statsData.ErrorRequests != 1 {
		t.Errorf("失败请求数不匹配: 期望 1, 实际 %d", statsData.ErrorRequests)
	}

	if statsData.AverageLatencyMs <= 0 {
		t.Error("平均延迟应该大于0")
	}

	// 测试连接计数
	stats.IncrementConnections()
	stats.IncrementConnections()
	stats.DecrementConnections()

	statsData = stats.GetStats()
	if statsData.CurrentConnections != 1 {
		t.Errorf("当前连接数不匹配: 期望 1, 实际 %d", statsData.CurrentConnections)
	}

	// 测试重置
	stats.Reset()
	statsData = stats.GetStats()
	if statsData.TotalRequests != 0 {
		t.Errorf("重置后总请求数应为0: 实际 %d", statsData.TotalRequests)
	}
}

// TestAdminServer 测试管理服务
func TestAdminServer(t *testing.T) {
	// 加载配置
	if err := config.LoadConfig("../config.toml"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 创建统计收集器
	stats := server.NewStatsCollector()

	// 创建日志器
	log := logger.NewLoggerFromConfig("info", "text")

	// 创建管理服务
	adminServer := server.NewAdminServer(stats, log)

	ctx := context.Background()

	// 测试GetStatus
	statusResp, err := adminServer.GetStatus(ctx, &rpc.StatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus失败: %v", err)
	}

	if !statusResp.Running {
		t.Error("服务器应该处于运行状态")
	}

	if statusResp.Version == "" {
		t.Error("版本信息不应为空")
	}

	// 测试GetStats
	statsResp, err := adminServer.GetStats(ctx, &rpc.StatsRequest{})
	if err != nil {
		t.Fatalf("GetStats失败: %v", err)
	}

	if statsResp == nil {
		t.Fatal("统计响应不应为空")
	}

	// 测试HealthCheck
	healthResp, err := adminServer.HealthCheck(ctx, &rpc.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("HealthCheck失败: %v", err)
	}

	if !healthResp.Healthy {
		t.Error("健康检查应该返回健康状态")
	}

	// 测试GetConnections
	connsResp, err := adminServer.GetConnections(ctx, &rpc.ConnectionsRequest{})
	if err != nil {
		t.Fatalf("GetConnections失败: %v", err)
	}

	if connsResp == nil {
		t.Fatal("连接响应不应为空")
	}
}

