# KYC服务完整Demo流程

## 1. 环境准备

### 1.1 启动基础设施
```bash
# 启动PostgreSQL
docker run -d --name postgres \
  -e POSTGRES_DB=kyc_db \
  -e POSTGRES_USER=kyc_user \
  -e POSTGRES_PASSWORD=kyc_password \
  -p 5432:5432 \
  postgres:15

# 启动Redis
docker run -d --name redis \
  -p 6379:6379 \
  redis:7-alpine

# 启动Kong
docker run -d --name kong-database \
  -e POSTGRES_DB=kong \
  -e POSTGRES_USER=kong \
  -e POSTGRES_PASSWORD=kong \
  -p 5433:5432 \
  postgres:15

docker run -d --name kong \
  --link kong-database:kong-database \
  -e KONG_DATABASE=postgres \
  -e KONG_PG_HOST=kong-database \
  -e KONG_PG_USER=kong \
  -e KONG_PG_PASSWORD=kong \
  -e KONG_CASSANDRA_CONTACT_POINTS=kong-database \
  -e KONG_ADMIN_ACCESS_LOG=/dev/stdout \
  -e KONG_ADMIN_ERROR_LOG=/dev/stderr \
  -e KONG_ADMIN_LISTEN=0.0.0.0:8001 \
  -e KONG_PROXY_ACCESS_LOG=/dev/stdout \
  -e KONG_PROXY_ERROR_LOG=/dev/stderr \
  -e KONG_PROXY_LISTEN=0.0.0.0:8000 \
  -p 8000:8000 \
  -p 8001:8001 \
  kong:3.4

# 启动Prometheus
docker run -d --name prometheus \
  -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus

# 启动Grafana
docker run -d --name grafana \
  -p 3000:3000 \
  -e GF_SECURITY_ADMIN_PASSWORD=admin \
  grafana/grafana
```

### 1.2 配置Prometheus
创建 `prometheus.yml`:
```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'kyc-service'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 5s

  - job_name: 'kong'
    static_configs:
      - targets: ['localhost:8001']
    metrics_path: '/metrics'
    scrape_interval: 15s
```

## 2. 服务配置

### 2.1 创建配置文件 `config.yaml`
```yaml
port: 8080
gin_mode: debug
log_level: info

database:
  host: localhost
  port: 5432
  user: kyc_user
  password: kyc_password
  dbname: kyc_db
  sslmode: disable
  max_open_conns: 25
  max_idle_conns: 5

redis:
  host: localhost
  port: 6379
  password: ""
  db: 0
  pool_size: 10
  min_idle_conns: 5
  max_retries: 3
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s

security:
  jwt_secret: "your-secret-key-here-must-be-32-bytes-long"
  jwt_expiration: 24h
  encryption_key: "your-encryption-key-here-32-bytes"
  rate_limit_per_second: 100
  rate_limit_burst: 200

third_party:
  ocr_service:
    url: "http://ocr-service:8080"
    api_key: "ocr-api-key"
    timeout: 30
    retry_count: 3
  
  face_service:
    url: "http://face-service:8080"
    api_key: "face-api-key"
    timeout: 30
    retry_count: 3
  
  liveness_service:
    url: "http://liveness-service:8080"
    api_key: "liveness-api-key"
    timeout: 60
    retry_count: 3
```

## 3. 完整API调用流程

### 3.1 获取访问令牌
```bash
# 请求
curl -X POST http://localhost:8000/api/v1/auth/token \
  -H "Content-Type: application/json" \
  -d '{
    "client_id": "kyc-client",
    "client_secret": "kyc-client-secret",
    "grant_type": "client_credentials",
    "scope": "kyc:read kyc:write"
  }'

# 响应
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 86400,
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "scope": "kyc:read kyc:write"
}
```

### 3.2 OCR识别
```bash
# 请求
curl -X POST http://localhost:8000/api/v1/kyc/ocr \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
  -H "Idempotency-Key: ocr-123456" \
  -F "image=@idcard.jpg" \
  -F "language=auto"

# 响应
{
  "success": true,
  "id_card": "123456789012345678",
  "name": "张三"
}
```

### 3.3 人脸识别
```bash
# 请求
curl -X POST http://localhost:8000/api/v1/kyc/face/verify \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
  -H "Idempotency-Key: face-123456" \
  -F "image1=@idcard.jpg" \
  -F "image2=@face.jpg"

# 响应
{
  "success": true,
  "score": 0.95,
  "threshold": 0.8
}
```

### 3.4 活体检测（WebSocket）
```javascript
// JavaScript客户端示例
const ws = new WebSocket('ws://localhost:8000/api/v1/kyc/liveness/ws');

ws.onopen = function() {
    console.log('WebSocket连接已建立');
    
    // 发送检测请求
    ws.send(JSON.stringify({
        action: 'blink'
    }));
};

ws.onmessage = function(event) {
    const result = JSON.parse(event.data);
    console.log('检测结果:', result);
    
    if (result.success) {
        console.log('活体检测通过');
    } else {
        console.log('活体检测失败:', result.error);
    }
};

ws.onerror = function(error) {
    console.error('WebSocket错误:', error);
};

ws.onclose = function() {
    console.log('WebSocket连接已关闭');
};
```

### 3.5 完整KYC流程
```bash
# 请求
curl -X POST http://localhost:8000/api/v1/kyc/verify \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
  -H "Idempotency-Key: kyc-123456" \
  -F "idcard_image=@idcard.jpg" \
  -F "face_image=@face.jpg" \
  -F "name=张三" \
  -F "idcard=123456789012345678" \
  -F "phone=13800138000"

# 响应
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "success",
  "message": "KYC认证成功"
}
```

### 3.6 查询KYC状态
```bash
# 请求
curl -X GET "http://localhost:8000/api/v1/kyc/status/550e8400-e29b-41d4-a716-446655440000" \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

# 响应
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "user_id": "user_123",
  "request_type": "complete",
  "status": "success",
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:30Z"
}
```

## 4. 监控和日志查看

### 4.1 查看Prometheus指标
访问: http://localhost:9090

查询示例:
```promql
# HTTP请求速率
rate(http_requests_total[5m])

# 错误率
rate(http_requests_total{status=~"5.."}[5m])

# P95响应时间
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

# KYC成功率
rate(kyc_requests_total{status="success"}[5m]) / rate(kyc_requests_total[5m])
```

### 4.2 查看Grafana仪表板
访问: http://localhost:3000 (admin/admin)

导入仪表板配置文件:
```json
{
  "dashboard": {
    "title": "KYC Service Monitoring",
    "panels": [
      {
        "title": "Request Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(http_requests_total[5m])",
            "legendFormat": "{{method}} {{endpoint}}"
          }
        ]
      },
      {
        "title": "Error Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(http_requests_total{status=~\"5..\"}[5m])",
            "legendFormat": "{{endpoint}}"
          }
        ]
      },
      {
        "title": "Response Time P95",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))",
            "legendFormat": "{{endpoint}}"
          }
        ]
      },
      {
        "title": "KYC Success Rate",
        "type": "singlestat",
        "targets": [
          {
            "expr": "rate(kyc_requests_total{status=\"success\"}[5m]) / rate(kyc_requests_total[5m])",
            "legendFormat": "Success Rate"
          }
        ]
      }
    ]
  }
}
```

### 4.3 查看日志
```bash
# 查看服务日志
tail -f logs/kyc-service.log

# 查看Kong访问日志
docker logs -f kong

# 查看错误日志
grep ERROR logs/kyc-service.log
```

## 5. 安全测试

### 5.1 测试限流
```bash
# 快速发送多个请求测试限流
for i in {1..200}; do
  curl -X GET "http://localhost:8000/api/v1/kyc/status/test" \
    -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
    -w "%{http_code}\n" \
    -s \
    -o /dev/null
done
```

### 5.2 测试幂等性
```bash
# 使用相同的幂等键发送多次请求
for i in {1..5}; do
  curl -X POST http://localhost:8000/api/v1/kyc/ocr \
    -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
    -H "Idempotency-Key: test-123" \
    -F "image=@idcard.jpg" \
    -w "Response: %{http_code} - Request: $i\n" \
    -s \
    -o /dev/null
done
```

### 5.3 测试数据脱敏
```bash
# 查看日志中的脱敏效果
tail -f logs/kyc-service.log | grep -E "(id_card|phone|name)"
```

## 6. 性能测试

### 6.1 使用Apache Bench
```bash
# 测试OCR接口性能
ab -n 1000 -c 10 -T 'multipart/form-data' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -p ocr_request.txt \
  http://localhost:8000/api/v1/kyc/ocr
```

### 6.2 使用wrk
```bash
# 测试KYC状态查询性能
wrk -t12 -c400 -d30s \
  -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8000/api/v1/kyc/status/test
```

## 7. 故障排查

### 7.1 检查服务状态
```bash
# 健康检查
curl http://localhost:8080/health

# Kong服务状态
curl http://localhost:8001/status
```

### 7.2 数据库连接检查
```bash
# 检查PostgreSQL连接
psql -h localhost -U kyc_user -d kyc_db -c "SELECT COUNT(*) FROM kyc_requests;"

# 检查Redis连接
redis-cli -h localhost -p 6379 ping
```

### 7.3 网络连通性检查
```bash
# 检查服务间网络连通性
docker exec kong ping kyc-service
docker exec kyc-service ping postgres
```

## 8. 清理环境

```bash
# 停止并删除容器
docker stop kong kyc-service postgres redis prometheus grafana
docker rm kong kyc-service postgres redis prometheus grafana

# 清理数据卷（谨慎操作）
docker volume prune
```