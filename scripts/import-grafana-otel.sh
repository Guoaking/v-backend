#!/bin/bash

# KYC服务OpenTelemetry监控仪表板自动导入脚本

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASSWORD="admin123"
DASHBOARD_DIR="./grafana"

# 等待Grafana启动
echo "等待Grafana启动..."
for i in {1..30}; do
    if curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" "${GRAFANA_URL}/api/health" > /dev/null; then
        echo "Grafana已启动"
        break
    fi
    echo "等待Grafana启动... (${i}/30)"
    sleep 2
done

# 检查Grafana是否可用
if ! curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" "${GRAFANA_URL}/api/health" > /dev/null; then
    echo "错误: Grafana无法访问，请检查服务状态"
    exit 1
fi

# 创建Prometheus数据源
echo "创建Prometheus数据源..."
curl -X POST \
    -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "Prometheus",
        "type": "prometheus",
        "url": "http://prometheus:9090",
        "access": "proxy",
        "isDefault": true
    }' \
    "${GRAFANA_URL}/api/datasources" || echo "数据源可能已存在"

# 导入OTel监控仪表板
echo "导入OpenTelemetry监控仪表板..."
if [ -f "${DASHBOARD_DIR}/kyc-otel-dashboard.json" ]; then
    # 读取仪表板JSON文件内容
    DASHBOARD_JSON=$(cat "${DASHBOARD_DIR}/kyc-otel-dashboard.json")
    
    # 导入仪表板
    curl -X POST \
        -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        -H "Content-Type: application/json" \
        -d "{
            \"dashboard\": ${DASHBOARD_JSON},
            \"overwrite\": true,
            \"folderId\": 0
        }" \
        "${GRAFANA_URL}/api/dashboards/db" || echo "仪表板导入失败"
    
    echo "OpenTelemetry监控仪表板导入完成"
else
    echo "错误: 找不到仪表板文件 ${DASHBOARD_DIR}/kyc-otel-dashboard.json"
fi

# 导入现有的KYC业务仪表板（如果存在）
for dashboard_file in "${DASHBOARD_DIR}"/*.json; do
    if [ -f "$dashboard_file" ] && [[ "$dashboard_file" != *"kyc-otel-dashboard.json"* ]]; then
        echo "导入仪表板: $(basename "$dashboard_file")"
        
        # 读取仪表板JSON文件内容
        DASHBOARD_JSON=$(cat "$dashboard_file")
        
        # 导入仪表板
        curl -X POST \
            -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
            -H "Content-Type: application/json" \
            -d "{
                \"dashboard\": ${DASHBOARD_JSON},
                \"overwrite\": true,
                \"folderId\": 0
            }" \
            "${GRAFANA_URL}/api/dashboards/db" || echo "仪表板导入失败: $(basename "$dashboard_file")"
    fi
done

echo "Grafana仪表板导入完成"
echo "访问地址: ${GRAFANA_URL}"
echo "用户名: ${GRAFANA_USER}"
echo "密码: ${GRAFANA_PASSWORD}"