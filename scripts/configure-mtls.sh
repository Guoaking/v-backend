#!/bin/bash

# mTLS配置脚本
# 配置Kong网关和后端服务的双向TLS

set -e

KONG_ADMIN_URL="http://localhost:8001"
CERT_DIR="./certs"
KONG_CERT_DIR="${CERT_DIR}/kong"
SERVICE_CERT_DIR="${CERT_DIR}/service"
CA_CERT_DIR="${CERT_DIR}/ca"

echo "🔧 开始配置mTLS..."

# 检查证书是否存在
if [ ! -f "$CA_CERT_DIR/ca-cert.pem" ] || [ ! -f "$KONG_CERT_DIR/kong-cert.pem" ] || [ ! -f "$SERVICE_CERT_DIR/service-cert.pem" ]; then
    echo "❌ 证书文件不存在，请先运行generate-mtls-certs.sh"
    exit 1
fi

echo "✅ 证书文件检查通过"

# 配置Kong网关mTLS
echo "🌐 配置Kong网关mTLS..."

# 1. 上传CA证书到Kong
echo "📤 上传CA证书到Kong..."
CA_CERT_CONTENT=$(cat "$CA_CERT_DIR/ca-cert.pem" | sed ':a;N;$!ba;s/\n/\\n/g')
curl -s -X POST "$KONG_ADMIN_URL/ca_certificates" \
  --data "cert=$CA_CERT_CONTENT" \
  --data "tags[]=mtls" \
  --data "tags[]=kyc-service"

# 2. 获取KYC服务信息
echo "🔍 获取KYC服务信息..."
KYC_SERVICE_INFO=$(curl -s "$KONG_ADMIN_URL/services/kyc-service")
KYC_SERVICE_ID=$(echo "$KYC_SERVICE_INFO" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
KYC_SERVICE_HOST=$(echo "$KYC_SERVICE_INFO" | grep -o '"host":"[^"]*"' | head -1 | cut -d'"' -f4)
KYC_SERVICE_PORT=$(echo "$KYC_SERVICE_INFO" | grep -o '"port":[0-9]*' | head -1 | cut -d':' -f2)

if [ -z "$KYC_SERVICE_ID" ]; then
    echo "❌ 未找到KYC服务，请先运行kong-oauth2-jwt-setup.sh"
    exit 1
fi

echo "✅ KYC服务信息: ID=$KYC_SERVICE_ID, HOST=$KYC_SERVICE_HOST, PORT=$KYC_SERVICE_PORT"

# 3. 更新KYC服务配置，启用mTLS
echo "🔒 更新KYC服务配置，启用mTLS..."
curl -s -X PATCH "$KONG_ADMIN_URL/services/$KYC_SERVICE_ID" \
  --data "protocol=https" \
  --data "client_certificate.id=$KYC_SERVICE_ID" \
  --data "tls_verify=true" \
  --data "tls_verify_depth=2"

# 4. 创建mTLS路由（用于测试）
echo "🛣️ 创建mTLS测试路由..."
curl -s -X POST "$KONG_ADMIN_URL/services/$KYC_SERVICE_ID/routes" \
  --data "name=kyc-service-mtls" \
  --data "paths[]=/api/v1/kyc/mtls" \
  --data "strip_path=false" \
  --data "protocols[]=https"

# 5. 配置Kong客户端证书
echo "📄 配置Kong客户端证书..."
# 这里需要在docker-compose.yml中挂载证书文件

# 6. 配置服务认证插件（结合mTLS）
echo "🔐 配置服务认证插件（结合mTLS）..."
curl -s -X POST "$KONG_ADMIN_URL/services/$KYC_SERVICE_ID/plugins" \
  --data "name=service-auth" \
  --data "config.service_secret_key=kyc-service-secret-key-2024" \
  --data "config.service_name=kyc-service" \
  --data "config.validate_timestamp=true" \
  --data "config.timestamp_window=300" \
  --data "config.enable_logging=true"

# 7. 创建mTLS消费者（可选）
echo "👥 创建mTLS消费者..."
curl -s -X POST "$KONG_ADMIN_URL/consumers" \
  --data "username=mtls-client" \
  --data "custom_id=mtls_client_001"

# 上传客户端证书
curl -s -X POST "$KONG_ADMIN_URL/consumers/mtls-client/certificates" \
  --data "cert=@$SERVICE_CERT_DIR/service-cert.pem" \
  --data "key=@$SERVICE_CERT_DIR/service-key.pem" \
  --data "tags[]=mtls" \
  --data "tags[]=client"

# 8. 配置ACL（基于证书）
echo "🔒 配置ACL（基于证书）..."
curl -s -X POST "$KONG_ADMIN_URL/consumers/mtls-client/acls" \
  --data "group=mtls-clients"

# 9. 配置高级限流（基于证书）
echo "⚡ 配置高级限流（基于证书）..."
curl -s -X POST "$KONG_ADMIN_URL/services/$KYC_SERVICE_ID/plugins" \
  --data "name=rate-limiting-advanced" \
  --data "config.window_size=60" \
  --data "config.limit=1000" \
  --data "config.sync_rate=10" \
  --data "config.namespace=kyc-mtls" \
  --data "config.strategy=redis" \
  --data "config.redis.host=redis" \
  --data "config.redis.port=6379" \
  --data "config.redis.database=4"

echo "✅ mTLS配置完成！"
echo ""
echo "📋 配置摘要："
echo "  • CA证书已上传到Kong"
echo "  • KYC服务已启用mTLS"
echo "  • 服务认证插件已配置"
echo "  • mTLS消费者已创建"
echo "  • ACL和限流已配置"
echo ""
echo "🔧 下一步："
echo "  1. 更新docker-compose.yml，挂载证书文件到Kong容器"
echo "  2. 重启Kong服务"
echo "  3. 测试mTLS连接"
echo ""
echo "🧪 测试命令："
echo "  # 使用证书访问（应该成功）"
echo "  curl --cert $SERVICE_CERT_DIR/service-cert.pem \\"
echo "       --key $SERVICE_CERT_DIR/service-key.pem \\"
echo "       --cacert $CA_CERT_DIR/ca-cert.pem \\"
echo "       https://localhost:8443/api/v1/kyc/mtls/status/test123"
echo ""
echo "  # 不使用证书访问（应该失败）"
echo "  curl -k https://localhost:8443/api/v1/kyc/mtls/status/test123"