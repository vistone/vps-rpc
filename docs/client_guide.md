# VPS-RPC 客户端使用指南

## 目录

1. [爬虫客户端使用](#爬虫客户端使用)
2. [管理客户端使用](#管理客户端使用)
3. [配置选项](#配置选项)
4. [最佳实践](#最佳实践)

## 爬虫客户端使用

### 基础使用

```go
import (
    "context"
    "time"
    "vps-rpc/client"
    "vps-rpc/rpc"
)

// 创建客户端配置
config := &client.ClientConfig{
    Address:            "localhost:4242",
    InsecureSkipVerify: true,
    Timeout:            10 * time.Second,
}

// 创建客户端
clt, err := client.NewClient(config)
if err != nil {
    log.Fatal(err)
}
defer clt.Close()

// 发起请求
ctx := context.Background()
resp, err := clt.Fetch(ctx, &rpc.FetchRequest{
    Url:       "https://www.example.com",
    TlsClient: rpc.TLSClientType_CHROME,
})
```

### 使用连接池

```go
config := &client.ClientConfig{
    Address:              "localhost:4242",
    InsecureSkipVerify:   true,
    Timeout:              10 * time.Second,
    EnableConnectionPool: true,  // 启用连接池
    MaxPoolSize:          10,     // 最大连接数
}
```

### 使用重试机制

```go
retryConfig := client.DefaultRetryConfig()
retryConfig.MaxRetries = 5

config := &client.ClientConfig{
    Address:            "localhost:4242",
    InsecureSkipVerify: true,
    Timeout:            10 * time.Second,
    EnableRetry:        true,      // 启用重试
    RetryConfig:        retryConfig,
}
```

## 管理客户端使用

### 获取服务器状态

```go
import "vps-rpc/admin"

config := &admin.ClientConfig{
    Address:            "localhost:4242",
    InsecureSkipVerify: true,
    Timeout:            10 * time.Second,
}

adminClient, err := admin.NewClient(config)
if err != nil {
    log.Fatal(err)
}
defer adminClient.Close()

// 获取状态
status, err := adminClient.GetStatus(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("服务器运行中: %v\n", status.Running)
fmt.Printf("运行时长: %d秒\n", status.UptimeSeconds)
```

### 获取统计信息

```go
// 获取统计信息（不重置）
stats, err := adminClient.GetStats(ctx, false)

// 获取统计信息并重置
stats, err := adminClient.GetStats(ctx, true)
```

### 健康检查

```go
health, err := adminClient.HealthCheck(ctx)
if err != nil {
    log.Fatal(err)
}

if health.Healthy {
    fmt.Println("服务器健康")
} else {
    fmt.Printf("服务器不健康: %s\n", health.Message)
}
```

### 获取日志

```go
// 获取所有日志（最多100条）
logs, err := adminClient.GetLogs(ctx, "", 100)

// 获取错误日志（最多50条）
logs, err := adminClient.GetLogs(ctx, "error", 50)
```

### 获取连接信息

```go
conns, err := adminClient.GetConnections(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("当前连接数: %d\n", conns.TotalConnections)
for _, conn := range conns.Connections {
    fmt.Printf("连接: %s, 状态: %s\n", conn.ClientAddress, conn.State)
}
```

## 配置选项

### ClientConfig 选项

- `Address`: 服务器地址（必需）
- `Timeout`: 连接超时时间
- `TLSConfig`: TLS配置
- `InsecureSkipVerify`: 跳过TLS证书验证
- `EnableConnectionPool`: 启用连接池
- `MaxPoolSize`: 连接池最大大小
- `EnableRetry`: 启用重试机制
- `RetryConfig`: 重试配置

### RetryConfig 选项

- `MaxRetries`: 最大重试次数（默认3）
- `InitialDelay`: 初始延迟时间（默认100ms）
- `MaxDelay`: 最大延迟时间（默认5s）
- `Multiplier`: 延迟乘数（默认2.0，指数退避）
- `RetryableErrors`: 可重试的错误代码列表

## 最佳实践

1. **连接管理**: 使用连接池以提高性能
2. **错误处理**: 启用重试机制处理临时错误
3. **超时控制**: 设置合理的超时时间
4. **资源清理**: 使用 defer 确保客户端关闭
5. **上下文管理**: 使用 context 控制请求生命周期

## 完整示例

参考 `examples/client_example.go` 查看完整的使用示例。

