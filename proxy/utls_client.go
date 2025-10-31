package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	configPkg "vps-rpc/config"
	"vps-rpc/rpc"

	"golang.org/x/net/http2"

	utls "github.com/refraction-networking/utls"
)

// UTLSClient 是基于uTLS的HTTP客户端
// 用于执行伪装TLS指纹的HTTP请求
type UTLSClient struct {
	// client 底层HTTP客户端（当前未使用，保留以备将来扩展）
	client *http.Client
	// config uTLS客户端配置
	config *UTLSConfig
	// dns DNS 池实例（可选）
	dns *DNSPool
    // 连接复用：按 host 缓存 http.Client（h2/h1 分开）
    mu        sync.Mutex
    h2Clients map[string]*http.Client
    h1Clients map[string]*http.Client
}

// UTLSConfig 是uTLS客户端配置
// 定义了uTLS客户端的行为参数
type UTLSConfig struct {
	// ClientType TLS客户端类型
	// 指定要模拟的TLS客户端类型（如Chrome、Firefox等）
	ClientType rpc.TLSClientType
	// Timeout 请求超时时间
	// 指定请求的最大执行时间
	Timeout time.Duration
}

// NewUTLSClient 创建一个新的uTLS客户端
// 根据提供的配置创建并初始化uTLS客户端实例
// 参数:
//
//	config: uTLS客户端配置
//
// 返回值:
//
//	*UTLSClient: 新创建的uTLS客户端实例
func NewUTLSClient(cfg *UTLSConfig) *UTLSClient {
	// 如果未设置超时时间，则使用默认值30秒
	if cfg.Timeout == 0 {
		// 设置默认超时时间为30秒
		cfg.Timeout = 30 * time.Second
	}

    client := &UTLSClient{config: cfg, h2Clients: map[string]*http.Client{}, h1Clients: map[string]*http.Client{}}
	// 复用全局DNS池
	if gp := GetGlobalDNSPool(); gp != nil {
		client.dns = gp
	} else if configPkg.AppConfig.DNS.Enabled && configPkg.AppConfig.DNS.DBPath != "" {
		if pool, err := NewDNSPool(configPkg.AppConfig.DNS.DBPath); err == nil {
			client.dns = pool
		}
	}
	return client
}

// UA 选择：根据 TlsClient 类型选择与 uTLS 指纹一致的 UA 候选并随机取一
func selectUserAgentByTLSClient(t rpc.TLSClientType) string {
	// Chrome 家族（对应 HelloChrome_Auto 及相近版本）
	chrome := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 13_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		// 稍早稳定版，匹配历史指纹族
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/112.0.0.0 Safari/537.36",
	}
	// Firefox 家族（对应 HelloFirefox_Auto 及相近版本/ESR）
	firefox := []string{
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:118.0) Gecko/20100101 Firefox/118.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:118.0) Gecko/20100101 Firefox/118.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:115.0) Gecko/20100101 Firefox/115.0",
	}
	// Safari（macOS）家族（对应 HelloSafari_Auto）
	safari := []string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 13_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Safari/605.1.15",
	}
	// iOS Safari 家族（对应 HelloIOS_Auto）
	iosSafari := []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (iPad; CPU OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1",
	}
	// Android Chrome/OkHttp 家族（对应 HelloAndroid_11_OkHttp 与常见移动 Chrome 指纹）
	android := []string{
		"Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 12; Pixel 6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/112.0.0.0 Mobile Safari/537.36",
	}
	// Edge 家族（对应 HelloEdge_Auto）
	edge := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edg/120.0.0.0 Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edg/112.0.0.0 Chrome/112.0.0.0 Safari/537.36",
	}

	switch t {
	case rpc.TLSClientType_CHROME:
		return chrome[int(time.Now().UnixNano())%len(chrome)]
	case rpc.TLSClientType_FIREFOX:
		return firefox[int(time.Now().UnixNano())%len(firefox)]
	case rpc.TLSClientType_SAFARI:
		return safari[int(time.Now().UnixNano())%len(safari)]
	case rpc.TLSClientType_IOS:
		return iosSafari[int(time.Now().UnixNano())%len(iosSafari)]
	case rpc.TLSClientType_ANDROID:
		return android[int(time.Now().UnixNano())%len(android)]
	case rpc.TLSClientType_EDGE:
		return edge[int(time.Now().UnixNano())%len(edge)]
	default:
		// DEFAULT/未指定：在主流指纹族中挑选（Chrome/Firefox/Safari）
		pool := append(append([]string{}, chrome...), firefox...)
		pool = append(pool, safari...)
		return pool[int(time.Now().UnixNano())%len(pool)]
	}
}

// GetClient 获取底层HTTP客户端
// 返回底层的HTTP客户端实例（当前未使用，保留以备将来扩展）
// 返回值:
//
//	*http.Client: 底层HTTP客户端实例
func (c *UTLSClient) GetClient() *http.Client {
	// 返回底层HTTP客户端
	return c.client
}

// getClientHelloID 根据配置获取ClientHelloID
// 根据配置中的客户端类型返回相应的uTLS ClientHelloID
// 返回值:
//
//	*utls.ClientHelloID: 对应的ClientHelloID
func (c *UTLSClient) getClientHelloID() *utls.ClientHelloID {
	// 根据配置中的客户端类型选择相应的ClientHelloID
	switch c.config.ClientType {
	case rpc.TLSClientType_CHROME:
		// 返回Chrome浏览器的ClientHelloID
		return &utls.HelloChrome_Auto
	case rpc.TLSClientType_FIREFOX:
		// 返回Firefox浏览器的ClientHelloID
		return &utls.HelloFirefox_Auto
	case rpc.TLSClientType_SAFARI:
		// 返回Safari浏览器的ClientHelloID
		return &utls.HelloIOS_Auto
	case rpc.TLSClientType_IOS:
		// 返回iOS设备的ClientHelloID
		return &utls.HelloIOS_Auto
	case rpc.TLSClientType_ANDROID:
		// 返回Android设备的ClientHelloID
		return &utls.HelloAndroid_11_OkHttp
	case rpc.TLSClientType_EDGE:
		// 返回Edge浏览器的ClientHelloID
		return &utls.HelloEdge_Auto
	default:
		// 返回随机化的ClientHelloID作为默认值
		return &utls.HelloRandomized
	}
}

// dialUTLS 使用 uTLS 完成 TLS 握手并返回连接
func (c *UTLSClient) dialUTLS(ctx context.Context, network, address, serverName string, helloID *utls.ClientHelloID, nextProtos []string) (net.Conn, error) {
	d := &net.Dialer{Timeout: c.config.Timeout}
    // 若目标是IPv6地址，从本机IPv6池轮询一个源地址进行绑定（如有）
	host, _, _ := net.SplitHostPort(address)
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
        if src := NextIPv6LocalAddr(); src != nil {
            d.LocalAddr = &net.TCPAddr{IP: src}
            log.Printf("[route] bind_src_ipv6=%s target_host=%s", src.String(), serverName)
        }
	}
	tcpConn, err := d.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	ucfg := &utls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, // 跳过证书验证（IP直连时证书不包含IP SAN）
	}
	if len(nextProtos) > 0 {
		ucfg.NextProtos = nextProtos
	}
    uconn := utls.UClient(tcpConn, ucfg, *helloID)
	if err := uconn.HandshakeContext(ctx); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("握手失败: %w", err)
	}
    // 将实际连接到的远端IP加入DNS池（即便本次是域名拨号）
    if c.dns != nil && serverName != "" {
        if ra, ok := tcpConn.RemoteAddr().(*net.TCPAddr); ok && ra != nil && ra.IP != nil {
            _ = c.dns.ReportResult(serverName, ra.IP.String(), 200)
        }
    }
	return uconn, nil
}

// buildHTTP2Client 基于 http2.Transport + uTLS 的 http.Client
// 优化：启用连接复用和更长的空闲超时以最大化复用
func (c *UTLSClient) buildHTTP2Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
    tr := &http2.Transport{
        DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
            return c.dialUTLS(ctx, network, address, host, helloID, []string{"h2"})
        },
        ReadIdleTimeout:  30 * time.Second, // 连接空闲超时
        PingTimeout:      15 * time.Second, // Ping超时
        WriteByteTimeout: 10 * time.Second, // 写入超时
        MaxReadFrameSize: 1 << 20,           // 1MB最大帧大小
        AllowHTTP:        false,
    }
    return &http.Client{
        Timeout:   c.config.Timeout,
        Transport: tr,
    }
}

// buildHTTP1Client 基于 http.Transport + uTLS 的 http.Client（HTTP/1.1）
func (c *UTLSClient) buildHTTP1Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
    tr := &http.Transport{
        DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            return c.dialUTLS(ctx, network, address, host, helloID, []string{"http/1.1"})
        },
        TLSHandshakeTimeout:   c.config.Timeout,
        ForceAttemptHTTP2:     false,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        IdleConnTimeout:       60 * time.Second,
        DisableKeepAlives:     false,
        ResponseHeaderTimeout: c.config.Timeout,
    }
    return &http.Client{Timeout: c.config.Timeout, Transport: tr}
}

// 获取/创建可复用的 h2 客户端
func (c *UTLSClient) getOrCreateH2Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
    c.mu.Lock()
    defer c.mu.Unlock()
    if cli, ok := c.h2Clients[host]; ok && cli != nil {
        return cli
    }
    cli := c.buildHTTP2Client(host, address, helloID)
    c.h2Clients[host] = cli
    return cli
}

// 获取/创建可复用的 h1 客户端
func (c *UTLSClient) getOrCreateH1Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
    c.mu.Lock()
    defer c.mu.Unlock()
    if cli, ok := c.h1Clients[host]; ok && cli != nil {
        return cli
    }
    cli := c.buildHTTP1Client(host, address, helloID)
    c.h1Clients[host] = cli
    return cli
}

// Fetch 发起HTTP请求
// 使用uTLS库发起HTTP请求，伪装成指定类型的客户端
// 参数:
//
//	ctx: 上下文对象，用于控制请求的生命周期
//	req: 抓取请求对象，包含请求的URL和其他配置
//
// 返回值:
//
//	*rpc.FetchResponse: 抓取响应对象，包含请求结果
//	error: 可能发生的错误
func (c *UTLSClient) Fetch(ctx context.Context, req *rpc.FetchRequest) (*rpc.FetchResponse, error) {
	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

    // 解析目标
	host := extractHost(req.Url)
	addr := extractHostPort(req.Url)
	if host == "" || addr == "" {
		return nil, fmt.Errorf("URL 解析失败")
	}

	// 若启用DNS池，选择一个直连IP作为目标地址（保持 SNI/Host 为原域名）
    var selectedIP string
    var selectedIsV6 bool
    if c.dns != nil {
        // 自动注册域名到ProbeManager进行定期探测
        if pm := GetGlobalProbeManager(); pm != nil {
            pm.RegisterDomain(host)
        }
        // 若池内总IP数 < 2，先做一次强化探测以丰富池子
        if v4a, v6a, e := c.dns.GetIPs(timeoutCtx, host); e == nil {
            if len(v4a)+len(v6a) < 2 {
                ctx2, cancel2 := context.WithTimeout(timeoutCtx, 2*time.Second)
                _, _ = c.dns.resolveAndStore(ctx2, host)
                cancel2()
            }
        }
        // 双栈并行：若同时存在v4与v6，各挑一个并发请求，先返回者为准
        if v4s, v6s, e := c.dns.GetIPs(timeoutCtx, host); e == nil && (len(v4s) > 0 || len(v6s) > 0) {
            // 计算端口
            parsed, _ := url.Parse(req.Url)
            port := "443"
            if parsed != nil && parsed.Scheme == "http" {
                port = "80"
            }
            // 候选目标
            var addrV4, addrV6 string
            if len(v4s) > 0 { addrV4 = net.JoinHostPort(v4s[0], port) }
            if len(v6s) > 0 { addrV6 = net.JoinHostPort(v6s[0], port) }
            // 若同时存在两族，则并发；否则走单一路径
            if addrV4 != "" && addrV6 != "" {
                type result struct{ resp *rpc.FetchResponse; ip string; isV6 bool; err error }
                resCh := make(chan result, 2)
                // 复制请求以便并发使用独立ctx
                mkReq := func() (*http.Request, error) {
                    r2, e2 := http.NewRequestWithContext(timeoutCtx, "GET", req.Url, nil)
                    if e2 != nil { return nil, e2 }
                    if len(configPkg.AppConfig.Crawler.DefaultHeaders) > 0 {
                        for k, v := range configPkg.AppConfig.Crawler.DefaultHeaders {
                            if r2.Header.Get(k) == "" { r2.Header.Set(k, v) }
                        }
                    }
                    for k, v := range req.Headers { r2.Header.Set(k, v) }
                    r2.Header.Set("User-Agent", selectUserAgentByTLSClient(req.TlsClient))
                    return r2, nil
                }
                chid := c.getClientHelloID()
                // 发起函数
                doOnce := func(targetAddr string, ip string, isV6 bool) {
                    r2, e2 := mkReq()
                    if e2 != nil { resCh <- result{nil, ip, isV6, e2}; return }
                    if ip != "" { r2.Host = host }
                    // h2
                    h2c := c.getOrCreateH2Client(host, targetAddr, chid)
                    st := time.Now()
                    if resp, e := h2c.Do(r2); e == nil {
                        defer resp.Body.Close()
                        body, _ := io.ReadAll(resp.Body)
                        headers := map[string]string{}
                        for k, v := range resp.Header { if len(v) > 0 { headers[k] = v[0] } }
                        if c.dns != nil && ip != "" { _ = c.dns.ReportResult(host, ip, resp.StatusCode); _ = c.dns.ReportLatency(host, ip, time.Since(st).Milliseconds()) }
                        resCh <- result{&rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, ip, isV6, nil}; return
                    }
                    // h1 回退
                    h1c := c.getOrCreateH1Client(host, targetAddr, chid)
                    st = time.Now()
                    if resp, e := h1c.Do(r2); e == nil {
                        defer resp.Body.Close()
                        body, _ := io.ReadAll(resp.Body)
                        headers := map[string]string{}
                        for k, v := range resp.Header { if len(v) > 0 { headers[k] = v[0] } }
                        if c.dns != nil && ip != "" { _ = c.dns.ReportResult(host, ip, resp.StatusCode); _ = c.dns.ReportLatency(host, ip, time.Since(st).Milliseconds()) }
                        resCh <- result{&rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, ip, isV6, nil}; return
                    }
                    resCh <- result{nil, ip, isV6, fmt.Errorf("both h2/h1 failed")}
                }
                go doOnce(addrV6, v6s[0], true)
                go doOnce(addrV4, v4s[0], false)
                // 先到先得
                r := <-resCh
                if r.err == nil && r.resp != nil {
                    return r.resp, nil
                }
                r2 := <-resCh
                if r2.err == nil && r2.resp != nil {
                    return r2.resp, nil
                }
                // 并发都失败则继续走原单路逻辑（下面会处理）
            }
            // 单一路径（只有一种族）
            if addrV6 != "" { selectedIP, selectedIsV6, addr = v6s[0], true, addrV6 }
            if addrV4 != "" { selectedIP, selectedIsV6, addr = v4s[0], false, addrV4 }
        } else if ip, isV6, e := c.dns.NextIP(timeoutCtx, host); e == nil && ip != "" {
            selectedIP = ip
            selectedIsV6 = isV6
            parsed, _ := url.Parse(req.Url)
            port := "443"
            if parsed != nil && parsed.Scheme == "http" { port = "80" }
            addr = net.JoinHostPort(ip, port)
        }
    }

	// 构造请求
	httpReq, err := http.NewRequestWithContext(timeoutCtx, "GET", req.Url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
    // 先合并配置默认头（只在未被设置时生效）
	if len(configPkg.AppConfig.Crawler.DefaultHeaders) > 0 {
		for k, v := range configPkg.AppConfig.Crawler.DefaultHeaders {
			if httpReq.Header.Get(k) == "" {
				httpReq.Header.Set(k, v)
			}
		}
	}
	// 再覆盖请求级头（优先生效）
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	// User-Agent 必须与 uTLS 指纹匹配：基于 TlsClient 类型从预置池随机选择
	httpReq.Header.Set("User-Agent", selectUserAgentByTLSClient(req.TlsClient))
	// 若走直连IP，需设置 Host 头为原域名
	if selectedIP != "" {
		httpReq.Host = host
	}

    // 若所选IP已被新近拉黑，则重新选择或回退域名
	if selectedIP != "" && c.dns != nil {
		if banned, _ := c.dns.IsBlacklisted(host, selectedIP); banned {
			selectedIP = ""
			// 再尝试挑一个IP
			if ip2, _, e2 := c.dns.NextIP(timeoutCtx, host); e2 == nil && ip2 != "" {
				parsed, _ := url.Parse(req.Url)
				port := "443"
				if parsed != nil && parsed.Scheme == "http" {
					port = "80"
				}
                addr = net.JoinHostPort(ip2, port)
                selectedIP = ip2
			} else {
				// 回退域名
				addr = extractHostPort(req.Url)
			}
		}
	}

    // 路由日志：明确 IPv4/IPv6 与目标
    if selectedIP != "" {
        log.Printf("[route] target=%s via_ip=%s ipv6=%v dial_addr=%s", host, selectedIP, selectedIsV6, addr)
    } else {
        log.Printf("[route] target=%s via_domain dial_addr=%s", host, addr)
    }

	// uTLS 指纹
	chid := c.getClientHelloID()

	// 1) 优先 HTTP/2 + uTLS
    h2c := c.getOrCreateH2Client(host, addr, chid)
    // 不再写死 TE 等头部，全部由配置或请求传入
    // 记录即将发送的请求头（HTTP/2 分支，注意部分头会被 http2 内部改写/剔除，例如 Connection）
	{
        log.Printf("[utls][h2] 准备发送请求: %s", httpReq.URL.String())
	}
    start := time.Now()
    resp, err := h2c.Do(httpReq)
	if err == nil {
		defer resp.Body.Close()
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, fmt.Errorf("读取响应体失败: %w", rerr)
		}
		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		// 记录IP结果（若启用DNS池且使用了直连IP）
        if c.dns != nil && selectedIP != "" {
            _ = c.dns.ReportResult(host, selectedIP, resp.StatusCode)
            _ = c.dns.ReportLatency(host, selectedIP, time.Since(start).Milliseconds())
        }
		return &rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, nil
	}
	errH2 := err

	// 2) 回退 HTTP/1.1 + uTLS
    h1c := c.getOrCreateH1Client(host, addr, chid)
    // 不再写死 Connection 行为，由 Transport 与外部头共同决定
    // 记录即将发送的请求头（HTTP/1.1 分支）
	{
        log.Printf("[utls][h1] 准备发送请求: %s", httpReq.URL.String())
	}
    start = time.Now()
    resp, err = h1c.Do(httpReq)
	if err == nil {
		defer resp.Body.Close()
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, fmt.Errorf("读取响应体失败: %w", rerr)
		}
		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
        if c.dns != nil && selectedIP != "" {
            _ = c.dns.ReportResult(host, selectedIP, resp.StatusCode)
            _ = c.dns.ReportLatency(host, selectedIP, time.Since(start).Milliseconds())
        }
		return &rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, nil
	}
	errH1 := err

	// 2.5) 进一步回退：标准 http.Client（系统 TLS），尽量保证功能成功
    start = time.Now()
    resp, err = (&http.Client{Timeout: c.config.Timeout}).Do(httpReq)
	if err == nil {
		defer resp.Body.Close()
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, fmt.Errorf("读取响应体失败: %w", rerr)
		}
		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
        if c.dns != nil && selectedIP != "" {
            _ = c.dns.ReportResult(host, selectedIP, resp.StatusCode)
            _ = c.dns.ReportLatency(host, selectedIP, time.Since(start).Milliseconds())
        }
		return &rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, nil
	}

	// 全部失败，返回聚合错误，方便定位
	return nil, fmt.Errorf("h2失败: %v; h1失败: %v; std失败: %v", errH2, errH1, err)
}

// extractHost 从URL中提取主机名
// 从完整的URL中提取主机名部分
// 参数:
//
//	urlStr: 完整的URL字符串
//
// 返回值:
//
//	string: 提取的主机名
func extractHost(urlStr string) string {
	// 使用net/url标准库解析URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// 如果解析失败，返回空字符串
		// 实际使用中应该记录错误日志
		return ""
	}

	// 返回解析后的主机名（包含端口号）
	return parsedURL.Hostname()
}

// extractHostPort 从URL中提取主机和端口
// 从完整的URL中提取主机和端口部分，如果没有指定端口则使用默认端口
// 参数:
//
//	urlStr: 完整的URL字符串
//
// 返回值:
//
//	string: 格式为"host:port"的主机和端口字符串
func extractHostPort(urlStr string) string {
	// 使用net/url标准库解析URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// 如果解析失败，返回空字符串
		return ""
	}

	// 如果Host字段中包含端口，直接返回
	if parsedURL.Port() != "" {
		return parsedURL.Host
	}

	// 如果没有端口，根据协议添加默认端口
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return ""
	}

	if parsedURL.Scheme == "https" {
		return hostname + ":443"
	}
	return hostname + ":80"
}
