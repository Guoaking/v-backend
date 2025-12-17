#!/bin/bash

# KYC服务完整负载测试集成脚本

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== KYC服务负载测试集成工具 ===${NC}"

# 检查服务状态
check_services() {
    echo -e "${YELLOW}检查服务状态...${NC}"
    
    # 检查KYC服务
    if curl -s -o /dev/null -w "%{http_code}" "http://localhost:8002/health" | grep -q "200"; then
        echo -e "${GREEN}✓ KYC服务正常 (http://localhost:8002)${NC}"
    else
        echo -e "${RED}✗ KYC服务不可用${NC}"
        return 1
    fi
    
    # 检查Prometheus
    if curl -s -o /dev/null -w "%{http_code}" "http://localhost:9090/-/healthy" | grep -q "200"; then
        echo -e "${GREEN}✓ Prometheus正常 (http://localhost:9090)${NC}"
    else
        echo -e "${YELLOW}⚠ Prometheus未运行${NC}"
    fi
    
    # 检查Grafana
    if curl -s -o /dev/null -w "%{http_code}" "http://localhost:3000/api/health" | grep -q "200"; then
        echo -e "${GREEN}✓ Grafana正常 (http://localhost:3000)${NC}"
    else
        echo -e "${YELLOW}⚠ Grafana未运行${NC}"
    fi
}

# 显示使用说明
show_usage() {
    echo ""
    echo -e "${BLUE}使用说明:${NC}"
    echo "  $0 [测试类型] [参数]"
    echo ""
    echo -e "${BLUE}测试类型:${NC}"
    echo "  basic     - 基础测试（100 QPS，5分钟）"
    echo "  stress    - 压力测试（逐步增加QPS）"
    echo "  auth      - 认证测试（JWT/OAuth2）"
    echo "  error     - 错误率测试（不同错误率）"
    echo "  custom    - 自定义测试（完全自定义参数）"
    echo "  monitor   - 仅启动监控指标服务"
    echo ""
    echo -e "${BLUE}示例:${NC}"
    echo "  $0 basic"
    echo "  $0 stress"
    echo "  $0 auth jwt"
    echo "  $0 error 0.2"
    echo "  $0 custom -q 200 -d 10m -e 0.1"
    echo "  $0 monitor"
}

# 基础测试
run_basic_test() {
    echo -e "${YELLOW}运行基础测试...${NC}"
    cd cmd/loadtest
    
    echo -e "${BLUE}测试1: 100 QPS，5分钟，10%错误率${NC}"
    ./loadtest \
        --target http://localhost:8002 \
        --qps 100 \
        --duration 5m \
        --error-rate 0.1 \
        --metrics-port 9091 \
        --concurrent 10
}

# 压力测试
run_stress_test() {
    echo -e "${YELLOW}运行压力测试...${NC}"
    cd cmd/loadtest
    
    for qps in 50 100 200 300 500; do
        echo -e "${BLUE}压力测试: ${qps} QPS，1分钟${NC}"
        timeout 70s ./loadtest \
            --target http://localhost:8002 \
            --qps $qps \
            --duration 1m \
            --error-rate 0.05 \
            --metrics-port $((9090 + qps/50)) \
            --concurrent 10 || true
        sleep 5s
    done
}

# 认证测试
run_auth_test() {
    local auth_type=${1:-jwt}
    echo -e "${YELLOW}运行${auth_type}认证测试...${NC}"
    cd cmd/loadtest
    
    if [ "$auth_type" = "jwt" ]; then
        echo -e "${BLUE}JWT认证测试${NC}"
        # 这里可以添加JWT令牌生成逻辑
        ./loadtest \
            --target http://localhost:8002 \
            --qps 50 \
            --duration 2m \
            --auth jwt \
            --error-rate 0.1 \
            --metrics-port 9091
    elif [ "$auth_type" = "oauth" ]; then
        echo -e "${BLUE}OAuth2认证测试${NC}"
        ./loadtest \
            --target http://localhost:8002 \
            --qps 50 \
            --duration 2m \
            --auth oauth \
            --error-rate 0.1 \
            --metrics-port 9091
    else
        echo -e "${RED}不支持的认证类型: $auth_type${NC}"
        return 1
    fi
}

# 错误率测试
run_error_test() {
    local error_rate=${1:-0.1}
    echo -e "${YELLOW}运行错误率测试（错误率: ${error_rate}）...${NC}"
    cd cmd/loadtest
    
    ./loadtest \
        --target http://localhost:8002 \
        --qps 100 \
        --duration 3m \
        --error-rate $error_rate \
        --metrics-port 9091
}

# 自定义测试
run_custom_test() {
    echo -e "${YELLOW}运行自定义测试...${NC}"
    cd cmd/loadtest
    
    ./loadtest "$@"
}

# 启动监控服务
start_monitor() {
    echo -e "${YELLOW}启动监控指标服务...${NC}"
    cd cmd/loadtest
    
    echo -e "${BLUE}监控服务启动在 http://localhost:9091/metrics${NC}"
    echo -e "${BLUE}按 Ctrl+C 停止监控服务${NC}"
    
    # 启动一个简单的HTTP服务器来暴露指标
    python3 -m http.server 9091 --directory . &
    MONITOR_PID=$!
    
    # 等待中断
    trap "kill $MONITOR_PID 2>/dev/null; exit" INT TERM
    wait $MONITOR_PID
}

# 显示测试结果
show_results() {
    echo -e "${GREEN}=== 测试结果 ===${NC}"
    echo -e "${BLUE}监控指标:${NC}"
    echo "  Prometheus指标: http://localhost:9091/metrics"
    echo "  Grafana仪表板: http://localhost:3000 (admin/admin123)"
    echo "  Prometheus服务: http://localhost:9090"
    echo ""
    echo -e "${BLUE}关键指标:${NC}"
    echo "  - loadtest_requests_total: 总请求数"
    echo "  - loadtest_request_duration_seconds: 请求耗时"
    echo "  - loadtest_qps: 实时QPS"
    echo "  - loadtest_error_rate: 错误率"
    echo "  - loadtest_success_rate: 成功率"
}

# 主函数
main() {
    if [ $# -eq 0 ]; then
        show_usage
        exit 1
    fi
    
    local test_type=$1
    shift
    
    # 检查服务状态
    if ! check_services; then
        echo -e "${YELLOW}某些服务未运行，继续执行测试...${NC}"
    fi
    
    case $test_type in
        basic)
            run_basic_test
            ;;
        stress)
            run_stress_test
            ;;
        auth)
            run_auth_test "$@"
            ;;
        error)
            run_error_test "$@"
            ;;
        custom)
            run_custom_test "$@"
            ;;
        monitor)
            start_monitor
            ;;
        help|--help|-h)
            show_usage
            ;;
        *)
            echo -e "${RED}未知的测试类型: $test_type${NC}"
            show_usage
            exit 1
            ;;
    esac
    
    show_results
}

# 运行主函数
main "$@"