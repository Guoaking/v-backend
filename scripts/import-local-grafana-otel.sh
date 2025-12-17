#!/bin/bash

# KYC服务OpenTelemetry监控仪表板导入到本地Grafana脚本

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="admin123"
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
        "name": "KYC-Prometheus-OTel",
        "type": "prometheus",
        "url": "http://host.docker.internal:9090",
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

# 创建第二个数据源用于原有指标
curl -X POST \
    -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "KYC-Prometheus-App",
        "type": "prometheus",
        "url": "http://host.docker.internal:8082/metrics",
        "access": "proxy",
        "isDefault": false,
        "jsonData": {
            "timeInterval": "30s",
            "queryTimeout": "60s",
            "httpMethod": "POST",
            "manageAlerts": true,
            "prometheusType": "Prometheus",
            "prometheusVersion": "2.40.0"
        }
    }' \
    "${GRAFANA_URL}/api/datasources" || echo "数据源可能已存在"

# 导入OTel监控仪表板
echo "📈 导入OpenTelemetry监控仪表板..."
if [ -f "${DASHBOARD_DIR}/kyc-otel-dashboard.json" ]; then
    # 读取仪表板JSON文件内容
    DASHBOARD_JSON=$(cat "${DASHBOARD_DIR}/kyc-otel-dashboard.json")
    
    # 修改数据源引用
    MODIFIED_JSON=$(echo "$DASHBOARD_JSON" | sed 's/"${datasource}"/"KYC-Prometheus-OTel"/g')
    
    # 导入仪表板
    curl -X POST \
        -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        -H "Content-Type: application/json" \
        -d "{
            \"dashboard\": ${MODIFIED_JSON},
            \"overwrite\": true,
            \"folderId\": 0
        }" \
        "${GRAFANA_URL}/api/dashboards/db"
    
    echo "✅ OpenTelemetry监控仪表板导入完成"
else
    echo "❌ 错误: 找不到仪表板文件 ${DASHBOARD_DIR}/kyc-otel-dashboard.json"
fi

# 导入其他KYC业务仪表板（如果存在）
for dashboard_file in "${DASHBOARD_DIR}"/*.json; do
    if [ -f "$dashboard_file" ] && [[ "$dashboard_file" != *"kyc-otel-dashboard.json"* ]]; then
        dashboard_name=$(basename "$dashboard_file" .json)
        echo "导入仪表板: $dashboard_name"
        
        # 读取仪表板JSON文件内容
        DASHBOARD_JSON=$(cat "$dashboard_file")
        
        # 修改数据源引用
        MODIFIED_JSON=$(echo "$DASHBOARD_JSON" | sed 's/"${datasource}"/"KYC-Prometheus-App"/g')
        
        # 导入仪表板
        curl -X POST \
            -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
            -H "Content-Type: application/json" \
            -d "{
                \"dashboard\": ${MODIFIED_JSON},
                \"overwrite\": true,
                \"folderId\": 0
            }" \
            "${GRAFANA_URL}/api/dashboards/db" || echo "仪表板导入失败: $dashboard_name"
    fi
done

# 创建自定义OTel业务监控仪表板
echo "🎯 创建自定义OTel业务监控仪表板..."
cat > /tmp/kyc-otel-business-dashboard.json << 'EOF'
{
  "dashboard": {
    "id": null,
    "title": "KYC服务 - OTel业务监控",
    "tags": ["kyc", "otel", "business"],
    "timezone": "browser",
    "panels": [
      {
        "id": 1,
        "title": "KYC业务成功率",
        "type": "stat",
        "targets": [
          {
            "expr": "business_kyc_success_rate * 100",
            "legendFormat": "KYC成功率",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "percent",
            "thresholds": {
              "steps": [
                {"color": "red", "value": 0},
                {"color": "yellow", "value": 80},
                {"color": "green", "value": 95}
              ]
            }
          }
        },
        "gridPos": {"h": 8, "w": 6, "x": 0, "y": 0}
      },
      {
        "id": 2,
        "title": "OCR成功率",
        "type": "stat",
        "targets": [
          {
            "expr": "business_ocr_success_rate * 100",
            "legendFormat": "OCR成功率",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "percent",
            "thresholds": {
              "steps": [
                {"color": "red", "value": 0},
                {"color": "yellow", "value": 85},
                {"color": "green", "value": 95}
              ]
            }
          }
        },
        "gridPos": {"h": 8, "w": 6, "x": 6, "y": 0}
      },
      {
        "id": 3,
        "title": "人脸识别成功率",
        "type": "stat",
        "targets": [
          {
            "expr": "business_face_verify_success_rate * 100",
            "legendFormat": "人脸识别成功率",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "percent",
            "thresholds": {
              "steps": [
                {"color": "red", "value": 0},
                {"color": "yellow", "value": 85},
                {"color": "green", "value": 95}
              ]
            }
          }
        },
        "gridPos": {"h": 8, "w": 6, "x": 12, "y": 0}
      },
      {
        "id": 4,
        "title": "活体检测成功率",
        "type": "stat",
        "targets": [
          {
            "expr": "business_liveness_success_rate * 100",
            "legendFormat": "活体检测成功率",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "percent",
            "thresholds": {
              "steps": [
                {"color": "red", "value": 0},
                {"color": "yellow", "value": 90},
                {"color": "green", "value": 98}
              ]
            }
          }
        },
        "gridPos": {"h": 8, "w": 6, "x": 18, "y": 0}
      },
      {
        "id": 5,
        "title": "KYC处理时间趋势",
        "type": "timeseries",
        "targets": [
          {
            "expr": "histogram_quantile(0.50, rate(business_kyc_processing_time_seconds_bucket[5m]))",
            "legendFormat": "P50",
            "refId": "A"
          },
          {
            "expr": "histogram_quantile(0.95, rate(business_kyc_processing_time_seconds_bucket[5m]))",
            "legendFormat": "P95",
            "refId": "B"
          },
          {
            "expr": "histogram_quantile(0.99, rate(business_kyc_processing_time_seconds_bucket[5m]))",
            "legendFormat": "P99",
            "refId": "C"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "s",
            "custom": {
              "drawStyle": "line",
              "fillOpacity": 10
            }
          }
        },
        "gridPos": {"h": 8, "w": 24, "x": 0, "y": 8}
      },
      {
        "id": 6,
        "title": "KYC请求速率",
        "type": "timeseries",
        "targets": [
          {
            "expr": "rate(kyc_requests_total[5m])",
            "legendFormat": "{{type}} - {{status}}",
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
        "gridPos": {"h": 8, "w": 12, "x": 0, "y": 16}
      },
      {
        "id": 7,
        "title": "今日KYC处理量",
        "type": "stat",
        "targets": [
          {
            "expr": "business_kyc_today_volume",
            "legendFormat": "今日处理量",
            "refId": "A"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "short"
          }
        },
        "gridPos": {"h": 8, "w": 12, "x": 12, "y": 16}
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

# 导入自定义业务监控仪表板
curl -X POST \
    -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
    -H "Content-Type: application/json" \
    -d @/tmp/kyc-otel-business-dashboard.json \
    "${GRAFANA_URL}/api/dashboards/db"

echo "✅ 自定义OTel业务监控仪表板创建完成"

# 显示访问信息
echo ""
echo "🎉 Grafana仪表板导入完成！"
echo ""
echo "📊 访问地址: ${GRAFANA_URL}"
echo "👤 用户名: ${GRAFANA_USER}"
echo "🔑 密码: ${GRAFANA_PASSWORD}"
echo ""
echo "📈 可用仪表板:"
echo "  • KYC服务 - OpenTelemetry监控 (kyc-otel-monitoring)"
echo "  • KYC服务 - OTel业务监控 (kyc-otel-business)"
echo ""
echo "🔗 数据源:"
echo "  • KYC-Prometheus-OTel: OpenTelemetry指标"
echo "  • KYC-Prometheus-App: 应用原有指标"
echo ""
echo "🎯 主要监控指标:"
echo "  • KYC业务成功率"
echo "  • OCR成功率"
echo "  • 人脸识别成功率"
echo "  • 活体检测成功率"
echo "  • KYC处理时间分布"
echo "  • KYC请求速率"
echo "  • 今日处理量"