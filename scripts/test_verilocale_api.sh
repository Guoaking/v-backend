#!/bin/bash

# Verilocale后端API测试脚本
# 测试用户认证、API密钥管理、组织管理等功能

set -e

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

API_BASE="http://localhost:8082/api/v1"
TEST_EMAIL="test@example.com"
TEST_PASSWORD="password123"
TEST_NAME="Test User"

# 存储变量
ACCESS_TOKEN=""
API_KEY=""
API_SECRET=""
ORG_ID=""

echo -e "${YELLOW}🚀 开始测试Verilocale后端API...${NC}"

# 1. 测试用户注册
echo -e "${YELLOW}1. 测试用户注册...${NC}"
REGISTER_RESPONSE=$(curl -s -X POST "${API_BASE}/auth/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"${TEST_NAME}\",
    \"email\": \"${TEST_EMAIL}\",
    \"password\": \"${TEST_PASSWORD}\",
    \"company\": \"Test Company\"
  }")

echo "注册响应: $REGISTER_RESPONSE"

# 2. 测试用户登录
echo -e "${YELLOW}2. 测试用户登录...${NC}"
LOGIN_RESPONSE=$(curl -s -X POST "${API_BASE}/auth/login" \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"${TEST_EMAIL}\",
    \"password\": \"${TEST_PASSWORD}\"
  }")

echo "登录响应: $LOGIN_RESPONSE"

# 提取访问令牌
ACCESS_TOKEN=$(echo $LOGIN_RESPONSE | grep -o '"token":"[^"]*' | grep -o '[^"]*$')
if [ -z "$ACCESS_TOKEN" ]; then
    echo -e "${RED}❌ 登录失败，无法获取访问令牌${NC}"
    exit 1
fi
echo -e "${GREEN}✅ 登录成功，获取到访问令牌${NC}"

# 3. 测试获取当前用户信息
echo -e "${YELLOW}3. 测试获取当前用户信息...${NC}"
ME_RESPONSE=$(curl -s -X GET "${API_BASE}/auth/me" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}")

echo "用户信息: $ME_RESPONSE"

# 4. 测试创建API密钥
echo -e "${YELLOW}4. 测试创建API密钥...${NC}"
KEY_RESPONSE=$(curl -s -X POST "${API_BASE}/keys" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test API Key",
    "scopes": ["ocr:read", "face:write"]
  }')

echo "API密钥创建响应: $KEY_RESPONSE"

# 提取API密钥和密钥
API_KEY=$(echo $KEY_RESPONSE | grep -o '"id":"[^"]*' | grep -o '[^"]*$')
API_SECRET=$(echo $KEY_RESPONSE | grep -o '"secret":"[^"]*' | grep -o '[^"]*$')
if [ -z "$API_KEY" ] || [ -z "$API_SECRET" ]; then
    echo -e "${RED}❌ API密钥创建失败${NC}"
    exit 1
fi
echo -e "${GREEN}✅ API密钥创建成功${NC}"

# 5. 测试获取API密钥列表
echo -e "${YELLOW}5. 测试获取API密钥列表...${NC}"
KEYS_LIST_RESPONSE=$(curl -s -X GET "${API_BASE}/keys" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}")

echo "API密钥列表: $KEYS_LIST_RESPONSE"

# 6. 测试获取当前组织信息
echo -e "${YELLOW}6. 测试获取当前组织信息...${NC}"
ORG_RESPONSE=$(curl -s -X GET "${API_BASE}/orgs/current" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}")

echo "组织信息: $ORG_RESPONSE"

# 7. 测试使用API密钥访问KYC接口
echo -e "${YELLOW}7. 测试使用API密钥访问KYC接口...${NC}"

# 创建一个简单的测试图片文件
echo "test image content" > /tmp/test_image.jpg

OCR_RESPONSE=$(curl -s -X POST "${API_BASE}/kyc/ocr" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Idempotency-Key: test-ocr-001" \
  -F "picture=@/tmp/test_image.jpg" \
  -F "id=idcard" \
  -F "language=auto")

echo "OCR响应: $OCR_RESPONSE"

# 8. 测试删除API密钥
echo -e "${YELLOW}8. 测试删除API密钥...${NC}"
DELETE_RESPONSE=$(curl -s -X DELETE "${API_BASE}/keys/${API_KEY}" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}")

echo "删除响应: $DELETE_RESPONSE"

# 9. 测试用户登出
echo -e "${YELLOW}9. 测试用户登出...${NC}"
LOGOUT_RESPONSE=$(curl -s -X POST "${API_BASE}/auth/logout" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}")

echo "登出响应: $LOGOUT_RESPONSE"

# 清理测试文件
rm -f /tmp/test_image.jpg

echo -e "${GREEN}✅ 所有测试完成！${NC}"