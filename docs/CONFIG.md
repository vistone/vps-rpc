# 配置说明（config.toml）

## 总览

- 所有配置集中于根目录 `config.toml`。
- 绑定地址固定为 `0.0.0.0`，仅需配置端口。
- 默认请求头完全外部化到 `[crawler.default_headers]`。
- TLS 证书自动生成/加载，位于 `data/tls/`。

## 配置项

### [server]
- `port` (int): 监听端口，默认 4242。

### [quic]
- `max_idle_timeout` (duration): 空闲超时，默认 30s。
- `handshake_idle_timeout` (duration): 握手空闲超时，默认 5s。
- `max_incoming_streams` (int): 最大入站双向流。
- `max_incoming_uni_streams` (int): 最大入站单向流。

### [crawler]
- `default_timeout` (duration): 抓取默认超时，默认 30s。
- `max_concurrent_requests` (int): 最大并发请求数。
- `user_agent` (string): 默认 UA，可被请求覆盖。

#### [crawler.default_headers]
- 可配置任意请求头；请求时同名优先生效。

### [dns]
- `enabled` (bool): 是否启用 DNS 池直连与黑白名单。
- `db_path` (string): bbolt 数据库路径，例如 `./dns_pool.bolt`。
- `refresh_interval` (duration): 记录刷新间隔，例如 `24h`。
- `blacklist_duration` (duration): 兼容字段，当前为“无过期黑名单”，该字段不生效（保留以兼容旧配置）。

### [logging]
- `level` (string): `debug|info|warn|error`，默认 `info`。
- `format` (string): `json|text`，默认 `text`。
- `buffer_size` (int): 内存日志缓冲区大小，默认 1000。

### [client]
- `default_timeout` (duration): 客户端超时，默认 10s。
- `default_pool_size` (int): 连接池大小，默认 10。

#### [client.retry]
- `max_retries` (int): 最大重试次数，默认 3。
- `initial_delay` (duration): 初始延迟，默认 100ms。
- `max_delay` (duration): 最大延迟，默认 5s。
- `multiplier` (float): 指数退避乘数，默认 2.0。

### [admin]
- `version` (string): 版本号，默认 1.0.0。
- `log_buffer_size` (int): 日志缓冲区大小，默认 1000。
- `default_max_log_lines` (int): 日志默认返回行数，默认 100。
- `max_log_lines` (int): 日志最大返回行数，默认 1000。

### [peer]
- `seeds` ([]string): 预留对等种子，当前仅配置预留，未启用。

## 与实现的关系

- `server.host`、`tls_cert_file`、`tls_key_file` 不再支持；系统自动在 `data/tls/` 处理证书。
- `dns.blacklist_duration` 为兼容字段，当前实现为“无过期黑名单”。
- IPv4/IPv6 能力自动探测；IPv6 直连绑定本机全局地址。
