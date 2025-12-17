#!/bin/bash

# KYC服务8082端口负载测试脚本

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== KYC服务8082端口负载测试工具 ===${NC}"

# 检查依赖
check_dependencies() {
    echo -e "${YELLOW}检查依赖...${NC}"
    
    if ! command -v go &> /dev/null; then
        echo -e "${RED}错误: Go未安装${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ Go已安装: $(go version)${NC}"
}

# 构建负载测试工具
build_tool() {
    echo -e "${YELLOW}构建负载测试工具...${NC}"
    cd "$(dirname "$0")"
    
    # 下载依赖
    go mod download
    
    # 构建
    if go build -o kyc-loadtest main.go; then
        echo -e "${GREEN}✓ 构建成功${NC}"
    else
        echo -e "${RED}✗ 构建失败${NC}"
        exit 1
    fi
}

# 检查目标服务
check_service() {
    local target_url=$1
    echo -e "${YELLOW}检查目标服务: $target_url${NC}"
    
    if curl -s -o /dev/null -w "%{http_code}" "$target_url/health" | grep -q "200\|401\|403"; then
        echo -e "${GREEN}✓ 目标服务可访问${NC}"
        return 0
    else
        echo -e "${RED}✗ 目标服务不可访问${NC}"
        return 1
    fi
}

# 显示使用说明
show_usage() {
    echo ""
    echo -e "${BLUE}使用说明:${NC}"
    echo "  $0 [测试类型] [参数]"
    echo ""
    echo -e "${BLUE}测试类型:${NC}"
    echo "  basic     - 基础测试（100 QPS，5分钟，10%错误率）"
    echo "  jwt       - JWT认证测试"
    echo "  oauth     - OAuth2认证测试"
    echo "  stress    - 压力测试（逐步增加QPS）"
    echo "  error     - 错误率测试（不同错误率）"
    echo "  custom    - 自定义测试"
    echo "  monitor   - 仅启动监控服务"
    echo ""
    echo -e "${BLUE}示例:${NC}"
    echo "  $0 basic"
    echo "  $0 jwt"
    echo "  $0 stress"
    echo "  $0 error 0.2"
    echo "  $0 custom -q 200 -d 10m -e 0.1"
}

# 基础测试
run_basic_test() {
    echo -e "${BLUE}运行基础测试: 100 QPS，5分钟，10%错误率${NC}"
    
    ./kyc-loadtest \
        --target http://localhost:8082 \
        --qps 100 \
        --duration 5m \
        --error-rate 0.1 \
        --auth none \
        --metrics-port 9091
}

# JWT认证测试
run_jwt_test() {
    echo -e "${BLUE}运行JWT认证测试${NC}"
    
    ./kyc-loadtest \
        --target http://localhost:8082 \
        --qps 50 \
        --duration 2m \
        --auth jwt \
        --error-rate 0.05 \
        --metrics-port 9092
}

# OAuth2认证测试
run_oauth_test() {
    echo -e "${BLUE}运行OAuth2认证测试${NC}"
    
    ./kyc-loadtest \
        --target http://localhost:8082 \
        --qps 50 \
        --duration 2m \
        --auth oauth \
        --error-rate 0.05 \
        --metrics-port 9093
}

# 压力测试
run_stress_test() {
    echo -e "${BLUE}运行压力测试${NC}"
    
    for qps in 50 100 200 300; do
        echo -e "${YELLOW}压力测试: ${qps} QPS，1分钟${NC}"
        timeout 70s ./kyc-loadtest \
            --target http://localhost:8082 \
            --qps $qps \
            --duration 1m \
            --error-rate 0.02 \
            --metrics-port $((9090 + qps/50)) || true
        sleep 5s
    done
}

# 错误率测试
run_error_test() {
    local error_rate=${1:-0.1}
    echo -e "${BLUE}运行错误率测试（错误率: ${error_rate}）${NC}"
    
    ./kyc-loadtest \
        --target http://localhost:8082 \
        --qps 100 \
        --duration 3m \
        --error-rate $error_rate \
        --auth none \
        --metrics-port 9094
}

# 自定义测试
run_custom_test() {
    echo -e "${BLUE}运行自定义测试${NC}"
    ./kyc-loadtest "$@"
}

# 启动监控服务
start_monitor() {
    echo -e "${BLUE}启动监控指标服务${NC}"
    echo -e "${YELLOW}监控服务启动在 http://localhost:9091/metrics${NC}"
    echo -e "${YELLOW}按 Ctrl+C 停止监控服务${NC}"
    
    # 启动一个简单的HTTP服务器来暴露指标
    python3 -c "
import http.server
import socketserver
import prometheus_client
import time

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/metrics':
            self.send_response(200)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            # 这里可以添加一些模拟指标
            self.wfile.write(b'# HELP kyc_loadtest_up KYC loadtest is up\n')
            self.wfile.write(b'# TYPE kyc_loadtest_up gauge\n')
            self.wfile.write(b'kyc_loadtest_up 1\n')
        else:
            self.send_response(404)
            self.end_headers()

with socketserver.TCPServer(('', 9091), Handler) as httpd:
    print('监控服务运行在 http://localhost:9091/metrics')
    httpd.serve_forever()
" 2>/dev/null || echo "需要Python3来启动监控服务"
}

# 显示监控信息
show_monitor_info() {
    echo ""
    echo -e "${GREEN}=== 监控信息 ===${NC}"
    echo -e "${BLUE}Prometheus指标:${NC}"
    echo "  基础测试: http://localhost:9091/metrics"
    echo "  JWT测试:  http://localhost:9092/metrics"
    echo "  OAuth测试: http://localhost:9093/metrics"
    echo "  压力测试: http://localhost:9094/metrics"
    echo ""
    echo -e "${BLUE}Grafana仪表板:${NC}"
    echo "  URL: http://localhost:3000 (admin/admin123)"
    echo "  数据源: Prometheus (http://localhost:9090)"
    echo ""
    echo -e "${BLUE}关键指标:${NC}"
    echo "  - kyc_loadtest_requests_total: 总请求数"
    echo "  - kyc_loadtest_request_duration_seconds: 请求耗时"
    echo "  - kyc_loadtest_qps: 实时QPS"
    echo "  - kyc_loadtest_error_rate: 错误率"
    echo "  - kyc_loadtest_success_rate: 成功率"
}

# 主函数
main() {
    if [ $# -eq 0 ]; then
        show_usage
        exit 1
    fi
    
    # 检查依赖
    check_dependencies
    
    # 构建工具
    build_tool
    
    local test_type=$1
    shift
    
    # 检查目标服务
#    if ! check_service "http://localhost:8082"; then
#        echo -e "${YELLOW}服务检查失败，但仍继续测试...${NC}"
#    fi
    
    case $test_type in
        basic)
            run_basic_test
            ;;
        jwt)
            run_jwt_test
            ;;
        oauth)
            run_oauth_test
            ;;
        stress)
            run_stress_test
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
    
    show_monitor_info
}

# 运行主函数
main "$@"