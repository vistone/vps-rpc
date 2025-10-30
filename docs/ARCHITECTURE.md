# 架构设计

## 总体架构

RPC Client -> gRPC/QUIC Server -> UTLS Client -> 目标站点
                          |
                          v
                         DNS 池（A/AAAA 轮询、黑白名单、本地验证）

- 功能成功优先：h2 优先，失败回退 h1，最终回退标准库。
- 去中心：仅共享观测报告，不共享黑白名单状态；本地复测为准。
- TLS 指纹：基于 uTLS 伪装主流客户端，优先 HTTP/2。

## 组件说明

- `server/`: gRPC/QUIC 服务与对外接口。
- `proxy/`: uTLS 客户端、DNS 池、网络能力探测与直连逻辑。
- `config/`: TOML 配置加载与默认值处理。
- `rpc/`: gRPC/Protobuf 接口定义与生成代码。
- `client/`, `admin/`: 客户端与管理端逻辑。
- `cmd/`: 工具（单次抓取、DNS 探测等）。
- `logger/`: 日志接口与内存日志缓冲。

## 关键数据流

1. RPC 请求进入 `server/`，转交 `proxy/UTLSClient`。
2. `UTLSClient` 基于 `config.toml` 与 `DNSPool` 决定是否直连某个 IP；SNI/Host 保持域名。
3. 优先 HTTP/2 + uTLS；失败回退 HTTP/1.1 + uTLS；再失败回退标准库。
4. 200/403 结果反馈给 `DNSPool`：403 入黑，200 解封；状态仅本地维护。

## 配置与自动探测

- 绑定地址固定 `0.0.0.0`（不再提供 `server.host`）。
- TLS 证书自动生成/加载（`data/tls/`）。
- 自动探测 IPv4/IPv6；IPv6 直连时绑定本机全局地址。
- 默认请求头外部化到 `crawler.default_headers`，代码不再硬编码。

## 后续规划

- Peer seeds：对等发现与报告共享通道；当前仅配置预留，服务端未启用。
