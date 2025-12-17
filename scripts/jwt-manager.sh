#!/bin/bash

# JWT 令牌生成和管理工具
# 用于生成和验证JWT令牌，支持内部服务通信

set -e

# JWT配置
JWT_SECRET="internal-service-secret"
JWT_KEY="internal-service-key"
JWT_ALGORITHM="HS256"
JWT_EXPIRATION=86400  # 24小时

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 帮助信息
show_help() {
    echo "JWT 令牌管理工具"
    echo ""
    echo "用法: $0 [命令] [选项]"
    echo ""
    echo "命令:"
    echo "  generate <consumer>    为指定消费者生成JWT令牌"
    echo "  verify <token>          验证JWT令牌"
    echo "  decode <token>          解码JWT令牌（不验证）"
    echo "  list-consumers          列出所有JWT消费者"
    echo "  test-kong <token>       使用令牌测试Kong API"
    echo ""
    echo "选项:"
    echo "  --expiry <seconds>      设置令牌过期时间（默认: 86400秒）"
    echo "  --scope <scope>         设置令牌作用域"
    echo "  --claims <json>         添加自定义声明"
    echo ""
    echo "示例:"
    echo "  $0 generate internal-service"
    echo "  $0 generate admin-client --expiry 3600 --scope 'admin:read admin:write'"
    echo "  $0 verify eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
    echo "  $0 test-kong eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9... --endpoint /api/v1/kyc/status"
}

# 安装依赖检查
check_dependencies() {
    if ! command -v jq &> /dev/null; then
        echo -e "${RED}❌ 需要安装 jq 工具${NC}"
        echo "请运行: brew install jq (macOS) 或 apt-get install jq (Ubuntu)"
        exit 1
    fi
    
    if ! command -v openssl &> /dev/null; then
        echo -e "${RED}❌ 需要安装 openssl 工具${NC}"
        exit 1
    fi
}

# Base64 URL编码
base64_url_encode() {
    echo -n "$1" | openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# Base64 URL解码
base64_url_decode() {
    local len=$((${#1} % 4))
    local result="$1"
    if [ $len -eq 2 ]; then
        result="$1=="
    elif [ $len -eq 3 ]; then
        result="$1="
    fi
    echo "$result" | tr '-_' '+/' | openssl base64 -d -A 2>/dev/null || echo "$result" | tr '-_' '+/' | base64 -d 2>/dev/null
}

# 生成JWT签名
generate_signature() {
    local header="$1"
    local payload="$2"
    local secret="$3"
    
    local message="${header}.${payload}"
    echo -n "$message" | openssl dgst -sha256 -hmac "$secret" -binary | openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# 生成JWT令牌
generate_jwt() {
    local consumer="$1"
    local expiry="${2:-$JWT_EXPIRATION}"
    local scope="$3"
    local custom_claims="$4"
    
    local now=$(date +%s)
    local exp=$((now + expiry))
    local nbf=$now
    local iat=$now
    
    # 根据消费者设置密钥
    case "$consumer" in
        "internal-service")
            JWT_KEY="internal-service-key"
            JWT_SECRET="internal-service-secret"
            ;;
        "admin-client")
            JWT_KEY="admin-client-key"
            JWT_SECRET="admin-client-secret"
            ;;
        *)
            echo -e "${RED}❌ 未知的消费者: $consumer${NC}"
            echo "支持的消费者: internal-service, admin-client"
            exit 1
            ;;
    esac
    
    # JWT Header
    local header="{\"alg\":\"$JWT_ALGORITHM\",\"typ\":\"JWT\",\"kid\":\"$JWT_KEY\"}"
    local header_encoded=$(base64_url_encode "$header")
    
    # JWT Payload
    local payload="{\"iss\":\"$JWT_KEY\",\"iat\":$iat,\"nbf\":$nbf,\"exp\":$exp"
    
    if [ -n "$scope" ]; then
        payload="$payload,\"scope\":\"$scope\""
    fi
    
    # 添加自定义声明
    if [ -n "$custom_claims" ]; then
        payload="$payload,$custom_claims"
    fi
    
    payload="$payload}"
    local payload_encoded=$(base64_url_encode "$payload")
    
    # 生成签名
    local signature=$(generate_signature "$header_encoded" "$payload_encoded" "$JWT_SECRET")
    
    # 组合JWT
    local jwt="${header_encoded}.${payload_encoded}.${signature}"
    
    echo -e "${GREEN}✅ JWT令牌生成成功！${NC}"
    echo ""
    echo "令牌信息:"
    echo "  消费者: $consumer"
    echo "  过期时间: $(date -r $exp '+%Y-%m-%d %H:%M:%S')"
    echo "  作用域: ${scope:-无}"
    echo ""
    echo "JWT令牌:"
    echo "$jwt"
    echo ""
    echo "使用示例:"
    echo "  curl -X POST http://localhost:8000/api/v1/kyc/verify \\"
    echo "    -H 'Authorization: Bearer $jwt' \\"
    echo "    -H 'Idempotency-Key: test-key-123' \\"
    echo "    -F 'idcard_image=@idcard.jpg'"
}

# 验证JWT令牌
verify_jwt() {
    local token="$1"
    
    # 分割JWT
    local parts=($(echo "$token" | tr '.' ' '))
    if [ ${#parts[@]} -ne 3 ]; then
        echo -e "${RED}❌ 无效的JWT格式${NC}"
        exit 1
    fi
    
    local header_encoded="${parts[0]}"
    local payload_encoded="${parts[1]}"
    local signature_provided="${parts[2]}"
    
    # 解码header获取kid
    local header=$(base64_url_decode "$header_encoded" | jq -r .)
    local kid=$(echo "$header" | jq -r .kid)
    
    if [ "$kid" == "null" ] || [ -z "$kid" ]; then
        echo -e "${RED}❌ JWT中缺少kid字段${NC}"
        exit 1
    fi
    
    # 根据kid选择密钥
    case "$kid" in
        "internal-service-key")
            JWT_SECRET="internal-service-secret"
            ;;
        "admin-client-key")
            JWT_SECRET="admin-client-secret"
            ;;
        *)
            echo -e "${RED}❌ 未知的kid: $kid${NC}"
            exit 1
            ;;
    esac
    
    # 验证签名
    local signature_calculated=$(generate_signature "$header_encoded" "$payload_encoded" "$JWT_SECRET")
    
    if [ "$signature_provided" != "$signature_calculated" ]; then
        echo -e "${RED}❌ JWT签名验证失败${NC}"
        exit 1
    fi
    
    # 解码payload
    local payload=$(base64_url_decode "$payload_encoded" | jq -r .)
    local exp=$(echo "$payload" | jq -r .exp)
    local now=$(date +%s)
    
    if [ "$exp" -lt "$now" ]; then
        echo -e "${RED}❌ JWT已过期${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✅ JWT验证成功！${NC}"
    echo ""
    echo "Header:"
    echo "$header" | jq .
    echo ""
    echo "Payload:"
    echo "$payload" | jq .
}

# 解码JWT（不验证）
decode_jwt() {
    local token="$1"
    
    local parts=($(echo "$token" | tr '.' ' '))
    if [ ${#parts[@]} -ne 3 ]; then
        echo -e "${RED}❌ 无效的JWT格式${NC}"
        exit 1
    fi
    
    echo -e "${BLUE}📋 JWT解码结果（未验证）:${NC}"
    echo ""
    echo "Header:"
    base64_url_decode "${parts[0]}" | jq .
    echo ""
    echo "Payload:"
    base64_url_decode "${parts[1]}" | jq .
}

# 列出JWT消费者
list_consumers() {
    echo -e "${BLUE}📋 JWT 消费者列表:${NC}"
    echo ""
    echo "消费者名称           | 密钥ID                  | 用途"
    echo "-------------------|------------------------|-------------------"
    echo "internal-service   | internal-service-key   | 内部服务通信"
    echo "admin-client       | admin-client-key       | 管理客户端"
    echo ""
    echo "密钥信息:"
    echo "  • internal-service-secret: 用于内部微服务间通信"
    echo "  • admin-client-secret: 用于管理后台API访问"
}

# 测试Kong API
test_kong_api() {
    local token="$1"
    local endpoint="${2:-/api/v1/kyc/status}"
    
    echo -e "${BLUE}🚀 测试 Kong API:${NC}"
    echo "端点: $endpoint"
    echo ""
    
    local response=$(curl -s -w "\n%{http_code}" \
        -X GET "http://localhost:8000$endpoint" \
        -H "Authorization: Bearer $token" \
        -H "X-Request-ID: test-$(date +%s)")
    
    local http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" -eq 200 ]; then
        echo -e "${GREEN}✅ API调用成功 (HTTP $http_code)${NC}"
        echo "响应:"
        echo "$body" | jq . 2>/dev/null || echo "$body"
    elif [ "$http_code" -eq 401 ]; then
        echo -e "${RED}❌ 认证失败 (HTTP $http_code)${NC}"
        echo "响应: $body"
    elif [ "$http_code" -eq 403 ]; then
        echo -e "${YELLOW}⚠️  权限不足 (HTTP $http_code)${NC}"
        echo "响应: $body"
    else
        echo -e "${RED}❌ API调用失败 (HTTP $http_code)${NC}"
        echo "响应: $body"
    fi
}

# 主函数
main() {
    check_dependencies
    
    case "${1:-}" in
        "generate")
            shift
            local consumer="$1"
            local expiry="$JWT_EXPIRATION"
            local scope=""
            local custom_claims=""
            
            while [[ $# -gt 0 ]]; do
                case $2 in
                    "--expiry")
                        expiry="$3"
                        shift 2
                        ;;
                    "--scope")
                        scope="$3"
                        shift 2
                        ;;
                    "--claims")
                        custom_claims="$3"
                        shift 2
                        ;;
                    *)
                        shift
                        ;;
                esac
            done
            
            generate_jwt "$consumer" "$expiry" "$scope" "$custom_claims"
            ;;
        "verify")
            verify_jwt "$2"
            ;;
        "decode")
            decode_jwt "$2"
            ;;
        "list-consumers")
            list_consumers
            ;;
        "test-kong")
            local token="$2"
            local endpoint=""
            
            while [[ $# -gt 2 ]]; do
                case $3 in
                    "--endpoint")
                        endpoint="$4"
                        shift 2
                        ;;
                    *)
                        shift
                        ;;
                esac
            done
            
            test_kong_api "$token" "$endpoint"
            ;;
        *)
            show_help
            ;;
    esac
}

main "$@"