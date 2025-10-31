#!/usr/bin/env bash
set -euo pipefail

# This script tunes UDP buffers and system limits for vps-rpc.
# It is idempotent and safe to run multiple times. Requires root.

require_root() {
  if [[ $(id -u) -ne 0 ]]; then
    echo "[tune_system] Please run as root." >&2
    exit 1
  fi
}

ensure_sysctl_conf() {
  local conf_path="/etc/sysctl.d/99-vps-rpc-udp.conf"
  mkdir -p "/etc/sysctl.d"
  cat >"${conf_path}" <<'EOF'
# vps-rpc UDP and networking tuning
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
net.core.rmem_default = 67108864
net.core.wmem_default = 67108864

net.ipv4.udp_rmem_min = 4194304
net.ipv4.udp_wmem_min = 4194304

net.core.netdev_max_backlog = 250000
net.core.somaxconn = 65535
EOF
  echo "[tune_system] Wrote ${conf_path}"
  sysctl --system >/dev/null
  echo "[tune_system] Applied sysctl settings"
}

ensure_service_override() {
  local service_name=${1:-vps-rpc.service}
  local dropin_dir="/etc/systemd/system/${service_name}.d"
  local override_path="${dropin_dir}/override.conf"
  mkdir -p "${dropin_dir}"
  cat >"${override_path}" <<'EOF'
[Service]
LimitNOFILE=1048576
LimitNPROC=262144
TasksMax=infinity

# Let Go pick CPU threads automatically
Environment=GOMAXPROCS=0
EOF
  echo "[tune_system] Wrote ${override_path}"
  systemctl daemon-reload
}

require_root
ensure_sysctl_conf
ensure_service_override "${1:-vps-rpc.service}"

echo "[tune_system] System tuning complete. You may restart the service: systemctl restart ${1:-vps-rpc.service}"


