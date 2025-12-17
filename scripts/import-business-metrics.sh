#!/bin/bash

# KYC企业级业务指标仪表板导入脚本
# 用于导入OpenTelemetry业务指标仪表板到Grafana

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="amdin123"
DASHBOARD_FILE="/Users/bytedance/Documents/project/go/d/grafana/kyc-business-metrics-otel.json"

echo "开始导入KYC企业级业务指标仪表板..."
echo "Grafana地址: $GRAFANA_URL"
echo "仪表板文件: $DASHBOARD_FILE"

# 检查文件是否存在
if [ ! -f "$DASHBOARD_FILE" ]; then
    echo "错误: 仪表板文件不存在: $DASHBOARD_FILE"
    exit 1
fi

# 使用curl导入仪表板
curl -X POST \
  -H "Content-Type: application/json" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -d "@$DASHBOARD_FILE" \
  "$GRAFANA_URL/api/dashboards/db"

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ 仪表板导入成功！"
    echo "访问地址: $GRAFANA_URL/d/kyc-business-metrics"
    echo ""
    echo "仪表板包含以下指标:"
    echo "• HTTP请求速率和错误率"
    echo "• HTTP请求延迟 (P95/P99)"
    echo "• 业务操作速率和延迟"
    echo "• 认证失败和权限拒绝速率"
    echo "• 敏感数据访问记录"
    echo "• 业务成功率 (OCR/人脸识别/活体检测/KYC)"
    echo "• 外部依赖调用速率和延迟"
else
    echo ""
    echo "❌ 仪表板导入失败，请检查:"
    echo "1. Grafana服务是否正常运行"
    echo "2. 用户名密码是否正确"
    echo "3. 网络连接是否正常"
    exit 1
fi