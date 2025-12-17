# KYC服务8082端口负载测试工具 - 快速使用指南

## 🚀 立即开始

### 1. 构建工具
```bash
cd cmd/kyc-loadtest
go build -o kyc-loadtest .
```

### 2. 运行基础测试（100 QPS）
```bash
# 100 QPS，5分钟，10%错误率
./kyc-loadtest --qps 100 --duration 5m --error-rate 0.1

# 或者使用便捷脚本
./run-tests.sh basic
```

### 3. 查看监控指标
- **Prometheus指标**: http://localhost:9091/metrics
- **Grafana仪表板**: http://localhost:3000 (admin/admin123)

## 📊 核心功能

### QPS控制
- ✅ 精确控制1-1000+ QPS
- ✅ 多并发worker支持
- ✅ 令牌桶算法流量控制

### 认证支持
- ✅ **JWT认证**: 自动生成JWT令牌
- ✅ **OAuth2认证**: 自动获取访问令牌
- ✅ **无认证**: 测试公开接口

### 错误模拟
- ✅ 可配置错误率（0-100%）
- ✅ 随机错误类型生成
- ✅ 真实错误场景模拟

### 监控指标
- ✅ **QPS监控**: 实时QPS统计
- ✅ **响应时间**: P50/P95/P99百分位数
- ✅ **错误率**: 实时错误率统计
- ✅ **成功率**: 请求成功率统计

## 🔧 常用命令

### 基础测试
```bash
# 100 QPS，5分钟，10%错误率
./kyc-loadtest --qps 100 --duration 5m --error-rate 0.1

# 50 QPS，2分钟，5%错误率
./kyc-loadtest --qps 50 --duration 2m --error-rate 0.05
```

### 认证测试
```bash
# JWT认证测试
./kyc-loadtest --qps 100 --duration 3m --auth jwt --error-rate 0.1

# OAuth2认证测试
./kyc-loadtest --qps 100 --duration 3m --auth oauth --error-rate 0.1

# 使用自定义JWT令牌
./kyc-loadtest --qps 100 --duration 3m --auth jwt --token "your-jwt-token"
```

### 压力测试
```bash
# 高QPS压力测试
./kyc-loadtest --qps 500 --duration 2m --concurrent 20 --error-rate 0.1

# 长时间稳定性测试
./kyc-loadtest --qps 100 --duration 30m --error-rate 0.05
```

### 错误率测试
```bash
# 高错误率测试（30%错误率）
./kyc-loadtest --qps 100 --duration 2m --error-rate 0.3

# 逐步增加错误率
for error_rate in 0.05 0.1 0.2 0.3; do
    ./kyc-loadtest --qps 100 --duration 1m --error-rate $error_rate
done
```

## 📈 监控面板

### Prometheus查询
```promql
# QPS趋势
rate(kyc_loadtest_requests_total[1m])

# 响应时间P95
histogram_quantile(0.95, rate(kyc_loadtest_request_duration_seconds_bucket[1m]))

# 错误率
rate(kyc_loadtest_requests_total{status=~"4..|5..|0"}[1m]) / rate(kyc_loadtest_requests_total[1m])

# 活跃请求数
kyc_loadtest_active_requests
```

### Grafana图表建议
1. **QPS趋势图**: 显示实时QPS变化
2. **响应时间热力图**: P50/P95/P99响应时间
3. **错误率饼图**: 按状态码分类的错误分布
4. **端点访问量排行**: 各API端点访问频率

## 🎯 测试场景

### 1. 性能基线测试
```bash
# 逐步增加负载，找到性能拐点
for qps in 50 100 200 300 500; do
    echo "测试 ${qps} QPS..."
    ./kyc-loadtest --qps $qps --duration 1m --error-rate 0.05
done
```

### 2. 压力测试
```bash
# 高并发压力测试
./kyc-loadtest --qps 300 --concurrent 15 --duration 5m --error-rate 0.1

# 极限压力测试
./kyc-loadtest --qps 500 --concurrent 25 --duration 2m --error-rate 0.2
```

### 3. 稳定性测试
```bash
# 长时间稳定性测试
./kyc-loadtest --qps 100 --duration 30m --error-rate 0.05

# 持续负载测试
./kyc-loadtest --qps 150 --duration 60m --error-rate 0.08
```

### 4. 错误恢复测试
```bash
# 高错误率测试后恢复正常
./kyc-loadtest --qps 100 --error-rate 0.3 --duration 2m
./kyc-loadtest --qps 100 --error-rate 0.05 --duration 2m
```

## 🔍 故障排查

### 常见问题

1. **连接超时**
```bash
# 检查服务状态
curl -I http://localhost:8082/health

# 增加超时时间（在代码中调整）
```

2. **QPS不达标**
```bash
# 增加并发worker数
./kyc-loadtest --qps 200 --concurrent 20 --duration 2m

# 检查网络延迟
ping localhost
```

3. **认证失败**
```bash
# JWT认证失败 - 检查密钥
./kyc-loadtest --auth jwt --jwt-secret "your-32-byte-secret-key-here"

# OAuth2认证失败 - 检查服务配置
./kyc-loadtest --auth oauth --target http://localhost:8082
```

4. **内存使用高**
```bash
# 降低并发数
./kyc-loadtest --qps 100 --concurrent 5 --duration 2m

# 降低QPS
./kyc-loadtest --qps 50 --duration 2m
```

### 调试命令
```bash
# 检查服务健康状态
curl http://localhost:8082/health

# 验证JWT令牌
echo "JWT_TOKEN" | cut -d. -f2 | base64 -d | jq .

# 实时监控指标
watch -n 1 'curl -s http://localhost:9091/metrics | grep kyc_loadtest'

# 检查端口占用
netstat -tuln | grep 9091
```

## 📋 最佳实践

### 测试策略
1. **渐进式加压**: 从低QPS开始，逐步增加负载
2. **多维度测试**: 同时测试不同错误率和并发数
3. **持续监控**: 实时监控各项指标变化
4. **基线对比**: 建立性能基线，对比优化效果

### 生产环境注意事项
- 避免在生产环境进行高压测试
- 使用专门的测试环境
- 设置合理的测试时间窗口
- 提前通知相关团队

### 数据分析要点
- 关注P95/P99响应时间，而非平均值
- 分析错误分布模式，识别系统瓶颈
- 建立性能趋势报告，跟踪长期变化
- 结合系统资源监控（CPU、内存、网络）

## 🚀 高级用法

### 自定义端点权重
修改 `main.go` 中的 `endpoints` 数组：
```go
endpoints = []APIEndpoint{
    {Path: "/api/v1/kyc/ocr", Method: "POST", Weight: 40}, // 提高OCR接口权重
    {Path: "/api/v1/kyc/verify", Method: "POST", Weight: 30},
    // ...
}
```

### 添加新指标
在 `metrics.go` 中添加新的Prometheus指标：
```go
myMetric := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "kyc_loadtest_my_metric",
        Help: "My custom metric",
    },
    []string{"label1", "label2"},
)
prometheus.MustRegister(myMetric)
```

### 集成到CI/CD
```bash
# 在CI/CD管道中添加性能测试
#!/bin/bash
set -e

cd cmd/kyc-loadtest
./kyc-loadtest --qps 100 --duration 2m --error-rate 0.05

# 检查错误率是否超过阈值
ERROR_RATE=$(curl -s http://localhost:9091/metrics | grep "error_rate" | tail -1 | awk '{print $2}')
if (( $(echo "$ERROR_RATE > 0.1" | bc -l) )); then
    echo "错误率过高: $ERROR_RATE"
    exit 1
fi
```

## 📞 支持

如有问题，请：
1. 检查目标服务是否正常运行
2. 验证网络连接是否畅通
3. 确认认证配置是否正确
4. 查看监控指标是否正常
5. 检查系统资源使用情况

## 🎉 总结

这个负载测试工具提供了：
- ✅ **精确的QPS控制**: 支持1-1000+ QPS
- ✅ **完整的认证支持**: JWT/OAuth2/无认证
- ✅ **丰富的监控指标**: Prometheus + Grafana
- ✅ **灵活的错误模拟**: 可配置错误率
- ✅ **易于扩展**: 模块化设计

立即开始使用，验证您的KYC服务在高并发场景下的性能表现！