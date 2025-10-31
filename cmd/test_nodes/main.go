package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
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

	nodes := []string{"tile0.zeromaps.cn:4242", "tile3.zeromaps.cn:4242"}
	url := "https://kh.google.com/rt/earth/PlanetoidMetadata"
	requestsPerNode := 100

	headers := map[string]string{
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br, zstd",
		"Origin":          "https://earth.google.com",
		"Referer":         "https://earth.google.com/",
	}

	var wg sync.WaitGroup
	results := make(map[string]*nodeResult)
	var mu sync.Mutex

	for _, node := range nodes {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			res := testNode(n, url, headers, requestsPerNode)
			mu.Lock()
			results[n] = res
			mu.Unlock()
		}(node)
	}

	wg.Wait()

	// 汇总输出
	fmt.Println("\n=== 测试结果汇总 ===")
	for _, node := range nodes {
		res := results[node]
		fmt.Printf("\n[%s]\n", node)
		fmt.Printf("  成功: %d 失败: %d\n", res.ok, res.fail)
		if res.ok > 0 {
			fmt.Printf("  平均耗时: %dms\n", res.totalLatency/int64(res.ok))
		}
		fmt.Printf("  IP分布:\n")
		for _, ipc := range res.ipCounts {
			fmt.Printf("    %s: %d次\n", ipc.ip, ipc.count)
		}
	}
}

type ipCount struct {
	ip    string
	count int
}

type nodeResult struct {
	ok           int
	fail         int
	totalLatency int64
	ipCounts     []ipCount
}

func testNode(node, url string, headers map[string]string, n int) *nodeResult {
	cli, err := client.NewClient(&client.ClientConfig{
		Address:            node,
		Timeout:            35 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: false,
		EnableRetry:       false,
	})
	if err != nil {
		return &nodeResult{fail: n}
	}
	defer cli.Close()

	res := &nodeResult{
		ipCounts: make([]ipCount, 0),
	}
	ipMap := make(map[string]int)

	for i := 1; i <= n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		start := time.Now()
		resp, err := cli.Fetch(ctx, &rpc.FetchRequest{
			Url:     url,
			Headers: headers,
			TlsClient: rpc.TLSClientType_CHROME,
		})
		cancel()
		duration := time.Since(start)

		if err != nil {
			res.fail++
		} else if resp.Error != "" {
			res.fail++
		} else if resp.StatusCode == 200 {
			res.ok++
			res.totalLatency += duration.Milliseconds()
			ip := resp.Headers["X-VPS-IP"]
			if ip != "" {
				ipMap[ip]++
			}
		} else {
			res.fail++
		}

		if i%25 == 0 {
			fmt.Printf("[%s] 进度: %d/%d (成功=%d 失败=%d)\n", node, i, n, res.ok, res.fail)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// 排序IP计数
	for ip, count := range ipMap {
		res.ipCounts = append(res.ipCounts, ipCount{ip: ip, count: count})
	}
	sort.Slice(res.ipCounts, func(i, j int) bool {
		return res.ipCounts[i].count > res.ipCounts[j].count
	})
	if len(res.ipCounts) > 10 {
		res.ipCounts = res.ipCounts[:10]
	}

	return res
}

