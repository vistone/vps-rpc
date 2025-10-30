# 运行与部署

## 运行（开发）

```bash
go mod tidy
go run .
```

- 默认监听：`0.0.0.0:4242`
- TLS：自动生成/加载至 `data/tls/`

## 构建

```bash
go build -o vps-rpc .
```

## 使用工具

- 单 URL 抓取：
  ```bash
  go build -o single_fetch ./cmd/single_fetch
  ./single_fetch
  ```
- DNS 探测与黑白名单更新：
  ```bash
  go build -o dns_probe ./cmd/dns_probe
  ./dns_probe --domain example.com --path /
  ```

## 部署（systemd 示例）

`/etc/systemd/system/vps-rpc.service`：

```
[Unit]
Description=VPS-RPC Service
After=network.target

[Service]
WorkingDirectory=/opt/vps-rpc
ExecStart=/opt/vps-rpc/vps-rpc
Restart=always
RestartSec=3
User=nobody
Group=nogroup

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now vps-rpc
```

## 端口与安全

- 建议仅对受信主体开放 gRPC 端口。
- 后续版本将引入 mTLS（Peer 通道）。

