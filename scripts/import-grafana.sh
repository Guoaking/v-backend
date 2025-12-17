#!/bin/bash

# Grafana 面板导入脚本
# 用于将 KYC 监控面板导入到现有的 Grafana

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="admin123"
DASHBOARD_FILE="/Users/bytedance/Documents/project/go/d/grafana/kyc-dashboard.json"

echo "📊 开始导入 KYC 监控面板到 Grafana..."

# 检查 Grafana 是否运行
if ! curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" "$GRAFANA_URL/api/health" > /dev/null; then
    echo "❌ Grafana 无法访问，请确保 Grafana 正在运行"
    exit 1
fi

echo "✅ Grafana 连接正常"

# 检查面板文件是否存在
if [ ! -f "$DASHBOARD_FILE" ]; then
    echo "❌ 面板文件不存在: $DASHBOARD_FILE"
    exit 1
fi

# 读取面板配置
DASHBOARD_JSON=$(cat "$DASHBOARD_FILE")

# 创建导入请求数据
IMPORT_DATA=$(cat <<EOF
{
  "dashboard": $DASHBOARD_JSON,
  "overwrite": true,
  "inputs": [
    {
      "name": "DS_PROMETHEUS",
      "type": "datasource",
      "pluginId": "prometheus",
      "value": "Prometheus"
    }
  ]
}
EOF
)

# 导入面板
echo "📤 导入面板..."
RESPONSE=$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -d "$IMPORT_DATA" \
  "$GRAFANA_URL/api/dashboards/import")

# 检查导入结果
if echo "$RESPONSE" | grep -q "success.*true"; then
    echo "✅ 面板导入成功！"
    
    # 提取面板URL
    DASHBOARD_URL=$(echo "$RESPONSE" | grep -o '"url":"[^"]*"' | cut -d'"' -f4)
    if [ -n "$DASHBOARD_URL" ]; then
        echo "📈 面板地址: $GRAFANA_URL$DASHBOARD_URL"
    fi
else
    echo "❌ 面板导入失败:"
    echo "$RESPONSE"
    exit 1
fi

echo ""
echo "🔍 验证数据源..."
# 检查 Prometheus 数据源
DATASOURCE_RESPONSE=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  "$GRAFANA_URL/api/datasources/name/Prometheus")

if echo "$DATASOURCE_RESPONSE" | grep -q "prometheus"; then
    echo "✅ Prometheus 数据源配置正确"
else
    echo "⚠️  Prometheus 数据源可能未配置，请手动添加:"
    echo "  • Name: Prometheus"
    echo "  • Type: Prometheus"
    echo "  • URL: http://prometheus:9090"
fi

echo ""
echo "🎯 面板功能说明："
echo "  • HTTP 请求速率: 监控 API 调用频率"
echo "  • 错误率: 5xx 错误监控"
echo "  • 响应时间 P95: 95% 请求响应时间"
echo "  • KYC 成功率: 业务成功率指标"
echo "  • 第三方服务延迟: OCR/人脸/活体检测延迟"
echo "  • 系统资源: CPU、内存、Goroutine 数量"
echo "  • 业务指标: 今日处理量、平均处理时间"