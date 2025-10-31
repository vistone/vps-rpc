# QUIC超时问题分析

## 测试结果
- **单个请求**：✅ 正常（417ms）
- **10个并发**：✅ 全部成功（平均36ms）
- **300个配额测试**：❌ 失败率66%（200/300失败）

## 根本原因

### 1. **服务端AcceptStream阻塞（无超时）**
```go
// server/quic_server.go:106
stream, err := conn.AcceptStream(context.Background())  // ❌ 无超时！
```
**问题**：高并发时，服务端在等待新流时可能无限期阻塞，导致客户端请求超时。

### 2. **QUIC连接未复用（每个Client创建新连接）**
```go
// client/client.go:98
usePool: false, // QUIC模式下暂不使用连接池
```
**问题**：虽然`test_quota`启用了连接池，但QUIC客户端本身不支持连接复用，高并发时会创建大量连接，超过系统限制。

### 3. **UDP缓冲区过小**
```
failed to sufficiently increase receive buffer size (was: 208 kiB, wanted: 7168 kiB, got: 416 kiB)
```
**问题**：UDP接收缓冲区过小，高并发时容易丢包，导致QUIC连接不稳定。

### 4. **MaxIdleTimeout过短（30秒）**
高并发持续请求时，连接可能因短暂空闲而超时断开。

### 5. **流数量限制**
```go
MaxIncomingStreams: 100
```
高并发时可能达到流数量上限，导致新流无法创建。

## 修复建议

### 优先级1：修复服务端AcceptStream超时
```go
// 添加超时context
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
stream, err := conn.AcceptStream(ctx)
cancel()
if err != nil {
    if err == context.DeadlineExceeded {
        // 超时，继续循环等待下一个流
        continue
    }
    log.Printf("接受QUIC流失败: %v", err)
    return
}
```

### 优先级2：增加UDP缓冲区
```bash
# 调整系统参数（需要root权限）
sudo sysctl -w net.core.rmem_max=8388608
sudo sysctl -w net.core.rmem_default=2097152
sudo sysctl -w net.core.wmem_max=8388608
sudo sysctl -w net.core.wmem_default=2097152
```

### 优先级3：增加MaxIdleTimeout
```toml
[quic]
max_idle_timeout = "120s"  # 从30秒增加到120秒
handshake_idle_timeout = "10s"  # 从5秒增加到10秒
```

### 优先级4：实现QUIC连接池
QUIC连接应该复用，而不是每个请求都创建新连接。

