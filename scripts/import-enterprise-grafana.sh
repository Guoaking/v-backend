#!/bin/bash

# KYC服务企业级业务监控仪表板导入到本地Grafana脚本

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="admin"
DASHBOARD_DIR="./grafana"

# 等待Grafana启动
echo "等待本地Grafana启动..."
for i in {1..30}; do
    if curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" "${GRAFANA_URL}/api/health" > /dev/null; then
        echo "✅ 本地Grafana已启动"
        break
    fi
    echo "等待Grafana启动... (${i}/30)"
    sleep 2
done

# 检查Grafana是否可用
if ! curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" "${GRAFANA_URL}/api/health" > /dev/null; then
    echo "❌ 错误: 本地Grafana无法访问，请检查服务状态"
    exit 1
fi

# 创建Prometheus数据源
echo "📊 创建Prometheus数据源..."
curl -X POST \
    -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "KYC-Enterprise-Prometheus",
        "type": "prometheus",
        "url": "http://host.docker.internal:8082/metrics",
        "access": "proxy",
        "isDefault": false,
        "jsonData": {
            "timeInterval": "15s",
            "queryTimeout": "60s",
            "httpMethod": "POST",
            "manageAlerts": true,
            "prometheusType": "Prometheus",
            "prometheusVersion": "2.40.0"
        }
    }' \
    "${GRAFANA_URL}/api/datasources" || echo "数据源可能已存在"

# 导入企业级业务监控仪表板
echo "📈 导入企业级业务监控仪表板..."
if [ -f "${DASHBOARD_DIR}/kyc-enterprise-business-dashboard.json" ]; then
    # 读取仪表板JSON文件内容
    DASHBOARD_JSON=$(cat "${DASHBOARD_DIR}/kyc-enterprise-business-dashboard.json")
    
    # 导入仪表板
    curl -X POST \
        -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        -H "Content-Type: application/json" \
        -d "${DASHBOARD_JSON}" \
        "${GRAFANA_URL}/api/dashboards/db"
    
    echo "✅ 企业级业务监控仪表板导入完成"
else
    echo "❌ 错误: 找不到仪表板文件 ${DASHBOARD_DIR}/kyc-enterprise-business-dashboard.json"
fi

# 创建额外的企业级监控面板
echo "🎯 创建额外的企业级监控面板..."
cat > /tmp/kyc-enterprise-metrics-dashboard.json << 'EOF'
{
  "dashboard": {
    "id": null,
    "title": "KYC服务 - 企业级指标详情",
    "tags": ["kyc", "enterprise", "metrics", "detail"],
    "timezone": "browser",
    "panels": [
      {
        "id": 1,
        "title": "HTTP请求详情",
        "type": "table",
        "targets": [
          {
            "expr": "sum by (method, endpoint, status_class) (rate(http_requests_total[5m]))",
            "legendFormat": "",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "custom": {
              "displayMode": "auto"
            }
          }
        },
        "gridPos": {"h": 8, "w": 24, "x": 0, "y": 0}
      },
      {
        "id": 2,
        "title": "认证失败详情",
        "type": "timeseries",
        "targets": [
          {
            "expr": "rate(auth_failures_total[5m])",
            "legendFormat": "认证失败速率",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "reqps",
            "custom": {
              "drawStyle": "line",
              "fillOpacity": 10
            }
          }
        },
        "gridPos": {"h": 8, "w": 12, "x": 0, "y": 8}
      },
      {
        "id": 3,
        "title": "权限拒绝详情",
        "type": "timeseries",
        "targets": [
          {
            "expr": "rate(permission_denied_total[5m])",
            "legendFormat": "权限拒绝速率",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "reqps",
            "custom": {
              "drawStyle": "line",
              "fillOpacity": 10
            }
          }
        },
        "gridPos": {"h": 8, "w": 12, "x": 12, "y": 8}
      },
      {
        "id": 4,
        "title": "依赖服务调用详情",
        "type": "table",
        "targets": [
          {
            "expr": "sum by (service, method, status) (rate(dependency_calls_total[5m]))",
            "legendFormat": "",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "custom": {
              "displayMode": "auto"
            }
          }
        },
        "gridPos": {"h": 8, "w": 24, "x": 0, "y": 16}
      },
      {
        "id": 5,
        "title": "敏感数据访问趋势",
        "type": "timeseries",
        "targets": [
          {
            "expr": "rate(sensitive_data_access_total[5m])",
            "legendFormat": "{{data_type}} - {{status}}",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "reqps",
            "custom": {
              "drawStyle": "line",
              "fillOpacity": 10
            }
          }
        },
        "gridPos": {"h": 8, "w": 24, "x": 0, "y": 24}
      }
    ],
    "time": {
      "from": "now-1h",
      "to": "now"
    },
    "refresh": "30s",
    "schemaVersion": 38,
    "version": 1
  },
  "overwrite": true,
  "folderId": 0
}
EOF

# 导入企业级指标详情仪表板
curl -X POST \
    -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
    -H "Content-Type: application/json" \
    -d @/tmp/kyc-enterprise-metrics-dashboard.json \
    "${GRAFANA_URL}/api/dashboards/db"

echo "✅ 企业级指标详情仪表板创建完成"

# 显示访问信息
echo ""
echo "🎉 企业级业务监控配置完成！"
echo ""
echo "📊 访问地址: ${GRAFANA_URL}"
echo "👤 用户名: ${GRAFANA_USER}"
echo "🔑 密码: ${GRAFANA_PASSWORD}"
echo ""
echo "📈 可用仪表板:"
echo "  • KYC服务 - 企业级业务监控 (kyc-enterprise-business)"
echo "  • KYC服务 - 企业级指标详情 (kyc-enterprise-metrics)"
echo ""
echo "🔗 数据源:"
echo "  • KYC-Enterprise-Prometheus: 企业级业务指标"
echo ""
echo "🎯 主要监控指标:"
echo "  • 业务成功率、错误率、处理时间"
echo "  • HTTP请求成功率、错误率、P95/P99延迟"
echo "  • 认证失败、权限拒绝、敏感数据访问"
echo "  • 依赖服务调用成功率、延迟"
echo "  • 系统资源利用率"
echo ""
echo "🔧 管理命令："
echo "  • 查看指标: curl -s http://localhost:8082/metrics | grep -E '(business_|http_|auth_|permission_|sensitive_|dependency_)'"
echo "  • 测试业务指标: 发送几个API请求后查看仪表板"
echo "  • 查看日志: docker-compose logs -f kyc-service"