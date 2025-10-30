package integration_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"vps-rpc/client"
	"vps-rpc/config"
	"vps-rpc/rpc"
	"vps-rpc/server"
)

// TestIntegration_Fetch 集成测试：测试客户端和服务器的完整交互
func TestIntegration_Fetch(t *testing.T) {
	// 加载配置
	if err := config.LoadConfig("../config.toml"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 为测试选择一个空闲端口，避免与本机占用冲突
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("分配端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	config.AppConfig.Server.Port = port

	// 创建并启动服务器
	grpcServer, err := server.NewGrpcServer()
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	stats := server.NewStatsCollector()
	crawlerServer := server.NewCrawlerServer(stats)
	grpcServer.RegisterService(crawlerServer)

	// 在goroutine中启动服务器
	go func() {
		if err := grpcServer.Serve(); err != nil {
			t.Logf("服务器启动失败: %v", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(1 * time.Second)

	// 清理：关闭服务器
	defer func() {
		if err := grpcServer.Close(); err != nil {
			t.Logf("关闭服务器失败: %v", err)
		}
	}()

	// 创建客户端
	clientConfig := &client.ClientConfig{
		Address:            net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)),
		Timeout:            10 * time.Second,
		InsecureSkipVerify: true, // 跳过TLS验证（因为使用自签名证书）
	}

	grpcClient, err := client.NewClient(clientConfig)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer grpcClient.Close()

	// 创建测试请求
	req := &rpc.FetchRequest{
		Url:       "https://www.example.com",
		TlsClient: rpc.TLSClientType_CHROME,
	}

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 执行请求
	resp, err := grpcClient.Fetch(ctx, req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}

	// 验证响应
	if resp == nil {
		t.Fatal("响应为空")
	}

	if resp.Url != req.Url {
		t.Errorf("URL不匹配: 期望 %s, 实际 %s", req.Url, resp.Url)
	}

	if resp.Error != "" {
		t.Logf("响应包含错误（可能是网络问题）: %s", resp.Error)
	} else {
		if resp.StatusCode == 0 {
			t.Error("响应状态码为空")
		}
		t.Logf("请求成功: URL=%s, StatusCode=%d, BodyLength=%d", resp.Url, resp.StatusCode, len(resp.Body))
	}
}

// TestIntegration_BatchFetch 集成测试：测试批量抓取
func TestIntegration_BatchFetch(t *testing.T) {
	// 加载配置
	if err := config.LoadConfig("../config.toml"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 为测试选择一个空闲端口，避免与本机占用冲突
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("分配端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	config.AppConfig.Server.Port = port

	// 创建并启动服务器
	grpcServer, err := server.NewGrpcServer()
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	stats := server.NewStatsCollector()
	crawlerServer := server.NewCrawlerServer(stats)
	grpcServer.RegisterService(crawlerServer)

	// 在goroutine中启动服务器
	go func() {
		if err := grpcServer.Serve(); err != nil {
			t.Logf("服务器启动失败: %v", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(1 * time.Second)

	// 清理：关闭服务器
	defer func() {
		if err := grpcServer.Close(); err != nil {
			t.Logf("关闭服务器失败: %v", err)
		}
	}()

	// 创建客户端
	clientConfig := &client.ClientConfig{
		Address:            net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)),
		Timeout:            10 * time.Second,
		InsecureSkipVerify: true,
	}

	grpcClient, err := client.NewClient(clientConfig)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer grpcClient.Close()

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

	// 执行批量请求
	resp, err := grpcClient.BatchFetch(ctx, req)
	if err != nil {
		t.Fatalf("批量请求失败: %v", err)
	}

	// 验证响应
	if resp == nil {
		t.Fatal("响应为空")
	}

	if len(resp.Responses) != len(req.Requests) {
		t.Errorf("响应数量不匹配: 期望 %d, 实际 %d", len(req.Requests), len(resp.Responses))
	}

	// 验证每个响应
	successCount := 0
	for i, r := range resp.Responses {
		if r == nil {
			t.Errorf("响应[%d]为空", i)
			continue
		}

		if r.Error == "" && r.StatusCode > 0 {
			successCount++
		}

		t.Logf("响应[%d]: URL=%s, StatusCode=%d, Error=%s", i, r.Url, r.StatusCode, r.Error)
	}

	t.Logf("批量请求完成: 总数=%d, 成功=%d", len(resp.Responses), successCount)
}
