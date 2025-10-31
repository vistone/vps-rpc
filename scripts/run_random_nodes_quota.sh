#!/usr/bin/env bash
set -euo pipefail

# 对每个节点总请求数=300；每轮并发随机3-8，直到达到配额
NODES=("tile0.zeromaps.cn:4242" "tile3.zeromaps.cn:4242")
TOTAL=300

cd "$(dirname "$0")/.."
go build -o tools/test_client ./cmd/single_fetch >/dev/null 2>&1 || true

for node in "${NODES[@]}"; do
  echo "=== 节点: $node (quota=$TOTAL) ==="
  ok=0; fail=0; total_latency=0; done_count=0
  tmpdir="/tmp/randq_${node//[:.]/_}_$RANDOM"; mkdir -p "$tmpdir"
  while (( done_count < TOTAL )); do
    C=$(shuf -i 3-8 -n 1)
    # 若剩余不足一轮并发，截断至剩余数
    remain=$(( TOTAL - done_count ))
    if (( C > remain )); then C=$remain; fi
    echo "[round $((done_count+1))..$((done_count+C))] concurrency=$C"
    pids=()
    for i in $(seq 1 "$C"); do
      outfile="$tmpdir/$((done_count+i)).out"
      ( env VPS_RPC_ADDR="$node" ./tools/test_client >"$outfile" 2>&1 || true ) &
      pids+=($!)
    done
    for pid in "${pids[@]}"; do wait "$pid" || true; done
    for i in $(seq 1 "$C"); do
      f="$tmpdir/$((done_count+i)).out"
      if grep -q "Status=200" "$f" 2>/dev/null; then
        ((ok++))
        if latency=$(grep "Cost=" "$f" | sed 's/.*Cost=\([0-9]*\).*/\1/'); then
          total_latency=$((total_latency + latency))
        fi
      else
        ((fail++))
      fi
    done
    done_count=$((done_count + C))
    if (( done_count % 50 == 0 )); then
      echo "  进度: $done_count/$TOTAL (成功=$ok 失败=$fail)"
    fi
  done
  avg=0; if (( ok>0 )); then avg=$(( total_latency / ok )); fi
  echo ""
  echo "[$node] 总结: 成功=$ok 失败=$fail 平均耗时=${avg}ms"
  echo "[$node] IP分布(Top15):"
  grep -h "IP=" "$tmpdir"/*.out 2>/dev/null | sed 's/.*IP=\([^ ]*\).*/\1/' | sort | uniq -c | sort -nr | head -n 15 || echo "  无数据"
  rm -rf "$tmpdir"
  echo ""
done

echo "=== 全部完成 ==="


