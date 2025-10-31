package client

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

    "vps-rpc/config"
    "vps-rpc/rpc"
    "vps-rpc/proxy"
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
    var ip4Address string
    var ip6Address string
	// 检查是否为IP地址
	if ip := net.ParseIP(host); ip != nil {
		// 已经是IP地址，直接使用（最快，无需DNS）
		ipAddress = address
	} else {
        // 是域名，快速解析为IP（收集v4/v6以做并行拨号）
		// 使用全局配置的客户端超时时间（通常较短，适合DNS解析）
		// DNS解析使用连接超时的一半，避免占用太多时间
		dnsTimeout := config.AppConfig.GetClientDefaultTimeout() / 2
		if dnsTimeout < 1*time.Second {
			dnsTimeout = 1 * time.Second // 最少1秒
		}
		if dnsTimeout > 5*time.Second {
			dnsTimeout = 5 * time.Second // 最多5秒
		}
		ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		cancel()
		
		if err != nil || len(ips) == 0 {
			// DNS解析失败或超时，回退使用域名（quic.DialAddrEarly会内部处理DNS）
			// 但这样会慢一些，建议用户直接使用IP地址
			ipAddress = address
        } else {
            // 记录首个v6和首个v4
            for _, ipa := range ips {
                if ipa.IP == nil { continue }
                // 若系统不支持IPv6，直接跳过IPv6地址，避免 net is unreachable
                if ipa.IP.To4() == nil {
                    if proxy.HasIPv6() && ip6Address == "" {
                        ip6Address = net.JoinHostPort(ipa.IP.String(), port)
                    }
                    // 如果不支持IPv6，直接忽略IPv6地址
                } else {
                    if ip4Address == "" {
                        ip4Address = net.JoinHostPort(ipa.IP.String(), port)
                    }
                }
            }
            // 优先选择可用的地址：如果设备支持IPv6且有IPv6地址，优先IPv6；否则使用IPv4
            if proxy.HasIPv6() && ip6Address != "" {
                ipAddress = ip6Address
            } else if ip4Address != "" {
                ipAddress = ip4Address
            } else {
                // 都没有找到可用地址，回退使用域名（让QUIC内部处理DNS）
                ipAddress = address
            }
		}
	}

    // 使用Early连接以支持0-RTT（最快速度）
	// 从全局配置读取QUIC参数，与服务器端保持一致
	quicConfig := &quic.Config{
		MaxIdleTimeout:       config.AppConfig.GetQuicMaxIdleTimeout(),
		HandshakeIdleTimeout: config.AppConfig.GetQuicHandshakeIdleTimeout(),
		MaxIncomingStreams:   100, // 客户端不需要太多入站流
	}

    // Happy Eyeballs：根据设备能力选择IPv4/IPv6，不支持IPv6时只尝试IPv4
    // 使用全局配置的客户端连接超时时间（而不是硬编码）
    connectTimeout := config.AppConfig.GetClientDefaultTimeout()
    // 如果配置的超时时间过短（小于5秒），则使用更合理的超时（15秒）以适应高延迟网络
    if connectTimeout < 5*time.Second {
        connectTimeout = 15 * time.Second
    }
    ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
    defer cancel()
    type dialResult struct{ conn *quic.Conn; err error }
    resCh := make(chan dialResult, 2)

    started := 0
    tryDial := func(addr string) {
        if addr == "" { return }
        started++
        go func(a string) {
            c, e := quic.DialAddrEarly(ctx, a, tlsConfig, quicConfig)
            resCh <- dialResult{c, e}
        }(addr)
    }

    // 如果设备不支持IPv6，清空IPv6地址，避免尝试连接
    if !proxy.HasIPv6() {
        ip6Address = ""
    }

    // 启动v6拨号（如有且设备支持）
    if ip6Address != "" {
        tryDial(ip6Address)
        // 立即启动v4（不等待300ms），在IPv6失败时快速回退
        // 这样可以更快检测到IPv6路由问题并回退到IPv4
        if ip4Address != "" {
            tryDial(ip4Address)
        }
    } else {
        // 不支持IPv6或没有IPv6地址，直接尝试IPv4
        tryDial(ip4Address)
    }
    // 若两者都为空（如传入的是IP或解析失败），至少拨一次原始地址（可为host或IP）
    if started == 0 {
        tryDial(ipAddress)
    }

    var conn *quic.Conn
    var dialErr error
    var ip6Failed bool // 标记IPv6连接是否失败
    
    for i := 0; i < started; i++ {
        r := <-resCh
        if r.err == nil && r.conn != nil {
            conn = r.conn
            dialErr = nil
            break
        }
        // 检查是否是IPv6连接失败（network is unreachable 或其他IPv6路由错误）
        isIPv6Err := false
        if r.err != nil && ip6Address != "" {
            errStr := r.err.Error()
            // 检测各种IPv6路由不可达的错误
            if strings.Contains(errStr, "network is unreachable") ||
               strings.Contains(errStr, "no route to host") ||
               strings.Contains(errStr, "No route to host") {
                isIPv6Err = true
            }
        }
        
        if isIPv6Err {
            // IPv6路由不可达，标记并立即尝试IPv4
            ip6Failed = true
            dialErr = r.err
            // 如果IPv4地址可用，立即尝试IPv4（使用新的context，避免使用已接近超时的context）
            if ip4Address != "" {
                log.Printf("[quic-client] IPv6连接失败 (%v)，立即回退到IPv4: %s", r.err, ip4Address)
                // 创建新的context，使用全局配置的超时时间
                fallbackTimeout := config.AppConfig.GetClientDefaultTimeout()
                if fallbackTimeout < 10*time.Second {
                    fallbackTimeout = 10 * time.Second // 回退连接至少给10秒
                }
                ctxV4, cancelV4 := context.WithTimeout(context.Background(), fallbackTimeout)
                if c4, e4 := quic.DialAddrEarly(ctxV4, ip4Address, tlsConfig, quicConfig); e4 == nil {
                    cancelV4()
                    conn = c4
                    dialErr = nil
                    break
                } else {
                    cancelV4()
                    log.Printf("[quic-client] IPv4连接也失败: %v", e4)
                }
            }
        } else {
            // 非IPv6路由错误，记录错误但继续等待其他连接结果
            if r.err != nil {
                dialErr = r.err
            }
        }
    }
    
    if conn == nil {
        // 如果IPv6失败且IPv4可用但未尝试，强制使用IPv4（使用新context）
        if ip6Failed && ip4Address != "" && ipAddress == ip6Address {
            log.Printf("[quic-client] 强制使用IPv4连接: %s", ip4Address)
            fallbackTimeout := config.AppConfig.GetClientDefaultTimeout()
            if fallbackTimeout < 10*time.Second {
                fallbackTimeout = 10 * time.Second
            }
            ctxV4, cancelV4 := context.WithTimeout(context.Background(), fallbackTimeout)
            if c4, e4 := quic.DialAddrEarly(ctxV4, ip4Address, tlsConfig, quicConfig); e4 == nil {
                cancelV4()
                conn = c4
                dialErr = nil
            } else {
                cancelV4()
                dialErr = e4
            }
        }
        
        // 最后兜底：尝试原始地址（可能是域名或IPv4，使用新context）
        if conn == nil {
            fallbackTimeout := config.AppConfig.GetClientDefaultTimeout()
            if fallbackTimeout < 10*time.Second {
                fallbackTimeout = 10 * time.Second
            }
            ctxFallback, cancelFallback := context.WithTimeout(context.Background(), fallbackTimeout)
            if c2, e2 := quic.DialAddrEarly(ctxFallback, ipAddress, tlsConfig, quicConfig); e2 == nil {
                cancelFallback()
                conn = c2
                dialErr = nil
            } else {
                cancelFallback()
                if dialErr == nil { dialErr = e2 }
                return nil, fmt.Errorf("QUIC连接失败: %w", dialErr)
            }
        }
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

