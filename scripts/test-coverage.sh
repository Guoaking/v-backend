#!/bin/bash

# 测试覆盖率报告脚本
# 生成详细的测试覆盖率报告

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "======================================"
echo "📊 测试覆盖率报告"
echo "======================================"
echo ""

# 创建覆盖率输出目录
COVERAGE_DIR="coverage"
mkdir -p "$COVERAGE_DIR"

# 删除旧的覆盖率文件
rm -f coverage.out
rm -f "$COVERAGE_DIR"/*.out

echo -e "${BLUE}运行测试并生成覆盖率数据...${NC}"
go test -coverprofile="$COVERAGE_DIR/coverage.out" ./... 2>&1 | tee "$COVERAGE_DIR/test.log"

if [ ! -f "$COVERAGE_DIR/coverage.out" ]; then
    echo -e "${RED}❌ 覆盖率文件生成失败${NC}"
    exit 1
fi

echo ""
echo -e "${BLUE}生成覆盖率报告...${NC}"

# 生成文本报告
echo ""
echo "======================================"
echo "📄 覆盖率统计"
echo "======================================"
go tool cover -func="$COVERAGE_DIR/coverage.out" | tee "$COVERAGE_DIR/coverage.txt"

# 计算总体覆盖率
TOTAL_COVERAGE=$(go tool cover -func="$COVERAGE_DIR/coverage.out" | grep total | awk '{print $3}')
echo ""
echo -e "${GREEN}总体覆盖率: $TOTAL_COVERAGE${NC}"

# 生成HTML报告
echo ""
echo -e "${BLUE}生成HTML覆盖率报告...${NC}"
go tool cover -html="$COVERAGE_DIR/coverage.out" -o "$COVERAGE_DIR/coverage.html"

echo ""
echo -e "${GREEN}✅ 覆盖率报告生成完成！${NC}"
echo ""
echo "报告文件位置:"
echo "  - 文本报告: $COVERAGE_DIR/coverage.txt"
echo "  - HTML报告: $COVERAGE_DIR/coverage.html"
echo "  - 测试日志: $COVERAGE_DIR/test.log"
echo ""

# 检查覆盖率是否达标
COVERAGE_PERCENT=$(echo ${TOTAL_COVERAGE%\%})
if (( $(echo "$COVERAGE_PERCENT < 50" | bc -l) )); then
    echo -e "${YELLOW}⚠️  覆盖率低于 50%，建议增加测试用例${NC}"
elif (( $(echo "$COVERAGE_PERCENT < 70" | bc -l) )); then
    echo -e "${YELLOW}⚠️  覆盖率低于 70%，建议继续完善测试${NC}"
else
    echo -e "${GREEN}✅ 覆盖率达到 $TOTAL_COVERAGE，测试覆盖良好！${NC}"
fi

echo ""
echo "提示: 使用浏览器打开 HTML 报告查看详细信息"
echo "  Linux: xdg-open $COVERAGE_DIR/coverage.html"
echo "  Mac: open $COVERAGE_DIR/coverage.html"
