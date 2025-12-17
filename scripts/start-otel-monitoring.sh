#!/bin/bash

# KYC服务OpenTelemetry监控启动脚本

set -e

echo "🚀 启动KYC服务OpenTelemetry监控配置..."

# 检查Docker是否运行
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker未运行，请先启动Docker服务"
    exit 1
fi

# 创建共享网络
echo "📦 创建共享网络..."
docker network create shared-network 2>/dev/null || echo "共享网络已存在"

# 启动基础设施服务
echo "🏗️  启动基础设施服务..."
docker-compose up -d postgres redis jaeger prometheus grafana

# 等待服务启动
echo "⏳ 等待服务启动..."
sleep 10

# 检查服务状态
echo "🔍 检查服务状态..."
services=("postgres" "redis" "jaeger" "prometheus" "grafana")
for service in "${services[@]}"; do
    if docker-compose ps | grep -q "${service}.*Up"; then
        echo "✅ ${service} 服务运行正常"
    else
        echo "❌ ${service} 服务未正常运行"
        exit 1
    fi
done

# 等待Grafana完全启动
echo "⏳ 等待Grafana完全启动..."
for i in {1..30}; do
    if curl -s -u "admin:admin123" "http://localhost:3000/api/health" > /dev/null; then
        echo "✅ Grafana已就绪"
        break
    fi
    echo "等待Grafana启动... (${i}/30)"
    sleep 2
done

# 导入Grafana仪表板
echo "📊 导入Grafana仪表板..."
if [ -f "./scripts/import-grafana-otel.sh" ]; then
    chmod +x ./scripts/import-grafana-otel.sh
    ./scripts/import-grafana-otel.sh
else
    echo "⚠️  未找到Grafana导入脚本，跳过仪表板导入"
fi

# 启动KYC服务
echo "🎯 启动KYC服务..."
if command -v go > /dev/null 2>&1; then
    echo "使用Go运行服务..."
    go run cmd/server/main.go &
    KYC_PID=$!
    echo "KYC服务PID: ${KYC_PID}"
else
    echo "⚠️  未找到Go环境，请手动启动KYC服务"
fi

# 等待KYC服务启动
echo "⏳ 等待KYC服务启动..."
sleep 5

# 验证服务端口
echo "🔍 验证服务端口..."
if curl -s "http://localhost:8082/health" > /dev/null; then
    echo "✅ KYC服务运行正常"
else
    echo "⚠️  KYC服务可能未完全启动，请稍后检查"
fi

# 验证Prometheus指标
echo "📈 验证Prometheus指标..."
if curl -s "http://localhost:9090/api/v1/label/__name__/values" | grep -q "http_requests_total"; then
    echo "✅ Prometheus指标正常"
else
    echo "⚠️  Prometheus指标可能未完全加载，请稍后检查"
fi

# 验证OTel指标
echo "🔬 验证OpenTelemetry指标..."
if curl -s "http://localhost:9090/metrics" | grep -q "otel"; then
    echo "✅ OpenTelemetry指标正常"
else
    echo "⚠️  OpenTelemetry指标可能未完全加载，请稍后检查"
fi

# 显示访问信息
echo ""
echo "🎉 KYC服务OpenTelemetry监控配置完成！"
echo ""
echo "📋 服务访问信息："
echo "  • KYC服务: http://localhost:8082"
echo "  • 健康检查: http://localhost:8082/health"
echo "  • 指标端点: http://localhost:8082/metrics"
echo "  • OTel指标: http://localhost:9090/metrics"
echo ""
echo "📊 监控面板："
echo "  • Grafana: http://localhost:3000"
echo "  • 用户名: admin"
echo "  • 密码: admin123"
echo ""
echo "🔍 链路追踪："
echo "  • Jaeger UI: http://localhost:16686"
echo ""
echo "📈 Prometheus："
echo "  • Prometheus UI: http://localhost:9090"
echo ""
echo "🔧 管理命令："
echo "  • 查看日志: docker-compose logs -f [service-name]"
echo "  • 停止服务: docker-compose down"
echo "  • 重启服务: docker-compose restart [service-name]"
echo ""
echo "📝 测试OTel指标："
echo "  • 发送测试请求: curl -X POST http://localhost:8082/api/v1/kyc/ocr -F 'image=@test.jpg'"
echo "  • 查看指标: curl -s http://localhost:9090/metrics | grep otel"
echo ""

# 保存PID以便后续管理
if [ ! -z "${KYC_PID}" ]; then
    echo "${KYC_PID}" > kyc-service.pid
    echo "KYC服务PID已保存到 kyc-service.pid"
fi

echo "✨ 所有服务启动完成！"