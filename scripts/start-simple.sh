#!/bin/bash

# KYC服务快速启动脚本
# 简化配置，快速启动

echo "🚀 KYC服务快速启动..."

# 设置环境变量
export KYC_PORT=8083
export KYC_GIN_MODE=debug
export KYC_LOG_LEVEL=info
export KYC_DATABASE_HOST=localhost
export KYC_DATABASE_PORT=5432
export KYC_DATABASE_USER=kyc_user
export KYC_DATABASE_PASSWORD=kyc_password
export KYC_DATABASE_DBNAME=kyc_db
export KYC_DATABASE_SSLMODE=disable
export KYC_REDIS_HOST=localhost
export KYC_REDIS_PORT=6379
export KYC_REDIS_PASSWORD=""
export KYC_REDIS_DB=0
export KYC_SECURITY_JWT_SECRET="your-secret-key-here-must-be-32-by"
export KYC_SECURITY_ENCRYPTION_KEY="your-encryption-key-here-32-by"

# 检查端口是否被占用
if lsof -Pi :8083 -sTCP:LISTEN -t >/dev/null ; then
    echo "⚠️  端口8083已被占用，正在终止占用进程..."
    lsof -ti:8083 | xargs kill -9
    sleep 2
fi

# 启动服务
echo "🏗️  正在构建和启动KYC服务..."
go build -o kyc-service ./cmd/server/main.go

if [ $? -ne 0 ]; then
    echo "❌ 构建失败，请检查错误信息"
    exit 1
fi

echo "✅ 构建成功，启动服务..."
./kyc-service

echo "🎯 服务启动完成！"
echo "📊 健康检查: curl http://localhost:8083/health"
echo "📈 监控指标: curl http://localhost:8083/metrics"
echo "🌐 API文档: http://localhost:8083/swagger/index.html"