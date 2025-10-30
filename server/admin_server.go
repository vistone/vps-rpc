package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"vps-rpc/config"
	"vps-rpc/logger"
	"vps-rpc/rpc"
)

// AdminServer 管理服务服务器
// 提供服务管理和监控功能
type AdminServer struct {
	rpc.UnimplementedAdminServiceServer
	stats       *StatsCollector
	logger      logger.Logger
	logBuffer   *logger.LogBuffer
	config      *config.Config
	startTime   time.Time
	mu          sync.RWMutex
	connections map[string]*ConnectionInfo
}

// ConnectionInfo 连接信息结构体
// 存储单个客户端连接的详细信息
type ConnectionInfo struct {
	// ClientAddress 客户端地址
	ClientAddress string
	// ConnectTime 连接建立时间
	ConnectTime time.Time
	// LastActivityTime 最后活动时间
	LastActivityTime time.Time
	// State 连接状态
	State string
}

// NewAdminServer 创建新的管理服务服务器
// 参数:
//
//	stats: 统计收集器实例
//	log: 日志器实例
//
// 返回值:
//
//	*AdminServer: 新创建的管理服务服务器实例
func NewAdminServer(stats *StatsCollector, log logger.Logger) *AdminServer {
	// 从配置中获取日志缓冲区大小
	logBufferSize := config.AppConfig.GetAdminLogBufferSize()
	// 创建日志缓冲区
	logBuffer := logger.GetLogBuffer(logBufferSize)

	return &AdminServer{
		stats:       stats,
		logger:      log,
		logBuffer:   logBuffer,
		config:      &config.AppConfig,
		startTime:   time.Now(),
		connections: make(map[string]*ConnectionInfo),
	}
}

// GetStatus 获取服务器状态
// 返回服务器的运行状态、启动时间、运行时长、版本号和监听地址等信息
// 参数:
//
//	ctx: 上下文对象
//	req: 状态请求对象
//
// 返回值:
//
//	*rpc.StatusResponse: 状态响应对象，包含服务器状态信息
//	error: 可能发生的错误
func (s *AdminServer) GetStatus(ctx context.Context, req *rpc.StatusRequest) (*rpc.StatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &rpc.StatusResponse{
		Running:       true,
		StartTime:     s.startTime.Unix(),
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		Version:       s.config.GetAdminVersion(),
		ListenAddress: config.AppConfig.GetServerAddr(),
	}, nil
}

// GetStats 获取统计信息
// 返回服务器的统计信息，包括请求数、延迟、错误统计等
// 参数:
//
//	ctx: 上下文对象
//	req: 统计请求对象（可以指定是否重置统计信息）
//
// 返回值:
//
//	*rpc.StatsResponse: 统计响应对象，包含详细的统计信息
//	error: 可能发生的错误
func (s *AdminServer) GetStats(ctx context.Context, req *rpc.StatsRequest) (*rpc.StatsResponse, error) {
	stats := s.stats.GetStats()

	// 如果请求重置，重置统计信息
	if req != nil && req.Reset_ {
		s.stats.Reset()
	}

	// 转换错误统计为map[string]int64
	errorStats := make(map[string]int64)
	for k, v := range stats.ErrorStats {
		errorStats[k] = v
	}

	return &rpc.StatsResponse{
		TotalRequests:      stats.TotalRequests,
		SuccessRequests:    stats.SuccessRequests,
		ErrorRequests:      stats.ErrorRequests,
		AverageLatencyMs:   stats.AverageLatencyMs,
		MinLatencyMs:       stats.MinLatencyMs,
		MaxLatencyMs:       stats.MaxLatencyMs,
		CurrentConnections: int64(stats.CurrentConnections),
		ErrorStats:         errorStats,
		StatsStartTime:     stats.StatsStartTime,
	}, nil
}

// UpdateConfig 更新配置
// 更新服务器的配置项（当前实现为框架，实际配置更新需要进一步实现）
// 参数:
//
//	ctx: 上下文对象
//	req: 配置更新请求对象，包含配置键和配置值
//
// 返回值:
//
//	*rpc.UpdateConfigResponse: 配置更新响应对象
//	error: 可能发生的错误
func (s *AdminServer) UpdateConfig(ctx context.Context, req *rpc.UpdateConfigRequest) (*rpc.UpdateConfigResponse, error) {
	if req == nil || req.ConfigKey == "" {
		return &rpc.UpdateConfigResponse{
			Success: false,
			Error:   "配置键不能为空",
		}, nil
	}

	// 这里实现配置更新逻辑
	// 注意：实际的配置更新可能需要重新加载配置文件或使用配置管理接口
	// 这里提供一个基础框架

	s.logger.WithFields(map[string]interface{}{
		"config_key":   req.ConfigKey,
		"config_value": req.ConfigValue,
	}).Info("配置更新请求")

	// 返回当前配置（JSON格式）
	configJSON, err := json.Marshal(s.config)
	if err != nil {
		return &rpc.UpdateConfigResponse{
			Success: false,
			Error:   "序列化配置失败: " + err.Error(),
		}, nil
	}

	return &rpc.UpdateConfigResponse{
		Success:       true,
		UpdatedConfig: string(configJSON),
	}, nil
}

// GetLogs 获取日志
// 参数:
//
//	ctx: 上下文对象
//	req: 日志请求对象
//
// 返回值:
//
//	*rpc.LogsResponse: 日志响应对象
//	error: 可能发生的错误
func (s *AdminServer) GetLogs(ctx context.Context, req *rpc.LogsRequest) (*rpc.LogsResponse, error) {
	// 如果请求为空，使用默认值
	if req == nil {
		// 从配置中获取默认最大日志行数
		defaultMaxLines := int32(s.config.GetAdminDefaultMaxLogLines())
		req = &rpc.LogsRequest{
			MaxLines: defaultMaxLines,
		}
	}

	maxLines := int(req.MaxLines)
	// 如果maxLines小于等于0，使用配置中的默认值
	if maxLines <= 0 {
		maxLines = s.config.GetAdminDefaultMaxLogLines()
	}
	// 如果maxLines超过配置的最大值，使用配置的最大值
	maxLogLines := s.config.GetAdminMaxLogLines()
	if maxLines > maxLogLines {
		maxLines = maxLogLines
	}

	// 从日志缓冲区获取日志
	entries := s.logBuffer.GetEntries(req.Level, maxLines)

	// 转换为protobuf格式
	logEntries := make([]*rpc.LogEntry, len(entries))
	for i, entry := range entries {
		var fieldsJSON string
		if len(entry.Fields) > 0 {
			fieldsBytes, err := json.Marshal(entry.Fields)
			if err == nil {
				fieldsJSON = string(fieldsBytes)
			}
		}

		logEntries[i] = &rpc.LogEntry{
			Timestamp: entry.Timestamp,
			Level:     entry.Level,
			Message:   entry.Message,
			Fields:    fieldsJSON,
		}
	}

	return &rpc.LogsResponse{
		Entries: logEntries,
		HasMore: false, // 简化实现，不支持分页
	}, nil
}

// HealthCheck 健康检查
// 检查服务器的健康状态
// 参数:
//
//	ctx: 上下文对象
//	req: 健康检查请求对象
//
// 返回值:
//
//	*rpc.HealthCheckResponse: 健康检查响应对象，包含健康状态和消息
//	error: 可能发生的错误
func (s *AdminServer) HealthCheck(ctx context.Context, req *rpc.HealthCheckRequest) (*rpc.HealthCheckResponse, error) {
	// 简单的健康检查：检查服务器是否在运行
	healthy := true
	message := "服务器运行正常"

	// 可以添加更多健康检查逻辑，如：
	// - 检查数据库连接
	// - 检查外部服务连接
	// - 检查资源使用情况

	return &rpc.HealthCheckResponse{
		Healthy:   healthy,
		Message:   message,
		CheckTime: time.Now().Unix(),
	}, nil
}

// GetConnections 获取连接信息
// 返回当前所有客户端连接的信息
// 参数:
//
//	ctx: 上下文对象
//	req: 连接信息请求对象
//
// 返回值:
//
//	*rpc.ConnectionsResponse: 连接信息响应对象，包含所有连接信息
//	error: 可能发生的错误
func (s *AdminServer) GetConnections(ctx context.Context, req *rpc.ConnectionsRequest) (*rpc.ConnectionsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	connections := make([]*rpc.ConnectionInfo, 0, len(s.connections))
	for clientAddr, connInfo := range s.connections {
		connections = append(connections, &rpc.ConnectionInfo{
			ClientAddress:    clientAddr,
			ConnectTime:      connInfo.ConnectTime.Unix(),
			LastActivityTime: connInfo.LastActivityTime.Unix(),
			State:            connInfo.State,
		})
	}

	return &rpc.ConnectionsResponse{
		Connections:      connections,
		TotalConnections: int32(len(connections)),
	}, nil
}

// RegisterConnection 注册连接
// 将新的客户端连接注册到管理服务中
// 参数:
//
//	clientAddr: 客户端地址字符串
func (s *AdminServer) RegisterConnection(clientAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connections[clientAddr] = &ConnectionInfo{
		ClientAddress:    clientAddr,
		ConnectTime:      time.Now(),
		LastActivityTime: time.Now(),
		State:            "connected",
	}

	s.stats.IncrementConnections()
}

// UnregisterConnection 注销连接
// 从管理服务中注销客户端连接
// 参数:
//
//	clientAddr: 客户端地址字符串
func (s *AdminServer) UnregisterConnection(clientAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.connections[clientAddr]; exists {
		delete(s.connections, clientAddr)
		s.stats.DecrementConnections()
	}
}

// UpdateConnectionActivity 更新连接活动时间
// 更新指定连接的最后活动时间
// 参数:
//
//	clientAddr: 客户端地址字符串
func (s *AdminServer) UpdateConnectionActivity(clientAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if connInfo, exists := s.connections[clientAddr]; exists {
		connInfo.LastActivityTime = time.Now()
	}
}
