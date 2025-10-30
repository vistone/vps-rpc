package client_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"vps-rpc/client"
)

// TestRetryExecutor 测试重试执行器
func TestRetryExecutor(t *testing.T) {
	// 测试默认配置
	config := client.DefaultRetryConfig()
	if config == nil {
		t.Fatal("默认配置创建失败")
	}

	if config.MaxRetries != 3 {
		t.Errorf("最大重试次数不匹配: 期望 3, 实际 %d", config.MaxRetries)
	}

	// 创建重试执行器
	executor := client.NewRetryExecutor(config)
	if executor == nil {
		t.Fatal("重试执行器创建失败")
	}

	// 测试成功执行（不重试）
	ctx := context.Background()
	result, err := executor.Execute(ctx, func() (interface{}, error) {
		return "success", nil
	})

	if err != nil {
		t.Errorf("执行失败: %v", err)
	}

	if result != "success" {
		t.Errorf("结果不匹配: 期望 'success', 实际 %v", result)
	}

	// 测试立即失败（不可重试的错误）
	nonRetryableError := status.Error(codes.InvalidArgument, "invalid argument")
	_, err = executor.Execute(ctx, func() (interface{}, error) {
		return nil, nonRetryableError
	})

	if err == nil {
		t.Error("期望失败，但成功了")
	}
}

// TestConnectionPool_Basic 测试连接池基本功能
func TestConnectionPool_Basic(t *testing.T) {
	// 创建测试用的TLS凭证
	creds := credentials.NewTLS(nil)

	pool := client.NewConnectionPool("localhost:4242", creds, 5, 1*time.Second)

	if pool == nil {
		t.Fatal("连接池创建失败")
	}

	if pool.MaxSize() != 5 {
		t.Errorf("最大连接数不匹配: 期望 5, 实际 %d", pool.MaxSize())
	}

	if pool.Size() != 0 {
		t.Errorf("初始连接数应为0: 实际 %d", pool.Size())
	}

	// 测试关闭连接池
	if err := pool.Close(); err != nil {
		t.Errorf("关闭连接池失败: %v", err)
	}
}

// TestClient_WithConnectionPool 测试带连接池的客户端配置
func TestClient_WithConnectionPool(t *testing.T) {
	config := &client.ClientConfig{
		Address:              "localhost:4242",
		InsecureSkipVerify:   true,
		Timeout:              1 * time.Second,
		EnableConnectionPool: true,
		MaxPoolSize:          5,
	}

	// 注意：这个测试会因为无法连接服务器而失败，但可以测试配置
	_, err := client.NewClient(config)
	if err != nil {
		t.Logf("客户端创建失败（预期的，因为服务器未运行）: %v", err)
	} else {
		t.Log("客户端创建成功")
	}
}

// TestClient_WithRetry 测试带重试的客户端配置
func TestClient_WithRetry(t *testing.T) {
	retryConfig := client.DefaultRetryConfig()
	retryConfig.MaxRetries = 2

	config := &client.ClientConfig{
		Address:            "localhost:4242",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
		EnableRetry:        true,
		RetryConfig:        retryConfig,
	}

	// 注意：这个测试会因为无法连接服务器而失败，但可以测试配置
	_, err := client.NewClient(config)
	if err != nil {
		t.Logf("客户端创建失败（预期的，因为服务器未运行）: %v", err)
	} else {
		t.Log("客户端创建成功")
	}
}

// TestRetryConfig_Custom 测试自定义重试配置
func TestRetryConfig_Custom(t *testing.T) {
	config := &client.RetryConfig{
		MaxRetries:   5,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   1.5,
		RetryableErrors: []codes.Code{
			codes.Unavailable,
			codes.DeadlineExceeded,
		},
	}

	executor := client.NewRetryExecutor(config)
	if executor == nil {
		t.Fatal("重试执行器创建失败")
	}

	t.Log("自定义重试配置创建成功")
}

// TestConnectionPool_Concurrent 测试连接池并发访问
func TestConnectionPool_Concurrent(t *testing.T) {
	creds := credentials.NewTLS(nil)
	pool := client.NewConnectionPool("localhost:4242", creds, 5, 1*time.Second)
	defer pool.Close()

	// 并发获取连接
	ctx := context.Background()
	const numGoroutines = 10

	done := make(chan bool, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() {
				done <- true
			}()
			_, err := pool.GetConnection(ctx)
			if err != nil {
				// 连接失败是预期的（服务器未运行）
				t.Logf("获取连接失败（预期的）: %v", err)
			}
		}()
	}

	// 等待所有goroutine完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 验证连接池大小不会超过最大值
	size := pool.Size()
	if size > pool.MaxSize() {
		t.Errorf("连接池大小超过最大值: %d > %d", size, pool.MaxSize())
	}
}

// TestConnectionPool_MaxSize 测试连接池最大大小限制
func TestConnectionPool_MaxSize(t *testing.T) {
	creds := credentials.NewTLS(nil)
	maxSize := 3
	pool := client.NewConnectionPool("localhost:4242", creds, maxSize, 1*time.Second)
	defer pool.Close()

	if pool.MaxSize() != maxSize {
		t.Errorf("最大连接数不匹配: 期望 %d, 实际 %d", maxSize, pool.MaxSize())
	}
}

// TestConnectionPool_Close 测试连接池关闭
func TestConnectionPool_Close(t *testing.T) {
	creds := credentials.NewTLS(nil)
	pool := client.NewConnectionPool("localhost:4242", creds, 5, 1*time.Second)

	// 关闭多次应该没有问题
	if err := pool.Close(); err != nil {
		t.Errorf("第一次关闭失败: %v", err)
	}

	if err := pool.Close(); err != nil {
		t.Errorf("第二次关闭失败: %v", err)
	}

	// 关闭后大小应该为0
	if pool.Size() != 0 {
		t.Errorf("关闭后大小应为0: 实际 %d", pool.Size())
	}
}

// TestRetryExecutor_ContextCancel 测试重试执行器上下文取消
func TestRetryExecutor_ContextCancel(t *testing.T) {
	config := client.DefaultRetryConfig()
	executor := client.NewRetryExecutor(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消上下文

	_, err := executor.Execute(ctx, func() (interface{}, error) {
		return nil, fmt.Errorf("test error")
	})

	if err == nil {
		t.Error("期望返回错误，但没有错误")
	}
}
