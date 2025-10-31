package client

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	"vps-rpc/rpc"
)

// QuicClient QUIC客户端，实现基于QUIC流的自定义RPC协议
type QuicClient struct {
	address   string // 原始地址（域名或IP）
	ipAddress string // 解析后的IP地址（用于直连，跳过DNS）
	conn      *quic.Conn // DialAddrEarly返回*quic.Conn，支持0-RTT
	config    *tls.Config
}

// NewQuicClient 创建新的QUIC客户端（启用0-RTT以获得最快速度）
// 优化：自动解析域名为IP并直连，节省DNS查询时间
func NewQuicClient(address string, insecureSkipVerify bool) (*QuicClient, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: insecureSkipVerify,
		NextProtos:         []string{"h3", "h2", "http/1.1"}, // QUIC ALPN协议
	}

	// 优化：解析域名为IP，使用IP直连以跳过DNS查询
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("地址格式错误: %w", err)
	}

	var ipAddress string
	// 检查是否为IP地址
	if ip := net.ParseIP(host); ip != nil {
		// 已经是IP地址，直接使用（最快，无需DNS）
		ipAddress = address
	} else {
		// 是域名，快速解析为IP（优先IPv4，其次IPv6）
		// 使用快速超时（1秒）避免阻塞太久
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		cancel()
		
		if err != nil || len(ips) == 0 {
			// DNS解析失败或超时，回退使用域名（quic.DialAddrEarly会内部处理DNS）
			// 但这样会慢一些，建议用户直接使用IP地址
			ipAddress = address
		} else {
			// 优先使用IPv4，其次IPv6
			var selectedIP net.IP
			for _, ip := range ips {
				if ip.IP.To4() != nil {
					selectedIP = ip.IP
					break
				}
			}
			if selectedIP == nil && len(ips) > 0 {
				selectedIP = ips[0].IP
			}
			ipAddress = net.JoinHostPort(selectedIP.String(), port)
		}
	}

	// 使用Early连接以支持0-RTT（最快速度）
	quicConfig := &quic.Config{
		MaxIdleTimeout:       30 * time.Second,
		HandshakeIdleTimeout: 5 * time.Second,
		MaxIncomingStreams:   100,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 使用IP直连（跳过DNS查询，节省时间）
	conn, err := quic.DialAddrEarly(ctx, ipAddress, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("QUIC连接失败: %w", err)
	}

	return &QuicClient{
		address:   address,
		ipAddress: ipAddress,
		conn:      conn,
		config:    tlsConfig,
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

