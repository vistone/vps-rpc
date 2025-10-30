# VPS-RPC 项目开发计划文档

## 一、项目现状分析

### 1.1 项目概述

**VPS-RPC** 是一个基于 Go 语言开发的高速数据爬虫代理系统，采用 uTLS + QUIC + gRPC 技术栈，旨在提供高性能、反检测的数据爬取服务。

### 1.2 当前项目结构

```
vps-rpc/
├── main.go                 # 程序入口
├── config.toml             # TOML配置文件
├── go.mod                  # Go模块依赖
├── config/
│   └── config.go           # 配置管理模块（已完成）
├── rpc/
│   ├── service.proto       # gRPC服务定义（已完成）
│   ├── service.pb.go       # 生成的protobuf代码（已完成）
│   └── service_grpc.pb.go  # 生成的gRPC代码（已完成）
├── server/
│   ├── quic_server.go      # QUIC服务器实现（部分完成）
│   └── server.go           # RPC服务实现（已完成）
├── proxy/
│   └── utls_client.go      # uTLS客户端实现（已完成）
└── crawler/                # 爬虫模块（空目录）
```

### 1.3 已完成功能

1. **配置管理模块** (`config/config.go`)
   - ✅ TOML配置文件解析
   - ✅ 配置项类型转换（超时时间等）
   - ✅ 配置结构体定义完善

2. **gRPC服务定义** (`rpc/service.proto`)
   - ✅ CrawlerService服务定义
   - ✅ Fetch和BatchFetch方法定义
   - ✅ 消息类型定义（FetchRequest、FetchResponse等）
   - ✅ TLS客户端类型枚举

3. **爬虫服务实现** (`server/server.go`)
   - ✅ CrawlerServiceServer接口实现
   - ✅ Fetch方法实现（单个URL抓取）
   - ✅ BatchFetch方法实现（批量抓取，但未实现并发控制）
   - ✅ TLS客户端类型映射

4. **uTLS客户端** (`proxy/utls_client.go`)
   - ✅ 多种浏览器TLS指纹模拟
   - ✅ HTTP请求发送和响应解析
   - ✅ 超时控制

5. **QUIC服务器框架** (`server/quic_server.go`)
   - ✅ TLS配置生成（自签名证书）
   - ✅ QUIC监听器创建
   - ✅ gRPC服务器创建
   - ⚠️ **问题：gRPC over QUIC集成不完整**

### 1.4 存在问题

#### 1.4.1 核心问题

1. **gRPC over QUIC集成不完整**
   - `quic_server.go` 的 `Serve()` 方法中，仅接受QUIC连接但未建立gRPC连接
   - 当前实现无法处理gRPC请求
   - 需要实现QUIC传输层到gRPC的连接桥接

2. **批量抓取未实现并发控制**
   - `BatchFetch` 使用顺序循环而非并发执行
   - `max_concurrent` 参数未使用
   - 未使用goroutine和channel进行并发控制

3. **URL解析实现简单**
   - `extractHost` 和 `extractHostPort` 手动解析URL
   - 建议使用 `net/url` 标准库

#### 1.4.2 缺失功能

1. **RPC客户端** - 完全缺失
   - 无客户端代码
   - 无客户端示例
   - 无客户端SDK

2. **服务器管理客户端** - 完全缺失
   - 无管理接口定义
   - 无管理服务实现
   - 无管理客户端实现

3. **日志系统** - 配置存在但未实现
   - 配置中有日志级别和格式配置
   - 但代码中仅使用标准 `log` 包
   - 未实现结构化日志

4. **错误处理和重试机制**
   - 错误处理不够完善
   - 无重试机制
   - 无错误分类

5. **监控和统计**
   - 无请求统计
   - 无性能监控
   - 无健康检查接口

## 二、开发目标

### 2.1 核心目标

1. **完善gRPC over QUIC集成**
   - 实现完整的QUIC传输层
   - 确保gRPC服务可以正常接收和处理请求

2. **开发RPC客户端**
   - 提供易用的客户端SDK
   - 支持连接池管理
   - 支持重试和超时控制

3. **开发服务器管理客户端**
   - 实现管理服务接口
   - 提供服务器状态监控
   - 支持配置热更新
   - 提供日志查看功能

### 2.2 功能目标

#### 2.2.1 RPC客户端功能

- ✅ 连接管理（连接池、自动重连）
- ✅ 请求发起（单个和批量）
- ✅ 错误处理和重试
- ✅ 超时控制
- ✅ TLS配置（支持自签名证书）
- ✅ 负载均衡（多服务器支持）

#### 2.2.2 服务器管理客户端功能

- ✅ 服务器状态查询（运行状态、性能指标）
- ✅ 配置管理（查看、更新配置）
- ✅ 日志查看（实时日志、历史日志）
- ✅ 连接管理（查看当前连接、断开连接）
- ✅ 统计信息（请求统计、错误统计）
- ✅ 健康检查

## 三、技术架构设计

### 3.1 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                   客户端层（Client Layer）              │
├─────────────────────────────────────────────────────────┤
│  ┌──────────────────┐      ┌──────────────────────┐   │
│  │   RPC客户端      │      │  管理客户端          │   │
│  │  (Client SDK)    │      │  (Admin Client)      │   │
│  └──────────────────┘      └──────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   传输层（Transport Layer）              │
├─────────────────────────────────────────────────────────┤
│              QUIC Transport (基于UDP)                    │
│              TLS 1.3 加密                               │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   服务层（Service Layer）               │
├─────────────────────────────────────────────────────────┤
│  ┌──────────────────┐      ┌──────────────────────┐   │
│  │  爬虫服务        │      │  管理服务            │   │
│  │  CrawlerService  │      │  AdminService       │   │
│  └──────────────────┘      └──────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   代理层（Proxy Layer）                  │
├─────────────────────────────────────────────────────────┤
│              uTLS Client (TLS指纹伪装)                   │
│              HTTP Client                                │
└─────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   目标服务器（Target Server）            │
└─────────────────────────────────────────────────────────┘
```

### 3.2 模块设计

#### 3.2.1 RPC客户端模块 (`client/`)

```
client/
├── client.go              # 客户端主结构体和连接管理
├── connection_pool.go     # 连接池管理
├── retry.go               # 重试机制
├── load_balancer.go       # 负载均衡（可选）
└── config.go              # 客户端配置
```

**核心类设计：**

```go
// Client - RPC客户端主类
type Client struct {
    servers      []string
    pool         *ConnectionPool
    config       *ClientConfig
    loadBalancer LoadBalancer
}

// ConnectionPool - 连接池管理
type ConnectionPool struct {
    connections map[string]*grpc.ClientConn
    maxSize     int
    mutex       sync.RWMutex
}

// ClientConfig - 客户端配置
type ClientConfig struct {
    Timeout         time.Duration
    MaxRetries      int
    RetryDelay      time.Duration
    TLSConfig       *tls.Config
    PoolSize        int
}
```

#### 3.2.2 管理服务模块

**新增Proto定义 (`rpc/admin_service.proto`):**

```protobuf
service AdminService {
  // 获取服务器状态
  rpc GetStatus(StatusRequest) returns (StatusResponse);
  
  // 获取统计信息
  rpc GetStats(StatsRequest) returns (StatsResponse);
  
  // 更新配置
  rpc UpdateConfig(UpdateConfigRequest) returns (UpdateConfigResponse);
  
  // 获取日志
  rpc GetLogs(LogsRequest) returns (LogsResponse);
  
  // 健康检查
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  
  // 获取连接信息
  rpc GetConnections(ConnectionsRequest) returns (ConnectionsResponse);
}
```

**管理服务实现 (`server/admin_server.go`):**

```go
type AdminServer struct {
    rpc.UnimplementedAdminServiceServer
    stats      *StatsCollector
    logger     Logger
    config     *config.Config
}

type StatsCollector struct {
    totalRequests    int64
    successRequests  int64
    errorRequests    int64
    averageLatency   time.Duration
    mutex            sync.RWMutex
}
```

**管理客户端 (`admin/client.go`):**

```go
type AdminClient struct {
    conn   *grpc.ClientConn
    client rpc.AdminServiceClient
}
```

#### 3.2.3 QUIC传输层改进

**需要实现QUIC传输层桥接：**

```go
// QUICTransport - QUIC传输层实现
type QUICTransport struct {
    listener quic.EarlyListener
    server   *grpc.Server
}

// 实现grpc.Transport接口
func (t *QUICTransport) Serve() error {
    for {
        conn, err := t.listener.Accept(context.Background())
        if err != nil {
            return err
        }
        
        // 创建QUIC到gRPC的连接桥接
        stream := newQUICStream(conn)
        go t.server.ServeStream(stream)
    }
}
```

**注意：** quic-go 可能需要特定的传输层适配器，需要研究 quic-go 和 gRPC 的集成方式。

## 四、详细开发计划

### 4.1 阶段一：修复核心问题（优先级：最高）

#### 任务1.1：完善gRPC over QUIC集成
**预计时间：3-5天**

**任务描述：**
- 研究 quic-go 和 gRPC 的集成方式
- 实现QUIC传输层桥接
- 确保gRPC服务可以正常接收和处理请求

**技术要点：**
- 需要查看 quic-go 是否提供了 gRPC 传输层支持
- 如果没有，需要实现自定义传输层
- 可能需要使用 `quic-go` 的流式API

**交付物：**
- ✅ 修复后的 `server/quic_server.go`
- ✅ 测试用例验证连接和请求处理

#### 任务1.2：实现批量抓取并发控制
**预计时间：2-3天**

**任务描述：**
- 实现 `BatchFetch` 的并发控制
- 使用 goroutine 和 channel 进行并发管理
- 实现 `max_concurrent` 参数限制

**技术要点：**
- 使用 `sync.WaitGroup` 管理goroutine
- 使用 buffered channel 限制并发数
- 实现错误聚合和响应组装

**交付物：**
- ✅ 修复后的 `server/server.go` 中的 `BatchFetch` 方法
- ✅ 并发测试用例

#### 任务1.3：改进URL解析
**预计时间：1天**

**任务描述：**
- 使用 `net/url` 标准库替换手动解析
- 改进错误处理

**交付物：**
- ✅ 修复后的 `proxy/utls_client.go`

### 4.2 阶段二：开发RPC客户端（优先级：高）

#### 任务2.1：设计客户端架构
**预计时间：1天**

**任务描述：**
- 设计客户端类结构
- 设计连接池机制
- 设计重试策略

**交付物：**
- ✅ 客户端架构设计文档
- ✅ 接口定义

#### 任务2.2：实现基础客户端
**预计时间：3-4天**

**创建文件：**
- `client/client.go` - 客户端主实现
- `client/config.go` - 客户端配置
- `client/connection_pool.go` - 连接池

**功能：**
- ✅ 连接管理（单个服务器）
- ✅ 基本请求发送（Fetch、BatchFetch）
- ✅ TLS配置支持
- ✅ 超时控制

**交付物：**
- ✅ 基础客户端代码
- ✅ 单元测试
- ✅ 使用示例

#### 任务2.3：实现连接池和重试机制
**预计时间：2-3天**

**创建文件：**
- `client/retry.go` - 重试机制
- 改进 `client/connection_pool.go`

**功能：**
- ✅ 连接池管理（复用连接）
- ✅ 自动重连机制
- ✅ 重试策略（指数退避）
- ✅ 错误分类和处理

**交付物：**
- ✅ 连接池和重试代码
- ✅ 测试用例

#### 任务2.4：实现负载均衡（可选）
**预计时间：2-3天**

**创建文件：**
- `client/load_balancer.go`

**功能：**
- ✅ 多服务器支持
- ✅ 负载均衡策略（轮询、随机、最少连接）
- ✅ 健康检查

**交付物：**
- ✅ 负载均衡代码
- ✅ 测试用例

#### 任务2.5：编写客户端使用文档和示例
**预计时间：1-2天**

**创建文件：**
- `examples/client_example.go` - 客户端使用示例
- `docs/client_guide.md` - 客户端使用指南

**交付物：**
- ✅ 使用文档
- ✅ 示例代码

### 4.3 阶段三：开发管理服务和客户端（优先级：中）

#### 任务3.1：设计管理服务接口
**预计时间：1-2天**

**创建文件：**
- `rpc/admin_service.proto` - 管理服务Proto定义

**接口设计：**
- GetStatus - 获取服务器状态
- GetStats - 获取统计信息
- UpdateConfig - 更新配置
- GetLogs - 获取日志
- HealthCheck - 健康检查
- GetConnections - 获取连接信息

**交付物：**
- ✅ Proto定义文件
- ✅ 生成代码

#### 任务3.2：实现统计收集器
**预计时间：2-3天**

**创建文件：**
- `server/stats.go` - 统计收集器

**功能：**
- ✅ 请求统计（总数、成功、失败）
- ✅ 延迟统计（平均、最大、最小）
- ✅ 错误统计（按类型分类）
- ✅ 并发统计（当前连接数）

**交付物：**
- ✅ 统计收集器代码
- ✅ 测试用例

#### 任务3.3：实现结构化日志系统
**预计时间：2-3天**

**依赖：**
- 可以选择 `logrus` 或 `zap` 作为日志库

**创建文件：**
- `logger/logger.go` - 日志封装

**功能：**
- ✅ 结构化日志输出
- ✅ 日志级别控制
- ✅ 日志格式支持（JSON、Text）
- ✅ 日志轮转（可选）

**交付物：**
- ✅ 日志系统代码
- ✅ 集成到现有代码中

#### 任务3.4：实现管理服务
**预计时间：3-4天**

**创建文件：**
- `server/admin_server.go` - 管理服务实现

**功能：**
- ✅ 实现所有管理接口
- ✅ 集成统计收集器
- ✅ 集成日志系统
- ✅ 配置热更新支持

**交付物：**
- ✅ 管理服务代码
- ✅ 测试用例

#### 任务3.5：实现管理客户端
**预计时间：2-3天**

**创建文件：**
- `admin/client.go` - 管理客户端
- `admin/commands.go` - CLI命令（可选）

**功能：**
- ✅ 客户端SDK实现
- ✅ CLI工具（可选）
- ✅ 使用示例

**交付物：**
- ✅ 管理客户端代码
- ✅ CLI工具（可选）
- ✅ 使用文档

### 4.4 阶段四：测试和优化（优先级：中）

#### 任务4.1：单元测试
**预计时间：3-5天**

**覆盖范围：**
- ✅ 所有核心模块单元测试
- ✅ 测试覆盖率 > 80%

#### 任务4.2：集成测试
**预计时间：2-3天**

**测试场景：**
- ✅ 客户端-服务器端到端测试
- ✅ 并发压力测试
- ✅ 错误场景测试

#### 任务4.3：性能优化
**预计时间：2-3天**

**优化点：**
- ✅ 连接池优化
- ✅ 内存使用优化
- ✅ 并发性能优化

### 4.5 阶段五：文档和部署（优先级：低）

#### 任务5.1：完善文档
**预计时间：2-3天**

**文档内容：**
- ✅ API文档
- ✅ 架构文档
- ✅ 部署文档
- ✅ 故障排查文档

#### 任务5.2：部署和运维
**预计时间：2-3天**

**内容：**
- ✅ Docker镜像构建
- ✅ Docker Compose配置
- ✅ 部署脚本
- ✅ 监控配置（可选）

## 五、技术难点和解决方案

### 5.1 gRPC over QUIC集成

**难点：**
- quic-go 和 gRPC 的集成方式不明确
- 需要实现自定义传输层

**解决方案：**
1. 研究 quic-go 的最新文档和示例
2. 查看是否有现成的集成方案（如 `grpc-go` 的 QUIC 支持）
3. 如果不存在，实现自定义传输层：
   - 实现 `grpc.Transport` 接口
   - 将 QUIC 流映射到 gRPC 流
   - 处理连接管理和错误

**备选方案：**
- 如果 QUIC 集成过于复杂，可以考虑：
   - 使用标准 gRPC over TLS（HTTP/2）
   - QUIC 作为可选传输层

### 5.2 连接池管理

**难点：**
- QUIC 连接的生命周期管理
- 连接复用和关闭策略

**解决方案：**
- 实现连接池管理器
- 使用连接状态机（idle、active、closed）
- 实现连接健康检查
- 实现连接超时和自动清理

### 5.3 并发控制

**难点：**
- 批量请求的并发控制
- 防止资源耗尽

**解决方案：**
- 使用 buffered channel 作为信号量
- 实现令牌桶算法
- 监控 goroutine 数量

### 5.4 配置热更新

**难点：**
- 运行时配置更新
- 配置变更通知

**解决方案：**
- 使用配置文件监听（fsnotify）
- 实现配置版本管理
- 实现配置变更回调机制

## 六、项目结构规划

### 6.1 完整项目结构

```
vps-rpc/
├── main.go                    # 服务器入口
├── client/
│   ├── main.go                # 客户端CLI入口（可选）
│   ├── client.go              # RPC客户端实现
│   ├── connection_pool.go     # 连接池管理
│   ├── retry.go               # 重试机制
│   ├── load_balancer.go       # 负载均衡（可选）
│   └── config.go              # 客户端配置
├── admin/
│   ├── main.go                # 管理客户端CLI入口
│   ├── client.go              # 管理客户端实现
│   └── commands.go             # CLI命令实现
├── server/
│   ├── quic_server.go         # QUIC服务器
│   ├── server.go               # 爬虫服务实现
│   ├── admin_server.go        # 管理服务实现
│   ├── stats.go                # 统计收集器
│   └── transport.go            # QUIC传输层（新增）
├── proxy/
│   └── utls_client.go          # uTLS客户端
├── config/
│   └── config.go               # 配置管理
├── logger/
│   └── logger.go               # 日志系统
├── rpc/
│   ├── service.proto           # 爬虫服务定义
│   ├── admin_service.proto     # 管理服务定义（新增）
│   ├── service.pb.go           # 生成的代码
│   ├── service_grpc.pb.go
│   ├── admin_service.pb.go     # 生成的代码（新增）
│   └── admin_service_grpc.pb.go # 生成的代码（新增）
├── crawler/
│   └── (待实现)                 # 爬虫核心逻辑（可选）
├── examples/
│   ├── client_example.go       # 客户端使用示例
│   └── server_example.go       # 服务器使用示例
├── docs/
│   ├── architecture.md         # 架构文档
│   ├── api.md                  # API文档
│   ├── client_guide.md         # 客户端指南
│   └── deployment.md           # 部署文档
├── scripts/
│   ├── build.sh                # 构建脚本
│   └── deploy.sh                # 部署脚本
├── docker/
│   ├── Dockerfile.server        # 服务器镜像
│   ├── Dockerfile.client        # 客户端镜像（可选）
│   └── docker-compose.yml       # Compose配置
├── config.toml                 # 配置文件
├── go.mod                       # Go模块
├── go.sum                       # 依赖校验
├── README.md                    # 项目说明
├── DEVELOPMENT_PLAN.md          # 开发计划（本文档）
└── CHANGELOG.md                 # 变更日志
```

### 6.2 目录说明

- **client/**: RPC客户端实现
- **admin/**: 管理客户端实现
- **server/**: 服务器端实现
- **proxy/**: 代理层实现
- **config/**: 配置管理
- **logger/**: 日志系统
- **rpc/**: gRPC服务定义和生成代码
- **crawler/**: 爬虫核心逻辑（可选，未来扩展）
- **examples/**: 使用示例
- **docs/**: 文档
- **scripts/**: 构建和部署脚本
- **docker/**: Docker相关文件

## 七、开发优先级和时间规划

### 7.1 优先级排序

**P0（必须完成）：**
1. 修复gRPC over QUIC集成
2. 实现批量抓取并发控制
3. 开发基础RPC客户端

**P1（重要）：**
4. 实现连接池和重试机制
5. 开发管理服务接口
6. 实现统计收集器

**P2（可选）：**
7. 实现结构化日志系统
8. 实现管理客户端
9. 实现负载均衡

**P3（未来）：**
10. 性能优化
11. 完整测试覆盖
12. 部署和运维工具

### 7.2 时间估算

**第一阶段（修复核心问题）：** 6-9天
- 任务1.1：3-5天
- 任务1.2：2-3天
- 任务1.3：1天

**第二阶段（RPC客户端）：** 9-13天
- 任务2.1：1天
- 任务2.2：3-4天
- 任务2.3：2-3天
- 任务2.4：2-3天（可选）
- 任务2.5：1-2天

**第三阶段（管理服务）：** 10-15天
- 任务3.1：1-2天
- 任务3.2：2-3天
- 任务3.3：2-3天
- 任务3.4：3-4天
- 任务3.5：2-3天

**第四阶段（测试和优化）：** 7-11天
- 任务4.1：3-5天
- 任务4.2：2-3天
- 任务4.3：2-3天

**第五阶段（文档和部署）：** 4-6天
- 任务5.1：2-3天
- 任务5.2：2-3天

**总计：** 36-54天（约5-8周）

### 7.3 里程碑

**Milestone 1: 核心功能修复完成**
- 完成时间：第1-2周
- 交付物：可用的服务器和基础客户端

**Milestone 2: RPC客户端完成**
- 完成时间：第3-4周
- 交付物：完整的RPC客户端SDK

**Milestone 3: 管理功能完成**
- 完成时间：第5-6周
- 交付物：管理服务和客户端

**Milestone 4: 测试和文档完成**
- 完成时间：第7-8周
- 交付物：完整的测试覆盖和文档

## 八、依赖和工具

### 8.1 新增依赖

```go
// 日志库（二选一）
github.com/sirupsen/logrus v1.9.0
// 或
go.uber.org/zap v1.26.0

// 配置文件监听（配置热更新）
github.com/fsnotify/fsnotify v1.7.0

// 命令行工具（管理客户端CLI）
github.com/spf13/cobra v1.8.0

// 配置文件（已存在）
github.com/BurntSushi/toml v1.5.0

// 测试框架
github.com/stretchr/testify v1.8.4
```

### 8.2 开发工具

- **protoc**: Protocol Buffers编译器
- **protoc-gen-go**: Go语言protobuf插件
- **protoc-gen-go-grpc**: Go语言gRPC插件
- **golangci-lint**: Go代码检查工具
- **go test**: Go测试框架

## 九、风险评估

### 9.1 技术风险

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|----------|
| gRPC over QUIC集成困难 | 高 | 中 | 研究现有方案，考虑备选方案（标准gRPC） |
| QUIC连接管理复杂 | 中 | 中 | 参考成熟实现，充分测试 |
| 性能不达标 | 中 | 低 | 性能测试，优化关键路径 |

### 9.2 进度风险

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|----------|
| 时间估算不准确 | 中 | 中 | 设置缓冲时间，优先完成核心功能 |
| 技术难点阻塞 | 高 | 低 | 提前研究技术难点，准备备选方案 |

## 十、成功标准

### 10.1 功能标准

- ✅ 服务器可以正常接收和处理gRPC请求
- ✅ RPC客户端可以成功连接服务器并发送请求
- ✅ 批量抓取支持并发控制
- ✅ 管理客户端可以提供基本的监控和管理功能

### 10.2 性能标准

- ✅ 单次请求延迟 < 100ms（本地网络）
- ✅ 支持并发请求数 > 100
- ✅ 连接建立时间 < 1s

### 10.3 质量标准

- ✅ 代码测试覆盖率 > 80%
- ✅ 无严重bug
- ✅ 代码符合Go规范
- ✅ 文档完整

## 十一、后续规划

### 11.1 功能扩展

- 代理池支持
- 请求去重
- 缓存机制
- 分布式部署支持
- Web管理界面

### 11.2 性能优化

- 连接复用优化
- 内存池管理
- 异步处理优化

### 11.3 运维支持

- Prometheus监控集成
- Grafana仪表板
- 告警系统
- 日志聚合（ELK）

---

## 附录

### A. 参考资源

- [quic-go文档](https://github.com/quic-go/quic-go)
- [gRPC-Go文档](https://github.com/grpc/grpc-go)
- [uTLS文档](https://github.com/refraction-networking/utls)
- [Protocol Buffers文档](https://developers.google.com/protocol-buffers)

### B. 相关项目

- [grpc-go](https://github.com/grpc/grpc-go)
- [quic-go](https://github.com/quic-go/quic-go)
- [utls](https://github.com/refraction-networking/utls)

---

**文档版本：** v1.0  
**创建日期：** 2024  
**最后更新：** 2024  
**作者：** VPS-RPC开发团队
