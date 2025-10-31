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
	conn    *quic.Conn
	config  *tls.Config
}

// NewQuicClient 创建新的QUIC客户端
func NewQuicClient(address string, insecureSkipVerify bool) (*QuicClient, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: insecureSkipVerify,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, address, tlsConfig, nil)
	if err != nil {
		return nil, fmt.Errorf("QUIC连接失败: %w", err)
	}

	return &QuicClient{
		address: address,
		conn:    conn,
		config:  tlsConfig,
	}, nil
}

// Fetch 发送单个抓取请求
func (c *QuicClient) Fetch(ctx context.Context, req *rpc.FetchRequest) (*rpc.FetchResponse, error) {
	// 打开一个新的QUIC流
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

	// 发送请求：长度 + 数据
	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	if _, err := stream.Write(length[:]); err != nil {
		return nil, fmt.Errorf("发送请求长度失败: %w", err)
	}
	if _, err := stream.Write(reqData); err != nil {
		return nil, fmt.Errorf("发送请求数据失败: %w", err)
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

