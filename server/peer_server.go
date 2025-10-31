package server

import (
	"context"
	"log"

	"vps-rpc/config"
	"vps-rpc/proxy"
	"vps-rpc/rpc"
)

// PeerServiceServer peer服务实现
type PeerServiceServer struct {
	rpc.UnimplementedPeerServiceServer
	// dnsPool DNS池，用于获取和更新DNS记录
	dnsPool *proxy.DNSPool
	// knownPeers 已知的peer节点列表
	knownPeers map[string]bool
}

// NewPeerServiceServer 创建新的PeerService服务器实例
func NewPeerServiceServer(dnsPool *proxy.DNSPool) *PeerServiceServer {
	return &PeerServiceServer{
		dnsPool:    dnsPool,
		knownPeers: make(map[string]bool),
	}
}

// AddKnownPeer 将地址加入已知节点（去重）
func (s *PeerServiceServer) AddKnownPeer(address string) {
    if address == "" {
        return
    }
    if s.knownPeers == nil {
        s.knownPeers = make(map[string]bool)
    }
    if !s.knownPeers[address] {
        s.knownPeers[address] = true
        log.Printf("[peer] 学到对端节点: %s", address)
    }
}

// ExchangeDNS 交换DNS记录（共享观测报告）
func (s *PeerServiceServer) ExchangeDNS(ctx context.Context, req *rpc.ExchangeDNSRequest) (*rpc.ExchangeDNSResponse, error) {
	if s.dnsPool == nil {
		return &rpc.ExchangeDNSResponse{Records: make(map[string]*rpc.DNSRecord)}, nil
	}

	// 从本地DNS池获取所有记录并转换为proto格式
	localRecords := make(map[string]*rpc.DNSRecord)
	
	// 获取所有域名的DNS记录
	allRecords := s.dnsPool.GetAllRecords(ctx)
	for domain, record := range allRecords {
		dnsRec := &rpc.DNSRecord{
			Ipv4: record.IPv4,
			Ipv6: record.IPv6,
		}
		// 注意：不共享黑白名单，只准备共享观测报告（后续实现）
		localRecords[domain] = dnsRec
	}

	// 处理远程节点发送过来的DNS记录
	// 注意：这里应该合并远程记录到本地，但为了安全性，只处理观测报告
	// 黑白名单保持本地独立管理
	if req.Records != nil {
		log.Printf("[peer] 收到 %d 个远程DNS记录", len(req.Records))
		// TODO: 合并远程记录到本地DNS池
	}

	return &rpc.ExchangeDNSResponse{Records: localRecords}, nil
}

// GetPeers 获取已知对等节点列表
func (s *PeerServiceServer) GetPeers(ctx context.Context, req *rpc.GetPeersRequest) (*rpc.GetPeersResponse, error) {
    // 返回去重后的种子+已知节点全集
    uniq := map[string]bool{}
    // include self if configured
    if sa := config.AppConfig.Peer.MyAddr; sa != "" {
        uniq[sa] = true
        s.knownPeers[sa] = true
    }
    // seeds
    for _, seed := range config.AppConfig.Peer.Seeds {
        if seed == "" { continue }
        uniq[seed] = true
        s.knownPeers[seed] = true
    }
    // known
    for p := range s.knownPeers {
        if p == "" { continue }
        uniq[p] = true
    }
    out := make([]string, 0, len(uniq))
    for p := range uniq { out = append(out, p) }
    return &rpc.GetPeersResponse{Peers: out}, nil
}

// ReportNode 上报节点信息（用于发现新节点）
func (s *PeerServiceServer) ReportNode(ctx context.Context, req *rpc.ReportNodeRequest) (*rpc.ReportNodeResponse, error) {
	if req.Address == "" {
		return &rpc.ReportNodeResponse{
			Accepted: false,
			Message:  "地址不能为空",
		}, nil
	}

	// 添加到已知peer列表
	if !s.knownPeers[req.Address] {
		s.knownPeers[req.Address] = true
		log.Printf("[peer] 发现新节点: %s", req.Address)
		return &rpc.ReportNodeResponse{
			Accepted: true,
			Message:  "节点已添加",
		}, nil
	}

	return &rpc.ReportNodeResponse{
		Accepted: true,
		Message:  "节点已存在",
	}, nil
}

