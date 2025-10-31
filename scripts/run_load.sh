#!/usr/bin/env bash
set -euo pipefail

# 随机并发 3-8，每轮执行一次，默认轮数 10
ROUNDS=${1:-10}
cd "$(dirname "$0")/.."

go build -o single_fetch ./cmd/single_fetch >/dev/null 2>&1 || true

for r in $(seq 1 "$ROUNDS"); do
  C=$(shuf -i 3-8 -n 1)
  echo "[round $r] concurrency=$C"
  for i in $(seq 1 "$C"); do
    ./single_fetch >/tmp/sf_$$_$i.out 2>&1 &
  done
  wait
  grep -h "Status=" /tmp/sf_$$_*.out 2>/dev/null | sort | uniq -c || true
  rm -f /tmp/sf_$$_*.out
done


