#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

HOOKS_DIR=".git/hooks"
mkdir -p "$HOOKS_DIR"

cat > "$HOOKS_DIR/pre-commit" <<'HOOK'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel)"
"$repo_root/scripts/bump_version.sh"
# 将变更加入本次提交
git add VERSION config.toml || true
HOOK

chmod +x "$HOOKS_DIR/pre-commit"

echo "git hook installed: pre-commit (auto bump patch version)"
