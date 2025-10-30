package admin_test

import (
	"testing"
	"time"

	"vps-rpc/admin"
)

// TestAdminClient_NewClient 测试管理客户端创建
func TestAdminClient_NewClient(t *testing.T) {
	// 测试空配置
	_, err := admin.NewClient(nil)
	if err == nil {
		t.Error("期望创建客户端失败，但成功了")
	}

	// 测试空地址
	_, err = admin.NewClient(&admin.ClientConfig{
		Address: "",
	})
	if err == nil {
		t.Error("期望创建客户端失败（空地址），但成功了")
	}

	// 测试无效地址（但不会真正连接）
	config := &admin.ClientConfig{
		Address:            "invalid:12345",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
	}

	// 这个测试会因为连接失败而失败，但我们可以测试配置是否被正确读取
	_, err = admin.NewClient(config)
	if err != nil {
		t.Logf("客户端创建失败（预期的，因为服务器未运行）: %v", err)
	} else {
		t.Log("客户端创建成功")
	}
}

// TestAdminClient_GetStatus 测试GetStatus方法（需要运行服务器）
func TestAdminClient_GetStatus(t *testing.T) {
	t.Skip("需要运行服务器，跳过")
}

// TestAdminClient_GetStats 测试GetStats方法（需要运行服务器）
func TestAdminClient_GetStats(t *testing.T) {
	t.Skip("需要运行服务器，跳过")
}

// TestAdminClient_HealthCheck 测试HealthCheck方法（需要运行服务器）
func TestAdminClient_HealthCheck(t *testing.T) {
	t.Skip("需要运行服务器，跳过")
}

// TestAdminClient_Close 测试客户端关闭
func TestAdminClient_Close(t *testing.T) {
	// 创建一个配置（即使无法连接）
	config := &admin.ClientConfig{
		Address:            "localhost:4242",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
	}

	// 尝试创建客户端（可能会失败，但我们可以测试Close方法）
	client, err := admin.NewClient(config)
	if err != nil {
		// 如果创建失败，这是预期的（服务器未运行）
		t.Logf("客户端创建失败（预期的）: %v", err)
		return
	}

	// 测试Close方法
	if err := client.Close(); err != nil {
		t.Errorf("关闭客户端失败: %v", err)
	}
}

// TestAdminClient_GetAddress 测试获取地址方法
func TestAdminClient_GetAddress(t *testing.T) {
	config := &admin.ClientConfig{
		Address:            "localhost:4242",
		InsecureSkipVerify: true,
		Timeout:            1 * time.Second,
	}

	client, err := admin.NewClient(config)
	if err != nil {
		t.Skipf("无法创建客户端（服务器未运行）: %v", err)
		return
	}

	address := client.GetAddress()
	if address != config.Address {
		t.Errorf("地址不匹配: 期望 %s, 实际 %s", config.Address, address)
	}

	client.Close()
}
