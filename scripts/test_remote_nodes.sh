#!/usr/bin/env bash
set -euo pipefail

NODES=("tile0.zeromaps.cn:4242" "tile3.zeromaps.cn:4242")
URL="https://kh.google.com/rt/earth/PlanetoidMetadata"
REQUESTS_PER_NODE=100

cd "$(dirname "$0")/.."
go build -o tools/test_client ./cmd/single_fetch 2>&1 || {
    echo "编译失败，使用 single_fetch"
    go build -o tools/test_client ./cmd/single_fetch
}

test_node() {
    local node=$1
    local output_dir="/tmp/test_${node//[:.]/_}"
    mkdir -p "$output_dir"
    rm -f "$output_dir"/req_*.out
    
    echo "[$node] 开始测试，共 $REQUESTS_PER_NODE 个请求..." >&2
    local ok=0 fail=0 total_latency=0
    
    for i in $(seq 1 "$REQUESTS_PER_NODE"); do
        local outfile="$output_dir/req_$i.out"
        if timeout 35 env VPS_RPC_ADDR="$node" ./tools/test_client >"$outfile" 2>&1; then
            if grep -q "Status=200" "$outfile"; then
                ((ok++))
                # 提取耗时
                if latency=$(grep "Cost=" "$outfile" | sed 's/.*Cost=\([0-9]*\).*/\1/'); then
                    total_latency=$((total_latency + latency))
                fi
            else
                ((fail++))
            fi
        else
            ((fail++))
        fi
        if ((i % 25 == 0)); then
            echo "[$node] 进度: $i/$REQUESTS_PER_NODE (成功=$ok 失败=$fail)" >&2
        fi
        sleep 0.05
    done
    
    local avg_latency=0
    if ((ok > 0)); then
        avg_latency=$((total_latency / ok))
    fi
    
    echo "[$node] 完成: 成功=$ok 失败=$fail 平均耗时=${avg_latency}ms" >&2
    echo "[$node] IP分布:" >&2
    grep -h "IP=" "$output_dir"/req_*.out 2>/dev/null | sed 's/.*IP=\([^ ]*\).*/\1/' | sort | uniq -c | sort -nr | head -n 10 || echo "  无数据" >&2
    echo "" >&2
}

# 并行测试两个节点
pids=()
for node in "${NODES[@]}"; do
    test_node "$node" &
    pids+=($!)
done

# 等待所有后台任务完成
for pid in "${pids[@]}"; do
    wait "$pid"
done

echo "=== 所有节点测试完成 ==="

