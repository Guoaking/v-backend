#!/bin/bash

# Grafana 面板导入脚本（简化版）
# 用于将 KYC 监控面板导入到现有的 Grafana

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="admin123"

echo "📊 开始导入 KYC 监控面板到 Grafana..."

# 检查 Grafana 是否运行
if ! curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" "$GRAFANA_URL/api/health" > /dev/null; then
    echo "❌ Grafana 无法访问，请确保 Grafana 正在运行"
    exit 1
fi

echo "✅ Grafana 连接正常"

# 创建面板JSON数据
cat > /tmp/kyc-dashboard.json << 'EOF'
{
  "dashboard": {
    "title": "KYC Service Monitoring Dashboard",
    "tags": ["kyc", "monitoring", "api"],
    "timezone": "browser",
    "panels": [
      {
        "id": 1,
        "title": "HTTP Request Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(http_requests_total[5m])",
            "legendFormat": "{{method}} {{endpoint}} - {{status}}",
            "refId": "A"
          }
        ],
        "gridPos": {
          "h": 8,
          "w": 12,
          "x": 0,
          "y": 0
        }
      },
      {
        "id": 2,
        "title": "Kong Gateway Status",
        "type": "singlestat",
        "targets": [
          {
            "expr": "kong_datastore_reachable",
            "legendFormat": "Kong Status",
            "refId": "A"
          }
        ],
        "valueName": "current",
        "format": "none",
        "colorValue": true,
        "thresholds": "0,1",
        "colors": ["#d44a3a", "#f9934e", "#299c46"],
        "gridPos": {
          "h": 4,
          "w": 6,
          "x": 12,
          "y": 0
        }
      },
      {
        "id": 3,
        "title": "Service Up Status",
        "type": "table",
        "targets": [
          {
            "expr": "up",
            "legendFormat": "",
            "refId": "A"
          }
        ],
        "gridPos": {
          "h": 8,
          "w": 12,
          "x": 0,
          "y": 8
        }
      }
    ],
    "time": {
      "from": "now-1h",
      "to": "now"
    },
    "refresh": "5s"
  },
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

# 使用Grafana API导入面板
echo "📤 导入面板..."
RESPONSE=$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -d @/tmp/kyc-dashboard.json \
  "$GRAFANA_URL/api/dashboards/import")

echo "响应: $RESPONSE"

# 检查导入结果
if echo "$RESPONSE" | grep -q "success.*true\|slug\|uid\|id"; then
    echo "✅ 面板导入成功！"
    
    # 提取面板信息
    DASHBOARD_URL=$(echo "$RESPONSE" | grep -o '"url":"[^"]*"' | cut -d'"' -f4 | head -1)
    DASHBOARD_UID=$(echo "$RESPONSE" | grep -o '"uid":"[^"]*"' | cut -d'"' -f4 | head -1)
    
    if [ -n "$DASHBOARD_URL" ]; then
        echo "📈 面板地址: $GRAFANA_URL$DASHBOARD_URL"
    fi
    
    if [ -n "$DASHBOARD_UID" ]; then
        echo "🔑 面板UID: $DASHBOARD_UID"
    fi
else
    echo "⚠️  面板导入可能需要手动操作"
    echo "📋 手动导入步骤："
    echo "  1. 访问: $GRAFANA_URL"
    echo "  2. 登录: admin/admin123"
    echo "  3. 点击左侧 '+' -> 'Import'"
    echo "  4. 上传文件: /tmp/kyc-dashboard.json"
fi

# 检查数据源
echo ""
echo "🔍 检查数据源..."
DATASOURCE_RESPONSE=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  "$GRAFANA_URL/api/datasources/name/Prometheus")

if echo "$DATASOURCE_RESPONSE" | grep -q "prometheus"; then
    echo "✅ Prometheus 数据源配置正确"
else
    echo "⚠️  Prometheus 数据源可能未配置"
    echo "📋 手动配置数据源："
    echo "  1. Configuration -> Data Sources"
    echo "  2. Add data source -> Prometheus"
    echo "  3. URL: http://localhost:9090"
    echo "  4. Save & Test"
fi

echo ""
echo "🎯 面板功能说明："
echo "  • HTTP Request Rate: 监控API请求速率"
echo "  • Kong Gateway Status: Kong网关连接状态"
echo "  • Service Up Status: 所有服务运行状态"
echo ""
echo "🔗 访问地址: $GRAFANA_URL/d/kyc-service-monitoring-dashboard"

# 清理临时文件
rm -f /tmp/kyc-dashboard.json