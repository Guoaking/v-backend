#!/bin/bash

# 双向鉴权完整部署脚本
# 一键部署和配置Kong网关与后端服务的双向鉴权机制

set -e

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🚀 开始双向鉴权完整部署...${NC}"

# 1. 检查环境
echo -e "${YELLOW}🔍 检查环境...${NC}"

# 检查Docker
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker未安装${NC}"
    exit 1
fi

# 检查Docker Compose
if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}❌ Docker Compose未安装${NC}"
    exit 1
fi

# 检查端口占用
for port in 8000 8001 8002 8443 8082 3000 9090; do
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo -e "${RED}❌ 端口 $port 已被占用${NC}"
        exit 1
    fi
done

echo -e "${GREEN}✅ 环境检查通过${NC}"

# 2. 启动基础设施
echo -e "${YELLOW}🏗️  启动基础设施...${NC}"
docker-compose up -d postgres redis kong prometheus grafana

# 等待服务启动
echo -e "${YELLOW}⏳ 等待基础设施启动...${NC}"
sleep 30

# 检查服务状态
services=("postgres" "redis" "kong" "prometheus" "grafana")
for service in "${services[@]}"; do
    if docker-compose ps | grep -q "$service.*Up"; then
        echo -e "${GREEN}✅ $service 已启动${NC}"
    else
        echo -e "${RED}❌ $service 启动失败${NC}"
        exit 1
    fi
done

# 3. 生成mTLS证书
echo -e "${YELLOW}🔐 生成mTLS证书...${NC}"
./scripts/generate-mtls-certs.sh

# 4. 配置Kong OAuth2 + JWT
echo -e "${YELLOW}🔑 配置Kong OAuth2 + JWT...${NC}"
./scripts/kong-oauth2-jwt-setup.sh

# 5. 安装双向鉴权插件
echo -e "${YELLOW}🔒 安装双向鉴权插件...${NC}"
./scripts/install-bidirectional-auth.sh

# 6. 配置mTLS
echo -e "${YELLOW}🌐 配置mTLS...${NC}"
./scripts/configure-mtls.sh

# 7. 构建和启动后端服务
echo -e "${YELLOW}🔧 构建和启动后端服务...${NC}"
go mod download
go build -o kyc-service cmd/server/main.go

# 启动服务
echo -e "${YELLOW}🚀 启动KYC服务...${NC}"
./kyc-service &
SERVICE_PID=$!

# 等待服务启动
sleep 10

# 检查服务状态
if curl -s http://localhost:8082/health > /dev/null; then
    echo -e "${GREEN}✅ KYC服务启动成功${NC}"
else
    echo -e "${RED}❌ KYC服务启动失败${NC}"
    kill $SERVICE_PID 2>/dev/null || true
    exit 1
fi

# 8. 运行测试
echo -e "${YELLOW}🧪 运行双向鉴权测试...${NC}"
if ./scripts/test-bidirectional-auth.sh; then
    echo -e "${GREEN}✅ 双向鉴权测试通过${NC}"
else
    echo -e "${RED}❌ 双向鉴权测试失败${NC}"
    kill $SERVICE_PID 2>/dev/null || true
    exit 1
fi

# 9. 配置监控告警
echo -e "${YELLOW}📊 配置监控告警...${NC}"

# 复制告警规则到Prometheus
docker cp prometheus/bidirectional-auth-alerts.yml prometheus:/etc/prometheus/rules/

# 重载Prometheus配置
curl -X POST http://localhost:9090/-/reload

# 10. 创建Grafana仪表板
echo -e "${YELLOW}📈 创建Grafana仪表板...${NC}"

# 创建双向鉴权监控仪表板
./scripts/create-bidirectional-auth-dashboard.sh

# 11. 输出部署结果
echo -e "${GREEN}🎉 双向鉴权部署完成！${NC}"
echo ""
echo "=================================="
echo "📋 部署摘要："
echo "  • Kong Admin API: http://localhost:8001"
echo "  • Kong Proxy: http://localhost:8000, https://localhost:8443"
echo "  • KYC Service: http://localhost:8082"
echo "  • Grafana: http://localhost:3000 (admin/admin123)"
echo "  • Prometheus: http://localhost:9090"
echo ""
echo "🔐 安全特性："
echo "  ✅ 双向鉴权已启用"
echo "  ✅ mTLS证书已配置"
echo "  ✅ OAuth2 + JWT认证已配置"
echo "  ✅ 限流和防绕过机制已启用"
echo "  ✅ 监控告警已配置"
echo ""
echo "🧪 测试命令："
echo "  # 获取访问令牌"
echo "  curl -X POST http://localhost:8000/oauth2/token \\"
echo "    -H 'Content-Type: application/x-www-form-urlencoded' \\"
echo "    -d 'client_id=kyc-web-client-id' \\"
echo "    -d 'client_secret=kyc-web-client-secret' \\"
echo "    -d 'grant_type=client_credentials' \\"
echo "    -d 'scope=kyc:read kyc:write'"
echo ""
echo "  # 访问KYC API"
echo "  curl -X GET http://localhost:8000/api/v1/kyc/status/test123 \\"
echo "    -H 'Authorization: Bearer YOUR_ACCESS_TOKEN'"
echo ""
echo "  # 测试绕过检测（应该失败）"
echo "  curl -X GET http://localhost:8082/api/v1/kyc/status/test123"
echo ""
echo "📊 监控仪表板："
echo "  • 访问 http://localhost:3000"
echo "  • 查看双向鉴权监控仪表板"
echo "  • 查看安全事件和告警"
echo ""
echo "⚠️  重要提醒："
echo "  • 请妥善保管证书文件"
echo "  • 定期更新证书和密钥"
echo "  • 监控告警通知"
echo "  • 定期审查安全日志"
echo ""
echo -e "${GREEN}✨ 部署成功！系统已具备完整的双向鉴权能力${NC}"

# 保存服务PID
echo $SERVICE_PID > kyc-service.pid

echo -e "${YELLOW}📝 服务PID已保存到 kyc-service.pid${NC}"
echo -e "${YELLOW}🔧 使用 ./scripts/stop-services.sh 停止所有服务${NC}"