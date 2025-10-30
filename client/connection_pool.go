package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

// ConnectionPool 连接池结构体
// 管理多个gRPC连接，提供连接复用和自动重连功能
type ConnectionPool struct {
	// address 服务器地址
	address string
	// tlsConfig TLS配置
	tlsConfig *credentials.TransportCredentials
	// connections 连接池
	connections []*grpc.ClientConn
	// maxSize 最大连接数
	maxSize int
	// currentSize 当前连接数
	currentSize int
	// mutex 保护并发访问
	mutex sync.RWMutex
	// dialTimeout 连接超时时间
	dialTimeout time.Duration
}

// NewConnectionPool 创建新的连接池
// 参数:
//
//	address: 服务器地址
//	tlsCreds: TLS凭证
//	maxSize: 最大连接数
//	dialTimeout: 连接超时时间
//
// 返回值:
//
//	*ConnectionPool: 新创建的连接池实例
func NewConnectionPool(address string, tlsCreds credentials.TransportCredentials, maxSize int, dialTimeout time.Duration) *ConnectionPool {
	// 如果maxSize小于等于0，使用默认值10
	// 注意：应该从配置读取，但ConnectionPool不依赖全局配置
	if maxSize <= 0 {
		maxSize = 10 // 默认最大连接数（应该从配置读取）
	}
	// 如果dialTimeout小于等于0，使用默认值10秒
	// 注意：应该从配置读取
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}

	return &ConnectionPool{
		address:     address,
		tlsConfig:   &tlsCreds,
		connections: make([]*grpc.ClientConn, 0, maxSize),
		maxSize:     maxSize,
		currentSize: 0,
		dialTimeout: dialTimeout,
	}
}

// GetConnection 从连接池获取一个可用连接
// 如果连接池为空或所有连接都不可用，创建新连接
// 返回值:
//
//	*grpc.ClientConn: gRPC连接
//	error: 可能发生的错误
func (p *ConnectionPool) GetConnection(ctx context.Context) (*grpc.ClientConn, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 查找可用的连接
	for i := len(p.connections) - 1; i >= 0; i-- {
		conn := p.connections[i]
		state := conn.GetState()

		// 如果连接已就绪，直接返回
		if state == connectivity.Ready {
			return conn, nil
		}

		// 如果连接已关闭或不可用，从池中移除
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			conn.Close()
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			p.currentSize--
		}
	}

	// 如果连接池未满，创建新连接
	if p.currentSize < p.maxSize {
		return p.createConnection(ctx)
	}

	// 连接池已满，尝试等待并重试（简化实现）
	// 注意：在实际生产环境中，应该实现更完善的等待机制
	if len(p.connections) > 0 {
		// 尝试返回第一个连接（即使可能不可用）
		// 调用者应该处理连接错误
		return p.connections[0], nil
	}

	// 如果池为空（不应该发生），创建新连接
	return p.createConnection(ctx)
}

// createConnection 创建新连接并添加到池中
// 参数:
//
//	ctx: 上下文对象
//
// 返回值:
//
//	*grpc.ClientConn: 新创建的gRPC连接
//	error: 可能发生的错误
func (p *ConnectionPool) createConnection(ctx context.Context) (*grpc.ClientConn, error) {
	// 创建超时上下文
	// 使用连接池的dialTimeout作为超时时间
	timeoutCtx, cancel := context.WithTimeout(ctx, p.dialTimeout)
	// 确保在函数退出时取消上下文
	defer cancel()

	// 创建gRPC连接
	// 使用超时上下文和TLS凭证创建连接
	conn, err := grpc.DialContext(timeoutCtx, p.address, grpc.WithTransportCredentials(*p.tlsConfig))
	if err != nil {
		// 如果创建连接失败，返回错误
		return nil, fmt.Errorf("创建连接失败: %w", err)
	}

	// 将连接添加到连接池中
	p.connections = append(p.connections, conn)
	// 增加当前连接数计数
	p.currentSize++

	// 返回新创建的连接
	return conn, nil
}

// Close 关闭连接池中的所有连接
// 返回值:
//
//	error: 关闭过程中可能发生的最后一个错误
func (p *ConnectionPool) Close() error {
	// 获取互斥锁
	p.mutex.Lock()
	// 确保在函数退出时释放锁
	defer p.mutex.Unlock()

	// 记录最后一个错误
	var lastErr error
	// 遍历所有连接并关闭它们
	for _, conn := range p.connections {
		if err := conn.Close(); err != nil {
			// 记录错误，但不中断关闭过程
			lastErr = err
		}
	}

	// 清空连接数组
	p.connections = nil
	// 重置当前连接数
	p.currentSize = 0

	// 返回最后一个错误（如果有）
	return lastErr
}

// Size 获取当前连接池大小
// 返回值:
//
//	int: 当前连接池中的连接数量
func (p *ConnectionPool) Size() int {
	// 获取读锁
	p.mutex.RLock()
	// 确保在函数退出时释放锁
	defer p.mutex.RUnlock()
	// 返回当前连接数
	return p.currentSize
}

// MaxSize 获取最大连接数
// 返回值:
//
//	int: 连接池允许的最大连接数
func (p *ConnectionPool) MaxSize() int {
	// 返回最大连接数（只读，不需要加锁）
	return p.maxSize
}
