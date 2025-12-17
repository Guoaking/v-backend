#!/bin/bash

# OAuth2 + JWT 认证测试脚本
# 用于验证Kong的OAuth2和JWT认证配置是否正确工作

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

KONG_URL="http://localhost:8000"
KONG_ADMIN_URL="http://localhost:8001"

# 测试配置
WEB_CLIENT_ID="kyc-web-client-id"
WEB_CLIENT_SECRET="kyc-web-client-secret"
MOBILE_CLIENT_ID="kyc-mobile-client-id"
MOBILE_CLIENT_SECRET="kyc-mobile-client-secret"
ADMIN_CLIENT_ID="admin-client-id"
ADMIN_CLIENT_SECRET="admin-client-secret"

# 帮助信息
show_help() {
    echo "OAuth2 + JWT 认证测试工具"
    echo ""
    echo "用法: $0 [测试类型]"
    echo ""
    echo "测试类型:"
    echo "  oauth2-full        完整的OAuth2认证流程测试"
    echo "  jwt-full           完整的JWT认证流程测试"
    echo "  mixed-auth         混合认证测试"
    echo "  performance        性能压力测试"
    echo "  health-check       健康检查测试"
    echo "  all                运行所有测试"
    echo ""
    echo "示例:"
    echo "  $0 oauth2-full"
    echo "  $0 jwt-full"
    echo "  $0 all"
}

# 检查依赖
check_dependencies() {
    if ! command -v curl &> /dev/null; then
        echo -e "${RED}❌ 需要安装 curl${NC}"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        echo -e "${RED}❌ 需要安装 jq${NC}"
        exit 1
    fi
}

# 检查服务状态
check_services() {
    echo -e "${BLUE}🔍 检查服务状态...${NC}"
    
    # 检查Kong
    if ! curl -s "$KONG_ADMIN_URL" > /dev/null; then
        echo -e "${RED}❌ Kong Admin API 无法访问${NC}"
        return 1
    fi
    echo -e "${GREEN}✅ Kong Admin API 正常${NC}"
    
    # 检查KYC服务
    if ! curl -s "$KONG_URL/health" > /dev/null 2>&1; then
        echo -e "${YELLOW}⚠️  KYC服务健康检查端点未响应${NC}"
    else
        echo -e "${GREEN}✅ KYC服务正常${NC}"
    fi
    
    return 0
}

# OAuth2客户端凭证认证测试
test_oauth2_client_credentials() {
    echo -e "${BLUE}🧪 测试OAuth2客户端凭证认证...${NC}"
    
    local client_id="$1"
    local client_secret="$2"
    local scope="$3"
    local client_name="$4"
    
    echo "客户端: $client_name ($client_id)"
    
    # 获取访问令牌
    local token_response=$(curl -s -w "\n%{http_code}" \
        -X POST "$KONG_URL/oauth2/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=$client_id" \
        -d "client_secret=$client_secret" \
        -d "grant_type=client_credentials" \
        -d "scope=$scope")
    
    local http_code=$(echo "$token_response" | tail -n1)
    local response=$(echo "$token_response" | sed '$d')
    
    if [ "$http_code" -eq 200 ]; then
        local access_token=$(echo "$response" | jq -r .access_token)
        local expires_in=$(echo "$response" | jq -r .expires_in)
        local token_type=$(echo "$response" | jq -r .token_type)
        
        echo -e "${GREEN}✅ 令牌获取成功${NC}"
        echo "  访问令牌: ${access_token:0:20}..."
        echo "  过期时间: $expires_in 秒"
        echo "  令牌类型: $token_type"
        
        # 测试使用令牌访问API
        echo "  测试API访问..."
        local api_response=$(curl -s -w "\n%{http_code}" \
            -X GET "$KONG_URL/api/v1/kyc/status/test123" \
            -H "Authorization: Bearer $access_token" \
            -H "X-Request-ID: oauth2-test-$(date +%s)")
        
        local api_http_code=$(echo "$api_response" | tail -n1)
        local api_body=$(echo "$api_response" | sed '$d')
        
        if [ "$api_http_code" -eq 200 ] || [ "$api_http_code" -eq 404 ]; then
            echo -e "${GREEN}  ✅ API访问成功 (HTTP $api_http_code)${NC}"
        else
            echo -e "${RED}  ❌ API访问失败 (HTTP $api_http_code)${NC}"
            echo "  响应: $api_body"
        fi
        
        return 0
    else
        echo -e "${RED}❌ 令牌获取失败 (HTTP $http_code)${NC}"
        echo "响应: $response"
        return 1
    fi
}

# JWT认证测试
test_jwt_authentication() {
    echo -e "${BLUE}🧪 测试JWT认证...${NC}"
    
    # 生成内部服务JWT令牌
    echo "生成内部服务JWT令牌..."
    local jwt_token=$(/Users/bytedance/Documents/project/go/d/scripts/jwt-manager.sh generate internal-service --scope "kyc:read kyc:write" | grep "JWT令牌:" | cut -d' ' -f3)
    
    if [ -z "$jwt_token" ]; then
        echo -e "${RED}❌ JWT令牌生成失败${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✅ JWT令牌生成成功${NC}"
    echo "令牌: ${jwt_token:0:50}..."
    
    # 测试使用JWT访问API
    echo "测试JWT API访问..."
    local api_response=$(curl -s -w "\n%{http_code}" \
        -X GET "$KONG_URL/api/v1/kyc/status/test456" \
        -H "Authorization: Bearer $jwt_token" \
        -H "X-Request-ID: jwt-test-$(date +%s)")
    
    local api_http_code=$(echo "$api_response" | tail -n1)
    local api_body=$(echo "$api_response" | sed '$d')
    
    if [ "$api_http_code" -eq 200 ] || [ "$api_http_code" -eq 404 ]; then
        echo -e "${GREEN}✅ JWT API访问成功 (HTTP $api_http_code)${NC}"
        return 0
    else
        echo -e "${RED}❌ JWT API访问失败 (HTTP $api_http_code)${NC}"
        echo "响应: $api_body"
        return 1
    fi
}

# 混合认证测试
test_mixed_authentication() {
    echo -e "${BLUE}🧪 测试混合认证场景...${NC}"
    
    # 1. OAuth2认证
    echo "1. OAuth2认证测试..."
    local oauth2_token=$(curl -s -X POST "$KONG_URL/oauth2/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=$WEB_CLIENT_ID" \
        -d "client_secret=$WEB_CLIENT_SECRET" \
        -d "grant_type=client_credentials" \
        -d "scope=kyc:read" | jq -r .access_token)
    
    if [ "$oauth2_token" != "null" ] && [ -n "$oauth2_token" ]; then
        echo -e "${GREEN}✅ OAuth2认证成功${NC}"
    else
        echo -e "${RED}❌ OAuth2认证失败${NC}"
        return 1
    fi
    
    # 2. JWT认证
    echo "2. JWT认证测试..."
    local jwt_token=$(/Users/bytedance/Documents/project/go/d/scripts/jwt-manager.sh generate internal-service --scope "admin:read" 2>/dev/null | grep "JWT令牌:" | cut -d' ' -f3)
    
    if [ -n "$jwt_token" ]; then
        echo -e "${GREEN}✅ JWT认证成功${NC}"
    else
        echo -e "${RED}❌ JWT认证失败${NC}"
        return 1
    fi
    
    # 3. 无认证访问（应该失败）
    echo "3. 无认证访问测试（应该失败）..."
    local no_auth_response=$(curl -s -w "\n%{http_code}" \
        -X GET "$KONG_URL/api/v1/kyc/status/test" \
        -H "X-Request-ID: no-auth-test-$(date +%s)")
    
    local no_auth_code=$(echo "$no_auth_response" | tail -n1)
    
    if [ "$no_auth_code" -eq 401 ]; then
        echo -e "${GREEN}✅ 无认证访问正确拒绝 (HTTP $no_auth_code)${NC}"
    else
        echo -e "${RED}❌ 无认证访问异常 (HTTP $no_auth_code)${NC}"
        return 1
    fi
    
    return 0
}

# 性能压力测试
test_performance() {
    echo -e "${BLUE}🚀 性能压力测试...${NC}"
    
    # 获取OAuth2令牌
    local access_token=$(curl -s -X POST "$KONG_URL/oauth2/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=$WEB_CLIENT_ID" \
        -d "client_secret=$WEB_CLIENT_SECRET" \
        -d "grant_type=client_credentials" \
        -d "scope=kyc:read" | jq -r .access_token)
    
    if [ "$access_token" == "null" ] || [ -z "$access_token" ]; then
        echo -e "${RED}❌ 无法获取访问令牌${NC}"
        return 1
    fi
    
    echo "进行10次并发请求测试..."
    
    # 并发请求测试
    for i in {1..10}; do
        (
            local response=$(curl -s -w "\n%{http_code}" \
                -X GET "$KONG_URL/api/v1/kyc/status/perf-test-$i" \
                -H "Authorization: Bearer $access_token" \
                -H "X-Request-ID: perf-test-$i-$(date +%s)" \
                -o /dev/null)
            
            local http_code=$(echo "$response" | tail -n1)
            
            if [ "$http_code" -eq 200 ] || [ "$http_code" -eq 404 ]; then
                echo -n "."
            else
                echo -n "x"
            fi
        ) &
    done
    
    wait
    echo ""
    echo -e "${GREEN}✅ 性能测试完成${NC}"
    
    return 0
}

# 健康检查测试
test_health_check() {
    echo -e "${BLUE}🏥 健康检查测试...${NC}"
    
    # Kong健康检查
    local kong_health=$(curl -s -w "\n%{http_code}" "$KONG_ADMIN_URL/status")
    local kong_code=$(echo "$kong_health" | tail -n1)
    
    if [ "$kong_code" -eq 200 ]; then
        echo -e "${GREEN}✅ Kong健康状态正常${NC}"
    else
        echo -e "${RED}❌ Kong健康检查失败 (HTTP $kong_code)${NC}"
        return 1
    fi
    
    # 认证插件状态检查
    echo "认证插件状态检查..."
    local oauth2_plugins=$(curl -s "$KONG_ADMIN_URL/plugins" | jq '.data[] | select(.name == "oauth2")' | jq -s length)
    local jwt_plugins=$(curl -s "$KONG_ADMIN_URL/plugins" | jq '.data[] | select(.name == "jwt")' | jq -s length)
    
    if [ "$oauth2_plugins" -gt 0 ]; then
        echo -e "${GREEN}✅ OAuth2插件已启用 ($oauth2_plugins 个)${NC}"
    else
        echo -e "${RED}❌ OAuth2插件未启用${NC}"
    fi
    
    if [ "$jwt_plugins" -gt 0 ]; then
        echo -e "${GREEN}✅ JWT插件已启用 ($jwt_plugins 个)${NC}"
    else
        echo -e "${RED}❌ JWT插件未启用${NC}"
    fi
    
    return 0
}

# 运行所有测试
run_all_tests() {
    echo -e "${BLUE}🎯 运行完整认证测试套件...${NC}"
    echo "========================================"
    
    local failed_tests=0
    local total_tests=0
    
    # 检查服务
    total_tests=$((total_tests + 1))
    if check_services; then
        echo -e "${GREEN}✅ 服务检查通过${NC}"
    else
        echo -e "${RED}❌ 服务检查失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    echo ""
    
    # OAuth2测试
    total_tests=$((total_tests + 1))
    if test_oauth2_client_credentials "$WEB_CLIENT_ID" "$WEB_CLIENT_SECRET" "kyc:read kyc:write" "Web客户端"; then
        echo -e "${GREEN}✅ OAuth2 Web客户端测试通过${NC}"
    else
        echo -e "${RED}❌ OAuth2 Web客户端测试失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    echo ""
    
    total_tests=$((total_tests + 1))
    if test_oauth2_client_credentials "$ADMIN_CLIENT_ID" "$ADMIN_CLIENT_SECRET" "admin:read admin:write" "管理客户端"; then
        echo -e "${GREEN}✅ OAuth2管理客户端测试通过${NC}"
    else
        echo -e "${RED}❌ OAuth2管理客户端测试失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    echo ""
    
    # JWT测试
    total_tests=$((total_tests + 1))
    if test_jwt_authentication; then
        echo -e "${GREEN}✅ JWT认证测试通过${NC}"
    else
        echo -e "${RED}❌ JWT认证测试失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    echo ""
    
    # 混合认证测试
    total_tests=$((total_tests + 1))
    if test_mixed_authentication; then
        echo -e "${GREEN}✅ 混合认证测试通过${NC}"
    else
        echo -e "${RED}❌ 混合认证测试失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    echo ""
    
    # 性能测试
    total_tests=$((total_tests + 1))
    if test_performance; then
        echo -e "${GREEN}✅ 性能测试通过${NC}"
    else
        echo -e "${RED}❌ 性能测试失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    echo ""
    
    # 健康检查
    total_tests=$((total_tests + 1))
    if test_health_check; then
        echo -e "${GREEN}✅ 健康检查通过${NC}"
    else
        echo -e "${RED}❌ 健康检查失败${NC}"
        failed_tests=$((failed_tests + 1))
    fi
    
    echo "========================================"
    echo -e "测试总结:"
    echo -e "总测试数: $total_tests"
    echo -e "通过: $((total_tests - failed_tests))"
    echo -e "失败: $failed_tests"
    
    if [ $failed_tests -eq 0 ]; then
        echo -e "${GREEN}🎉 所有测试通过！OAuth2 + JWT集成配置成功${NC}"
        return 0
    else
        echo -e "${RED}❌ 部分测试失败，请检查配置${NC}"
        return 1
    fi
}

# 主函数
main() {
    check_dependencies
    
    case "${1:-}" in
        "oauth2-full")
            check_services
            test_oauth2_client_credentials "$WEB_CLIENT_ID" "$WEB_CLIENT_SECRET" "kyc:read kyc:write" "Web客户端"
            test_oauth2_client_credentials "$ADMIN_CLIENT_ID" "$ADMIN_CLIENT_SECRET" "admin:read admin:write" "管理客户端"
            ;;
        "jwt-full")
            check_services
            test_jwt_authentication
            ;;
        "mixed-auth")
            check_services
            test_mixed_authentication
            ;;
        "performance")
            check_services
            test_performance
            ;;
        "health-check")
            test_health_check
            ;;
        "all")
            run_all_tests
            ;;
        *)
            show_help
            ;;
    esac
}

main "$@"