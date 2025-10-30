# 模块/功能/配置 对照清单

## 模块清点（目录）
- `server/`: gRPC/QUIC 服务、统计与管理接口
- `proxy/`: `utls_client.go`, `dns_pool.go`, `net_cap.go`, `probe_manager.go`, `global.go`
- `config/`: `config.go`（TOML 加载、默认值转换）
- `rpc/`: `*.proto` 与生成代码
- `client/`, `admin/`: 客户端/管理端逻辑与测试
- `cmd/`: `single_fetch`, `dns_probe`, `more_https` 等工具入口
- `logger/`: 统一日志接口与内存缓冲
- `examples/`: 使用示例
- `integration_test/`: 端到端集成测试

## 功能清点（实现）
- uTLS 指纹伪装（h2 优先，h1 回退，std 兜底）: `proxy/utls_client.go`
- DNS 池（A/AAAA、轮询、403 拉黑、200 解封、自动刷新）: `proxy/dns_pool.go`
- IPv4/IPv6 能力自动探测与 IPv6 源地址绑定: `proxy/net_cap.go`, `utls_client.go`
- 观测报告共享（本地复测后状态生效）: `proxy/probe_manager.go`
- gRPC/RPC 服务接口: `rpc/`, `server/`
- 日志与内存缓冲: `logger/logger.go`

## 配置清点（字段）
- `server.port`: 生效（`config.Config.GetServerAddr` 绑定 `0.0.0.0:port`）
- `quic.*`: 生效（默认值与解析在 `config.Config` 中）
- `crawler.default_timeout/max_concurrent_requests/user_agent/default_headers`: 生效（默认头合并逻辑在 `utls_client.go`）
- `dns.enabled/db_path/refresh_interval`: 生效（`dns_pool.go`）
- `dns.blacklist_duration`: 兼容字段，当前实现为“无过期黑名单”，不生效
- `logging.level/format/buffer_size`: 生效（`logger/logger.go`）
- `client.*` 与 `client.retry.*`: 生效（默认值转换在 `config.Config`）
- `admin.*`: 生效（默认值转换在 `config.Config`）
- `peer.seeds`: 预留，当前无运行时使用

## 文档与代码差异（需处理）
- Peer seeds：文档为规划，代码未启用；保持“预留”标注
- README 与 docs/README 配置段落需要指明 `blacklist_duration` 为兼容字段（已在本次文档修订中补齐）

## 硬编码项（核查）
- 请求头默认值：已外部化（核查通过）
- 绑定地址：固定 `0.0.0.0`（设计决策，文档已解释）
- TLS 路径：自动生成/加载（无硬编码路径，使用 `data/tls/` 约定）
- 日志：默认级别与格式可通过配置覆盖（核查通过）
