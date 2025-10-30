#!/usr/bin/env bash
set -euo pipefail

# 环境变量（需外部提供）
# SSH_HOST: 目标主机（必填）
# SSH_USER: SSH 用户（默认 root）
# SSH_PORT: SSH 端口（默认 22）
# APP_DIR:  远端部署目录（默认 /opt/vps-rpc）
# SERVICE:  systemd 服务名（默认 vps-rpc）

SSH_HOST=${SSH_HOST:-}
SSH_USER=${SSH_USER:-root}
SSH_PORT=${SSH_PORT:-22}
APP_DIR=${APP_DIR:-/opt/vps-rpc}
SERVICE=${SERVICE:-vps-rpc}

if [[ -z "$SSH_HOST" ]]; then
  echo "[deploy] 请设置 SSH_HOST 环境变量" >&2
  exit 1
fi

echo "[deploy] 构建二进制..."
GOOS=linux GOARCH=amd64 go build -o build/vps-rpc .

echo "[deploy] 创建远端目录: $APP_DIR"
ssh -p "$SSH_PORT" "$SSH_USER@$SSH_HOST" "sudo mkdir -p $APP_DIR && sudo chown -R \$(whoami) $APP_DIR"

echo "[deploy] 上传文件..."
scp -P "$SSH_PORT" build/vps-rpc "$SSH_USER@$SSH_HOST:$APP_DIR/vps-rpc"
scp -P "$SSH_PORT" config.toml "$SSH_USER@$SSH_HOST:$APP_DIR/config.toml"

echo "[deploy] 安装 systemd 单元（若不存在）..."
ssh -p "$SSH_PORT" "$SSH_USER@$SSH_HOST" "sudo bash -s" <<'REMOTE'
set -euo pipefail
SERVICE_NAME=${SERVICE:-vps-rpc}
APP_DIR=${APP_DIR:-/opt/vps-rpc}
UNIT_FILE=/etc/systemd/system/${SERVICE_NAME}.service
if [[ ! -f "$UNIT_FILE" ]]; then
  sudo tee "$UNIT_FILE" >/dev/null <<EOF
[Unit]
Description=VPS-RPC Service
After=network.target

[Service]
WorkingDirectory=${APP_DIR}
ExecStart=${APP_DIR}/vps-rpc
Restart=always
RestartSec=3
User=nobody
Group=nogroup

[Install]
WantedBy=multi-user.target
EOF
  sudo systemctl daemon-reload
  sudo systemctl enable ${SERVICE_NAME}
fi
REMOTE

echo "[deploy] 重启服务..."
ssh -p "$SSH_PORT" "$SSH_USER@$SSH_HOST" "sudo systemctl restart ${SERVICE} && sudo systemctl status ${SERVICE} --no-pager -l | head -n 50"

echo "[deploy] 完成"


