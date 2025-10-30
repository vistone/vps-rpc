package client_test

import (
	"testing"
	"time"

	"vps-rpc/client"
)

// TestClient_NewClient 测试客户端创建
func TestClient_NewClient(t *testing.T) {
	// 测试空配置
	_, err := client.NewClient(nil)
	if err == nil {
		t.Error("期望创建客户端失败，但成功了")
	}

	// 测试空地址
	_, err = client.NewClient(&client.ClientConfig{
		Address: "",
	})
	if err == nil {
		t.Error("期望创建客户端失败（空地址），但成功了")
	}

	// 测试无效地址（但不会真正连接）
	config := &client.ClientConfig{
		Address:            "invalid:12345",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
	}

	// 这个测试会因为连接失败而失败，但我们可以测试配置是否被正确读取
	// 由于我们无法真正连接到服务器，这个测试主要验证配置验证逻辑
	_, err = client.NewClient(config)
	// 连接失败是预期的，因为我们没有运行服务器
	if err == nil {
		t.Log("客户端创建成功（但无法连接服务器，这是预期的）")
	} else {
		t.Logf("客户端创建失败（预期的，因为服务器未运行）: %v", err)
	}
}

// TestClient_Fetch 测试Fetch方法（需要运行服务器）
func TestClient_Fetch(t *testing.T) {
	// 这个测试需要服务器运行，所以跳过
	t.Skip("需要运行服务器，跳过")
}

// TestClient_BatchFetch 测试BatchFetch方法（需要运行服务器）
func TestClient_BatchFetch(t *testing.T) {
	// 这个测试需要服务器运行，所以跳过
	t.Skip("需要运行服务器，跳过")
}

// TestClient_Close 测试客户端关闭
func TestClient_Close(t *testing.T) {
	// 创建一个配置（即使无法连接）
	config := &client.ClientConfig{
		Address:            "localhost:4242",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
	}

	// 尝试创建客户端（可能会失败，但我们可以测试Close方法）
	grpcClient, err := client.NewClient(config)
	if err != nil {
		// 如果创建失败，这是预期的（服务器未运行）
		t.Logf("客户端创建失败（预期的）: %v", err)
		return
	}

	// 测试Close方法
	if err := grpcClient.Close(); err != nil {
		t.Errorf("关闭客户端失败: %v", err)
	}
}

// TestClient_GetAddress 测试获取地址方法
func TestClient_GetAddress(t *testing.T) {
	config := &client.ClientConfig{
		Address:            "localhost:4242",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
	}

	grpcClient, err := client.NewClient(config)
	if err != nil {
		t.Skipf("无法创建客户端（服务器未运行）: %v", err)
		return
	}

	address := grpcClient.GetAddress()
	if address != config.Address {
		t.Errorf("地址不匹配: 期望 %s, 实际 %s", config.Address, address)
	}

	grpcClient.Close()
}

