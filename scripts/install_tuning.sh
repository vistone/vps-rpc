#!/usr/bin/env bash
set -euo pipefail

# Installs a systemd oneshot service that applies tuning before vps-rpc starts.
# Requires root.

SERVICE_NAME="${1:-vps-rpc.service}"
UNIT_NAME="vps-rpc-tuning.service"

require_root() {
  if [[ $(id -u) -ne 0 ]]; then
    echo "[install_tuning] Please run as root." >&2
    exit 1
  fi
}

install_unit() {
  local unit_path="/etc/systemd/system/${UNIT_NAME}"
  mkdir -p /etc/systemd/system
  cat >"${unit_path}" <<EOF
[Unit]
Description=Apply kernel and service tuning for vps-rpc
DefaultDependencies=no
Before=${SERVICE_NAME}
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
ExecStart=$(readlink -f $(dirname "$0")/tune_system.sh) ${SERVICE_NAME}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
  echo "[install_tuning] Installed ${unit_path}"
}

enable_unit() {
  systemctl daemon-reload
  systemctl enable --now ${UNIT_NAME}
  echo "[install_tuning] Enabled and started ${UNIT_NAME}"
}

require_root
install_unit
enable_unit

echo "[install_tuning] Completed. Tuning will be applied at boot before ${SERVICE_NAME}."


