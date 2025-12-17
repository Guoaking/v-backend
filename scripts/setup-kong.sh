#!/bin/bash

# Kong 路由自动注册脚本
# 用于将 KYC 服务注册到现有的 Kong Gateway

set -e

KONG_ADMIN_URL="http://localhost:8001"
KYC_SERVICE_URL="http://kyc-service:8080"

echo "🚀 开始注册 KYC 服务到 Kong Gateway..."

# 检查 Kong 是否运行
if ! curl -s "$KONG_ADMIN_URL" > /dev/null; then
    echo "❌ Kong Admin API 无法访问，请确保 Kong 正在运行"
    exit 1
fi

echo "✅ Kong Admin API 连接正常"

# 1. 创建 KYC 服务
echo "📋 创建 KYC 服务..."
curl -s -X POST "$KONG_ADMIN_URL/services" \
  --data name=kyc-service \
  --data url="$KYC_SERVICE_URL" \
  --data retries=3 \
  --data connect_timeout=5000 \
  --data write_timeout=30000 \
  --data read_timeout=30000

# 2. 创建认证服务（复用同一个后端）
echo "📋 创建认证服务..."
curl -s -X POST "$KONG_ADMIN_URL/services" \
  --data name=kyc-auth-service \
  --data url="$KYC_SERVICE_URL" \
  --data retries=3 \
  --data connect_timeout=5000 \
  --data write_timeout=30000 \
  --data read_timeout=30000

# 3. 注册 KYC API 路由
echo "🛣️  注册 KYC API 路由..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/routes" \
  --data name=kyc-api \
  --data paths[]=/api/v1/kyc \
  --data strip_path=false \
  --data preserve_host=true \
  --data protocols[]=http \
  --data protocols[]=https

# 4. 注册认证 API 路由
echo "🛣️  注册认证 API 路由..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-auth-service/routes" \
  --data name=kyc-auth \
  --data paths[]=/api/v1/auth \
  --data strip_path=false \
  --data preserve_host=true \
  --data protocols[]=http \
  --data protocols[]=https

# 5. 注册 WebSocket 路由（活体检测）
echo "🛣️  注册 WebSocket 路由..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/routes" \
  --data name=kyc-liveness-ws \
  --data paths[]=/api/v1/kyc/liveness/ws \
  --data strip_path=false \
  --data preserve_host=true \
  --data protocols[]=websocket

# 6. 配置限流插件（KYC API）
echo "⚡ 配置限流插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=rate-limiting \
  --data config.minute=6000 \
  --data config.policy=redis \
  --data config.redis_host=redis \
  --data config.redis_port=6379 \
  --data config.redis_timeout=2000 \
  --data config.redis_database=1 \
  --data config.hide_client_headers=false \
  --data config.error_code=429 \
  --data config.error_message="请求过于频繁，请稍后再试"

# 7. 配置 JWT 认证插件（KYC API）
echo "🔐 配置 JWT 认证插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=jwt \
  --data config.key_claim_name=iss \
  --data config.secret_is_base64=false \
  --data config.claims_to_verify=exp \
  --data config.anonymous= \
  --data config.run_on_preflight=true \
  --data config.maximum_expiration=86400

# 8. 配置 CORS 插件（全局）
echo "🌐 配置 CORS 插件..."
curl -s -X POST "$KONG_ADMIN_URL/plugins" \
  --data name=cors \
  --data config.origins="*" \
  --data config.methods="GET,POST,PUT,DELETE,OPTIONS" \
  --data config.headers="Accept,Accept-Version,Content-Length,Content-MD5,Content-Type,Date,Authorization,Idempotency-Key,X-Request-ID" \
  --data config.exposed_headers="X-Auth-Token" \
  --data config.credentials=true \
  --data config.max_age=3600 \
  --data config.preflight_continue=false

# 9. 配置 Prometheus 插件（全局）
echo "📊 配置 Prometheus 插件..."
curl -s -X POST "$KONG_ADMIN_URL/plugins" \
  --data name=prometheus \
  --data config.per_consumer=true \
  --data config.status_code_metrics=true \
  --data config.latency_metrics=true \
  --data config.bandwidth_metrics=true \
  --data config.upstream_health_metrics=true

# 10. 配置请求转换插件（添加请求ID）
echo "🆔 配置请求转换插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=request-transformer \
  --data config.add.headers="X-Request-ID:$request_id" \
  --data config.add.headers="X-Kong-Proxy:true" \
  --data config.add.headers="X-Forwarded-For:$remote_addr"

# 11. 配置响应转换插件（移除敏感头）
echo "🛡️  配置响应转换插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=response-transformer \
  --data config.remove.headers="Server" \
  --data config.remove.headers="X-Powered-By" \
  --data config.remove.headers="Via"

# 12. 配置 IP 限制插件（可选）
echo "🔒 配置 IP 限制插件..."
curl -s -X POST "$KONG_ADMIN_URL/services/kyc-service/plugins" \
  --data name=ip-restriction \
  --data config.whitelist=0.0.0.0/0 \
  --data config.deny=192.168.1.1

echo "✅ Kong 路由注册完成！"
echo ""
echo "📋 已注册的服务和路由："
echo "  • KYC API: http://localhost:8000/api/v1/kyc/*"
echo "  • 认证 API: http://localhost:8000/api/v1/auth/*"
echo "  • WebSocket: ws://localhost:8000/api/v1/kyc/liveness/ws"
echo ""
echo "🔧 已启用的插件："
echo "  • Rate Limiting (6000/分钟)"
echo "  • JWT Authentication"
echo "  • CORS"
echo "  • Prometheus Metrics"
echo "  • Request/Response Transformer"
echo "  • IP Restriction"
echo ""
echo "📊 监控地址："
echo "  • Kong Metrics: http://localhost:8001/metrics"
echo "  • KYC Metrics: http://localhost:8080/metrics"
echo "  • Grafana: http://localhost:3000"