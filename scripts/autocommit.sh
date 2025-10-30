#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_DIR"

# 配置
INTERVAL_SEC=${INTERVAL_SEC:-10}
BRANCH=${BRANCH:-main}
MSG_PREFIX=${MSG_PREFIX:-"chore(auto): save work"}

echo "[autocommit] watching $REPO_DIR every ${INTERVAL_SEC}s, branch=$BRANCH"

while true; do
  # 跳过如果没有变更
  if [[ -z "$(git status --porcelain)" ]]; then
    sleep "$INTERVAL_SEC"
    continue
  fi

  # 加入与提交
  git add -A || true
  TS=$(date +"%Y-%m-%d %H:%M:%S")
  # 使用版本自增钩子（pre-commit 已安装会自动 bump）
  git commit -m "${MSG_PREFIX} @ ${TS}" || true

  # 推送（如未设置远端或无权限则忽略错误）
  git push -u origin "$BRANCH" || true

  sleep "$INTERVAL_SEC"
done


