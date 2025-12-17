#!/bin/bash

# KYC服务快速启动脚本
echo "🚀 启动KYC服务环境..."

# 1. 启动基础设施
echo "📦 启动基础设施（Redis、Jaeger等）..."
docker-compose up -d redis jaeger

# 等待服务启动
echo "⏳ 等待服务启动..."
sleep 10

# 2. 启动KYC服务
echo "🔧 启动KYC服务..."
go run cmd/server/main.go &
KYC_PID=$!

# 3. 等待KYC服务启动
echo "⏳ 等待KYC服务启动..."
for i in {1..30}; do
    if curl -s http://localhost:8082/health > /dev/null; then
        echo "✅ KYC服务启动成功！"
        break
    fi
    echo "等待中... ($i/30)"
    sleep 2
done

# 4. 生成测试JWT令牌
echo "🔑 生成测试JWT令牌..."
JWT_RESPONSE=$(curl -s -X POST http://localhost:8082/api/v1/token/generate \
  -H "Content-Type: application/json" \
  -d '{
    "issuer": "test-app",
    "subject": "test-user",
    "audience": ["api", "web"],
    "expiration": 3600,
    "custom_claims": {
      "role": "admin",
      "department": "engineering"
    }
  }')

JWT_TOKEN=$(echo $JWT_RESPONSE | grep -o '"access_token":"[^"]*' | cut -d'"' -f4)

echo "✅ JWT令牌生成成功！"
echo "📝 Token: $JWT_TOKEN"

# 5. 测试OCR接口
echo "🧪 测试OCR接口..."
curl -X POST http://localhost:8082/api/v1/kyc/ocr \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Idempotency-Key: test-123" \
  -F "image=@/path/to/test/idcard.jpg" \
  -F "language=auto" \
  -v

echo ""
echo "🎉 环境启动完成！"
echo "📊 监控面板: http://localhost:16686 (Jaeger)"
echo "🔍 健康检查: curl http://localhost:8082/health"
echo "🔑 JWT测试: 已生成测试令牌"

# 保存PID以便后续关闭
echo "KYC服务PID: $KYC_PID"
echo "使用 'kill $KYC_PID' 停止服务"