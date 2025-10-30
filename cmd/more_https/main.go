package main

import (
	"context"
	"fmt"
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

	cli, err := client.NewClient(&client.ClientConfig{
		Address:            config.AppConfig.GetServerAddr(),
		Timeout:            config.AppConfig.GetClientDefaultTimeout(),
		InsecureSkipVerify: true,
		EnableConnectionPool: false,
		EnableRetry:          false,
	})
	if err != nil {
		fmt.Println("创建客户端失败:", err)
		return
	}
	defer cli.Close()

	urls := []string{
		"https://www.example.com",
		"https://www.google.com",
		"https://www.wikipedia.org",
		"https://www.github.com",
		"https://www.youtube.com",
		"https://www.cloudflare.com",
		"https://www.amazon.com",
		"https://www.bing.com",
		"https://www.qq.com",
		"https://www.baidu.com",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("=== 批量HTTPS抓取测试 ===")
	resp, err := cli.BatchFetch(ctx, &rpc.BatchFetchRequest{
		Requests: func() []*rpc.FetchRequest {
			arr := make([]*rpc.FetchRequest, 0, len(urls))
			for _, u := range urls {
				arr = append(arr, &rpc.FetchRequest{Url: u})
			}
			return arr
		}(),
	})
	if err != nil {
		fmt.Println("批量请求失败:", err)
		return
	}

	ok, fail := 0, 0
	for i, r := range resp.Responses {
		if r.Error != "" {
			fmt.Printf("[%d] %s -> Error=%s\n", i, r.Url, r.Error)
			fail++
			continue
		}
		fmt.Printf("[%d] %s -> Status=%d, Body=%d bytes\n", i, r.Url, r.StatusCode, len(r.Body))
		ok++
	}
	fmt.Printf("完成: 成功=%d 失败=%d 总数=%d\n", ok, fail, len(resp.Responses))
}
