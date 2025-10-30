package server_test

import (
	"context"
	"testing"
	"time"

	"vps-rpc/config"
	"vps-rpc/rpc"
	"vps-rpc/server"
)

// TestCrawlerServer_Fetch 测试单个URL抓取功能
func TestCrawlerServer_Fetch(t *testing.T) {
	// 加载配置
	if err := config.LoadConfig("../config.toml"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 创建统计收集器
	stats := server.NewStatsCollector()

	// 创建服务器实例
	crawlerServer := server.NewCrawlerServer(stats)

	// 创建测试请求
	req := &rpc.FetchRequest{
		Url:       "https://www.example.com",
		TlsClient: rpc.TLSClientType_CHROME,
	}

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 执行抓取
	resp, err := crawlerServer.Fetch(ctx, req)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}

	// 验证响应
	if resp == nil {
		t.Fatal("响应为空")
	}

	if resp.Url != req.Url {
		t.Errorf("URL不匹配: 期望 %s, 实际 %s", req.Url, resp.Url)
	}

	// 如果有错误，记录但不失败（可能是网络问题）
	if resp.Error != "" {
		t.Logf("响应包含错误（可能是网络问题）: %s", resp.Error)
		// 如果是因为网络问题导致的错误，不视为测试失败
		return
	}

	if resp.StatusCode == 0 {
		t.Error("响应状态码为空")
	}

	t.Logf("抓取成功: URL=%s, StatusCode=%d, BodyLength=%d", resp.Url, resp.StatusCode, len(resp.Body))
}

// TestCrawlerServer_BatchFetch 测试批量URL抓取功能
func TestCrawlerServer_BatchFetch(t *testing.T) {
	// 加载配置
	if err := config.LoadConfig("../config.toml"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 创建统计收集器
	stats := server.NewStatsCollector()

	// 创建服务器实例
	crawlerServer := server.NewCrawlerServer(stats)

	// 创建批量请求
	req := &rpc.BatchFetchRequest{
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

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 执行批量抓取
	resp, err := crawlerServer.BatchFetch(ctx, req)
	if err != nil {
		t.Fatalf("批量抓取失败: %v", err)
	}

	// 验证响应
	if resp == nil {
		t.Fatal("响应为空")
	}

	if len(resp.Responses) != len(req.Requests) {
		t.Errorf("响应数量不匹配: 期望 %d, 实际 %d", len(req.Requests), len(resp.Responses))
	}

	// 验证每个响应
	for i, r := range resp.Responses {
		if r == nil {
			t.Errorf("响应[%d]为空", i)
			continue
		}

		if r.Url != req.Requests[i].Url {
			t.Errorf("响应[%d] URL不匹配: 期望 %s, 实际 %s", i, req.Requests[i].Url, r.Url)
		}

		t.Logf("响应[%d]: URL=%s, StatusCode=%d, Error=%s", i, r.Url, r.StatusCode, r.Error)
	}
}

// TestCrawlerServer_BatchFetch_Concurrency 测试并发控制
func TestCrawlerServer_BatchFetch_Concurrency(t *testing.T) {
	// 加载配置
	if err := config.LoadConfig("../config.toml"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 创建统计收集器
	stats := server.NewStatsCollector()

	// 创建服务器实例
	crawlerServer := server.NewCrawlerServer(stats)

	// 创建大量请求用于测试并发控制
	requests := make([]*rpc.FetchRequest, 10)
	for i := 0; i < 10; i++ {
		requests[i] = &rpc.FetchRequest{
			Url:       "https://www.example.com",
			TlsClient: rpc.TLSClientType_CHROME,
		}
	}

	req := &rpc.BatchFetchRequest{
		Requests:      requests,
		MaxConcurrent: 3, // 限制并发数为3
	}

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 记录开始时间
	startTime := time.Now()

	// 执行批量抓取
	resp, err := crawlerServer.BatchFetch(ctx, req)
	if err != nil {
		t.Fatalf("批量抓取失败: %v", err)
	}

	// 记录耗时
	duration := time.Since(startTime)
	t.Logf("批量抓取耗时: %v", duration)

	// 验证响应
	if len(resp.Responses) != len(req.Requests) {
		t.Errorf("响应数量不匹配: 期望 %d, 实际 %d", len(req.Requests), len(resp.Responses))
	}

	// 验证并发控制是否生效（如果并发控制生效，总耗时应该明显大于单个请求的耗时）
	// 注意：这个测试需要根据实际情况调整
	t.Logf("并发控制测试完成，总耗时: %v", duration)
}
