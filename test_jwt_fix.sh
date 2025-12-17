#!/bin/bash

# 测试修复后的JWT生成
echo "测试JWT生成器修复..."

# 构建服务
echo "构建服务..."
go build -o kyc-service cmd/server/main.go

# 生成测试JWT令牌
echo "生成测试JWT令牌..."
curl -X POST http://localhost:8082/api/v1/token/generate \
  -H "Content-Type: application/json" \
  -d '{
    "issuer": "test-app",
    "subject": "test-user-123",
    "audience": ["api", "web"],
    "expiration": 3600,
    "custom_claims": {
      "role": "admin",
      "department": "engineering"
    }
  }' \
  -v

echo ""
echo "测试完成！"