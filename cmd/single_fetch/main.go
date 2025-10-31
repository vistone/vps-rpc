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
	if err := config.LoadConfig("./config.toml"); err != nil {
		fmt.Println("加载配置失败:", err)
		return
	}

	// 支持通过环境变量 VPS_RPC_ADDR 覆盖配置地址
	addr := config.AppConfig.GetServerAddr()
	if envAddr := os.Getenv("VPS_RPC_ADDR"); envAddr != "" {
		addr = envAddr
	}
	
	cli, err := client.NewClient(&client.ClientConfig{
		Address:              addr,
		Timeout:              config.AppConfig.GetClientDefaultTimeout(),
		InsecureSkipVerify:   true,
		EnableConnectionPool: false,
		EnableRetry:          false,
	})
	if err != nil {
		fmt.Println("创建客户端失败:", err)
		return
	}
	defer cli.Close()

    url := "https://kh.google.com/rt/earth/PlanetoidMetadata"
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

    // 模拟浏览器常见请求头（如需严格对齐，可进一步调整）
    headers := map[string]string{
        "Accept":           "*/*",
        "Accept-Encoding":  "gzip, deflate, br, zstd",
        "Accept-Language":  "zh-CN,zh;q=0.8,zh-TW;q=0.7,zh-HK;q=0.5,en-US;q=0.3,en;q=0.2",
        "Origin":           "https://earth.google.com",
        "Referer":          "https://earth.google.com/",
        "Sec-Fetch-Dest":   "empty",
        "Sec-Fetch-Mode":   "cors",
        "Sec-Fetch-Site":   "same-site",
        "TE":               "trailers",
    }

    start := time.Now()
    resp, err := cli.Fetch(ctx, &rpc.FetchRequest{Url: url, Headers: headers})
	if err != nil {
		fmt.Println("请求失败:", err)
		return
	}
	if resp.Error != "" {
		fmt.Printf("返回错误: %s\n", resp.Error)
		return
	}
    cost := time.Since(start).Milliseconds()
    ip := resp.Headers["X-VPS-IP"]
    vpsLatency := resp.Headers["X-VPS-LatencyMs"]
    if vpsLatency == "" { vpsLatency = "-" }
    fmt.Printf("URL=%s\nStatus=%d\nBody=%d bytes\nCost=%d ms\nIP=%s (server-latency=%sms)\n", resp.Url, resp.StatusCode, len(resp.Body), cost, ip, vpsLatency)
    fmt.Println("Headers:")
    for k, v := range resp.Headers {
        fmt.Printf("  %s: %s\n", k, v)
    }
}


