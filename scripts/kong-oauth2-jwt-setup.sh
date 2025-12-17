#!/bin/bash

# Kong OAuth2 + JWT 集成配置脚本
# 用于配置Kong的OAuth2和JWT认证，实现完整的API安全保护

set -e

KONG_ADMIN_URL="http://localhost:8001"
KYC_SERVICE_URL="http://kyc-service:8080"

echo "🚀 开始配置 Kong OAuth2 + JWT 集成..."

# 检查 Kong 是否运行
if ! curl -s "$KONG_ADMIN_URL" > /dev/null; then
    echo "❌ Kong Admin API 无法访问，请确保 Kong 正在运行"
    exit 1
fi

echo "✅ Kong Admin API 连接正常"

# 1. 创建 OAuth2 认证服务（独立的认证服务）
echo "📋 创建 OAuth2 认证服务..."
curl -s -X POST "$KONG_ADMIN_URL/services" \
  --data name=oauth2-auth-service \
  --data url="$KYC_SERVICE_URL" \
  --data retries=3 \
  --data connect_timeout=5000 \
  --data write_timeout=30000 \
  --data read_timeout=30000

# 2. 创建 OAuth2 认证路由
echo "🛣️ 创建 OAuth2 认证路由..."
curl -s -X POST "$KONG_ADMIN_URL/services/oauth2-auth-service/routes" \
  --data name=oauth2-auth \
  --data paths[]=/oauth2 \
  --data paths[]=/api/v1/oauth2 \
  --data strip_path=false \
  --data preserve_host=true \
  --data protocols[]=http \
  --data protocols[]=https

# 3. 配置 OAuth2 插件（全局）
echo "🔐 配置 OAuth2 插件..."
curl -s -X POST "$KONG_ADMIN_URL/plugins" \
  --data name=oauth2 \
  --data config.scopes="kyc:read,kyc:write,admin:read,admin:write" \
  --data config.mandatory_scope=true \
  --data config.enable_authorization_code=true \
  --data config.enable_client_credentials=true \
  --data config.enable_implicit_grant=false \
  --data config.enable_password_grant=false \
  --data config.accept_http_if_already_terminated=true \
  --data config.auth_header_name=authorization \
  --data config.anonymous= \
  --data config.global_credentials=true \
  --data config.refresh_token_ttl=2592000 \
  --data config.access_token_ttl=7200

# 4. 创建 OAuth2 客户端应用
echo "👥 创建 OAuth2 客户端应用..."
curl -s -X POST "$KONG_ADMIN_URL/consumers" \
  --data username=kyc-web-client \
  --data custom_id=kyc_web_client_001

curl -s -X POST "$KONG_ADMIN_URL/consumers/kyc-web-client/oauth2" \
  --data name="KYC Web Application" \
  --data client_id="kyc-web-client-id" \
  --data client_secret="kyc-web-client-secret" \
  --data redirect_uris[]="http://localhost:3000/callback" \
  --data redirect_uris[]="http://localhost:8080/callback"

curl -s -X POST "$KONG_ADMIN_URL/consumers" \
  --data username=kyc-mobile-client \
  --data custom_id=kyc_mobile_client_001

curl -s -X POST "$KONG_ADMIN_URL/consumers/kyc-mobile-client/oauth2" \
  --data name="KYC Mobile Application" \
  --data client_id="kyc-mobile-client-id" \
  --data client_secret="kyc-mobile-client-secret" \
  --data redirect_uris[]="http://localhost:3001/callback" \
  --data redirect_uris[]="kycapp://callback"

# 5. 配置 JWT 插件（用于内部服务通信）
echo "🔑 配置 JWT 插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=jwt \
  --data config.key_claim_name=iss \
  --data config.secret_is_base64=false \
  --data config.claims_to_verify=exp,nbf \
  --data config.maximum_expiration=86400 \
  --data config.run_on_preflight=true

# 6. 创建 JWT 消费者（内部服务）
echo "👥 创建 JWT 消费者..."
curl -s -X POST "$KONG_ADMIN_URL/consumers" \
  --data username=internal-service \
  --data custom_id=internal_service_001

curl -s -X POST "$KONG_ADMIN_URL/consumers/internal-service/jwt" \
  --data key="internal-service-key" \
  --data secret="internal-service-secret" \
  --data algorithm="HS256"

# 7. 创建管理员消费者
curl -s -X POST "$KONG_ADMIN_URL/consumers" \
  --data username=admin-client \
  --data custom_id=admin_client_001

curl -s -X POST "$KONG_ADMIN_URL/consumers/admin-client/oauth2" \
  --data name="KYC Admin Application" \
  --data client_id="admin-client-id" \
  --data client_secret="admin-client-secret" \
  --data redirect_uris[]="http://localhost:3002/callback"

curl -s -X POST "$KONG_ADMIN_URL/consumers/admin-client/jwt" \
  --data key="admin-client-key" \
  --data secret="admin-client-secret" \
  --data algorithm="HS256"

# 8. 配置 ACL 插件（权限控制）
echo "🔒 配置 ACL 插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=acl \
  --data config.whitelist="kyc-users,admin-users" \
  --data config.hide_groups_header=true

# 9. 为消费者添加 ACL 组
curl -s -X POST "$KONG_ADMIN_URL/consumers/kyc-web-client/acls" \
  --data group="kyc-users"

curl -s -X POST "$KONG_ADMIN_URL/consumers/kyc-mobile-client/acls" \
  --data group="kyc-users"

curl -s -X POST "$KONG_ADMIN_URL/consumers/admin-client/acls" \
  --data group="admin-users"

curl -s -X POST "$KONG_ADMIN_URL/consumers/internal-service/acls" \
  --data group="admin-users"

# 10. 配置高级限流（基于消费者）
echo "⚡ 配置高级限流..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=rate-limiting-advanced \
  --data config.window_size="60" \
  --data config.limit="6000" \
  --data config.sync_rate=10 \
  --data config.namespace=kyc-service \
  --data config.strategy=redis \
  --data config.redis.host=redis \
  --data config.redis.port=6379 \
  --data config.redis.database=2

# 11. 配置 OAuth2 特定的限流
curl -s -X POST "$KONG_ADMIN_URL/plugins" \
  --data name=rate-limiting-advanced \
  --data config.window_size="60" \
  --data config.limit="1000" \
  --data config.sync_rate=10 \
  --data config.namespace=oauth2-global \
  --data config.strategy=redis \
  --data config.redis.host=redis \
  --data config.redis.port=6379 \
  --data config.redis.database=3

echo "✅ Kong OAuth2 + JWT 集成配置完成！"
echo ""
echo "📋 已创建的应用和消费者："
echo "  • Web客户端: kyc-web-client-id / kyc-web-client-secret"
echo "  • 移动端客户端: kyc-mobile-client-id / kyc-mobile-client-secret"  
echo "  • 管理客户端: admin-client-id / admin-client-secret"
echo "  • 内部服务: internal-service-key / internal-service-secret"
echo ""
echo "🔐 认证端点："
echo "  • OAuth2 令牌: http://localhost:8000/oauth2/token"
echo "  • OAuth2 授权: http://localhost:8000/oauth2/authorize"
echo ""
echo "📊 权限组："
echo "  • kyc-users: 普通KYC用户权限"
echo "  • admin-users: 管理员权限"
echo ""
echo "🚀 使用示例："
echo "  # 获取访问令牌（客户端凭证）"
echo "  curl -X POST http://localhost:8000/oauth2/token \\"
echo "    -H 'Content-Type: application/x-www-form-urlencoded' \\"
echo "    -d 'client_id=kyc-web-client-id' \\"
echo "    -d 'client_secret=kyc-web-client-secret' \\"
echo "    -d 'grant_type=client_credentials' \\"
echo "    -d 'scope=kyc:read kyc:write'"
echo ""
echo "  # 使用令牌访问KYC API"
echo "  curl -X POST http://localhost:8000/api/v1/kyc/ocr \\"
echo "    -H 'Authorization: Bearer YOUR_ACCESS_TOKEN' \\"
echo "    -H 'Idempotency-Key: unique-key-123' \\"
echo "    -F 'image=@idcard.jpg'"
echo ""
echo "  # 内部服务JWT认证"
echo "  curl -X POST http://localhost:8000/api/v1/kyc/verify \\"
echo "    -H 'Authorization: Bearer YOUR_JWT_TOKEN' \\"
echo "    -H 'Idempotency-Key: unique-key-456' \\"
echo "    -F 'idcard_image=@idcard.jpg' \\"
echo "    -F 'face_image=@face.jpg'"