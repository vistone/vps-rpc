VPS-RPC 高速爬虫代理（gRPC over TLS，uTLS 指纹伪装，IPv4/IPv6 直连，DNS 池与黑白名单，本地验证，零配置TLS，自举种子）

## 特性
- uTLS 指纹伪装：优先 HTTP/2，失败回退 HTTP/1.1，标准库兜底；Header 外部化
- DNS IP 池：A/AAAA 解析、轮询、403 入黑、200 解封、自动刷新
- IPv4/IPv6 自动探测：可用族自动选择；IPv6 直连绑定本机全局地址
- 直连 IP + 域名回退：SNI/Host 保持域名，确保证书与路由正确
- 观测报告共享、本地验证：仅共享 200/403 报告，本地复测后才落黑/解封
- 种子自举（Peer seeds）：配置预留（服务未启用）
- 零配置 TLS：启动自动加载/生成证书

文档索引见 `docs/INDEX.md`（架构、配置、部署、设计决策、对照清单）。

## 快速开始
```bash
go mod tidy
go build -o vps-rpc .
./vps-rpc
```
默认监听 `0.0.0.0:4242`，TLS 自动生成于 `data/tls/`。

## 配置（config.toml）
详见 `docs/CONFIG.md`。示例：
```toml
[server]
port = 4242

[dns]
enabled = true
db_path = "./dns_pool.bolt"
refresh_interval = "24h"
blacklist_duration = "6h"  # 兼容字段；当前实现为“无过期黑名单”（不生效）

[crawler.default_headers]
Accept = "*/*"
Accept-Encoding = "gzip, deflate, br, zstd"
Accept-Language = "zh-CN,zh;q=0.8,zh-TW;q=0.7,zh-HK;q=0.5,en-US;q=0.3,en;q=0.2"
TE = "trailers"
```
说明：
- 不再需要 `server.host`、`tls_cert_file`、`tls_key_file`；证书自动处理。
- Header 完全外部化；请求时同名覆盖默认值。
- DNS 池仅在本地决策：403 入黑、200 解封；跨节点仅共享观测报告。

## 运行与测试
- 开发运行：`go run .`
- 单 URL 测试：`go build -o single_fetch ./cmd/single_fetch && ./single_fetch`
- DNS 探测：`go build -o dns_probe ./cmd/dns_probe && ./dns_probe --domain example.com`
- 集成测试：`go test ./integration_test -v`

更多部署方式见 `docs/DEPLOY.md`（含 systemd 示例）。

## 设计决策（摘要）
- 功能成功优先：h2→h1→std 三级回退
- 安全：默认自签 TLS；后续集群根CA + mTLS（Peer 通道）
- 去中心：seeds 自举 + 报告共享 + 本地验证

完整细节见 `docs/DESIGN_DECISIONS.md`。

## FAQ
- 绑定地址能否配置？目前固定 `0.0.0.0`。
- 如何关闭 DNS 直连？`dns.enabled = false`。
- `blacklist_duration` 是否生效？为兼容字段，当前实现为“无过期黑名单”。