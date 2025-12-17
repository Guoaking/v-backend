#!/bin/bash

# Kong双向鉴权插件安装脚本
# 安装和配置服务认证插件，实现网关与服务的双向鉴权

set -e

KONG_ADMIN_URL="http://localhost:8001"
KONG_PLUGINS_DIR="/usr/local/share/lua/5.1/kong/plugins"
PLUGIN_NAME="service-auth"
PLUGIN_VERSION="1.0.0"

echo "🚀 开始安装 Kong 双向鉴权插件..."

# 检查 Kong 是否运行
if ! curl -s "$KONG_ADMIN_URL" > /dev/null; then
    echo "❌ Kong Admin API 无法访问，请确保 Kong 正在运行"
    exit 1
fi

echo "✅ Kong Admin API 连接正常"

# 创建插件目录
echo "📁 创建插件目录..."
mkdir -p "$KONG_PLUGINS_DIR/$PLUGIN_NAME"

# 复制插件文件
echo "📦 复制插件文件..."
cp /Users/bytedance/Documents/project/go/d/scripts/kong-plugins/service-auth.lua "$KONG_PLUGINS_DIR/$PLUGIN_NAME/handler.lua"

# 创建插件schema文件
cat > "$KONG_PLUGINS_DIR/$PLUGIN_NAME/schema.lua" << 'EOF'
local typedefs = require "kong.db.schema.typedefs"

return {
  name = "service-auth",
  fields = {
    { protocols = typedefs.protocols_http },
    { config = {
        type = "record",
        fields = {
          { service_secret_key = { type = "string", required = true, referenceable = true }, },
          { service_name = { type = "string", required = true }, },
          { validate_timestamp = { type = "boolean", default = true }, },
          { timestamp_window = { type = "number", default = 300 }, },
          { enable_logging = { type = "boolean", default = true }, },
        },
      },
    },
  },
}
EOF

# 重启Kong以加载插件
echo "🔄 重启Kong以加载插件..."
docker-compose restart kong

# 等待Kong启动
echo "⏳ 等待Kong启动..."
sleep 10

# 验证插件是否加载成功
echo "🔍 验证插件是否加载成功..."
if curl -s "$KONG_ADMIN_URL/plugins/schema/$PLUGIN_NAME" > /dev/null; then
    echo "✅ 插件加载成功"
else
    echo "❌ 插件加载失败，请检查Kong日志"
    exit 1
fi

# 配置服务认证插件
echo "🔐 配置服务认证插件..."

# 获取KYC服务ID
KYC_SERVICE_ID=$(curl -s "$KONG_ADMIN_URL/services" | grep -o '"id":"[^"]*"' | grep -A1 "kyc-service" | tail -1 | cut -d'"' -f4)

if [ -z "$KYC_SERVICE_ID" ]; then
    echo "❌ 未找到KYC服务，请先运行kong-oauth2-jwt-setup.sh"
    exit 1
fi

# 为KYC服务添加服务认证插件
curl -s -X POST "$KONG_ADMIN_URL/services/$KYC_SERVICE_ID/plugins" \
  --data name="service-auth" \
  --data config.service_secret_key="kyc-service-secret-key-2024" \
  --data config.service_name="kyc-service" \
  --data config.validate_timestamp=true \
  --data config.timestamp_window=300 \
  --data config.enable_logging=true

# 为OAuth2认证服务添加服务认证插件（如果需要）
OAUTH_SERVICE_ID=$(curl -s "$KONG_ADMIN_URL/services" | grep -o '"id":"[^"]*"' | grep -A1 "oauth2-auth-service" | tail -1 | cut -d'"' -f4)

if [ -n "$OAUTH_SERVICE_ID" ]; then
    curl -s -X POST "$KONG_ADMIN_URL/services/$OAUTH_SERVICE_ID/plugins" \
      --data name="service-auth" \
      --data config.service_secret_key="oauth-service-secret-key-2024" \
      --data config.service_name="oauth2-auth-service" \
      --data config.validate_timestamp=true \
      --data config.timestamp_window=300 \
      --data config.enable_logging=true
fi

echo "✅ Kong 双向鉴权插件安装和配置完成！"
echo ""
echo "🔐 插件配置信息："
echo "  • KYC服务密钥: kyc-service-secret-key-2024"
echo "  • OAuth服务密钥: oauth-service-secret-key-2024"
echo "  • 时间戳窗口: 5分钟"
echo ""
echo "📋 安全特性："
echo "  • Kong到服务：请求添加Kong签名认证头"
echo "  • 服务到Kong：响应验证服务签名"
echo "  • 时间戳验证：防止重放攻击"
echo "  • 绕过检测：阻止直接访问后端服务"
echo ""
echo "🧪 测试命令："
echo "  # 正常通过Kong访问"
echo "  curl -X GET http://localhost:8000/api/v1/kyc/status/test123 \\"
echo "    -H 'Authorization: Bearer YOUR_TOKEN'"
echo ""
echo "  # 尝试直接访问后端服务（应该被拒绝）"
echo "  curl -X GET http://localhost:8082/api/v1/kyc/status/test123"
echo ""
echo "⚠️  注意：后端服务需要更新以支持双向鉴权验证"