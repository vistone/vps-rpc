package admin

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"vps-rpc/rpc"
)

// Client 管理客户端结构体
// 提供与服务器管理服务通信的接口
type Client struct {
	// conn gRPC连接
	conn *grpc.ClientConn
	// client 管理服务客户端
	client rpc.AdminServiceClient
	// address 服务器地址
	address string
}

// ClientConfig 管理客户端配置结构体
// 定义了管理客户端创建和连接所需的所有配置参数
type ClientConfig struct {
	// Address 服务器地址（格式：host:port）
	Address string
	// Timeout 连接超时时间
	// 如果为0，将使用默认值10秒
	Timeout time.Duration
	// TLSConfig TLS配置（如果为nil，则跳过TLS验证）
	TLSConfig *tls.Config
	// InsecureSkipVerify 跳过TLS证书验证（仅用于测试）
	// 设置此选项为true将跳过TLS证书验证
	InsecureSkipVerify bool
}

// NewClient 创建新的管理客户端
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
	// 如果配置中未指定超时时间，使用默认值10秒
	// 注意：这里应该从全局配置读取，但ClientConfig是客户端配置，不依赖全局配置
	timeout := config.Timeout
	if timeout == 0 {
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

	// 创建gRPC连接
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, config.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("连接服务器失败: %w", err)
	}

	// 创建管理服务客户端
	client := rpc.NewAdminServiceClient(conn)

	return &Client{
		conn:    conn,
		client:  client,
		address: config.Address,
	}, nil
}

// GetStatus 获取服务器状态
// 调用管理服务的GetStatus方法获取服务器运行状态信息
// 参数:
//
//	ctx: 上下文对象
//
// 返回值:
//
//	*rpc.StatusResponse: 状态响应对象，包含服务器状态信息
//	error: 可能发生的错误
func (c *Client) GetStatus(ctx context.Context) (*rpc.StatusResponse, error) {
	resp, err := c.client.GetStatus(ctx, &rpc.StatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("获取状态失败: %w", err)
	}
	return resp, nil
}

// GetStats 获取统计信息
// 调用管理服务的GetStats方法获取服务器统计信息
// 参数:
//
//	ctx: 上下文对象
//	reset: 是否在获取统计信息后重置统计
//
// 返回值:
//
//	*rpc.StatsResponse: 统计响应对象，包含详细的统计信息
//	error: 可能发生的错误
func (c *Client) GetStats(ctx context.Context, reset bool) (*rpc.StatsResponse, error) {
	resp, err := c.client.GetStats(ctx, &rpc.StatsRequest{
		Reset_: reset,
	})
	if err != nil {
		return nil, fmt.Errorf("获取统计信息失败: %w", err)
	}
	return resp, nil
}

// UpdateConfig 更新配置
// 调用管理服务的UpdateConfig方法更新服务器配置
// 参数:
//
//	ctx: 上下文对象
//	configKey: 配置键名称
//	configValue: 配置值（JSON格式字符串）
//
// 返回值:
//
//	*rpc.UpdateConfigResponse: 配置更新响应对象
//	error: 可能发生的错误
func (c *Client) UpdateConfig(ctx context.Context, configKey string, configValue string) (*rpc.UpdateConfigResponse, error) {
	resp, err := c.client.UpdateConfig(ctx, &rpc.UpdateConfigRequest{
		ConfigKey:   configKey,
		ConfigValue: configValue,
	})
	if err != nil {
		return nil, fmt.Errorf("更新配置失败: %w", err)
	}
	return resp, nil
}

// GetLogs 获取日志
// 调用管理服务的GetLogs方法获取服务器日志
// 参数:
//
//	ctx: 上下文对象
//	level: 日志级别过滤（空字符串表示不过滤）
//	maxLines: 最大返回行数
//
// 返回值:
//
//	*rpc.LogsResponse: 日志响应对象，包含日志条目
//	error: 可能发生的错误
func (c *Client) GetLogs(ctx context.Context, level string, maxLines int32) (*rpc.LogsResponse, error) {
	resp, err := c.client.GetLogs(ctx, &rpc.LogsRequest{
		Level:    level,
		MaxLines: maxLines,
		Follow:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("获取日志失败: %w", err)
	}
	return resp, nil
}

// HealthCheck 健康检查
// 调用管理服务的HealthCheck方法检查服务器健康状态
// 参数:
//
//	ctx: 上下文对象
//
// 返回值:
//
//	*rpc.HealthCheckResponse: 健康检查响应对象，包含健康状态信息
//	error: 可能发生的错误
func (c *Client) HealthCheck(ctx context.Context) (*rpc.HealthCheckResponse, error) {
	resp, err := c.client.HealthCheck(ctx, &rpc.HealthCheckRequest{})
	if err != nil {
		return nil, fmt.Errorf("健康检查失败: %w", err)
	}
	return resp, nil
}

// GetConnections 获取连接信息
// 调用管理服务的GetConnections方法获取当前客户端连接信息
// 参数:
//
//	ctx: 上下文对象
//
// 返回值:
//
//	*rpc.ConnectionsResponse: 连接信息响应对象，包含所有连接信息
//	error: 可能发生的错误
func (c *Client) GetConnections(ctx context.Context) (*rpc.ConnectionsResponse, error) {
	resp, err := c.client.GetConnections(ctx, &rpc.ConnectionsRequest{})
	if err != nil {
		return nil, fmt.Errorf("获取连接信息失败: %w", err)
	}
	return resp, nil
}

// Close 关闭客户端连接
// 关闭客户端使用的gRPC连接，释放相关资源
// 返回值:
//
//	error: 关闭过程中可能发生的错误
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// GetAddress 获取服务器地址
// 返回客户端连接的服务器地址
// 返回值:
//
//	string: 服务器地址字符串
func (c *Client) GetAddress() string {
	return c.address
}
