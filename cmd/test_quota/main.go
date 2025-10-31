package main

import (
	"context"
	"fmt"
	"math/rand"
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
	totalPerNode := 300

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
			res := testNodeQuota(n, url, headers, totalPerNode)
			mu.Lock()
			results[n] = res
			mu.Unlock()
		}(node)
	}

	wg.Wait()

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

func testNodeQuota(node, url string, headers map[string]string, total int) *nodeResult {
	cli, err := client.NewClient(&client.ClientConfig{
		Address:            node,
		Timeout:            35 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: true,
		MaxPoolSize:       20,
		EnableRetry:       false,
	})
	if err != nil {
		return &nodeResult{fail: total}
	}
	defer cli.Close()

	res := &nodeResult{
		ipCounts: make([]ipCount, 0),
	}
	ipMap := make(map[string]int)
	var ipMu sync.Mutex

	done := 0
	rand.Seed(time.Now().UnixNano())

	for done < total {
		// 随机并发数 3-8
		concurrency := rand.Intn(6) + 3
		remain := total - done
		if concurrency > remain {
			concurrency = remain
		}

		fmt.Printf("[%s] round %d..%d concurrency=%d\n", node, done+1, done+concurrency, concurrency)

		var wg sync.WaitGroup
		sem := make(chan struct{}, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
				start := time.Now()
				resp, err := cli.Fetch(ctx, &rpc.FetchRequest{
					Url:     url,
					Headers: headers,
					TlsClient: rpc.TLSClientType_CHROME,
				})
				cancel()
				duration := time.Since(start)

				ipMu.Lock()
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
				ipMu.Unlock()
			}()
		}
		wg.Wait()
		done += concurrency

		if done%50 == 0 {
			ipMu.Lock()
			fmt.Printf("  [%s] 进度: %d/%d (成功=%d 失败=%d)\n", node, done, total, res.ok, res.fail)
			ipMu.Unlock()
		}
	}

	// 排序IP计数
	ipMu.Lock()
	for ip, count := range ipMap {
		res.ipCounts = append(res.ipCounts, ipCount{ip: ip, count: count})
	}
	ipMu.Unlock()

	sort.Slice(res.ipCounts, func(i, j int) bool {
		return res.ipCounts[i].count > res.ipCounts[j].count
	})
	if len(res.ipCounts) > 15 {
		res.ipCounts = res.ipCounts[:15]
	}

	return res
}

