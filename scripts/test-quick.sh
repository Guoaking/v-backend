#!/bin/bash

# 快速测试脚本
# 只运行基本的代码检查，适合开发时快速验证

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "======================================"
echo "⚡ 快速测试模式"
echo "======================================"
echo ""

# 1. 代码格式检查
echo -e "${BLUE}[1/3] 代码格式检查...${NC}"
if gofmt -l . | grep -q .; then
    echo -e "${RED}❌ 代码格式错误，请运行: gofmt -w .${NC}"
    exit 1
else
    echo -e "${GREEN}✅ 格式检查通过${NC}"
fi
echo ""

# 2. 静态分析
echo -e "${BLUE}[2/3] 静态分析...${NC}"
if go vet ./...; then
    echo -e "${GREEN}✅ 静态分析通过${NC}"
else
    echo -e "${RED}❌ 静态分析失败${NC}"
    exit 1
fi
echo ""

# 3. 单元测试
echo -e "${BLUE}[3/3] 单元测试...${NC}"
if go test ./...; then
    echo -e "${GREEN}✅ 单元测试通过${NC}"
else
    echo -e "${RED}❌ 单元测试失败${NC}"
    exit 1
fi
echo ""

echo -e "${GREEN}🎉 快速测试全部通过！${NC}"
