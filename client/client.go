package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"

	"vps-rpc/rpc"
)

// Client QUIC客户端结构体（已切换为QUIC协议）
// 提供与服务器通信的接口
type Client struct {
	// conn gRPC连接（QUIC模式下不再使用）
	conn *grpc.ClientConn
	// client 爬虫服务客户端（QUIC模式下不再使用）
	client rpc.CrawlerServiceClient
	// quicClient QUIC客户端
	quicClient *QuicClient
	// address 服务器地址
	address string
	// pool 连接池（QUIC模式下不再使用）
	pool *ConnectionPool
	// retryExecutor 重试执行器
	retryExecutor *RetryExecutor
	// usePool 是否使用连接池
	usePool bool
	// useRetry 是否使用重试
	useRetry bool
}

// ClientConfig 客户端配置结构体
// 定义了客户端创建和运行所需的所有配置参数
type ClientConfig struct {
	// Address 服务器地址（格式：host:port）
	Address string
	// Timeout 连接超时时间
	// 如果为0，将使用默认值
	Timeout time.Duration
	// TLSConfig TLS配置（如果为nil，则跳过TLS验证）
	TLSConfig *tls.Config
	// InsecureSkipVerify 跳过TLS证书验证（仅用于测试）
	// 设置此选项为true将跳过TLS证书验证
	InsecureSkipVerify bool
	// EnableConnectionPool 是否启用连接池
	// 启用后，客户端将使用连接池管理多个连接
	EnableConnectionPool bool
	// MaxPoolSize 连接池最大大小（仅在启用连接池时有效）
	// 如果为0，将使用默认值
	MaxPoolSize int
	// EnableRetry 是否启用重试机制
	// 启用后，请求失败时会自动重试
	EnableRetry bool
	// RetryConfig 重试配置（仅在启用重试时有效）
	// 如果为nil，将使用默认重试配置
	RetryConfig *RetryConfig
}

// NewClient 创建新的gRPC客户端
// 参数:
//
//	config: 客户端配置
//
// 返回值:
//
//	*Client: 新创建的客户端实例
//	error: 可能发生的错误
func NewClient(config *ClientConfig) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("配置不能为空")
	}

	if config.Address == "" {
		return nil, fmt.Errorf("服务器地址不能为空")
	}

	// 设置默认超时时间
	// 如果配置中未指定超时时间，使用配置中的默认值
	timeout := config.Timeout
	if timeout == 0 {
		// 注意：这里应该从全局配置读取，但ClientConfig是客户端配置，不依赖全局配置
		// 如果需要在客户端也使用全局配置，需要传入config.Config
		timeout = 10 * time.Second
	}

	// 使用QUIC协议（最快速度）
	quicClient, err := NewQuicClient(config.Address, config.InsecureSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("创建QUIC客户端失败: %w", err)
	}

	// 将QUIC客户端包装为标准Client接口
	client := &Client{
		address:  config.Address,
		usePool:  false, // QUIC模式下暂不使用连接池
		useRetry: config.EnableRetry,
		quicClient: quicClient,
		client:   nil, // gRPC客户端不再使用
	}

	// 如果启用重试，创建重试执行器
	if config.EnableRetry {
		retryConfig := config.RetryConfig
		if retryConfig == nil {
			retryConfig = DefaultRetryConfig()
		}
		client.retryExecutor = NewRetryExecutor(retryConfig)
	}

	return client, nil
}

// Fetch 发起单个URL抓取请求
// 参数:
//
//	ctx: 上下文对象
//	req: 抓取请求
//
// 返回值:
//
//	*rpc.FetchResponse: 抓取响应
//	error: 可能发生的错误
func (c *Client) Fetch(ctx context.Context, req *rpc.FetchRequest) (*rpc.FetchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}

	// 使用QUIC客户端
	if c.quicClient == nil {
		return nil, fmt.Errorf("QUIC客户端未初始化")
	}

	// 定义执行函数
	fn := func() (interface{}, error) {
		return c.quicClient.Fetch(ctx, req)
	}

	// 如果启用重试，使用重试执行器
	if c.useRetry && c.retryExecutor != nil {
		result, err := c.retryExecutor.Execute(ctx, fn)
		if err != nil {
			return nil, err
		}
		return result.(*rpc.FetchResponse), nil
	}

	// 否则直接执行
	result, err := fn()
	if err != nil {
		return nil, fmt.Errorf("抓取失败: %w", err)
	}
	return result.(*rpc.FetchResponse), nil
}

// BatchFetch 发起批量URL抓取请求
// 参数:
//
//	ctx: 上下文对象
//	req: 批量抓取请求
//
// 返回值:
//
//	*rpc.BatchFetchResponse: 批量抓取响应
//	error: 可能发生的错误
func (c *Client) BatchFetch(ctx context.Context, req *rpc.BatchFetchRequest) (*rpc.BatchFetchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}

	// QUIC模式下：并发调用多个Fetch请求
	if c.quicClient == nil {
		return nil, fmt.Errorf("QUIC客户端未初始化")
	}

	responses := make([]*rpc.FetchResponse, 0, len(req.Requests))
	maxConcurrent := int(req.MaxConcurrent)
	if maxConcurrent <= 0 {
		maxConcurrent = 10 // 默认并发数
	}

	// 使用goroutine和channel控制并发
	sem := make(chan struct{}, maxConcurrent)
	results := make(chan *rpc.FetchResponse, len(req.Requests))

	for _, r := range req.Requests {
		sem <- struct{}{} // 获取信号量
		go func(fetchReq *rpc.FetchRequest) {
			defer func() { <-sem }() // 释放信号量
			resp, _ := c.quicClient.Fetch(ctx, fetchReq)
			results <- resp
		}(r)
	}

	// 收集结果
	for i := 0; i < len(req.Requests); i++ {
		responses = append(responses, <-results)
	}

	return &rpc.BatchFetchResponse{Responses: responses}, nil
}

// getClient 获取爬虫服务客户端
// 根据客户端配置（连接池模式或单连接模式）返回对应的客户端实例
// 参数:
//
//	ctx: 上下文对象
//
// 返回值:
//
//	rpc.CrawlerServiceClient: 爬虫服务客户端实例
//	error: 可能发生的错误
func (c *Client) getClient(ctx context.Context) (rpc.CrawlerServiceClient, error) {
	if c.usePool {
		if c.pool == nil {
			return nil, fmt.Errorf("连接池未初始化")
		}
		// 从连接池获取连接
		conn, err := c.pool.GetConnection(ctx)
		if err != nil {
			return nil, fmt.Errorf("从连接池获取连接失败: %w", err)
		}
		// 每次创建新的客户端（因为连接可能不同）
		return rpc.NewCrawlerServiceClient(conn), nil
	}

	// 单连接模式，使用缓存的客户端
	if c.client == nil {
		return nil, fmt.Errorf("客户端未初始化")
	}
	return c.client, nil
}

// Close 关闭客户端连接
// 关闭客户端使用的所有连接（连接池或单连接）
// 返回值:
//
//	error: 关闭过程中可能发生的错误
func (c *Client) Close() error {
	var err error
	if c.usePool && c.pool != nil {
		// 关闭连接池
		err = c.pool.Close()
	} else if c.conn != nil {
		// 关闭单个连接
		err = c.conn.Close()
	}
	return err
}

// GetAddress 获取服务器地址
// 返回客户端连接的服务器地址
// 返回值:
//
//	string: 服务器地址字符串
func (c *Client) GetAddress() string {
	return c.address
}
