#!/bin/bash

# KYC仪表板导入脚本 - 修复版本
# 导入所有创建的监控仪表板到本地Grafana

GRAFANA_URL="http://localhost:3000"
GRAFANA_USER="admin"
GRAFANA_PASS="admin123"
DASHBOARD_DIR="/Users/bytedance/Documents/project/go/d/grafana"

echo "=== KYC监控仪表板导入工具 ==="
echo "Grafana地址: $GRAFANA_URL"
echo "仪表板目录: $DASHBOARD_DIR"
echo

# 检查Grafana是否可访问
echo "检查Grafana服务状态..."
if ! curl -s -u "$GRAFANA_USER:$GRAFANA_PASS" "$GRAFANA_URL/api/health" > /dev/null; then
    echo "❌ 无法连接到Grafana服务，请确保服务正在运行"
    exit 1
fi
echo "✅ Grafana服务正常"
echo

# 函数：导入仪表板
import_dashboard() {
    local file=$1
    local title=$2
    local uid=$3
    
    echo "导入 $title..."
    
    # 读取仪表板JSON并构建正确的导入格式
    dashboard_content=$(cat "$file" | jq -c '.dashboard')
    
    # 创建导入请求 - 正确的Grafana API格式
    import_data=$(cat <<EOF
{
  "dashboard": $dashboard_content,
  "overwrite": true,
  "inputs": []
}
EOF
)
    
    # 发送导入请求
    response=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASS" \
        -X POST \
        -H "Content-Type: application/json" \
        -d "$import_data" \
        "$GRAFANA_URL/api/dashboards/db")
    
    if echo "$response" | grep -q '"id"' || echo "$response" | grep -q '"uid"'; then
        echo "✅ $title 导入成功"
        return 0
    else
        echo "❌ $title 导入失败: $response"
        return 1
    fi
}

# 检查jq是否可用
if ! command -v jq &> /dev/null; then
    echo "❌ 需要安装jq工具来处理JSON"
    echo "请安装jq: brew install jq (macOS) 或 apt-get install jq (Ubuntu)"
    exit 1
fi

# 导入核心业务指标仪表板
import_dashboard "$DASHBOARD_DIR/kyc-core-business-dashboard.json" "KYC核心业务指标监控" "kyc-core-business-001"

# 导入RBAC权限管理监控仪表板
import_dashboard "$DASHBOARD_DIR/kyc-rbac-monitoring-dashboard.json" "RBAC权限管理监控" "kyc-rbac-monitoring-001"

# 导入API性能监控仪表板
import_dashboard "$DASHBOARD_DIR/kyc-api-performance-dashboard.json" "API性能监控" "kyc-api-performance-001"

# 导入业务运营分析仪表板
import_dashboard "$DASHBOARD_DIR/kyc-business-operations-dashboard.json" "业务运营分析" "kyc-business-operations-001"

echo
echo "=== 导入完成 ==="
echo "您可以在Grafana中查看以下仪表板："
echo "1. KYC核心业务指标监控"
echo "2. RBAC权限管理监控" 
echo "3. API性能监控"
echo "4. 业务运营分析"
echo
echo "访问地址: $GRAFANA_URL"
echo "用户名: $GRAFANA_USER"
echo "密码: $GRAFANA_PASS"