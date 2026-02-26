#!/bin/bash

# 综合CI测试脚本
# 执行所有代码质量检查和测试

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 统计变量
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# 测试函数
run_test() {
    local test_name=$1
    local test_command=$2

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "${BLUE}[$TOTAL_TESTS] 测试: $test_name${NC}"

    if eval "$test_command"; then
        echo -e "${GREEN}✅ $test_name 通过${NC}"
        PASSED_TESTS=$((PASSED_TESTS + 1))
        return 0
    else
        echo -e "${RED}❌ $test_name 失败${NC}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        return 1
    fi
}

echo "======================================"
echo "🔍 开始本地CI测试..."
echo "======================================"
echo ""

# 1. 代码格式检查
echo "======================================"
echo "📝 代码质量检查"
echo "======================================"
echo ""

run_test "代码格式检查" "gofmt -l . | grep -q . && exit 1 || exit 0"
echo ""

# 2. 静态分析
echo "======================================"
echo "🔬 静态分析"
echo "======================================"
echo ""

run_test "Go静态分析" "go vet ./..."
echo ""

# 3. 单元测试
echo "======================================"
echo "🧪 单元测试"
echo "======================================"
echo ""

run_test "运行所有单元测试" "go test ./..."
echo ""

# 4. 竞态检测
echo "======================================"
echo "⚡ 并发安全测试"
echo "======================================"
echo ""

run_test "竞态检测" "go test -race ./..."
echo ""

# 5. 构建检查
echo "======================================"
echo "🏗️  构建检查"
echo "======================================"
echo ""

run_test "构建所有包" "go build ./..."
echo ""

# 6. 构建主程序
echo "======================================"
echo "📦 主程序构建"
echo "======================================"
echo ""

run_test "构建服务端程序" "go build -o /tmp/kyc-service-test ./cmd/server/main.go"
echo ""

# 7. 检查敏感信息泄露
echo "======================================"
echo "🔒 安全检查"
echo "======================================"
echo ""

run_test "敏感信息检查" "! grep -r 'password\|secret\|token' --include='*.go' . | grep -v 'test\|Test\|example' | grep -q ."
echo ""

# 清理临时文件
if [ -f /tmp/kyc-service-test ]; then
    rm -f /tmp/kyc-service-test
fi

# 输出测试结果
echo "======================================"
echo "📊 测试结果汇总"
echo "======================================"
echo ""
echo -e "总测试数: $TOTAL_TESTS"
echo -e "${GREEN}通过: $PASSED_TESTS${NC}"
if [ $FAILED_TESTS -gt 0 ]; then
    echo -e "${RED}失败: $FAILED_TESTS${NC}"
else
    echo -e "${GREEN}失败: $FAILED_TESTS${NC}"
fi
echo ""

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}🎉 所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}❌ 有 $FAILED_TESTS 个测试失败${NC}"
    exit 1
fi
