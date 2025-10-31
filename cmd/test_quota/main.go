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
	// 测试多个URL，每个URL请求100次，总共300次
	urls := []string{
		"https://kh.google.com/rt/earth/PlanetoidMetadata",
		"https://kh.google.com/rt/earth/BulkMetadata/pb=!1m2!1s!2u1002",
		"https://kh.google.com/rt/earth/NodeData/pb=!1m2!1s02!2u1002!2e1!3u1028!4b0",
	}
	requestsPerURL := 100

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
			res := testNodeQuota(n, urls, requestsPerURL, headers)
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
		fmt.Printf("  URL请求分布:\n")
		for _, url := range urls {
			count := res.urlCounts[url]
			fmt.Printf("    %s: %d次\n", url, count)
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
	urlCounts    map[string]int // 记录每个URL的请求次数
}

func testNodeQuota(node string, urls []string, requestsPerURL int, headers map[string]string) *nodeResult {
	total := len(urls) * requestsPerURL
	cli, err := client.NewClient(&client.ClientConfig{
		Address:            node,
		Timeout:            35 * time.Second,
		InsecureSkipVerify: true,
		EnableConnectionPool: true,
		MaxPoolSize:       20,
		EnableRetry:       false,
	})
	if err != nil {
		return &nodeResult{fail: total, urlCounts: make(map[string]int)}
	}
	defer cli.Close()

	res := &nodeResult{
		ipCounts:  make([]ipCount, 0),
		urlCounts: make(map[string]int),
	}
	ipMap := make(map[string]int)
	var ipMu sync.Mutex

	// 预先创建所有请求任务：每个URL创建requestsPerURL个任务
	taskQueue := make(chan string, total)
	for _, url := range urls {
		for i := 0; i < requestsPerURL; i++ {
			taskQueue <- url
		}
	}
	close(taskQueue)

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

				// 从任务队列中获取URL
				selectedURL, ok := <-taskQueue
				if !ok {
					return
				}

				ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
				start := time.Now()
				resp, err := cli.Fetch(ctx, &rpc.FetchRequest{
					Url:       selectedURL,
					Headers:   headers,
					TlsClient: rpc.TLSClientType_CHROME,
				})
				cancel()
				duration := time.Since(start)

				ipMu.Lock()
				// 统计URL请求次数
				res.urlCounts[selectedURL]++
				
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
				done++
				ipMu.Unlock()
			}()
		}
		wg.Wait()

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

