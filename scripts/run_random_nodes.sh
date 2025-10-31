#!/usr/bin/env bash
set -euo pipefail

# 随机并发 3-8，按轮次对指定节点发起请求
# 使用 cmd/single_fetch（已支持 VPS_RPC_ADDR 环境变量）

NODES=("tile0.zeromaps.cn:4242" "tile3.zeromaps.cn:4242")
ROUNDS=${1:-10}

cd "$(dirname "$0")/.."
go build -o tools/test_client ./cmd/single_fetch >/dev/null 2>&1 || true

for node in "${NODES[@]}"; do
  echo "=== 节点: $node ==="
  ok=0; fail=0; total_latency=0
  tmpdir="/tmp/rand_${node//[:.]/_}_$RANDOM"; mkdir -p "$tmpdir"
  for r in $(seq 1 "$ROUNDS"); do
    C=$(shuf -i 3-8 -n 1)
    echo "[round $r] concurrency=$C"
    pids=()
    for i in $(seq 1 "$C"); do
      outfile="$tmpdir/${r}_$i.out"
      ( env VPS_RPC_ADDR="$node" ./tools/test_client >"$outfile" 2>&1 || true ) &
      pids+=($!)
    done
    for pid in "${pids[@]}"; do wait "$pid" || true; done
    # 统计本轮
    for f in "$tmpdir"/${r}_*.out; do
      if grep -q "Status=200" "$f"; then
        ((ok++))
        if latency=$(grep "Cost=" "$f" | sed 's/.*Cost=\([0-9]*\).*/\1/'); then
          total_latency=$((total_latency + latency))
        fi
      else
        ((fail++))
      fi
    done
  done
  # 汇总
  avg=0; if (( ok>0 )); then avg=$(( total_latency / ok )); fi
  echo "[$node] 成功=$ok 失败=$fail 平均耗时=${avg}ms"
  echo "[$node] IP分布(Top10):"
  grep -h "IP=" "$tmpdir"/*.out 2>/dev/null | sed 's/.*IP=\([^ ]*\).*/\1/' | sort | uniq -c | sort -nr | head -n 10 || echo "  无数据"
  echo "[$node] IPv6 命中次数:"
  grep -h "IP=" "$tmpdir"/*.out 2>/dev/null | grep -E "\[[0-9a-fA-F:]+\]|:[0-9a-fA-F]" -o | wc -l || true
  rm -rf "$tmpdir"
done

echo "=== 全部完成 ==="


