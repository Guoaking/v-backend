#!/bin/bash

# 停止双向鉴权服务脚本
# 优雅地停止所有相关服务

set -e

echo "🛑 正在停止双向鉴权服务..."

# 停止后端服务
if [ -f "kyc-service.pid" ]; then
    SERVICE_PID=$(cat kyc-service.pid)
    if kill -0 "$SERVICE_PID" 2>/dev/null; then
        echo "停止KYC服务 (PID: $SERVICE_PID)..."
        kill "$SERVICE_PID"
        
        # 等待服务停止
        for i in {1..30}; do
            if ! kill -0 "$SERVICE_PID" 2>/dev/null; then
                echo "✅ KYC服务已停止"
                break
            fi
            sleep 1
        done
        
        # 强制停止
        if kill -0 "$SERVICE_PID" 2>/dev/null; then
            echo "强制停止KYC服务..."
            kill -9 "$SERVICE_PID" 2>/dev/null || true
        fi
    else
        echo "KYC服务未运行"
    fi
    rm -f kyc-service.pid
else
    echo "未找到KYC服务PID文件"
fi

# 停止Docker容器
echo "停止Docker容器..."
docker-compose down

# 清理证书文件（可选）
echo "清理证书文件..."
if [ -d "certs" ]; then
    read -p "是否删除证书文件？(y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf certs
        echo "✅ 证书文件已删除"
    else
        echo "保留证书文件"
    fi
fi

# 清理日志文件（可选）
echo "清理日志文件..."
if [ -f "kyc-service.log" ]; then
    read -p "是否删除日志文件？(y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -f kyc-service.log kyc.log
        echo "✅ 日志文件已删除"
    else
        echo "保留日志文件"
    fi
fi

echo "✅ 服务停止完成！"
echo ""
echo "📋 清理选项："
echo "  • 证书文件: $([ -d "certs" ] && echo "存在" || echo "已删除")"
echo "  • 日志文件: $([ -f "kyc-service.log" ] && echo "存在" || echo "已删除")"
echo "  • 服务PID: $([ -f "kyc-service.pid" ] && echo "存在" || echo "已删除")"
echo ""
echo "🔧 如需重新启动，请运行:"
echo "  ./scripts/deploy-bidirectional-auth.sh"