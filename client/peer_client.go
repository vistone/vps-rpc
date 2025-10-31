package client

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"vps-rpc/config"
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

	var resp rpc.ExchangeDNSResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("反序列化响应失败: %w", err)
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

	var resp rpc.GetPeersResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("反序列化响应失败: %w", err)
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
	mu           sync.RWMutex
	wg           sync.WaitGroup
	closed       chan struct{}
}

// NewPeerSyncManager 创建peer同步管理器
func NewPeerSyncManager() *PeerSyncManager {
	return &PeerSyncManager{
		knownPeers:  make(map[string]bool),
		peerClients: make(map[string]*PeerClient),
		closed:      make(chan struct{}),
	}
}

// Start 启动peer同步管理器
func (p *PeerSyncManager) Start() {
	log.Printf("[peer-sync] 启动peer同步管理器")

	// 启动定期同步任务
	p.wg.Add(1)
	go p.periodicSync()

	log.Printf("[peer-sync] peer同步管理器已启动")
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

	ticker := time.NewTicker(5 * time.Minute) // 每5分钟同步一次
	defer ticker.Stop()

	// 首次同步
	p.syncWithSeeds()

	for {
		select {
		case <-p.closed:
			return
		case <-ticker.C:
			p.syncWithSeeds()
		}
	}
}

// syncWithSeeds 与种子节点同步
func (p *PeerSyncManager) syncWithSeeds() {
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
	// 获取或创建peer客户端
	p.mu.RLock()
	client, exists := p.peerClients[peerAddr]
	p.mu.RUnlock()

	if !exists {
		// 创建新客户端
		c, err := NewPeerClient(peerAddr, true) // TODO: 使用配置的TLS设置
		if err != nil {
			log.Printf("[peer-sync] 连接peer失败 %s: %v", peerAddr, err)
			return
		}
		client = c
		p.mu.Lock()
		p.peerClients[peerAddr] = client
		p.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

    // 获取已知peers列表
	peers, err := client.GetPeers(ctx)
	if err != nil {
		log.Printf("[peer-sync] 获取peers失败 %s: %v", peerAddr, err)
		return
	}
    // 自动打印本次从该节点获取到的peers结果
    log.Printf("[peer-sync] 来自 %s 的已知节点: %v", peerAddr, peers)

	// 添加新的peers到已知列表
	p.mu.Lock()
	for _, peer := range peers {
		if peer != peerAddr && !p.knownPeers[peer] {
			p.knownPeers[peer] = true
			log.Printf("[peer-sync] 发现新节点: %s", peer)
		}
	}
	p.mu.Unlock()

    // 自动打印当前聚合后的已知节点数量，便于观察是否互相发现
    p.mu.RLock()
    total := len(p.knownPeers)
    p.mu.RUnlock()

    // TODO: 实际DNS记录交换逻辑
    log.Printf("[peer-sync] 与 %s 同步完成，本次返回=%d，当前已知节点总数=%d", peerAddr, len(peers), total)
}

