#!/bin/bash

# KYC服务OpenTelemetry指标部署和测试脚本

echo "🚀 开始部署KYC企业级OpenTelemetry指标系统..."

# 检查Docker环境
echo "检查Docker环境..."
if ! command -v docker &> /dev/null; then
    echo "❌ Docker未安装，请先安装Docker"
    exit 1
fi

if ! command -v docker-compose &> /dev/null; then
    echo "❌ Docker Compose未安装，请先安装Docker Compose"
    exit 1
fi

# 启动基础设施
echo "启动基础设施服务..."
docker-compose up -d postgres redis kong prometheus grafana

if [ $? -ne 0 ]; then
    echo "❌ 基础设施启动失败"
    exit 1
fi

echo "等待服务启动..."
sleep 10

# 检查服务状态
echo "检查服务状态..."
services=("postgres" "redis" "kong" "prometheus" "grafana")
for service in "${services[@]}"; do
    if docker-compose ps | grep -q "$service.*Up"; then
        echo "✅ $service 运行正常"
    else
        echo "❌ $service 未正常运行"
        docker-compose logs "$service"
    fi
done

# 启动KYC服务
echo "编译并启动KYC服务..."
cd /Users/bytedance/Documents/project/go/d
go mod download

if [ $? -ne 0 ]; then
    echo "❌ Go模块下载失败"
    exit 1
fi

# 后台启动KYC服务
echo "启动KYC服务..."
nohup go run cmd/server/main.go > kyc-service.log 2>&1 &
KYC_PID=$!
echo "KYC服务PID: $KYC_PID"

# 等待服务启动
sleep 5

# 检查KYC服务是否启动成功
if ps -p $KYC_PID > /dev/null; then
    echo "✅ KYC服务启动成功"
else
    echo "❌ KYC服务启动失败"
    cat kyc-service.log
    exit 1
fi

# 导入Grafana仪表板
echo "导入Grafana仪表板..."
./scripts/import-business-metrics.sh

# 等待仪表板导入完成
sleep 3

# 生成测试流量
echo "生成测试流量..."
echo "正在发送测试请求以生成指标数据..."

# 测试OCR接口
curl -X POST http://localhost:8080/api/v1/kyc/ocr \
  -H "Content-Type: multipart/form-data" \
  -F "image=@test_image.jpg" \
  -F "language=auto" \
  -H "Authorization: Bearer test-token" \
  -H "Idempotency-Key: test-ocr-001" \
  -w "\nOCR接口响应时间: %{time_total}s\n" \
  -o /dev/null \
  -s

# 测试人脸识别接口
curl -X POST http://localhost:8080/api/v1/kyc/face/verify \
  -H "Content-Type: multipart/form-data" \
  -F "image1=@test_image.jpg" \
  -F "image2=@test_image.jpg" \
  -H "Authorization: Bearer test-token" \
  -H "Idempotency-Key: test-face-001" \
  -w "\n人脸识别接口响应时间: %{time_total}s\n" \
  -o /dev/null \
  -s

# 测试完整KYC流程
curl -X POST http://localhost:8080/api/v1/kyc/verify \
  -H "Content-Type: multipart/form-data" \
  -F "idcard_image=@test_image.jpg" \
  -F "face_image=@test_image.jpg" \
  -F "name=张三" \
  -F "idcard=123456789012345678" \
  -F "phone=13800138000" \
  -H "Authorization: Bearer test-token" \
  -H "Idempotency-Key: test-kyc-001" \
  -w "\n完整KYC接口响应时间: %{time_total}s\n" \
  -o /dev/null \
  -s

echo ""
echo "✅ 测试流量生成完成"

# 等待指标收集
echo "等待指标收集..."
sleep 10

# 验证指标
echo "验证指标收集..."
curl -s http://localhost:9090/api/v1/label/__name__/values | grep -E "(http_requests_total|business_operations_total|auth_failures_total|permission_denied_total|sensitive_data_access_total|dependency_calls_total)" > /dev/null

if [ $? -eq 0 ]; then
    echo "✅ Prometheus指标收集正常"
else
    echo "❌ Prometheus指标收集异常"
fi

# 显示访问信息
echo ""
echo "🎉 部署完成！"
echo ""
echo "访问信息:"
echo "• KYC服务: http://localhost:8080"
echo "• Prometheus: http://localhost:9090"
echo "• Grafana: http://localhost:3000 (admin/amdin123)"
echo "• 业务指标仪表板: http://localhost:3000/d/kyc-business-metrics"
echo ""
echo "关键指标:"
echo "• HTTP请求速率: rate(http_requests_total[5m])"
echo "• HTTP错误率: rate(http_request_errors_total[5m]) / rate(http_requests_total[5m])"
echo "• 业务操作速率: rate(business_operations_total[5m])"
echo "• 认证失败速率: rate(auth_failures_total[5m])"
echo "• 权限拒绝速率: rate(permission_denied_total[5m])"
echo "• 敏感数据访问速率: rate(sensitive_data_access_total[5m])"
echo "• 外部依赖调用速率: rate(dependency_calls_total[5m])"
echo ""
echo "服务日志:"
echo "• KYC服务日志: kyc-service.log"
echo "• Docker日志: docker-compose logs -f"
echo ""
echo "停止服务:"
echo "• 停止KYC服务: kill $KYC_PID"
echo "• 停止基础设施: docker-compose down"

# 保存PID到文件
echo $KYC_PID > kyc-service.pid