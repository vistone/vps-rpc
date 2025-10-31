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
	"strings"
	"sync"
	"time"

	configPkg "vps-rpc/config"
	"vps-rpc/rpc"

	"golang.org/x/net/http2"

	utls "github.com/refraction-networking/utls"
)

// 全局预建连接池：所有UTLSClient实例共享
var (
	globalPrewarmedH2Clients = make(map[string]*http.Client)
	globalPrewarmMu          sync.RWMutex
	globalPrewarmOnce        sync.Once
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
	// 连接复用：按 host+IP 缓存 http.Client（h2/h1 分开）
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

	client := &UTLSClient{
		config:    cfg,
		h2Clients: map[string]*http.Client{},
		h1Clients: map[string]*http.Client{},
	}
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

// PrewarmConnections 预建连接池：为DNS池中的所有IP建立热连接
// 在系统启动时调用，预热所有已知IP的连接，避免首次请求时的连接建立开销
// 这是一个全局函数，所有UTLSClient实例共享预建的连接池
func PrewarmConnections(ctx context.Context) {
	// 只执行一次预热
	globalPrewarmOnce.Do(func() {
		dnsPool := GetGlobalDNSPool()
		if dnsPool == nil {
			log.Printf("[prewarm] DNS池未启用，跳过连接预热")
			return
		}

		// 获取所有域名的所有IP
		domainIPs := dnsPool.GetAllDomainsAndIPs()
		totalIPs := 0
		for _, ips := range domainIPs {
			totalIPs += len(ips)
		}

		if totalIPs == 0 {
			log.Printf("[prewarm] 未发现任何IP地址，跳过连接预热")
			return
		}

		log.Printf("[prewarm] 开始预热连接池：%d个域名，共%d个IP", len(domainIPs), totalIPs)

		// 创建一个临时UTLSClient用于构建连接（使用默认配置）
		tempClient := &UTLSClient{
			config: &UTLSConfig{
				ClientType: rpc.TLSClientType_CHROME, // 使用Chrome作为默认指纹
				Timeout:    30 * time.Second,
			},
			dns: dnsPool,
		}

		// 并发预建立连接，但限制并发数避免过载
		sem := make(chan struct{}, 20) // 最多20个并发
		var wg sync.WaitGroup
		successCount := 0
		failCount := 0
		var countMu sync.Mutex

		chid := tempClient.getClientHelloID()

		// 为每个IP预建立连接
		for domain, ips := range domainIPs {
			for _, addr := range ips {
				wg.Add(1)
				sem <- struct{}{}
				go func(d, a string) {
					defer wg.Done()
					defer func() { <-sem }()

					// 解析IP地址
					host, port, err := net.SplitHostPort(a)
					if err != nil {
						countMu.Lock()
						failCount++
						countMu.Unlock()
						return
					}

					// 创建预建的HTTP/2客户端
					key := d + "|" + a
					h2c := tempClient.buildHTTP2Client(d, a, chid)

					// 关键修复：使用HTTP/1.1来预热连接，避免HTTP/2的协议错误
					// HTTP/1.1对HEAD请求返回数据的容忍度更高，不会报协议错误
					// 预热的目标是建立连接，HTTP/1.1和HTTP/2的连接都可以被后续请求复用
					// 注意：虽然用HTTP/1.1预热，但缓存的是HTTP/2客户端供后续使用
					h1c := tempClient.buildHTTP1Client(d, a, chid)
					
					// 尝试发送一个HEAD请求来建立连接（轻量级）
					// 使用很短的超时，仅用于建立连接，不等待完整响应
					prewarmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					req, _ := http.NewRequestWithContext(prewarmCtx, "HEAD", "https://"+host+":"+port, nil)
					req.Host = d // 设置Host头为域名
					resp, err := h1c.Do(req) // 使用HTTP/1.1避免协议错误
					cancel()

					// 关闭响应体
					if resp != nil {
						_ = resp.Body.Close()
					}

					// 检查是否成功：只要连接建立（有响应）即可
					if resp != nil {
						// 连接建立成功，缓存HTTP/2客户端到全局预建池（供后续HTTP/2请求复用）
						globalPrewarmMu.Lock()
						globalPrewarmedH2Clients[key] = h2c
						globalPrewarmMu.Unlock()
						countMu.Lock()
						successCount++
						countMu.Unlock()
						log.Printf("[prewarm] ✓ %s -> %s", d, a)
					} else if err != nil {
						// 连接失败
						countMu.Lock()
						failCount++
						countMu.Unlock()
					}
				}(domain, addr)
			}
		}

		wg.Wait()
		log.Printf("[prewarm] 连接预热完成：成功=%d 失败=%d 总计=%d", successCount, failCount, totalIPs)
	})
}

// isProtocolError 检查错误是否是协议错误（如HEAD请求返回数据）
// 这类错误不影响连接建立，可以安全忽略
// HTTP/2库会在内部记录这些错误，但我们可以识别并忽略它们
func isProtocolError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 匹配各种可能的协议错误格式
	return strings.Contains(errStr, "protocol error") &&
		strings.Contains(errStr, "received DATA on a HEAD request") ||
		strings.Contains(errStr, "http2: received DATA on a HEAD request") ||
		errStr == "protocol error: received DATA on a HEAD request"
}

// PrewarmSingleConnection 为单个IP预建连接（当发现新IP时调用）
// 用于在运行时动态预热新发现的IP，保持连接池始终是热的
func PrewarmSingleConnection(ctx context.Context, domain, address string) {
	dnsPool := GetGlobalDNSPool()
	if dnsPool == nil {
		return
	}

	key := domain + "|" + address

	// 检查是否已经预热过
	globalPrewarmMu.RLock()
	if _, ok := globalPrewarmedH2Clients[key]; ok {
		globalPrewarmMu.RUnlock()
		return // 已经预热过
	}
	globalPrewarmMu.RUnlock()

	// 创建临时客户端用于预热
	tempClient := &UTLSClient{
		config: &UTLSConfig{
			ClientType: rpc.TLSClientType_CHROME,
			Timeout:    30 * time.Second,
		},
		dns: dnsPool,
	}

	chid := tempClient.getClientHelloID()
	
	// 关键修复：使用HTTP/1.1来预热连接，避免HTTP/2的协议错误
	// HTTP/1.1对HEAD请求返回数据的容忍度更高，不会报协议错误
	// 预热的目标是建立连接，HTTP/1.1和HTTP/2的连接都可以被后续请求复用
	h1c := tempClient.buildHTTP1Client(domain, address, chid)
	h2c := tempClient.buildHTTP2Client(domain, address, chid) // 仍然创建h2客户端供后续使用

	// 解析IP地址
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return
	}

	// 使用HTTP/1.1发送HEAD请求建立连接（避免HTTP/2协议错误）
	prewarmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	req, _ := http.NewRequestWithContext(prewarmCtx, "HEAD", "https://"+host+":"+port, nil)
	req.Host = domain
	resp, err := h1c.Do(req)
	cancel()

	// 关闭响应体
	if resp != nil {
		_ = resp.Body.Close()
	}

	// 检查是否成功：只要连接建立（有响应）即可
	if resp != nil {
		// 连接建立成功，加入全局预建池（使用HTTP/2客户端，供后续HTTP/2请求复用）
		globalPrewarmMu.Lock()
		globalPrewarmedH2Clients[key] = h2c
		globalPrewarmMu.Unlock()
		log.Printf("[prewarm] ✓ 新IP预热成功: %s -> %s", domain, address)
	}
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
	// 无论目标是IPv4还是IPv6，都从本机IPv6池轮询一个源地址进行绑定
	// 这样可以充分利用系统的多个IPv6地址进行负载分散
	// 注意：不打印日志以避免高并发时的性能问题
	if src := NextIPv6LocalAddr(); src != nil {
		d.LocalAddr = &net.TCPAddr{IP: src}
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
// 注意：HTTP2 Transport的连接复用基于DialTLSContext的addr参数
// 我们使用实际的IP地址作为addr，确保同一IP的连接被复用
func (c *UTLSClient) buildHTTP2Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
	tr := &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			// 关键修复：使用实际的address（IP地址）而不是Transport传入的addr
			// 这样可以确保HTTP2 Transport基于实际连接的IP地址来判断连接复用
			// addr参数可能是hostname格式，但我们实际连接的是IP地址
			return c.dialUTLS(ctx, network, address, host, helloID, []string{"h2"})
		},
		ReadIdleTimeout:  30 * time.Second, // 连接空闲超时
		PingTimeout:      15 * time.Second, // Ping超时
		WriteByteTimeout: 10 * time.Second, // 写入超时
		MaxReadFrameSize: 1 << 20,          // 1MB最大帧大小
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
// 注意：HTTP2 Transport的连接复用是基于目标地址（IP:port）的
// 为了最大化连接复用，我们按 host+IP 的组合作为key，同一IP的连接会被复用
// 优先使用全局预建的连接池，如果没有则创建新连接并缓存
func (c *UTLSClient) getOrCreateH2Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
	key := host + "|" + address

	// 优先检查全局预建的连接池
	globalPrewarmMu.RLock()
	if prewarmed, ok := globalPrewarmedH2Clients[key]; ok && prewarmed != nil {
		globalPrewarmMu.RUnlock()
		// 同时缓存到当前实例，后续直接使用
		c.mu.Lock()
		c.h2Clients[key] = prewarmed
		c.mu.Unlock()
		return prewarmed
	}
	globalPrewarmMu.RUnlock()

	// 检查当前实例的缓存
	c.mu.Lock()
	defer c.mu.Unlock()
	if cli, ok := c.h2Clients[key]; ok && cli != nil {
		return cli
	}

	// 创建新客户端并缓存
	cli := c.buildHTTP2Client(host, address, helloID)
	c.h2Clients[key] = cli

	// 同时加入全局预建池（后续其他实例也可以复用）
	globalPrewarmMu.Lock()
	globalPrewarmedH2Clients[key] = cli
	globalPrewarmMu.Unlock()

	return cli
}

// 获取/创建可复用的 h1 客户端
// 注意：HTTP/1.1 Transport的连接复用也是基于目标地址的
// 为了最大化连接复用，我们按 host+IP 的组合作为key，同一IP的连接会被复用
func (c *UTLSClient) getOrCreateH1Client(host, address string, helloID *utls.ClientHelloID) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 使用 host+address 作为key，确保同一IP的连接被复用
	// 同时允许不同IP有各自的连接池
	key := host + "|" + address
	if cli, ok := c.h1Clients[key]; ok && cli != nil {
		return cli
	}
	cli := c.buildHTTP1Client(host, address, helloID)
	c.h1Clients[key] = cli
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

	// 无条件解析 A/AAAA 并将所有解析到的 IP 追加入 JSON（去重，由 ReportResult 保障）
	if c.dns != nil {
		resCtx, resCancel := context.WithTimeout(timeoutCtx, 500*time.Millisecond)
		defer resCancel()
		resolver := &net.Resolver{}
		if ips4, err := resolver.LookupIP(resCtx, "ip4", host); err == nil {
			for _, ip := range ips4 {
				_ = c.dns.ReportResult(host, ip.String(), 200)
			}
		}
		if ips6, err := resolver.LookupIP(resCtx, "ip6", host); err == nil {
			for _, ip := range ips6 {
				_ = c.dns.ReportResult(host, ip.String(), 200)
			}
		}
	}

	// 若启用DNS池，选择一个直连IP作为目标地址（保持 SNI/Host 为原域名）
	var selectedIP string
	var selectedIsV6 bool
	// 策略：优先白名单IP直连，偶尔用域名探测新IP（每10次有1次）
	useDomainProbe := false
	if c.dns != nil {
		probeCounter := time.Now().UnixNano() % 10
		useDomainProbe = (probeCounter == 0)
		
		if !useDomainProbe {
			// 优先使用白名单IP池直连（NextIP会合并whitelist到候选）
			if ip, isV6, e := c.dns.NextIP(timeoutCtx, host); e == nil && ip != "" {
				parsed, _ := url.Parse(req.Url)
				port := "443"
				if parsed != nil && parsed.Scheme == "http" { port = "80" }
				selectedIP, selectedIsV6, addr = ip, isV6, net.JoinHostPort(ip, port)
				log.Printf("[route] target=%s via_ip=%s ipv6=%v (whitelist-direct)", host, ip, isV6)
			}
		} else {
			// 探测模式：用域名访问，触发DNS解析以发现新IP
			log.Printf("[probe] target=%s via_domain (discovery)", host)
			// 异步触发深度探测，不阻塞请求
			go func() {
				ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel2()
				if rec, err := c.dns.resolveAndStore(ctx2, host); err == nil && rec != nil {
					log.Printf("[probe] discovered %d IPv4 + %d IPv6 for %s", len(rec.IPv4), len(rec.IPv6), host)
				}
			}()
			// 同时使用当前已知IP直连（如果可用），加速响应
			if ip, _, e := c.dns.NextIP(timeoutCtx, host); e == nil && ip != "" {
				parsed, _ := url.Parse(req.Url)
				port := "443"
				if parsed != nil && parsed.Scheme == "http" { port = "80" }
				selectedIP, selectedIsV6, addr = ip, false, net.JoinHostPort(ip, port)
				log.Printf("[route] target=%s via_ip=%s (probe-mode-with-ip)", host, ip)
			} else {
				addr = extractHostPort(req.Url)
			}
		}
		
		// 自动注册域名到ProbeManager进行定期探测
		if pm := GetGlobalProbeManager(); pm != nil {
			pm.RegisterDomain(host)
		}
	}
	
	// 双栈并行逻辑（仅在非探测模式且有多个候选时）
	if c.dns != nil && !useDomainProbe {
		v4s, v6s, _ := c.dns.GetIPs(timeoutCtx, host)
		if len(v4s) > 1 || len(v6s) > 1 {
			// 计算端口
			parsed, _ := url.Parse(req.Url)
			port := "443"
			if parsed != nil && parsed.Scheme == "http" {
				port = "80"
			}
			// 候选目标：使用NextIP轮询选择
			var addrV4, addrV6 string
			var ipV4, ipV6 string
			// 尝试获取IPv6
			if len(v6s) > 0 {
				if ip, isV6, e := c.dns.NextIP(timeoutCtx, host); e == nil && isV6 && ip != "" {
					ipV6, addrV6 = ip, net.JoinHostPort(ip, port)
				} else if len(v4s) == 0 {
					// 若IPv6失败且无IPv4，回退到GetIPs的第一个
					addrV6 = net.JoinHostPort(v6s[0], port)
					ipV6 = v6s[0]
				}
			}
			// 尝试获取IPv4
			if len(v4s) > 0 {
				if ip, isV6, e := c.dns.NextIP(timeoutCtx, host); e == nil && !isV6 && ip != "" {
					ipV4, addrV4 = ip, net.JoinHostPort(ip, port)
				} else if addrV6 == "" {
					// 若IPv4轮询失败且无IPv6备选，回退到GetIPs的第一个
					addrV4 = net.JoinHostPort(v4s[0], port)
					ipV4 = v4s[0]
				}
			}
			// 若同时存在两族，则并发；否则走单一路径
			if addrV4 != "" && addrV6 != "" {
				type result struct {
					resp *rpc.FetchResponse
					ip   string
					isV6 bool
					err  error
				}
				resCh := make(chan result, 2)
				// 复制请求以便并发使用独立ctx
				mkReq := func() (*http.Request, error) {
					r2, e2 := http.NewRequestWithContext(timeoutCtx, "GET", req.Url, nil)
					if e2 != nil {
						return nil, e2
					}
					if len(configPkg.AppConfig.Crawler.DefaultHeaders) > 0 {
						for k, v := range configPkg.AppConfig.Crawler.DefaultHeaders {
							if r2.Header.Get(k) == "" {
								r2.Header.Set(k, v)
							}
						}
					}
					for k, v := range req.Headers {
						r2.Header.Set(k, v)
					}
					r2.Header.Set("User-Agent", selectUserAgentByTLSClient(req.TlsClient))
					return r2, nil
				}
				chid := c.getClientHelloID()
				// 发起函数
				doOnce := func(targetAddr string, ip string, isV6 bool) {
					r2, e2 := mkReq()
					if e2 != nil {
						resCh <- result{nil, ip, isV6, e2}
						return
					}
					if ip != "" {
						r2.Host = host
					}
					// h2
					h2c := c.getOrCreateH2Client(host, targetAddr, chid)
					st := time.Now()
					if resp, e := h2c.Do(r2); e == nil {
						defer resp.Body.Close()
						body, _ := io.ReadAll(resp.Body)
						headers := map[string]string{}
						for k, v := range resp.Header {
							if len(v) > 0 {
								headers[k] = v[0]
							}
						}
						if c.dns != nil && ip != "" {
							_ = c.dns.ReportResult(host, ip, resp.StatusCode)
							_ = c.dns.ReportLatency(host, ip, time.Since(st).Milliseconds())
						}
						resCh <- result{&rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, ip, isV6, nil}
						return
					}
					// h1 回退
					h1c := c.getOrCreateH1Client(host, targetAddr, chid)
					st = time.Now()
					if resp, e := h1c.Do(r2); e == nil {
						defer resp.Body.Close()
						body, _ := io.ReadAll(resp.Body)
						headers := map[string]string{}
						for k, v := range resp.Header {
							if len(v) > 0 {
								headers[k] = v[0]
							}
						}
						if c.dns != nil && ip != "" {
							_ = c.dns.ReportResult(host, ip, resp.StatusCode)
							_ = c.dns.ReportLatency(host, ip, time.Since(st).Milliseconds())
						}
						resCh <- result{&rpc.FetchResponse{Url: req.Url, StatusCode: int32(resp.StatusCode), Headers: headers, Body: body}, ip, isV6, nil}
						return
					}
					resCh <- result{nil, ip, isV6, fmt.Errorf("both h2/h1 failed")}
				}
				go doOnce(addrV6, ipV6, true)
				go doOnce(addrV4, ipV4, false)
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
			if addrV6 != "" {
				selectedIP, selectedIsV6, addr = ipV6, true, addrV6
			}
			if addrV4 != "" {
				selectedIP, selectedIsV6, addr = ipV4, false, addrV4
			}
		} else if ip, isV6, e := c.dns.NextIP(timeoutCtx, host); e == nil && ip != "" {
			selectedIP = ip
			selectedIsV6 = isV6
			parsed, _ := url.Parse(req.Url)
			port := "443"
			if parsed != nil && parsed.Scheme == "http" {
				port = "80"
			}
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
        if selectedIP != "" {
            headers["X-VPS-IP"] = selectedIP
            headers["X-VPS-LatencyMs"] = fmt.Sprintf("%d", time.Since(start).Milliseconds())
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
        if selectedIP != "" {
            headers["X-VPS-IP"] = selectedIP
            headers["X-VPS-LatencyMs"] = fmt.Sprintf("%d", time.Since(start).Milliseconds())
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
        if selectedIP != "" {
            headers["X-VPS-IP"] = selectedIP
            headers["X-VPS-LatencyMs"] = fmt.Sprintf("%d", time.Since(start).Milliseconds())
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
