#!/usr/bin/env bash
set -euo pipefail

NODES=("tile0.zeromaps.cn:4242" "tile3.zeromaps.cn:4242")
REQUESTS=100

cd "$(dirname "$0")/.."
go build -o tools/test_client ./cmd/single_fetch 2>&1 || true

test_node() {
    local node=$1
    local ok=0 fail=0
    local ips=()
    
    echo "[$node] 开始测试，共 $REQUESTS 个请求..."
    for i in $(seq 1 "$REQUESTS"); do
        if output=$(timeout 35 env VPS_RPC_ADDR="$node" ./tools/test_client 2>&1); then
            if echo "$output" | grep -q "Status=200"; then
                ((ok++))
                ip=$(echo "$output" | grep "IP=" | sed 's/.*IP=\([^ ]*\).*/\1/')
                if [ -n "$ip" ]; then
                    ips+=("$ip")
                fi
            else
                ((fail++))
            fi
        else
            ((fail++))
        fi
        if ((i % 25 == 0)); then
            echo "[$node] 进度: $i/$REQUESTS (成功=$ok 失败=$fail)"
        fi
        sleep 0.03
    done
    
    echo "[$node] 完成: 成功=$ok 失败=$fail"
    echo "[$node] IP分布:"
    printf '%s\n' "${ips[@]}" | sort | uniq -c | sort -nr | head -n 10
    echo ""
}

# 并行测试
for node in "${NODES[@]}"; do
    test_node "$node" &
done
wait

echo "=== 测试完成 ==="

