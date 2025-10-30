#!/bin/bash
# 测试脚本 - 运行所有测试并显示结果

set -e

echo "=========================================="
echo "运行 VPS-RPC 项目测试"
echo "=========================================="
echo ""

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 测试结果统计
PASSED=0
FAILED=0

# 运行测试函数
run_test() {
    local test_name=$1
    local test_path=$2
    
    echo -e "${YELLOW}运行测试: ${test_name}${NC}"
    
    if go test -v ${test_path} -timeout 120s; then
        echo -e "${GREEN}✓ ${test_name} 通过${NC}"
        ((PASSED++))
    else
        echo -e "${RED}✗ ${test_name} 失败${NC}"
        ((FAILED++))
    fi
    echo ""
}

# 1. 编译检查
echo -e "${YELLOW}检查编译...${NC}"
if go build ./...; then
    echo -e "${GREEN}✓ 编译成功${NC}"
else
    echo -e "${RED}✗ 编译失败${NC}"
    exit 1
fi
echo ""

# 2. 运行单元测试
echo "=========================================="
echo "单元测试"
echo "=========================================="
echo ""

run_test "服务器测试" "./server"
run_test "代理测试" "./proxy"
run_test "客户端测试" "./client"

# 3. 运行集成测试（可选，需要服务器运行）
echo "=========================================="
echo "集成测试（可选）"
echo "=========================================="
echo ""

if [ -d "integration_test" ]; then
    echo -e "${YELLOW}注意: 集成测试需要网络连接，可能会失败${NC}"
    if go test -v ./integration_test -timeout 180s 2>/dev/null; then
        echo -e "${GREEN}✓ 集成测试通过${NC}"
        ((PASSED++))
    else
        echo -e "${YELLOW}集成测试未运行或失败（可能是网络问题）${NC}"
        ((FAILED++))
    fi
    echo ""
fi

# 4. 显示测试结果
echo "=========================================="
echo "测试结果汇总"
echo "=========================================="
echo -e "通过: ${GREEN}${PASSED}${NC}"
echo -e "失败: ${RED}${FAILED}${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}部分测试失败${NC}"
    exit 1
fi

