package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"vps-rpc/rpc"
)

// Client gRPC客户端结构体
// 提供与服务器通信的接口
type Client struct {
	// conn gRPC连接（单连接模式）
	conn *grpc.ClientConn
	// client 爬虫服务客户端
	client rpc.CrawlerServiceClient
	// address 服务器地址
	address string
	// pool 连接池（连接池模式）
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

	// 创建TLS凭证
	var creds credentials.TransportCredentials
	if config.TLSConfig != nil || config.InsecureSkipVerify {
		tlsConfig := config.TLSConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{
				InsecureSkipVerify: config.InsecureSkipVerify,
			}
		}
		creds = credentials.NewTLS(tlsConfig)
	}

	client := &Client{
		address:  config.Address,
		usePool:  config.EnableConnectionPool,
		useRetry: config.EnableRetry,
		client:   nil, // 将在需要时创建
	}

	// 如果启用连接池，创建连接池
	if config.EnableConnectionPool {
		maxPoolSize := config.MaxPoolSize
		// 如果未指定连接池大小，使用默认值
		// 注意：这里应该从全局配置读取，但ClientConfig是客户端配置，不依赖全局配置
		if maxPoolSize <= 0 {
			maxPoolSize = 10 // 默认最大连接数（应该从配置读取）
		}
		// 创建连接池实例
		client.pool = NewConnectionPool(config.Address, creds, maxPoolSize, timeout)
	} else {
		// 否则创建单个连接
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		conn, err := grpc.DialContext(ctx, config.Address, grpc.WithTransportCredentials(creds))
		if err != nil {
			return nil, fmt.Errorf("连接服务器失败: %w", err)
		}
		client.conn = conn
		client.client = rpc.NewCrawlerServiceClient(conn)
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

	// 定义执行函数
	fn := func() (interface{}, error) {
		// 获取客户端（连接池模式需要从池中获取连接）
		client, err := c.getClient(ctx)
		if err != nil {
			return nil, err
		}

		return client.Fetch(ctx, req)
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

	// 定义执行函数
	fn := func() (interface{}, error) {
		// 获取客户端（连接池模式需要从池中获取连接）
		client, err := c.getClient(ctx)
		if err != nil {
			return nil, err
		}

		return client.BatchFetch(ctx, req)
	}

	// 如果启用重试，使用重试执行器
	if c.useRetry && c.retryExecutor != nil {
		result, err := c.retryExecutor.Execute(ctx, fn)
		if err != nil {
			return nil, err
		}
		return result.(*rpc.BatchFetchResponse), nil
	}

	// 否则直接执行
	result, err := fn()
	if err != nil {
		return nil, fmt.Errorf("批量抓取失败: %w", err)
	}
	return result.(*rpc.BatchFetchResponse), nil
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
