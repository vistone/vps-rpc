package server

import (
	"context"
	"log"

	"github.com/quic-go/quic-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"vps-rpc/config"
	"vps-rpc/rpc"
)

// QuicRpcServer QUIC RPC服务器结构体
// 包含gRPC服务器和QUIC监听器，用于提供基于QUIC的RPC服务
type QuicRpcServer struct {
	// grpcServer gRPC服务器实例
	// 用于处理gRPC请求
	grpcServer *grpc.Server
	// quicListener QUIC监听器
	// 用于监听和接受QUIC连接
	quicListener quic.EarlyListener
}

// 注意：GenerateTLSConfig函数在grpc_server.go中定义，这里直接使用

// NewQuicRpcServer 创建新的QUIC RPC服务器
// 初始化QUIC监听器和gRPC服务器，创建QUIC RPC服务器实例
// 返回值:
//
//	*QuicRpcServer: 新创建的QUIC RPC服务器实例
//	error: 可能发生的错误
func NewQuicRpcServer() (*QuicRpcServer, error) {
	// 生成TLS配置
	tlsConfig, err := GenerateTLSConfig()
	if err != nil {
		// 如果生成TLS配置失败，返回错误
		return nil, err
	}

	// 创建gRPC服务器，并配置TLS凭证
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))

	// 获取QUIC配置
	// 从应用程序配置中获取QUIC相关参数
	quicConfig := &quic.Config{
		// 设置最大空闲超时时间
		MaxIdleTimeout: config.AppConfig.GetQuicMaxIdleTimeout(),
		// 设置握手空闲超时时间
		HandshakeIdleTimeout: config.AppConfig.GetQuicHandshakeIdleTimeout(),
		// 设置最大入站流数量
		MaxIncomingStreams: int64(config.AppConfig.Quic.MaxIncomingStreams),
		// 设置最大入站单向流数量
		MaxIncomingUniStreams: int64(config.AppConfig.Quic.MaxIncomingUniStreams),
	}

	// 创建QUIC监听器
	// 使用指定地址、TLS配置和QUIC配置创建监听器
	quicListener, err := quic.ListenAddrEarly(config.AppConfig.GetServerAddr(), tlsConfig, quicConfig)
	if err != nil {
		// 如果创建监听器失败，返回错误
		return nil, err
	}

	// 记录QUIC服务器监听地址的日志
	log.Printf("QUIC服务器监听在地址: %s", config.AppConfig.GetServerAddr())

	// 返回新的QUIC RPC服务器实例
	return &QuicRpcServer{
		// 设置gRPC服务器
		grpcServer: grpcServer,
		// 设置QUIC监听器（需要解引用，因为ListenAddrEarly返回的是指针）
		quicListener: *quicListener,
	}, nil
}

// RegisterService 注册服务
// 将指定的服务注册到gRPC服务器中
// 参数:
//
//	service: 要注册的RPC服务实例
func (s *QuicRpcServer) RegisterService(service rpc.CrawlerServiceServer) {
	// 将爬虫服务注册到gRPC服务器
	rpc.RegisterCrawlerServiceServer(s.grpcServer, service)
}

// Serve 启动服务器服务
// 开始接受和处理客户端连接
// 返回值:
//
//	error: 可能发生的错误
func (s *QuicRpcServer) Serve() error {
	// 记录服务器开始服务的日志
	log.Println("QUIC RPC服务器开始服务")

	// 循环接受客户端连接
	for {
		// 接受新的QUIC连接
		conn, err := s.quicListener.Accept(context.Background())
		if err != nil {
			// 如果接受连接失败，记录错误日志并返回错误
			log.Printf("接受连接失败: %v", err)
			return err
		}

		// 为每个连接启动独立的goroutine进行处理
		go func(quicConn *quic.Conn) {
			// 在函数退出时关闭QUIC连接
			defer quicConn.CloseWithError(0, "服务器关闭连接")

			// 记录接受新连接的日志
			log.Printf("已接受来自 %s 的新连接", quicConn.RemoteAddr().String())

			// 持续接受该连接上的所有流
			for {
				// 接受连接上的数据流
				stream, err := quicConn.AcceptStream(context.Background())
				if err != nil {
					// 如果接受数据流失败（可能是连接关闭），记录日志并退出
					log.Printf("接受数据流失败: %v", err)
					return
				}

				// 为每个流启动独立的goroutine进行处理
				go func(quicStream *quic.Stream) {
					// 在函数退出时关闭流
					defer quicStream.Close()

					// 创建QUIC流到gRPC的连接适配器
					// 将QUIC流包装为gRPC可以使用的连接
					// 注意：gRPC over QUIC的完整实现需要解析HTTP/2帧
					// 这里先预留接口，后续实现完整的传输层
					wrapper := &quicConnWrapper{
						conn:   quicConn,
						stream: quicStream,
					}

					// TODO: 实现完整的gRPC over QUIC协议处理
					// 需要解析HTTP/2帧并调用gRPC服务处理器
					// 当前先使用占位符，后续实现时会调用s.grpcServer.Serve()
					_ = wrapper
					_ = s.grpcServer
				}(stream)
			}
		}(conn)
	}
}

// quicConnWrapper 将QUIC连接和流包装为gRPC可以使用的连接
// 这是一个简化的实现，用于桥接QUIC和gRPC
type quicConnWrapper struct {
	conn   *quic.Conn
	stream *quic.Stream
}

// Close 关闭服务器
// 关闭gRPC服务器和QUIC监听器，释放相关资源
// 返回值:
//
//	error: 可能发生的错误
func (s *QuicRpcServer) Close() error {
	// 记录关闭服务器的日志
	log.Println("正在关闭 QUIC RPC 服务器")
	// 停止gRPC服务器
	s.grpcServer.Stop()
	// 关闭QUIC监听器并返回可能的错误
	return s.quicListener.Close()
}
