package client

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	"vps-rpc/rpc"
)

// QuicClient QUIC客户端，实现基于QUIC流的自定义RPC协议
type QuicClient struct {
	address string
	conn    *quic.Conn // DialAddrEarly返回*quic.Conn，支持0-RTT
	config  *tls.Config
}

// NewQuicClient 创建新的QUIC客户端（启用0-RTT以获得最快速度）
func NewQuicClient(address string, insecureSkipVerify bool) (*QuicClient, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: insecureSkipVerify,
		NextProtos:         []string{"h3", "h2", "http/1.1"}, // QUIC ALPN协议
	}

	// 使用Early连接以支持0-RTT（最快速度）
	quicConfig := &quic.Config{
		MaxIdleTimeout:       30 * time.Second,
		HandshakeIdleTimeout: 5 * time.Second,
		MaxIncomingStreams:   100,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 使用DialAddrEarly支持0-RTT（后续请求可立即发送，无需等待握手）
	conn, err := quic.DialAddrEarly(ctx, address, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("QUIC连接失败: %w", err)
	}

	return &QuicClient{
		address: address,
		conn:    conn,
		config:  tlsConfig,
	}, nil
}

// Fetch 发送单个抓取请求（优化：使用异步流打开，批量写入）
func (c *QuicClient) Fetch(ctx context.Context, req *rpc.FetchRequest) (*rpc.FetchResponse, error) {
	// 使用OpenStreamSync确保流已就绪（虽然稍慢，但更可靠）
	// 注意：DialAddrEarly已启用0-RTT，后续请求的流打开会更快
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("打开QUIC流失败: %w", err)
	}
	defer stream.Close()

	// 序列化请求
	reqData, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 批量写入：一次性写入长度和数据（减少系统调用）
	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	
	// 合并写入以提高性能
	request := make([]byte, 4+len(reqData))
	copy(request[:4], length[:])
	copy(request[4:], reqData)
	
	if _, err := stream.Write(request); err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	// 读取响应长度
	if _, err := stream.Read(length[:]); err != nil {
		return nil, fmt.Errorf("读取响应长度失败: %w", err)
	}

	respLen := int(binary.BigEndian.Uint32(length[:]))
	if respLen > 10*1024*1024 { // 限制10MB
		return nil, fmt.Errorf("响应过大: %d", respLen)
	}

	// 读取响应数据
	respData := make([]byte, respLen)
	if _, err := io.ReadFull(stream, respData); err != nil {
		return nil, fmt.Errorf("读取响应数据失败: %w", err)
	}

	// 解析响应
	var resp rpc.FetchResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &resp, nil
}

// Close 关闭QUIC连接
func (c *QuicClient) Close() error {
	if c.conn != nil {
		return c.conn.CloseWithError(0, "客户端关闭连接")
	}
	return nil
}

