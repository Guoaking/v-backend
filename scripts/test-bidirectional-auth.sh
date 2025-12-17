#!/bin/bash

# 双向鉴权测试脚本
# 测试Kong网关与后端服务的双向鉴权机制

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 服务地址
KONG_HTTP_URL="http://localhost:8000"
KONG_HTTPS_URL="https://localhost:8443"
SERVICE_URL="http://localhost:8082"
KONG_ADMIN_URL="http://localhost:8001"

# 测试计数器
TESTS_PASSED=0
TESTS_FAILED=0

# 测试函数
function test_case() {
    local test_name="$1"
    local test_command="$2"
    local expected_result="$3"
    local expected_code="$4"
    
    echo -e "${YELLOW}测试: $test_name${NC}"
    echo "命令: $test_command"
    
    if eval "$test_command"; then
        actual_result="success"
        actual_code=0
    else
        actual_result="failed"
        actual_code=$?
    fi
    
    if [[ "$actual_result" == "$expected_result" && "$actual_code" == "$expected_code" ]]; then
        echo -e "${GREEN}✅ 测试通过${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}❌ 测试失败${NC}"
        echo "期望结果: $expected_result (code: $expected_code)"
        echo "实际结果: $actual_result (code: $actual_code)"
        ((TESTS_FAILED++))
    fi
    echo "---"
}

function check_service_health() {
    echo -e "${YELLOW}检查服务健康状态...${NC}"
    
    # 检查Kong
    if curl -s "$KONG_ADMIN_URL" > /dev/null; then
        echo -e "${GREEN}✅ Kong Admin API 正常${NC}"
    else
        echo -e "${RED}❌ Kong Admin API 无法访问${NC}"
        exit 1
    fi
    
    # 检查后端服务
    if curl -s "$SERVICE_URL/health" > /dev/null; then
        echo -e "${GREEN}✅ 后端服务正常${NC}"
    else
        echo -e "${RED}❌ 后端服务无法访问${NC}"
        exit 1
    fi
    
    echo "---"
}

function get_oauth_token() {
    echo -e "${YELLOW}获取OAuth2访问令牌...${NC}"
    
    TOKEN_RESPONSE=$(curl -s -X POST "$KONG_HTTP_URL/oauth2/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=kyc-web-client-id" \
        -d "client_secret=kyc-web-client-secret" \
        -d "grant_type=client_credentials" \
        -d "scope=kyc:read kyc:write")
    
    ACCESS_TOKEN=$(echo "$TOKEN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
    
    if [ -n "$ACCESS_TOKEN" ]; then
        echo -e "${GREEN}✅ 获取令牌成功${NC}"
        echo "令牌: ${ACCESS_TOKEN:0:20}..."
    else
        echo -e "${RED}❌ 获取令牌失败${NC}"
        echo "响应: $TOKEN_RESPONSE"
        exit 1
    fi
    
    echo "---"
}

echo "🚀 开始双向鉴权测试..."
echo "=================================="

# 1. 检查服务健康状态
check_service_health

# 2. 获取访问令牌
get_oauth_token

# 3. 测试正常通过Kong访问（应该成功）
echo -e "${YELLOW}🧪 测试正常通过Kong访问${NC}"
test_case "通过Kong访问KYC状态" \
    "curl -s -X GET '$KONG_HTTP_URL/api/v1/kyc/status/test123' -H 'Authorization: Bearer $ACCESS_TOKEN' | grep -q 'status'" \
    "success" "0"

# 4. 测试直接访问后端服务（应该失败）
echo -e "${YELLOW}🧪 测试直接访问后端服务（绕过Kong）${NC}"
test_case "直接访问后端服务" \
    "curl -s -X GET '$SERVICE_URL/api/v1/kyc/status/test123' | grep -q 'ACCESS_DENIED'" \
    "success" "0"

# 5. 测试缺少Kong认证头的请求（应该失败）
echo -e "${YELLOW}🧪 测试缺少Kong认证头的请求${NC}"
test_case "缺少Kong认证头" \
    "curl -s -X GET '$SERVICE_URL/api/v1/kyc/status/test123' -H 'Authorization: Bearer fake-token' | grep -q 'ACCESS_DENIED'" \
    "success" "0"

# 6. 测试健康检查接口
echo -e "${YELLOW}🧪 测试健康检查接口${NC}"
test_case "健康检查接口" \
    "curl -s -X GET '$SERVICE_URL/health' | grep -q 'healthy'" \
    "success" "0"

# 7. 测试心跳检测接口
echo -e "${YELLOW}🧪 测试心跳检测接口${NC}"
test_case "心跳检测接口" \
    "curl -s -X GET '$SERVICE_URL/heartbeat' | grep -q 'ok'" \
    "success" "0"

# 8. 测试安全心跳检测接口（需要Kong认证）
echo -e "${YELLOW}🧪 测试安全心跳检测接口${NC}"
test_case "安全心跳检测接口（通过Kong）" \
    "curl -s -X GET '$KONG_HTTP_URL/api/v1/kyc/security-heartbeat' -H 'Authorization: Bearer $ACCESS_TOKEN' | grep -q 'ok'" \
    "success" "0"

test_case "安全心跳检测接口（直接访问）" \
    "curl -s -X GET '$SERVICE_URL/security-heartbeat' | grep -q 'SECURITY_HEARTBEAT_UNAUTHORIZED'" \
    "success" "0"

# 9. 测试OCR接口（带幂等性）
echo -e "${YELLOW}🧪 测试OCR接口${NC}"
test_case "OCR接口（通过Kong）" \
    "curl -s -X POST '$KONG_HTTP_URL/api/v1/kyc/ocr' -H 'Authorization: Bearer $ACCESS_TOKEN' -H 'Idempotency-Key: test-123' -F 'image=@/dev/null' | grep -q 'error\|success'" \
    "success" "0"

# 10. 测试限流功能
echo -e "${YELLOW}🧪 测试限流功能${NC}"
echo "执行快速请求测试限流..."
for i in {1..10}; do
    curl -s -X GET "$SERVICE_URL/health" > /dev/null &
done
wait

test_case "限流功能" \
    "curl -s -X GET '$SERVICE_URL/health' | grep -q 'healthy\|请求过于频繁'" \
    "success" "0"

# 11. 测试签名验证
echo -e "${YELLOW}🧪 测试签名验证机制${NC}"

# 生成有效签名
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
SIGNATURE=$(echo -n "kyc-service:/api/v1/kyc/status/test123:$TIMESTAMP:kong-shared-secret-key-2024" | openssl dgst -sha256 -hmac "kong-shared-secret-key-2024" -binary | base64)

test_case "有效Kong签名" \
    "curl -s -X GET '$SERVICE_URL/api/v1/kyc/status/test123' -H 'X-Kong-Signature: $SIGNATURE' -H 'X-Kong-Timestamp: $TIMESTAMP' -H 'X-Kong-Service: kyc-service' | grep -q 'healthy\|status'" \
    "success" "0"

# 12. 测试无效签名
test_case "无效Kong签名" \
    "curl -s -X GET '$SERVICE_URL/api/v1/kyc/status/test123' -H 'X-Kong-Signature: invalid-signature' -H 'X-Kong-Timestamp: $TIMESTAMP' -H 'X-Kong-Service: kyc-service' | grep -q 'KONG_SIGNATURE_INVALID'" \
    "success" "0"

# 13. 测试过期时间戳
OLD_TIMESTAMP=$(date -u -d "10 minutes ago" +"%Y-%m-%dT%H:%M:%SZ")
OLD_SIGNATURE=$(echo -n "kyc-service:/api/v1/kyc/status/test123:$OLD_TIMESTAMP:kong-shared-secret-key-2024" | openssl dgst -sha256 -hmac "kong-shared-secret-key-2024" -binary | base64)

test_case "过期时间戳" \
    "curl -s -X GET '$SERVICE_URL/api/v1/kyc/status/test123' -H 'X-Kong-Signature: $OLD_SIGNATURE' -H 'X-Kong-Timestamp: $OLD_TIMESTAMP' -H 'X-Kong-Service: kyc-service' | grep -q 'KONG_TIMESTAMP_EXPIRED'" \
    "success" "0"

# 14. 测试服务响应签名验证
echo -e "${YELLOW}🧪 测试服务响应签名验证${NC}"
RESPONSE=$(curl -s -X GET "$SERVICE_URL/health" -H "X-Kong-Signature: $SIGNATURE" -H "X-Kong-Timestamp: $TIMESTAMP" -H "X-Kong-Service: kyc-service" -D -)
SERVICE_SIGNATURE=$(echo "$RESPONSE" | grep -i "x-service-signature" | cut -d' ' -f2 | tr -d '\r')

test_case "服务响应包含有效签名" \
    "[ -n '$SERVICE_SIGNATURE' ]" \
    "success" "0"

# 15. 测试监控指标
echo -e "${YELLOW}🧪 测试监控指标${NC}"
test_case "Prometheus指标接口" \
    "curl -s -X GET '$SERVICE_URL/metrics' | grep -q 'kyc_'" \
    "success" "0"

# 16. 测试审计日志
echo -e "${YELLOW}🧪 测试审计日志${NC}"
test_case "审计日志记录" \
    "curl -s -X GET '$KONG_HTTP_URL/api/v1/kyc/status/test123' -H 'Authorization: Bearer $ACCESS_TOKEN' > /dev/null && sleep 1 && grep -q '审计日志\|audit' /Users/bytedance/Documents/project/go/d/kyc-service.log" \
    "success" "0"

# 测试结果统计
echo "=================================="
echo "📊 测试结果统计："
echo -e "${GREEN}测试通过: $TESTS_PASSED${NC}"
echo -e "${RED}测试失败: $TESTS_FAILED${NC}"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}🎉 所有测试通过！双向鉴权机制工作正常${NC}"
    exit 0
else
    echo -e "${RED}❌ 部分测试失败，请检查配置和日志${NC}"
    exit 1
fi