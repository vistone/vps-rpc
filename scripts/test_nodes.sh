#!/usr/bin/env bash
set -euo pipefail

NODES=("tile0.zeromaps.cn:4242" "tile3.zeromaps.cn:4242")
URL="https://kh.google.com/rt/earth/PlanetoidMetadata"
REQUESTS_PER_NODE=100

cd "$(dirname "$0")/.."
go build -o single_fetch ./cmd/single_fetch >/dev/null 2>&1 || true

for node in "${NODES[@]}"; do
    echo "=== 测试节点: $node ==="
    (
        export VPS_RPC_ADDR="$node"
        ok=0
        fail=0
        for i in $(seq 1 "$REQUESTS_PER_NODE"); do
            if timeout 30 ./single_fetch >/tmp/test_${node//[:.]/_}_$i.out 2>&1; then
                if grep -q "Status=200" /tmp/test_${node//[:.]/_}_$i.out; then
                    ((ok++))
                else
                    ((fail++))
                fi
            else
                ((fail++))
            fi
        done
        echo "节点 $node: 成功=$ok 失败=$fail"
        # 统计IP分布
        echo "IP分布:"
        grep -h "IP=" /tmp/test_${node//[:.]/_}_*.out 2>/dev/null | sed 's/.*IP=\([^ ]*\).*/\1/' | sort | uniq -c | sort -nr | head -n 10 || true
    ) &
done

wait
echo "=== 测试完成 ==="

