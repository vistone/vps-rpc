package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vps-rpc/proxy"
)

// TestDNSPool_Basic 测试DNS池的基本功能
func TestDNSPool_Basic(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	// 创建DNS池
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("创建DNS池失败: %v", err)
	}
	defer pool.Close()
	
	t.Logf("DNS池创建成功: %s", dnsPoolPath)
	
	// 测试ReportResult：添加新IP
	if err := pool.ReportResult("test.example.com", "1.2.3.4", 200); err != nil {
		t.Fatalf("ReportResult失败: %v", err)
	}
	
	// 等待持久化完成
	time.Sleep(600 * time.Millisecond)
	
	// 读取文件验证
	data, err := os.ReadFile(dnsPoolPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	
	var records map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("JSON解析失败: %v", err)
	}
	
	record, ok := records["test.example.com"]
	if !ok {
		t.Fatal("未找到test.example.com记录")
	}
	
	recMap := record.(map[string]interface{})
	ipv4List := recMap["ipv4"].([]interface{})
	if len(ipv4List) != 1 || ipv4List[0].(string) != "1.2.3.4" {
		t.Fatalf("IPv4记录不正确: %v", ipv4List)
	}
	
	t.Logf("✓ 基本功能测试通过")
}

// TestDNSPool_Deduplication 测试去重功能
func TestDNSPool_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("创建DNS池失败: %v", err)
	}
	defer pool.Close()
	
	// 添加相同的IP多次
	pool.ReportResult("test.example.com", "1.2.3.4", 200)
	pool.ReportResult("test.example.com", "1.2.3.4", 200)
	pool.ReportResult("test.example.com", "1.2.3.4", 200)
	
	// 等待持久化
	time.Sleep(600 * time.Millisecond)
	
	// 读取文件验证
	data, err := os.ReadFile(dnsPoolPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	
	var records map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("JSON解析失败: %v", err)
	}
	
	record := records["test.example.com"].(map[string]interface{})
	ipv4List := record["ipv4"].([]interface{})
	
	if len(ipv4List) != 1 {
		t.Fatalf("去重失败，IP数量不正确: %v (期望1个)", ipv4List)
	}
	
	t.Logf("✓ 去重测试通过: IP数量=%d", len(ipv4List))
}

// TestDNSPool_IPv6 测试IPv6功能
func TestDNSPool_IPv6(t *testing.T) {
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("创建DNS池失败: %v", err)
	}
	defer pool.Close()
	
	// 添加IPv4和IPv6
	pool.ReportResult("test.example.com", "1.2.3.4", 200)
	pool.ReportResult("test.example.com", "2001:db8::1", 200)
	
	time.Sleep(600 * time.Millisecond)
	
	// 读取文件验证
	data, err := os.ReadFile(dnsPoolPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	
	var records map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("JSON解析失败: %v", err)
	}
	
	record := records["test.example.com"].(map[string]interface{})
	ipv4List := record["ipv4"].([]interface{})
	ipv6List := record["ipv6"].([]interface{})
	
	if len(ipv4List) != 1 || ipv4List[0].(string) != "1.2.3.4" {
		t.Fatalf("IPv4记录不正确: %v", ipv4List)
	}
	
	if len(ipv6List) != 1 || ipv6List[0].(string) != "2001:db8::1" {
		t.Fatalf("IPv6记录不正确: %v", ipv6List)
	}
	
	t.Logf("✓ IPv6测试通过: IPv4=%d, IPv6=%d", len(ipv4List), len(ipv6List))
}

// TestDNSPool_Concurrent 测试并发安全性
func TestDNSPool_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("创建DNS池失败: %v", err)
	}
	defer pool.Close()
	
	// 并发添加
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			domain := fmt.Sprintf("test%d.example.com", idx%10)
			ip := fmt.Sprintf("1.2.3.%d", idx)
			pool.ReportResult(domain, ip, 200)
			done <- true
		}(i)
	}
	
	// 等待所有goroutine完成
	for i := 0; i < 100; i++ {
		<-done
	}
	
	// 等待持久化
	time.Sleep(600 * time.Millisecond)
	
	// 读取文件验证
	data, err := os.ReadFile(dnsPoolPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	
	var records map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("JSON解析失败: %v", err)
	}
	
	// 应该有10个域名
	if len(records) != 10 {
		t.Fatalf("域名数量不正确: %d (期望10个)", len(records))
	}
	
	t.Logf("✓ 并发测试通过: %d个域名", len(records))
}

// TestDNSPool_Blacklist 测试黑名单功能
func TestDNSPool_Blacklist(t *testing.T) {
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("创建DNS池失败: %v", err)
	}
	defer pool.Close()
	
	// 添加正常IP
	pool.ReportResult("test.example.com", "1.2.3.4", 200)
	
	// 添加被封禁的IP
	pool.ReportResult("test.example.com", "1.2.3.5", 403)
	
	time.Sleep(600 * time.Millisecond)
	
	// 读取文件验证
	data, err := os.ReadFile(dnsPoolPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	
	var records map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("JSON解析失败: %v", err)
	}
	
	record := records["test.example.com"].(map[string]interface{})
	blacklist := record["blacklist"].(map[string]interface{})
	whitelist := record["whitelist"].(map[string]interface{})
	
	// 验证黑名单
	if !blacklist["1.2.3.5"].(bool) {
		t.Fatal("黑名单记录缺失")
	}
	
	// 验证白名单
	if !whitelist["1.2.3.4"].(bool) {
		t.Fatal("白名单记录缺失")
	}
	
	t.Logf("✓ 黑名单测试通过")
}

// TestDNSPool_LoadFromDisk 测试从磁盘加载
func TestDNSPool_LoadFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	// 手动创建文件
	manualData := map[string]interface{}{
		"test.example.com": map[string]interface{}{
			"domain":   "test.example.com",
			"ipv4":     []string{"1.2.3.4", "1.2.3.5"},
			"ipv6":     []string{"2001:db8::1"},
			"updated_at": time.Now().Unix(),
			"next_index4": 0,
			"next_index6": 0,
			"next_prefer_v6": false,
			"blacklist": map[string]bool{},
			"whitelist": map[string]bool{
				"1.2.3.4": true,
				"1.2.3.5": true,
				"2001:db8::1": true,
			},
			"ewma_latency4": 0.0,
			"ewma_latency6": 0.0,
		},
	}
	
	jsonData, err := json.MarshalIndent(manualData, "", "  ")
	if err != nil {
		t.Fatalf("JSON序列化失败: %v", err)
	}
	
	if err := os.WriteFile(dnsPoolPath, jsonData, 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	
	// 加载DNS池
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("加载DNS池失败: %v", err)
	}
	defer pool.Close()
	
	// 验证GetIPs
	ctx := context.Background()
	v4, v6, err := pool.GetIPs(ctx, "test.example.com")
	if err != nil {
		t.Fatalf("GetIPs失败: %v", err)
	}
	
	if len(v4) != 2 {
		t.Fatalf("IPv4数量不正确: %d (期望2个)", len(v4))
	}
	
	if len(v6) != 1 {
		t.Fatalf("IPv6数量不正确: %d (期望1个)", len(v6))
	}
	
	t.Logf("✓ 从磁盘加载测试通过: IPv4=%d, IPv6=%d", len(v4), len(v6))
}

// TestDNSPool_PersistThrottle 测试持久化节流
func TestDNSPool_PersistThrottle(t *testing.T) {
	tmpDir := t.TempDir()
	dnsPoolPath := filepath.Join(tmpDir, "dns_pool.json")
	
	pool, err := proxy.NewDNSPool(dnsPoolPath)
	if err != nil {
		t.Fatalf("创建DNS池失败: %v", err)
	}
	defer pool.Close()
	
	start := time.Now()
	
	// 快速连续添加10个IP
	for i := 0; i < 10; i++ {
		pool.ReportResult("test.example.com", fmt.Sprintf("1.2.3.%d", i), 200)
	}
	
	firstWriteTime := time.Since(start)
	t.Logf("首次写入耗时: %v", firstWriteTime)
	
	// 等待持久化完成（600ms = 500ms节流 + 100ms缓冲）
	time.Sleep(600 * time.Millisecond)
	
	// 验证文件存在
	if _, err := os.Stat(dnsPoolPath); err != nil {
		t.Fatalf("文件不存在: %v", err)
	}
	
	// 读取并验证所有IP都被写入
	data, err := os.ReadFile(dnsPoolPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	
	var records map[string]interface{}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("JSON解析失败: %v", err)
	}
	
	record := records["test.example.com"].(map[string]interface{})
	ipv4List := record["ipv4"].([]interface{})
	
	if len(ipv4List) != 10 {
		t.Fatalf("IP数量不正确: %d (期望10个)", len(ipv4List))
	}
	
	t.Logf("✓ 持久化节流测试通过: %d个IP", len(ipv4List))
}

