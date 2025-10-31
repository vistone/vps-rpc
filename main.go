package main

import (
	"os"
	"os/signal"
	"syscall"

	"vps-rpc/config"
	"vps-rpc/logger"
	"vps-rpc/proxy"
	"vps-rpc/server"
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

	// 记录程序启动日志
	log.Info("正在启动 VPS-RPC 爬虫代理服务器...")

	// 创建统计收集器
	stats := server.NewStatsCollector()

	// 初始化全局 DNS 池与 ProbeManager
	var probe *proxy.ProbeManager
	if config.AppConfig.DNS.Enabled && config.AppConfig.DNS.DBPath != "" {
		if p, err := proxy.NewDNSPool(config.AppConfig.DNS.DBPath); err == nil {
			proxy.SetGlobalDNSPool(p)
			probe = proxy.NewProbeManager(p)
			proxy.SetGlobalProbeManager(probe) // 设置全局ProbeManager
			defer probe.Close()
		}
	}

	// 创建gRPC服务器实例
	// NewGrpcServer函数会初始化标准HTTP/2 over TLS的gRPC服务器
	grpcServer, err := server.NewGrpcServer()
	if err != nil {
		// 如果创建服务器失败，则记录错误日志并终止程序
		log.Fatalf("创建 gRPC 服务器失败: %v", err)
	}

	// 创建爬虫服务实例（传入统计收集器）
	// NewCrawlerServer函数返回一个实现了CrawlerServiceServer接口的实例
	crawlerServer := server.NewCrawlerServer(stats)

	// 注册爬虫服务到gRPC服务器
	// 这样gRPC服务器就能处理针对CrawlerService的请求
	grpcServer.RegisterService(crawlerServer)

	// 创建管理服务实例
	adminServer := server.NewAdminServer(stats, log)

	// 注册管理服务到gRPC服务器
	grpcServer.RegisterAdminService(adminServer)

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
