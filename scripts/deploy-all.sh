#!/bin/bash

# KYC 服务一键部署脚本
# 集成 Kong 路由注册和 Grafana 面板导入

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "🚀 KYC 服务一键部署开始..."
echo "📁 项目目录: $PROJECT_DIR"

# 1. 检查依赖服务
echo "🔍 检查依赖服务状态..."

# 检查 Kong
if ! curl -s http://localhost:8001 > /dev/null; then
    echo "❌ Kong 未运行，请确保 Kong 已启动"
    echo "   检查路径: /Users/Documents/project/go/kong/docker-compose.yml"
    exit 1
fi
echo "✅ Kong 运行正常"

# 检查 Prometheus
if ! curl -s http://localhost:9090/-/healthy > /dev/null; then
    echo "❌ Prometheus 未运行，请确保 Prometheus 已启动"
    echo "   检查路径: /Users/bytedance/Documents/project/go/monitor_2/enterprise-monitoring/docker-compose.yml"
    exit 1
fi
echo "✅ Prometheus 运行正常"

# 检查 Grafana
if ! curl -s http://localhost:3000/api/health > /dev/null; then
    echo "❌ Grafana 未运行，请确保 Grafana 已启动"
    echo "   检查路径: /Users/bytedance/Documents/project/go/monitor_2/enterprise-monitoring/docker-compose.yml"
    exit 1
fi
echo "✅ Grafana 运行正常"

# 2. 构建和启动 KYC 服务
echo ""
echo "🏗️  构建 KYC 服务..."
cd "$PROJECT_DIR"

# 构建 Docker 镜像
docker build -t kyc-service:latest .

# 3. 启动基础设施（PostgreSQL + Redis）
echo ""
echo "🗄️  启动基础设施..."
docker-compose up -d postgres redis

# 等待数据库启动
echo "⏳ 等待数据库启动..."
sleep 10

# 4. 启动 KYC 服务
echo ""
echo "🚀 启动 KYC 服务..."
docker-compose up -d kyc-service

# 等待服务启动
echo "⏳ 等待 KYC 服务启动..."
sleep 15

# 5. 注册 Kong 路由
echo ""
echo "🌐 注册 Kong 路由..."
"$SCRIPT_DIR/setup-kong.sh"

# 6. 导入 Grafana 面板
echo ""
echo "📊 导入 Grafana 面板..."
"$SCRIPT_DIR/import-grafana.sh"

# 7. 验证部署
echo ""
echo "🔍 验证部署状态..."

# 检查服务健康状态
if curl -s http://localhost:8080/health > /dev/null; then
    echo "✅ KYC 服务运行正常"
else
    echo "❌ KYC 服务未正常运行"
    exit 1
fi

# 检查 Kong 路由
if curl -s http://localhost:8000/api/v1/kyc/status/test -H "Authorization: Bearer dummy" | grep -q "401"; then
    echo "✅ Kong 路由配置正确（返回401表示JWT认证生效）"
else
    echo "⚠️  Kong 路由可能需要手动验证"
fi

# 8. 显示访问信息
echo ""
echo "🎉 部署完成！"
echo ""
echo "📋 服务访问信息："
echo "  • KYC 服务: http://localhost:8080"
echo "  • Kong 网关: http://localhost:8000"
echo "  • Kong Admin: http://localhost:8001"
echo "  • Prometheus: http://localhost:9090"
echo "  • Grafana: http://localhost:3000 (admin/admin)"
echo ""
echo "🔗 API 端点："
echo "  • 认证: POST http://localhost:8000/api/v1/auth/token"
echo "  • OCR: POST http://localhost:8000/api/v1/kyc/ocr"
echo "  • 人脸识别: POST http://localhost:8000/api/v1/kyc/face/verify"
echo "  • 活体检测: WS http://localhost:8000/api/v1/kyc/liveness/ws"
echo "  • 完整KYC: POST http://localhost:8000/api/v1/kyc/verify"
echo "  • 状态查询: GET http://localhost:8000/api/v1/kyc/status/{id}"
echo ""
echo "📊 监控面板："
echo "  • Grafana 面板: http://localhost:3000/d/kyc-service"
echo "  • Prometheus 指标: http://localhost:9090/targets"
echo ""
echo "🧪 测试命令："
echo "  # 获取访问令牌"
echo "  curl -X POST http://localhost:8000/api/v1/auth/token \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"client_id\":\"kyc-client\",\"client_secret\":\"kyc-client-secret\",\"grant_type\":\"client_credentials\"}'"
echo ""
echo "  # 健康检查"
echo "  curl http://localhost:8080/health"
echo ""
echo "📚 更多信息请参考："
echo "  • 项目文档: $PROJECT_DIR/README.md"
echo "  • 完整Demo: $PROJECT_DIR/demo-complete.md"
echo "  • Kong 配置: $PROJECT_DIR/kong-config.md"