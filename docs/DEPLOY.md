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

## 自动部署脚本（SSH）

- 本地执行（需可 SSH 到目标主机）：
```bash
SSH_HOST=your.server SSH_USER=root APP_DIR=/opt/vps-rpc SERVICE=vps-rpc \
bash scripts/deploy.sh
```

变量说明：
- SSH_HOST: 目标主机（必填）
- SSH_USER: SSH 用户（默认 root）
- SSH_PORT: 端口（默认 22）
- APP_DIR: 部署目录（默认 /opt/vps-rpc）
- SERVICE: systemd 服务名（默认 vps-rpc）

## GitHub Actions 自动部署（可选）

- 在仓库 Secrets 设置以下变量：
  - DEPLOY_HOST, DEPLOY_USER, DEPLOY_KEY(私钥), DEPLOY_PORT(可选)
  - DEPLOY_APP_DIR(默认 /opt/vps-rpc), DEPLOY_SERVICE(默认 vps-rpc)
- 推送到 `main` 将触发 `.github/workflows/deploy.yml`，自动构建并通过 SSH 上传与重启服务。

