package main

import (
    "os"
    "os/signal"
    "syscall"

    "vps-rpc/client"
    "vps-rpc/config"
    "vps-rpc/logger"
    "vps-rpc/proxy"
    "vps-rpc/server"
    "vps-rpc/systemcheck"
)

// main 程序入口函数
// 负责初始化配置、启动服务器、处理系统信号等核心功能
func main() {
	// 加载配置文件
	// 从config.toml文件中读取所有配置信息
	if err := config.LoadConfig("config.toml"); err != nil {
		// 如果加载配置失败，则记录错误日志并终止程序
		panic("加载配置失败: " + err.Error())
	}

	// 创建日志器
	log := logger.NewLoggerFromConfig(
		config.AppConfig.Logging.Level,
		config.AppConfig.Logging.Format,
	)

	// 记录程序启动日志（包含版本信息）
	version := config.AppConfig.GetAdminVersion()
	log.Infof("正在启动 VPS-RPC 爬虫代理服务器... 版本: %s", version)

	// 创建统计收集器
	stats := server.NewStatsCollector()

	// 初始化全局 DNS 池与 ProbeManager
	var probe *proxy.ProbeManager
	var dnsPool *proxy.DNSPool
	if config.AppConfig.DNS.Enabled && config.AppConfig.DNS.DBPath != "" {
		if p, err := proxy.NewDNSPool(config.AppConfig.DNS.DBPath); err == nil {
			dnsPool = p
			proxy.SetGlobalDNSPool(p)
			probe = proxy.NewProbeManager(p)
			proxy.SetGlobalProbeManager(probe) // 设置全局ProbeManager
			defer probe.Close()
		}
	}

    // 系统预检 + 自动调优（按资源自适应，非root仅做进程级优化）
    {
        systemcheck.ApplyAutoTuning()
        _ = systemcheck.RunPreflightChecks()
    }

	// 创建爬虫服务实例（传入统计收集器）
	crawlerServer := server.NewCrawlerServer(stats)

	// 使用QUIC协议（最快速度）
	grpcServer, err := server.NewQuicRpcServer(crawlerServer)
	if err != nil {
		log.Fatalf("创建 QUIC 服务器失败: %v", err)
	}

	// 注册服务（QUIC模式下已在NewQuicRpcServer传入，此处保留用于兼容）
	grpcServer.RegisterService(crawlerServer)

	// 初始化并注册PeerService服务器（用于节点间DNS池共享）
	var peerSync *client.PeerSyncManager
	if dnsPool != nil && len(config.AppConfig.Peer.Seeds) > 0 {
		peerServer := server.NewPeerServiceServer(dnsPool)
		grpcServer.SetPeerServer(peerServer)
		log.Info("PeerService已启用，支持节点间DNS池共享")
		
		// 启动PeerSyncManager客户端，定期与seeds同步DNS记录
		peerSync = client.NewPeerSyncManager()
		peerSync.Start()
		defer peerSync.Stop()
		log.Info("PeerSyncManager已启动，定期与种子节点同步")
	}

	// 注意：QUIC模式下暂不支持管理服务（AdminServer）
	// 如需管理功能，可单独启动一个gRPC over TCP的管理端口

	// 启动服务（在独立的goroutine中运行）
	// Serve方法会持续监听并处理客户端连接
	go func() {
		// 如果服务启动过程中出现错误，则记录错误日志并终止程序
		if err := grpcServer.Serve(); err != nil {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	// 记录服务器启动成功的日志，包含监听地址信息
	// 版本号从配置中读取
	log.WithFields(map[string]interface{}{
		"address": config.AppConfig.GetServerAddr(),
		"version": config.AppConfig.GetAdminVersion(),
	}).Info("VPS-RPC 爬虫代理服务器已启动")

	// 创建系统信号通道，用于接收中断信号
	// 用于优雅地关闭服务器
	sigChan := make(chan os.Signal, 1)
	// 注册要监听的系统信号：SIGINT（Ctrl+C）和SIGTERM（终止信号）
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	// 阻塞等待信号
	<-sigChan

	// 收到中断信号后，记录关闭服务器的日志
	log.Info("正在关闭服务器...")

	// 关闭服务器并处理可能的错误
	if err := grpcServer.Close(); err != nil {
		// 如果关闭过程中出现错误，则记录错误日志
		log.WithField("error", err.Error()).Error("关闭服务器时出错")
	}

	// 记录服务器已停止的日志
	log.Info("服务器已停止")
}
