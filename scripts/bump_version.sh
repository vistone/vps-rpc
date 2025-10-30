#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION_FILE="VERSION"
CONFIG_FILE="config.toml"

cur_ver="$(cat "$VERSION_FILE" 2>/dev/null || echo "1.0.0")"
IFS='.' read -r MAJ MIN PAT <<<"${cur_ver}"
PAT=$((PAT+1))
new_ver="${MAJ}.${MIN}.${PAT}"
echo "$new_ver" > "$VERSION_FILE"

# 替换 config.toml 中的 admin.version（假定唯一一处 version 字段）
if [[ -f "$CONFIG_FILE" ]]; then
  sed -i -E "s/^version\s*=\s*\".*\"/version = \"${new_ver}\"/" "$CONFIG_FILE"
fi

echo "bumped version: ${cur_ver} -> ${new_ver}"
