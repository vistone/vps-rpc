package server

import (
	"context"
	"log"
	"sync"
	"time"

	"vps-rpc/config"
	"vps-rpc/proxy"
	"vps-rpc/rpc"
)

// getRandomUserAgent 返回一个随机User-Agent字符串
func getRandomUserAgent() string {
	candidates := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 13_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Safari/605.1.15",
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:118.0) Gecko/20100101 Firefox/118.0",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edg/120.0.0.0 Chrome/120.0.0.0 Safari/537.36",
	}
	idx := int(time.Now().UnixNano() % int64(len(candidates)))
	return candidates[idx]
}

// getRandomTLSClientType 随机返回一个受支持的TLS客户端类型
func getRandomTLSClientType() rpc.TLSClientType {
	candidates := []rpc.TLSClientType{
		rpc.TLSClientType_CHROME,
		rpc.TLSClientType_FIREFOX,
		rpc.TLSClientType_SAFARI,
		rpc.TLSClientType_IOS,
		rpc.TLSClientType_ANDROID,
		rpc.TLSClientType_EDGE,
	}
	idx := int(time.Now().UnixNano() % int64(len(candidates)))
	return candidates[idx]
}

// getRandomConcurrency 返回随机并发数（5-8），避免固定模式被识别为机器流量
func getRandomConcurrency() int {
	// 5-8的随机数
	candidates := []int{5, 6, 7, 8}
	idx := int(time.Now().UnixNano() % int64(len(candidates)))
	return candidates[idx]
}

// CrawlerServer 爬虫服务器结构体
// 实现了rpc.CrawlerServiceServer接口，提供网页抓取服务
type CrawlerServer struct {
	// UnimplementedCrawlerServiceServer 提供未实现方法的默认实现
	// 确保即使我们没有实现所有方法，代码也能正常编译
	rpc.UnimplementedCrawlerServiceServer
	// stats 统计收集器
    stats *StatsCollector
    // recent 最近抓取去重：key=域名+"+"+IP，value=上次时间
    recent sync.Map
}

// NewCrawlerServer 创建新的爬虫服务器实例
// 返回一个实现了CrawlerServiceServer接口的CrawlerServer实例
// 参数:
//
//	stats: 统计收集器（可选，如果为nil则创建新的）
//
// 返回值:
//
//	*CrawlerServer: 新创建的爬虫服务器实例
func NewCrawlerServer(stats *StatsCollector) *CrawlerServer {
	if stats == nil {
		stats = NewStatsCollector()
	}
	// 返回一个新的CrawlerServer实例
	return &CrawlerServer{
		stats: stats,
	}
}

// Fetch 处理单个网页抓取请求
// 实现了rpc.CrawlerServiceServer接口的Fetch方法
// 参数:
//
//	ctx: 上下文对象，用于控制请求的生命周期
//	req: 抓取请求对象，包含要抓取的URL和其他配置
//
// 返回值:
//
//	*rpc.FetchResponse: 抓取响应对象，包含抓取结果
//	error: 可能发生的错误
func (s *CrawlerServer) Fetch(ctx context.Context, req *rpc.FetchRequest) (*rpc.FetchResponse, error) {
	// 记录RPC请求到达时间（用于计算端到端延迟）
	rpcStartTime := time.Now()
	
    // 记录抓取日志，包含目标URL
    log.Printf("正在抓取 URL: %s", req.Url)

    // 去重窗口（URL级）：窗口内相同URL不重复抓取
    var dedupeWindow time.Duration
    if w := config.AppConfig.Crawler.DedupeWindow; w != "" {
        if d, err := time.ParseDuration(w); err == nil && d > 0 {
            dedupeWindow = d
        }
    }
    if dedupeWindow > 0 {
        if v, ok := s.recent.Load(req.Url); ok {
            if last, ok2 := v.(time.Time); ok2 {
                if time.Since(last) < dedupeWindow {
                    return &rpc.FetchResponse{
                        Url:        req.Url,
                        StatusCode: 204,
                        Headers:    map[string]string{"X-Dedup": "true"},
                    }, nil
                }
            }
        }
    }

    // 设置User-Agent：优先使用配置默认UA；如未配置则随机选择
    if req.UserAgent == "" {
        if config.AppConfig.Crawler.UserAgent != "" {
            req.UserAgent = config.AppConfig.Crawler.UserAgent
        } else {
            req.UserAgent = getRandomUserAgent()
        }
    }

	// 设置TLS客户端类型：如果未指定，随机选择一个受支持的TLS指纹
	if req.TlsClient == rpc.TLSClientType_DEFAULT {
		req.TlsClient = getRandomTLSClientType()
	}

	// 创建uTLS客户端配置
	// 根据请求中的配置创建uTLS客户端配置对象
	utlsConfig := &proxy.UTLSConfig{
		// 设置TLS客户端类型
		ClientType: req.TlsClient,
		// 设置超时时间，从配置中获取默认超时时间
		Timeout: config.AppConfig.GetCrawlerDefaultTimeout(),
	}

	// 创建新的uTLS客户端实例
	// NewUTLSClient函数会根据配置创建相应的客户端
	client := proxy.NewUTLSClient(utlsConfig)

	// 执行抓取操作
	// Fetch方法会执行实际的网页抓取并返回结果
	httpFetchStart := time.Now()
	response, err := client.Fetch(ctx, req)
	httpFetchLatency := time.Since(httpFetchStart)

	// 计算总延迟时间（RPC请求处理总时间）
	rpcTotalLatency := time.Since(rpcStartTime)

    // 记录详细耗时日志（包含HTTP抓取和RPC总时间）
	log.Printf("抓取完成 URL: %s, HTTP抓取: %v, RPC总耗时: %v (RPC传输: %v)", 
		req.Url, httpFetchLatency, rpcTotalLatency, rpcTotalLatency-httpFetchLatency)

    // 记录此次时间用于去重
    if dedupeWindow > 0 && err == nil {
        s.recent.Store(req.Url, time.Now())
    }

	// 记录统计信息（使用HTTP抓取时间作为主要指标）
	if s.stats != nil {
		success := err == nil && response != nil && response.Error == ""
		errorType := ""
		if err != nil {
			errorType = "fetch_error"
		} else if response != nil && response.Error != "" {
			errorType = "response_error"
		}
		s.stats.RecordRequest(success, httpFetchLatency, errorType)
	}

	if err != nil {
		// 如果抓取过程中发生错误，返回包含错误信息的响应
		return &rpc.FetchResponse{
			// 设置URL
			Url: req.Url,
			// 设置错误信息
			Error: err.Error(),
		}, nil
	}

	// 如果抓取成功，返回抓取结果
	return response, nil
}

// BatchFetch 处理批量网页抓取请求
// 实现了rpc.CrawlerServiceServer接口的BatchFetch方法
// 使用并发控制机制，支持限制最大并发数
// 参数:
//
//	ctx: 上下文对象，用于控制请求的生命周期
//	req: 批量抓取请求对象，包含多个要抓取的请求
//
// 返回值:
//
//	*rpc.BatchFetchResponse: 批量抓取响应对象，包含所有抓取结果
//	error: 可能发生的错误
func (s *CrawlerServer) BatchFetch(ctx context.Context, req *rpc.BatchFetchRequest) (*rpc.BatchFetchResponse, error) {
	// 记录开始时间
	startTime := time.Now()

	// 记录批量抓取日志，包含请求的数量
	log.Printf("正在批量抓取 %d 个 URL", len(req.Requests))

	// 确定最大并发数：如果请求中指定了max_concurrent，则使用该值；否则从配置读取
	maxConcurrent := int(req.MaxConcurrent)
	if maxConcurrent <= 0 {
		maxConcurrent = config.AppConfig.Crawler.MaxConcurrentRequests
	}
	// 如果配置中的值也为0或未设置，使用随机化并发数（5-8，避免被识别规律）
	if maxConcurrent <= 0 {
		maxConcurrent = getRandomConcurrency()
	}

	// 创建响应数组，用于存储所有抓取结果
	// 数组长度与请求的数量相同
	responses := make([]*rpc.FetchResponse, len(req.Requests))

	// 创建用于控制并发的信号量通道
	// 使用buffered channel作为信号量，限制同时运行的goroutine数量
	semaphore := make(chan struct{}, maxConcurrent)

	// 创建WaitGroup，用于等待所有goroutine完成
	var wg sync.WaitGroup

	// 创建互斥锁，保护responses数组的并发写入
	var mu sync.Mutex

	// 遍历所有请求，并发执行抓取操作
	for i, fetchReq := range req.Requests {
		// 增加WaitGroup计数器
		wg.Add(1)

		// 启动goroutine执行抓取
		go func(index int, request *rpc.FetchRequest) {
			// 在函数退出时减少WaitGroup计数器
			defer wg.Done()

			// 获取信号量（如果通道已满，这里会阻塞）
			semaphore <- struct{}{}
			// 在函数退出时释放信号量
			defer func() { <-semaphore }()

            // 设置User-Agent：优先使用配置默认UA；如未配置则随机选择
            if request.UserAgent == "" {
                if config.AppConfig.Crawler.UserAgent != "" {
                    request.UserAgent = config.AppConfig.Crawler.UserAgent
                } else {
                    request.UserAgent = getRandomUserAgent()
                }
            }

			// 设置TLS客户端类型：如果未指定，随机选择一个受支持的TLS指纹
			if request.TlsClient == rpc.TLSClientType_DEFAULT {
				request.TlsClient = getRandomTLSClientType()
			}

			// 创建uTLS客户端配置
			// 根据请求中的配置创建uTLS客户端配置对象
			utlsConfig := &proxy.UTLSConfig{
				// 设置TLS客户端类型
				ClientType: request.TlsClient,
				// 设置超时时间，从配置中获取默认超时时间
				Timeout: config.AppConfig.GetCrawlerDefaultTimeout(),
			}

			// 创建新的uTLS客户端实例
			// NewUTLSClient函数会根据配置创建相应的客户端
			client := proxy.NewUTLSClient(utlsConfig)

			// 执行抓取操作
			// Fetch方法会执行实际的网页抓取并返回结果
			requestStartTime := time.Now()
			resp, err := client.Fetch(ctx, request)
			requestLatency := time.Since(requestStartTime)
			// 记录该 URL 的单次抓取耗时
			log.Printf("抓取完成 URL: %s, 耗时: %v", request.Url, requestLatency)

			// 使用互斥锁保护responses数组的并发写入
			mu.Lock()

			// 记录单个请求的统计信息
			if s.stats != nil {
				success := err == nil && resp != nil && resp.Error == ""
				errorType := ""
				if err != nil {
					errorType = "fetch_error"
				} else if resp != nil && resp.Error != "" {
					errorType = "response_error"
				}
				s.stats.RecordRequest(success, requestLatency, errorType)
			}
			if err != nil {
				// 如果抓取过程中发生错误，创建包含错误信息的响应
				responses[index] = &rpc.FetchResponse{
					// 设置URL
					Url: request.Url,
					// 设置错误信息
					Error: err.Error(),
				}
			} else {
				// 如果抓取成功，将结果存储到响应数组中
				responses[index] = resp
			}
			mu.Unlock()
		}(i, fetchReq)
	}

	// 等待所有goroutine完成
	wg.Wait()

	// 计算总延迟
	totalLatency := time.Since(startTime)

	// 记录批量抓取完成的日志
	log.Printf("批量抓取完成，共处理 %d 个请求，总耗时: %v", len(responses), totalLatency)

	// 返回包含所有抓取结果的批量响应对象
	return &rpc.BatchFetchResponse{
		// 设置响应列表
		Responses: responses,
	}, nil
}
