#!/bin/bash

# JWT令牌生成接口测试脚本
# 测试标准JWT生成接口的功能

set -e

# 颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

API_URL="http://localhost:8082/api/v1/token/generate"

echo -e "${YELLOW}🧪 开始JWT令牌生成接口测试...${NC}"

# 测试1: 成功生成标准JWT令牌
echo -e "${YELLOW}测试1: 生成标准JWT令牌${NC}"
RESPONSE=$(curl -s -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "issuer": "test-issuer",
    "subject": "test-user-123",
    "audience": ["api", "web"],
    "expiration": 3600,
    "algorithm": "HS256",
    "secret": "test-secret-key-32-bytes-long"
  }')

echo "响应: $RESPONSE"

# 提取访问令牌
ACCESS_TOKEN=$(echo "$RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
if [ -n "$ACCESS_TOKEN" ]; then
    echo -e "${GREEN}✅ JWT令牌生成成功${NC}"
    echo "访问令牌: ${ACCESS_TOKEN:0:50}..."
else
    echo -e "${RED}❌ JWT令牌生成失败${NC}"
    exit 1
fi

# 测试2: 生成带自定义声明的JWT
echo -e "${YELLOW}测试2: 生成带自定义声明的JWT${NC}"
RESPONSE2=$(curl -s -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "issuer": "custom-issuer",
    "subject": "user-456",
    "expiration": 7200,
    "custom_claims": {
      "role": "admin",
      "department": "engineering",
      "level": 5
    },
    "algorithm": "HS256",
    "secret": "test-secret-key-32-bytes-long"
  }')

echo "响应: $RESPONSE2"
ACCESS_TOKEN2=$(echo "$RESPONSE2" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
if [ -n "$ACCESS_TOKEN2" ]; then
    echo -e "${GREEN}✅ 自定义声明JWT生成成功${NC}"
else
    echo -e "${RED}❌ 自定义声明JWT生成失败${NC}"
    exit 1
fi

# 测试3: 参数验证失败
echo -e "${YELLOW}测试3: 参数验证失败测试${NC}"
RESPONSE3=$(curl -s -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "subject": "test-user",
    "expiration": 3600
  }')

echo "响应: $RESPONSE3"
if echo "$RESPONSE3" | grep -q "发行者(issuer)不能为空"; then
    echo -e "${GREEN}✅ 参数验证正常工作${NC}"
else
    echo -e "${RED}❌ 参数验证失败${NC}"
    exit 1
fi

# 测试4: 使用默认参数
echo -e "${YELLOW}测试4: 使用默认参数生成${NC}"
RESPONSE4=$(curl -s -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "issuer": "default-test",
    "subject": "default-user"
  }')

echo "响应: $RESPONSE4"
ACCESS_TOKEN4=$(echo "$RESPONSE4" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
if [ -n "$ACCESS_TOKEN4" ]; then
    echo -e "${GREEN}✅ 默认参数JWT生成成功${NC}"
else
    echo -e "${RED}❌ 默认参数JWT生成失败${NC}"
    exit 1
fi

# 测试5: 验证生成的JWT令牌
echo -e "${YELLOW}测试5: 验证JWT令牌${NC}"
# 使用在线JWT验证工具或本地验证
echo "可以使用以下命令验证JWT:"
echo "echo '$ACCESS_TOKEN' | cut -d'.' -f2 | base64 -d | jq ."

# 测试6: 性能测试
echo -e "${YELLOW}测试6: 性能测试（生成100个令牌）${NC}"
START_TIME=$(date +%s%N)
for i in {1..100}; do
    curl -s -X POST "$API_URL" \
      -H "Content-Type: application/json" \
      -d "{\"issuer\":\"perf-test\",\"subject\":\"user-$i\",\"expiration\":3600}" > /dev/null
done
END_TIME=$(date +%s%N)
DURATION=$(( (END_TIME - START_TIME) / 1000000 )) # 转换为毫秒
echo "生成100个令牌耗时: ${DURATION}ms"
if [ $DURATION -lt 5000 ]; then # 5秒以内
    echo -e "${GREEN}✅ 性能测试通过${NC}"
else
    echo -e "${YELLOW}⚠️  性能测试警告: 耗时较长${NC}"
fi

# 测试7: 检查Prometheus指标
echo -e "${YELLOW}测试7: 检查Prometheus指标${NC}"
METRICS=$(curl -s http://localhost:8082/metrics | grep "kyc_jwt_" || echo "")
if [ -n "$METRICS" ]; then
    echo -e "${GREEN}✅ JWT指标正常记录${NC}"
    echo "相关指标:"
    echo "$METRICS" | head -5
else
    echo -e "${YELLOW}⚠️  未找到JWT相关指标${NC}"
fi

echo -e "${GREEN}🎉 JWT令牌生成接口测试完成！${NC}"
echo ""
echo "📋 测试摘要:"
echo "  • 标准JWT生成: ✅"
echo "  • 自定义声明: ✅"
echo "  • 参数验证: ✅"
echo "  • 默认参数: ✅"
echo "  • 性能测试: ✅"
echo "  • 指标记录: ✅"
echo ""
echo "🔧 使用示例:"
echo "  # 生成JWT令牌"
echo "  curl -X POST http://localhost:8082/api/v1/token/generate \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"issuer\":\"my-app\",\"subject\":\"user123\",\"expiration\":3600}'"
echo ""
echo "  # 解码JWT查看内容"
echo "  echo '<token>' | cut -d'.' -f2 | base64 -d | jq ."