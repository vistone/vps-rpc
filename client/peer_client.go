package client

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
    "net"
    "strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"vps-rpc/config"
    "vps-rpc/proxy"
	"vps-rpc/rpc"
)

// PeerClient PeerService客户端，负责节点间通信
type PeerClient struct {
	quicClient *QuicClient
	address    string
	mu         sync.RWMutex
	closed     bool
}

// NewPeerClient 创建新的PeerService客户端
func NewPeerClient(address string, insecureSkipVerify bool) (*PeerClient, error) {
	quicClient, err := NewQuicClient(address, insecureSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("创建QUIC客户端失败: %w", err)
	}

	return &PeerClient{
		quicClient: quicClient,
		address:    address,
	}, nil
}

// ExchangeDNS 交换DNS记录
func (c *PeerClient) ExchangeDNS(ctx context.Context, req *rpc.ExchangeDNSRequest) (*rpc.ExchangeDNSResponse, error) {
	stream, err := c.quicClient.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("打开流失败: %w", err)
	}
	defer stream.Close()

	reqData, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 关键修复：如果序列化后为空（len=0），添加一个标记字节来区分 ExchangeDNSRequest
	// 这样服务器端可以通过检查消息长度和第一个字节来正确识别
	// 对于空的 ExchangeDNSRequest，我们在前面添加 0x01 标记
	if len(reqData) == 0 {
		// 空的 ExchangeDNSRequest：添加类型标记 0x01
		reqData = []byte{0x01} // 类型标记：0x01 = ExchangeDNSRequest
		log.Printf("[peer-client] ExchangeDNS请求为空，添加类型标记 0x01")
	}

	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	request := make([]byte, 4+len(reqData))
	copy(request[:4], length[:])
	copy(request[4:], reqData)

	if _, err := stream.Write(request); err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	if _, err := stream.Read(length[:]); err != nil {
		return nil, fmt.Errorf("读取响应长度失败: %w", err)
	}

	respLen := binary.BigEndian.Uint32(length[:])
	if respLen > 10*1024*1024 {
		return nil, fmt.Errorf("响应过大: %d", respLen)
	}

	respData := make([]byte, respLen)
	if _, err := io.ReadFull(stream, respData); err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 调试：记录响应数据的前几个字节，帮助诊断问题
	if len(respData) > 0 {
		hexPrefix := ""
		maxBytes := 16
		if len(respData) < maxBytes {
			maxBytes = len(respData)
		}
		for i := 0; i < maxBytes; i++ {
			hexPrefix += fmt.Sprintf("%02x ", respData[i])
		}
		log.Printf("[peer-client] ExchangeDNS响应: len=%d, 前%d字节(hex)=%s", len(respData), maxBytes, hexPrefix)
	} else {
		log.Printf("[peer-client] ExchangeDNS响应: 空响应")
	}

	var resp rpc.ExchangeDNSResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		// 尝试诊断：可能是返回了错误的消息类型
		var getPeersResp rpc.GetPeersResponse
		if err2 := proto.Unmarshal(respData, &getPeersResp); err2 == nil {
			log.Printf("[peer-client] 错误：服务器返回了 GetPeersResponse 而不是 ExchangeDNSResponse (peers=%d)", len(getPeersResp.Peers))
			return nil, fmt.Errorf("服务器返回了错误的消息类型（GetPeersResponse而非ExchangeDNSResponse）")
		}
		return nil, fmt.Errorf("反序列化响应失败: %w (响应长度=%d)", err, len(respData))
	}

	return &resp, nil
}

// GetPeers 获取对等节点列表
func (c *PeerClient) GetPeers(ctx context.Context) ([]string, error) {
	req := &rpc.GetPeersRequest{}
	
	stream, err := c.quicClient.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("打开流失败: %w", err)
	}
	defer stream.Close()

	reqData, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 关键修复：如果序列化后为空（len=0），添加类型标记 0x02 来区分 GetPeersRequest
	// 这样服务器端可以通过检查消息长度和第一个字节来正确识别
	// 对于空的 GetPeersRequest，我们在前面添加 0x02 标记
	if len(reqData) == 0 {
		// 空的 GetPeersRequest：添加类型标记 0x02
		reqData = []byte{0x02} // 类型标记：0x02 = GetPeersRequest
		log.Printf("[peer-client] GetPeers请求为空，添加类型标记 0x02")
	}

	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	request := make([]byte, 4+len(reqData))
	copy(request[:4], length[:])
	copy(request[4:], reqData)

	if _, err := stream.Write(request); err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	if _, err := stream.Read(length[:]); err != nil {
		return nil, fmt.Errorf("读取响应长度失败: %w", err)
	}

	respLen := binary.BigEndian.Uint32(length[:])
	if respLen > 10*1024*1024 {
		return nil, fmt.Errorf("响应过大: %d", respLen)
	}

	respData := make([]byte, respLen)
	if _, err := io.ReadFull(stream, respData); err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 调试：记录响应数据的前几个字节，帮助诊断问题
	if len(respData) > 0 {
		hexPrefix := ""
		maxBytes := 16
		if len(respData) < maxBytes {
			maxBytes = len(respData)
		}
		for i := 0; i < maxBytes; i++ {
			hexPrefix += fmt.Sprintf("%02x ", respData[i])
		}
		log.Printf("[peer-client] GetPeers响应: len=%d, 前%d字节(hex)=%s", len(respData), maxBytes, hexPrefix)
	} else {
		log.Printf("[peer-client] GetPeers响应: 空响应")
	}

	var resp rpc.GetPeersResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		// 尝试诊断：可能是返回了错误的消息类型
		var exchangeDNSResp rpc.ExchangeDNSResponse
		if err2 := proto.Unmarshal(respData, &exchangeDNSResp); err2 == nil {
			log.Printf("[peer-client] 错误：服务器返回了 ExchangeDNSResponse 而不是 GetPeersResponse (records=%d)", len(exchangeDNSResp.Records))
			return nil, fmt.Errorf("服务器返回了错误的消息类型（ExchangeDNSResponse而非GetPeersResponse）")
		}
		return nil, fmt.Errorf("反序列化响应失败: %w (响应长度=%d)", err, len(respData))
	}

	return resp.Peers, nil
}

// ReportNode 上报节点信息
func (c *PeerClient) ReportNode(ctx context.Context, address string) error {
	req := &rpc.ReportNodeRequest{Address: address}
	
	stream, err := c.quicClient.conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("打开流失败: %w", err)
	}
	defer stream.Close()

	reqData, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	request := make([]byte, 4+len(reqData))
	copy(request[:4], length[:])
	copy(request[4:], reqData)

	if _, err := stream.Write(request); err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}

	if _, err := stream.Read(length[:]); err != nil {
		return fmt.Errorf("读取响应长度失败: %w", err)
	}

	respLen := binary.BigEndian.Uint32(length[:])
	if respLen > 10*1024*1024 {
		return fmt.Errorf("响应过大: %d", respLen)
	}

	respData := make([]byte, respLen)
	if _, err := io.ReadFull(stream, respData); err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	var resp rpc.ReportNodeResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("反序列化响应失败: %w", err)
	}

	if !resp.Accepted {
		return fmt.Errorf("节点上报被拒绝: %s", resp.Message)
	}

	return nil
}

// Close 关闭客户端
func (c *PeerClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.quicClient != nil {
		return c.quicClient.Close()
	}
	return nil
}

// PeerSyncManager 管理peer同步的组件
type PeerSyncManager struct {
	dnsPool      interface{} // 避免循环依赖，使用interface{}
	knownPeers   map[string]bool
	peerClients  map[string]*PeerClient
    lastSeen     map[string]time.Time
    failCount    map[string]int
    syncing      map[string]bool
    selfIPs      map[string]bool   // 本机所有接口IP（字符串）
    selfAddrs    map[string]bool   // 本机可视为自身的 host:port 列表
    serverPort   int               // 本机服务端口
    nextRetryAt  map[string]time.Time // 下次允许尝试时间（退避）
    backoffSec   map[string]int       // 当前退避秒数（指数增长，上限300）
	mu           sync.RWMutex
	wg           sync.WaitGroup
	closed       chan struct{}
}

// NewPeerSyncManager 创建peer同步管理器
func NewPeerSyncManager() *PeerSyncManager {
    p := &PeerSyncManager{
		knownPeers:  make(map[string]bool),
		peerClients: make(map[string]*PeerClient),
        lastSeen:    make(map[string]time.Time),
        failCount:   make(map[string]int),
        syncing:     make(map[string]bool),
        selfIPs:     make(map[string]bool),
        selfAddrs:   make(map[string]bool),
        serverPort:  config.AppConfig.Server.Port,
        nextRetryAt: make(map[string]time.Time),
        backoffSec:  make(map[string]int),
        closed:      make(chan struct{}),
    }
    // 采集本机接口IP
    if addrs, err := net.InterfaceAddrs(); err == nil {
        for _, a := range addrs {
            var ip net.IP
            switch v := a.(type) {
            case *net.IPNet:
                ip = v.IP
            case *net.IPAddr:
                ip = v.IP
            }
            if ip == nil {
                continue
            }
            // 规范化为IPv4（若可），否则保留原始IPv6/非IPv4地址
            orig := ip
            if v4 := ip.To4(); v4 != nil {
                ip = v4
            } else {
                ip = orig
            }
            if ip != nil {
                p.selfIPs[ip.String()] = true
            }
            // 生成 host:port 形式
            host := ip.String()
            if strings.Contains(host, ":") {
                host = "[" + host + "]"
            }
            p.selfAddrs[net.JoinHostPort(host, fmt.Sprintf("%d", p.serverPort))] = true
        }
    }
    // 如配置了 my_addr，也视为自身
    if my := config.AppConfig.Peer.MyAddr; my != "" {
        p.selfAddrs[my] = true
        h, _, err := net.SplitHostPort(my)
        if err == nil {
            if ips, err2 := net.LookupIP(h); err2 == nil {
                for _, ip := range ips {
                    p.selfIPs[ip.String()] = true
                }
            }
        }
    }
    return p
}

// Start 启动peer同步管理器
func (p *PeerSyncManager) Start() {
	log.Printf("[peer-sync] 启动peer同步管理器")

    // 从本地持久化恢复已知peers（即使seed下线也能继续发现）
    for _, addr := range LoadKnownPeers() {
        if addr != "" {
            p.knownPeers[addr] = true
        }
    }

    // 启动定期同步任务
	p.wg.Add(1)
	go p.periodicSync()

	log.Printf("[peer-sync] peer同步管理器已启动")

    // 中心模式：启动心跳（周期性与中心交互）
    if addr := config.AppConfig.Center.Address; addr != "" {
        hb := 2 * time.Second
        if s := config.AppConfig.Center.HeartbeatInterval; s != "" {
            if d, err := time.ParseDuration(s); err == nil && d > 0 { hb = d }
        }
        log.Printf("[center] 已启用中心模式 address=%s heartbeat_interval=%s", addr, hb.String())
        p.wg.Add(1)
        go func(){
            defer p.wg.Done()
            ticker := time.NewTicker(hb)
            defer ticker.Stop()
            for {
                select {
                case <-p.closed:
                    return
                case <-ticker.C:
                    p.syncWithPeer(addr)
                }
            }
        }()
    }
}

// Stop 停止peer同步管理器
func (p *PeerSyncManager) Stop() {
	log.Printf("[peer-sync] 停止peer同步管理器")
	close(p.closed)
	p.wg.Wait()

	// 关闭所有peer客户端连接
	p.mu.Lock()
	for addr, client := range p.peerClients {
		if client != nil {
			client.Close()
		}
		delete(p.peerClients, addr)
	}
	p.mu.Unlock()

	log.Printf("[peer-sync] peer同步管理器已停止")
}

// periodicSync 定期同步DNS记录
func (p *PeerSyncManager) periodicSync() {
	defer p.wg.Done()

    // 从配置读取同步周期，默认5s
    interval := 5 * time.Second
    if s := config.AppConfig.Peer.SyncInterval; s != "" {
        if d, err := time.ParseDuration(s); err == nil && d > 0 {
            interval = d
        }
    }
    ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 首次同步
    p.syncAll()

	for {
		select {
		case <-p.closed:
			return
		case <-ticker.C:
            p.syncAll()
		}
	}
}

// syncAll 同步 seeds 与 当前已知的所有 peers（去中心化扩散）
func (p *PeerSyncManager) syncAll() {
    p.syncWithSeeds()
    // 同步已知peers
    p.mu.RLock()
    peers := make([]string, 0, len(p.knownPeers))
    for addr := range p.knownPeers {
        peers = append(peers, addr)
    }
    p.mu.RUnlock()
    for _, addr := range peers {
        go p.syncWithPeer(addr)
    }
}

// syncWithSeeds 与种子节点同步
func (p *PeerSyncManager) syncWithSeeds() {
    if addr := config.AppConfig.Center.Address; addr != "" {
        go p.syncWithPeer(addr)
        return
    }
    seeds := config.AppConfig.Peer.Seeds
	if len(seeds) == 0 {
		return
	}

	for _, seedAddr := range seeds {
		go p.syncWithPeer(seedAddr)
	}
}

// syncWithPeer 与指定peer同步
func (p *PeerSyncManager) syncWithPeer(peerAddr string) {
    // 跳过自身地址（避免自连）
    if p.isSelfPeer(peerAddr) {
        return
    }
    // 退避：未到允许时间则跳过
    p.mu.RLock()
    nr := p.nextRetryAt[peerAddr]
    p.mu.RUnlock()
    if !nr.IsZero() && time.Now().Before(nr) {
        left := time.Until(nr).Truncate(time.Second)
        log.Printf("[peer-sync] 跳过 %s：退避中，剩余 %v", peerAddr, left)
        return
    }
    // 避免同一peer的同步重入（周期短导致叠加）
    p.mu.Lock()
    if p.syncing[peerAddr] {
        p.mu.Unlock()
        return
    }
    p.syncing[peerAddr] = true
    p.mu.Unlock()
    defer func(){
        p.mu.Lock()
        delete(p.syncing, peerAddr)
        p.mu.Unlock()
    }()
	// 获取或创建peer客户端
	p.mu.RLock()
	client, exists := p.peerClients[peerAddr]
	p.mu.RUnlock()

    if !exists {
		// 检查是否是纯IPv6地址且设备不支持IPv6路由
		host, _, errSplit := net.SplitHostPort(peerAddr)
		if errSplit == nil {
			// 移除IPv6地址的方括号
			host = strings.Trim(host, "[]")
			if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
				// 这是纯IPv6地址，检查设备是否支持IPv6
				if !proxy.HasIPv6() {
					// 设备不支持IPv6，跳过此peer
					log.Printf("[peer-sync] 跳过纯IPv6地址 %s（设备不支持IPv6路由）", peerAddr)
					// 标记为失败，使用较长的退避时间
					p.mu.Lock()
					p.failCount[peerAddr] = 100 // 标记为高失败次数
					p.nextRetryAt[peerAddr] = time.Now().Add(1 * time.Hour) // 1小时后重试
					p.mu.Unlock()
					return
				}
			}
		}
		
		// 创建新客户端
		c, err := NewPeerClient(peerAddr, true) // TODO: 使用配置的TLS设置
		if err != nil {
			// 检查是否是IPv6路由错误，如果是且是纯IPv6地址，标记为不可用
			errStr := err.Error()
			if strings.Contains(errStr, "network is unreachable") || 
			   strings.Contains(errStr, "no route to host") {
				host, _, errSplit := net.SplitHostPort(peerAddr)
				if errSplit == nil {
					host = strings.Trim(host, "[]")
					if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
						// 纯IPv6地址且路由不可达，标记为不可用
						log.Printf("[peer-sync] 纯IPv6地址 %s 路由不可达，标记为不可用", peerAddr)
						p.mu.Lock()
						p.failCount[peerAddr] = 100
						p.nextRetryAt[peerAddr] = time.Now().Add(1 * time.Hour)
						p.mu.Unlock()
						return
					}
				}
			}
			log.Printf("[peer-sync] 连接peer失败 %s: %v", peerAddr, err)
			return
		}
		client = c
		p.mu.Lock()
		p.peerClients[peerAddr] = client
		p.mu.Unlock()
	}
    log.Printf("[peer-sync] 尝试与 %s 同步", peerAddr)

    // 缩短超时，避免周期重入造成堆积
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

    // 获取已知peers列表
    peers, err := client.GetPeers(ctx)
    if err != nil {
        if isTimeoutErr(err) {
            // 统计失败次数并判定下线 + 退避
            p.mu.Lock()
            p.failCount[peerAddr]++
            fc := p.failCount[peerAddr]
            // 指数退避（秒），上限300s，带10%抖动
            b := p.backoffSec[peerAddr]
            if b <= 0 { b = 1 } else { b = b * 2 }
            if b > 300 { b = 300 }
            p.backoffSec[peerAddr] = b
            jitter := time.Duration(b) * time.Second / 10
            next := time.Now().Add(time.Duration(b)*time.Second + time.Duration(int64(jitter)-(time.Now().UnixNano()%int64(2*jitter))))
            p.nextRetryAt[peerAddr] = next
            p.mu.Unlock()
            if fc%10 == 0 { // 每10次打印一次摘要
                log.Printf("[peer-sync] 节点疑似离线: %s (连续超时=%d, 退避=%ds)", peerAddr, fc, b)
            }
        } else {
            log.Printf("[peer-sync] 获取peers失败 %s: %v", peerAddr, err)
        }
        return
    }
    // 成功，重置失败计数并更新最后活跃
    p.mu.Lock()
    p.failCount[peerAddr] = 0
    p.lastSeen[peerAddr] = time.Now()
    p.backoffSec[peerAddr] = 0
    p.nextRetryAt[peerAddr] = time.Time{}
    p.mu.Unlock()
    log.Printf("[peer-sync] 与 %s 连接正常，已恢复心跳", peerAddr)
    // 自动打印本次从该节点获取到的peers结果
    log.Printf("[peer-sync] 来自 %s 的已知节点: %v", peerAddr, peers)

    // 自广播：上报本节点地址；当未配置时发送空字符串，由服务端基于连接远端地址自动推断
    {
        my := config.AppConfig.Peer.MyAddr // 可为空
        ctxR, cancelR := context.WithTimeout(context.Background(), 2*time.Second)
        _ = client.ReportNode(ctxR, my)
        cancelR()
    }

    // 添加新的peers到已知列表（并立即触发与新节点的同步）
	p.mu.Lock()
	for _, peer := range peers {
		if peer != peerAddr && !p.knownPeers[peer] {
			p.knownPeers[peer] = true
            SavePeer(peer)
			log.Printf("[peer-sync] 发现新节点: %s", peer)
            // 立即与新学到的节点进行一次同步，不等周期
            go p.syncWithPeer(peer)
		}
	}
	p.mu.Unlock()

    // 自动打印当前聚合后的已知节点数量，便于观察是否互相发现
    p.mu.RLock()
    total := len(p.knownPeers)
    p.mu.RUnlock()

    // 与对端交换DNS记录并合并入本地数据库
    if pool := proxy.GetGlobalDNSPool(); pool != nil {
        // 构造本地记录：发送所有记录（包括IPv4/IPv6），不仅仅白名单
        // 即使本地为空，也要发送请求以获取中心节点的DNS记录
        local := map[string]*rpc.DNSRecord{}
        ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
        all := pool.GetAllRecords(ctx2)
        cancel2()
        log.Printf("[peer-sync] 准备交换DNS记录，本地记录数=%d", len(all))
        for domain, r := range all {
            // 发送所有IPv4和IPv6记录（不仅仅是白名单），让中心节点知道我们有哪些IP
            if len(r.IPv4) > 0 || len(r.IPv6) > 0 {
                local[domain] = &rpc.DNSRecord{
                    Ipv4: append([]string(nil), r.IPv4...), // 发送所有IPv4
                    Ipv6: append([]string(nil), r.IPv6...), // 发送所有IPv6
                }
            }
        }
        log.Printf("[peer-sync] 构造的本地DNS记录数=%d（将发送到 %s）", len(local), peerAddr)
        // 无论本地是否有记录，都发送ExchangeDNS请求，以获取中心节点的DNS记录
        // 这样即使本地为空，也能从中心节点获取DNS记录
        ctx3, cancel3 := context.WithTimeout(context.Background(), 3*time.Second)
        defer cancel3()
        resp, err := client.ExchangeDNS(ctx3, &rpc.ExchangeDNSRequest{Records: local})
        if err != nil {
            if isTimeoutErr(err) {
                log.Printf("[peer-sync] ExchangeDNS超时 %s: %v", peerAddr, err)
            } else {
                log.Printf("[peer-sync] ExchangeDNS失败 %s: %v", peerAddr, err)
            }
        } else {
            if len(resp.Records) > 0 {
                log.Printf("[peer-sync] 收到来自 %s 的 %d 个域的DNS记录", peerAddr, len(resp.Records))
                if err := pool.MergeFromPeer(resp.Records); err != nil {
                    log.Printf("[peer-sync] 合并对端DNS记录失败 %s: %v", peerAddr, err)
                } else {
                    log.Printf("[peer-sync] ✅ 成功合并来自 %s 的 %d 个域的DNS记录到本地", peerAddr, len(resp.Records))
                    // 合并后立即触发持久化，确保写入文件
                    if err := pool.PersistNow(); err != nil {
                        log.Printf("[peer-sync] 警告：合并后持久化失败: %v", err)
                    }
                }
            } else {
                log.Printf("[peer-sync] 来自 %s 的DNS记录为空（对方可能也没有记录）", peerAddr)
            }
        }
    } else {
        log.Printf("[peer-sync] 警告：DNS池未初始化，无法交换DNS记录")
    }

    log.Printf("[peer-sync] 与 %s 同步完成，本次返回=%d，当前已知节点总数=%d", peerAddr, len(peers), total)
}

// isSelfPeer 判断目标地址是否为本机（解析IP后与本机接口比对，且端口等于本端口）
func (p *PeerSyncManager) isSelfPeer(addr string) bool {
    if addr == "" {
        return true
    }
    if p.selfAddrs[addr] {
        return true
    }
    host, port, err := net.SplitHostPort(addr)
    if err != nil {
        return false
    }
    if port != fmt.Sprintf("%d", p.serverPort) {
        return false
    }
    // 解析host对应的IP
    ips, err := net.LookupIP(host)
    if err != nil || len(ips) == 0 {
        return false
    }
    for _, ip := range ips {
        if p.selfIPs[ip.String()] {
            return true
        }
    }
    return false
}

// isTimeoutErr 判断是否为空闲/超时类错误
func isTimeoutErr(err error) bool {
    if err == nil { return false }
    s := err.Error()
    return strings.Contains(s, "timeout: no recent network activity") ||
        strings.Contains(s, "i/o timeout") ||
        strings.Contains(s, "deadline exceeded")
}

