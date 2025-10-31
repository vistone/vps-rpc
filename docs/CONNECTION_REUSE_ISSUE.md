# HTTP连接复用问题分析

## 问题现象

从日志观察到：
1. **每个请求都选择不同的IP**（严格轮询的正常行为）
2. **每次请求都显示`[utls][h2] 准备发送请求`**（说明每次都在建立新连接）
3. **响应时间差异大**（45ms到176ms），说明连接建立开销显著
4. **即使同一时间点的并发请求，选择的IP也不同**，没有复用连接的机会

## 根本原因

### HTTP2 Transport连接复用机制

HTTP2 Transport的连接复用基于**`DialTLSContext`的`addr`参数**作为key，而不是实际连接的IP地址。

当前实现的问题：
```go
// buildHTTP2Client
DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
    return c.dialUTLS(ctx, network, address, host, helloID, []string{"h2"})
    //                           ^^^^^^^^ 实际连接的是 address (IP:port)
    //      但HTTP2 Transport基于 addr 参数来判断是否复用连接！
}
```

**关键问题**：
- HTTP2 Transport传入的`addr`可能是hostname格式（如`kh.google.com:443`）
- 我们实际连接的是`address`（IP地址，如`142.251.15.93:443`）
- Transport认为`addr`不同（hostname vs IP），所以每次都创建新连接

### 另一个问题：严格轮询

即使修复了上述问题，由于严格轮询机制：
- 每个请求都选择不同的IP（9-32次的使用分布）
- 即使同一IP的连接可以被复用，但由于轮询，很少有相同IP的连续请求
- 高并发时，同一时间点的请求选择不同IP，无法复用

## 解决方案

### 方案1：修复HTTP2 Transport的连接复用（推荐）

修改`DialTLSContext`，使其基于实际连接的IP地址来判断复用：

```go
DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
    // 解析address获取实际IP
    host, port, _ := net.SplitHostPort(address)
    // 构造标准格式的地址用于Transport复用判断
    actualAddr := net.JoinHostPort(host, port)
    // 但这里有个问题：Transport传入的addr可能包含hostname...
}
```

实际上，更好的方法是：**让Transport基于实际连接的地址来判断复用**，但这需要修改HTTP2 Transport的行为。

### 方案2：使用连接池预建立连接（更激进）

在`getOrCreateH2Client`层面，为每个IP预建立连接池，但需要管理连接的生命周期。

### 方案3：调整轮询策略（影响均衡性）

改为"批次内复用"策略：
- 同一批次（如10个请求）使用相同IP
- 批次间轮询切换IP
- 这样可以在批次内复用连接，同时保持IP均衡

## 当前状态

虽然我们按`host+IP`作为client的key，但HTTP2 Transport内部的连接复用机制仍然基于它传入的`addr`参数，导致连接无法真正复用。

## 建议

1. **接受当前行为**：严格轮询本身就是设计目标，连接复用是次要优化
2. **优化连接建立开销**：使用连接预热、连接池等方式减少开销
3. **考虑批次复用**：在保持IP均衡的前提下，允许短期内的连接复用

