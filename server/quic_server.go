package server

import (
	"context"
	"encoding/binary"
    "fmt"
	"io"
	"log"
    "net"
	"time"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	"vps-rpc/config"
	"vps-rpc/rpc"
)

// QuicRpcServer QUIC RPC服务器结构体
// 基于QUIC流的自定义RPC协议，不使用gRPC以追求最快速度
type QuicRpcServer struct {
	// crawlerServer 爬虫服务实例
	crawlerServer *CrawlerServer
	// peerServer peer服务实例
	peerServer rpc.PeerServiceServer
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
func NewQuicRpcServer(crawlerServer *CrawlerServer) (*QuicRpcServer, error) {
	// 生成TLS配置
	tlsConfig, err := GenerateTLSConfig()
	if err != nil {
		return nil, err
	}

	// 获取QUIC配置
	quicConfig := &quic.Config{
		MaxIdleTimeout:         config.AppConfig.GetQuicMaxIdleTimeout(),
		HandshakeIdleTimeout:   config.AppConfig.GetQuicHandshakeIdleTimeout(),
		MaxIncomingStreams:     int64(config.AppConfig.Quic.MaxIncomingStreams),
		MaxIncomingUniStreams:   int64(config.AppConfig.Quic.MaxIncomingUniStreams),
	}

	// 创建QUIC监听器
	quicListener, err := quic.ListenAddrEarly(config.AppConfig.GetServerAddr(), tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}

	log.Printf("QUIC服务器监听在地址: %s", config.AppConfig.GetServerAddr())

	return &QuicRpcServer{
		crawlerServer: crawlerServer,
		peerServer:    nil, // 暂时为nil，后续会在main.go中设置
		quicListener:  *quicListener,
	}, nil
}

// RegisterService 注册服务（兼容接口，QUIC模式下已在NewQuicRpcServer传入）
func (s *QuicRpcServer) RegisterService(service rpc.CrawlerServiceServer) {
	// QUIC模式下服务已在创建时传入，此方法保留用于兼容
}

// RegisterAdminService 注册管理服务（QUIC模式下暂不支持）
func (s *QuicRpcServer) RegisterAdminService(service *AdminServer) {
	// QUIC模式下暂不支持管理服务
}

// SetPeerServer 设置PeerService服务器（用于main.go初始化）
func (s *QuicRpcServer) SetPeerServer(peerServer rpc.PeerServiceServer) {
	s.peerServer = peerServer
}

// Serve 启动服务器服务
// 基于QUIC流的自定义RPC协议：每个请求使用一个新流，直接传输protobuf消息
func (s *QuicRpcServer) Serve() error {
	log.Println("QUIC RPC服务器开始服务")

	for {
		conn, err := s.quicListener.Accept(context.Background())
		if err != nil {
			log.Printf("接受QUIC连接失败: %v", err)
			return err
		}

		go s.handleConnection(conn)
	}
}

// handleConnection 处理单个QUIC连接
func (s *QuicRpcServer) handleConnection(conn *quic.Conn) {
	defer conn.CloseWithError(0, "服务器关闭连接")
	log.Printf("已接受来自 %s 的QUIC连接", conn.RemoteAddr().String())

	for {
        stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Printf("接受QUIC流失败: %v", err)
			return
		}

        go s.handleStreamWithConn(conn, stream)
	}
}

// handleStreamWithConn 处理单个QUIC流：实现自定义RPC协议
// 协议格式：4字节长度（大端序）+ protobuf消息
// 支持多服务路由：通过消息字段特征识别请求类型
func (s *QuicRpcServer) handleStreamWithConn(conn *quic.Conn, stream *quic.Stream) {
	defer stream.Close()

	// 读取请求长度
	var length [4]byte
    _ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
    if _, err := stream.Read(length[:]); err != nil {
		log.Printf("读取消息长度失败: %v", err)
		return
	}

	msgLen := int(binary.BigEndian.Uint32(length[:]))
	if msgLen > 10*1024*1024 { // 限制10MB
		log.Printf("消息过大: %d", msgLen)
		return
	}

    // 读取protobuf消息
    msgData := make([]byte, msgLen)
    if _, err := io.ReadFull(stream, msgData); err != nil {
		log.Printf("读取消息数据失败: %v", err)
		return
	}
    _ = stream.SetReadDeadline(time.Time{})

    // 解析并路由到对应服务
	ctx := context.Background()
	var respData []byte

    // 若消息体为空且开启了PeerService，直接作为GetPeers处理
    if msgLen == 0 && s.peerServer != nil {
        var getPeersReq rpc.GetPeersRequest
        resp, err := s.peerServer.GetPeers(ctx, &getPeersReq)
        if err != nil {
            log.Printf("GetPeers失败: %v", err)
            return
        }
        respData, err = proto.Marshal(resp)
        if err != nil {
            log.Printf("序列化响应失败: %v", err)
            return
        }
        log.Printf("[quic-rpc] GetPeers: %d个节点", len(resp.Peers))
    } else {
    // 尝试解析为FetchRequest（CrawlerService）
	var fetchReq rpc.FetchRequest
	if err := proto.Unmarshal(msgData, &fetchReq); err == nil && fetchReq.Url != "" {
		// 确认是FetchRequest，调用CrawlerService
		quicRpcStart := time.Now()
		resp, err := s.crawlerServer.Fetch(ctx, &fetchReq)
		quicRpcLatency := time.Since(quicRpcStart)
		if err != nil {
			resp = &rpc.FetchResponse{
				Url:        fetchReq.Url,
				StatusCode: 500,
				Error:      err.Error(),
			}
		}
		respData, err = proto.Marshal(resp)
		if err != nil {
			log.Printf("序列化响应失败: %v", err)
			return
		}
		log.Printf("[quic-rpc] URL=%s, 服务处理=%v, 总计=%v", fetchReq.Url, quicRpcLatency, quicRpcLatency)
    } else if s.peerServer != nil {
		// 尝试解析为PeerService相关请求
		// ExchangeDNSRequest: 有records字段
        var exchangeDNSReq rpc.ExchangeDNSRequest
        if err := proto.Unmarshal(msgData, &exchangeDNSReq); err == nil {
            // 学到对端地址
            if conn != nil {
                if ps, ok := s.peerServer.(*PeerServiceServer); ok {
                    rhost, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
                    if rhost != "" {
                        ps.AddKnownPeer(net.JoinHostPort(rhost, fmt.Sprintf("%d", config.AppConfig.Server.Port)))
                    }
                }
            }
			resp, err := s.peerServer.ExchangeDNS(ctx, &exchangeDNSReq)
			if err != nil {
				log.Printf("ExchangeDNS失败: %v", err)
				return
			}
			respData, err = proto.Marshal(resp)
			if err != nil {
				log.Printf("序列化响应失败: %v", err)
				return
			}
            log.Printf("[quic-rpc] ExchangeDNS: 本地=%d, 远程=%d", len(exchangeDNSReq.Records), len(resp.Records))
		} else {
			// GetPeersRequest: 空消息
            var getPeersReq rpc.GetPeersRequest
			if err := proto.Unmarshal(msgData, &getPeersReq); err == nil {
                // 学到对端地址
                if conn != nil {
                    if ps, ok := s.peerServer.(*PeerServiceServer); ok {
                        rhost, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
                        if rhost != "" {
                            ps.AddKnownPeer(net.JoinHostPort(rhost, fmt.Sprintf("%d", config.AppConfig.Server.Port)))
                        }
                    }
                }
				resp, err := s.peerServer.GetPeers(ctx, &getPeersReq)
				if err != nil {
					log.Printf("GetPeers失败: %v", err)
					return
				}
				respData, err = proto.Marshal(resp)
				if err != nil {
					log.Printf("序列化响应失败: %v", err)
					return
				}
				log.Printf("[quic-rpc] GetPeers: %d个节点", len(resp.Peers))
			} else {
				// ReportNodeRequest: 有address字段
            var reportNodeReq rpc.ReportNodeRequest
            if err := proto.Unmarshal(msgData, &reportNodeReq); err == nil {
                // 若对方未提供地址，自动根据远端IP和本服务端口生成 <remoteIP>:<server.port>
                if reportNodeReq.Address == "" && conn != nil {
                    rhost, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
                    if rhost != "" {
                        reportNodeReq.Address = net.JoinHostPort(rhost, fmt.Sprintf("%d", config.AppConfig.Server.Port))
                    }
                }
                if reportNodeReq.Address == "" {
                    log.Printf("ReportNode缺少address，忽略")
                    return
                }
					resp, err := s.peerServer.ReportNode(ctx, &reportNodeReq)
					if err != nil {
						log.Printf("ReportNode失败: %v", err)
						return
					}
					respData, err = proto.Marshal(resp)
					if err != nil {
						log.Printf("序列化响应失败: %v", err)
						return
					}
					log.Printf("[quic-rpc] ReportNode: %s", reportNodeReq.Address)
				} else {
					log.Printf("无法识别的请求类型")
					return
				}
			}
		}
	} else {
		log.Printf("未知请求类型且PeerServer未初始化")
		return
	}
    }

	// 发送响应：长度 + 数据
	respLen := uint32(len(respData))
	binary.BigEndian.PutUint32(length[:], respLen)
    _ = stream.SetWriteDeadline(time.Now().Add(3 * time.Second))
    if _, err := stream.Write(length[:]); err != nil {
		log.Printf("发送响应长度失败: %v", err)
		return
	}
    if _, err := stream.Write(respData); err != nil {
		log.Printf("发送响应数据失败: %v", err)
		return
	}
    _ = stream.SetWriteDeadline(time.Time{})
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
	return s.quicListener.Close()
}
