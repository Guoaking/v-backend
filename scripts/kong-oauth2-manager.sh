#!/bin/bash

# Kong OAuth2 客户端管理脚本
# 用于自动化创建和管理OAuth2客户端

KONG_ADMIN_URL="http://localhost:8001"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 函数：创建消费者和OAuth2客户端
create_oauth_client() {
    local username=$1
    local custom_id=$2
    local app_name=$3
    local scopes=$4
    
    echo -e "${YELLOW}创建OAuth2客户端: $username${NC}"
    
    # 1. 创建消费者
    consumer_response=$(curl -s -X POST "$KONG_ADMIN_URL/consumers" \
        -H "Content-Type: application/json" \
        -d "{\"username\": \"$username\", \"custom_id\": \"$custom_id\"}")
    
    consumer_id=$(echo $consumer_response | jq -r '.id // empty')
    if [ -z "$consumer_id" ]; then
        echo -e "${RED}创建消费者失败${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✓ 消费者创建成功: $consumer_id${NC}"
    
    # 2. 创建OAuth2凭证
    oauth_response=$(curl -s -X POST "$KONG_ADMIN_URL/consumers/$consumer_id/oauth2" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"$app_name\",
            \"redirect_uris\": [\"http://localhost:3000/callback\"]
        }")
    
    client_id=$(echo $oauth_response | jq -r '.client_id // empty')
    client_secret=$(echo $oauth_response | jq -r '.client_secret // empty')
    
    if [ -z "$client_id" ] || [ -z "$client_secret" ]; then
        echo -e "${RED}创建OAuth2凭证失败${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✓ OAuth2凭证创建成功${NC}"
    echo -e "  客户端ID: ${YELLOW}$client_id${NC}"
    echo -e "  客户端密钥: ${YELLOW}$client_secret${NC}"
    echo -e "  权限范围: ${YELLOW}$scopes${NC}"
    
    # 3. 创建消费者级别的限流插件
    curl -s -X POST "$KONG_ADMIN_URL/consumers/$consumer_id/plugins" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"rate-limiting\",
            \"config\": {
                \"minute\": 100,
                \"hour\": 1000,
                \"policy\": \"local\"
            }
        }" > /dev/null
    
    echo -e "${GREEN}✓ 限流配置完成${NC}"
    echo "----------------------------------------"
}

# 函数：创建不同权限级别的客户端
setup_clients() {
    echo -e "${YELLOW}=== 设置OAuth2客户端 ===${NC}"
    
    # 普通用户客户端（只读权限）
    create_oauth_client \
        "normal-user" \
        "user-001" \
        "普通用户应用" \
        "kyc:read"
    
    # 企业用户客户端（读写权限）
    create_oauth_client \
        "enterprise-user" \
        "enterprise-001" \
        "企业用户应用" \
        "kyc:read,kyc:write"
    
    # 管理员客户端（全部权限）
    create_oauth_client \
        "admin-user" \
        "admin-001" \
        "管理后台" \
        "kyc:read,kyc:write,admin"
}

# 函数：获取访问令牌
test_token() {
    local client_id=$1
    local client_secret=$2
    local scope=$3
    
    echo -e "${YELLOW}测试获取访问令牌...${NC}"
    
    token_response=$(curl -s -X POST "http://localhost:8000/api/v1/kyc/oauth2/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "grant_type=client_credentials" \
        -d "client_id=$client_id" \
        -d "client_secret=$client_secret" \
        -d "scope=$scope" \
        -d "provision_key=kyc-service-provision-key-2024")
    
    access_token=$(echo $token_response | jq -r '.access_token // empty')
    if [ -n "$access_token" ]; then
        echo -e "${GREEN}✓ 访问令牌获取成功${NC}"
        echo -e "  令牌: ${YELLOW}${access_token:0:20}...${NC}"
        echo $access_token > /tmp/kyc_access_token.txt
    else
        echo -e "${RED}✗ 获取访问令牌失败${NC}"
        echo $token_response
    fi
}

# 函数：测试API访问
test_api() {
    local endpoint=$1
    local expected_status=$2
    
    if [ -f /tmp/kyc_access_token.txt ]; then
        access_token=$(cat /tmp/kyc_access_token.txt)
        
        response=$(curl -s -w "\n%{http_code}" \
            -X GET "http://localhost:8000$endpoint" \
            -H "Authorization: Bearer $access_token")
        
        http_code=$(echo "$response" | tail -n1)
        body=$(echo "$response" | sed '$d')
        
        if [ "$http_code" = "$expected_status" ]; then
            echo -e "${GREEN}✓ API测试通过: $endpoint ($http_code)${NC}"
        else
            echo -e "${RED}✗ API测试失败: $endpoint (期望: $expected_status, 实际: $http_code)${NC}"
            echo $body
        fi
    else
        echo -e "${RED}✗ 未找到访问令牌${NC}"
    fi
}

# 主菜单
main_menu() {
    echo -e "${YELLOW}=== Kong OAuth2 管理工具 ===${NC}"
    echo "1. 创建OAuth2客户端"
    echo "2. 测试令牌获取"
    echo "3. 完整测试流程"
    echo "4. 查看已创建客户端"
    echo "5. 退出"
    echo -n "请选择操作: "
    read choice
    
    case $choice in
        1)
            setup_clients
            ;;
        2)
            echo -n "输入客户端ID: "
            read client_id
            echo -n "输入客户端密钥: "
            read client_secret
            echo -n "输入权限范围: "
            read scope
            test_token $client_id $client_secret $scope
            ;;
        3)
            setup_clients
            # 这里可以添加更多测试逻辑
            ;;
        4)
            curl -s http://localhost:8001/consumers | jq '.data[] | {username: .username, id: .id}'
            ;;
        5)
            exit 0
            ;;
        *)
            echo -e "${RED}无效选择${NC}"
            ;;
    esac
}

# 检查依赖
if ! command -v jq &> /dev/null; then
    echo -e "${RED}错误: 需要安装 jq${NC}"
    exit 1
fi

# 运行主菜单
main_menu