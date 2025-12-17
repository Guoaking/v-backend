# KYC企业级认证服务

这是一个基于Go语言的企业级KYC（Know Your Customer）认证服务，集成了OCR识别、人脸识别、活体检测等功能，通过Kong API Gateway提供统一的API访问入口。

## 功能特性

### 🔐 安全特性
- **OAuth 2.0认证**: 基于JWT的访问令牌机制
- **数据加密**: 敏感数据AES-256加密存储
- **数据脱敏**: 身份证号、手机号、姓名等敏感信息脱敏处理
- **审计日志**: 完整的操作审计追踪
- **PII保护**: 个人身份信息保护机制

### 🚀 性能优化
- **限流控制**: 基于Redis的分布式限流
- **幂等性**: 防止重复请求的幂等机制
- **缓存策略**: 多级缓存提升响应速度
- **连接池**: 数据库和Redis连接池优化

### 📊 监控告警
- **Prometheus指标**: 全面的业务和技术指标
- **Grafana仪表板**: 可视化监控面板
- **链路追踪**: 分布式链路追踪支持
- **告警机制**: 多维度告警规则配置

### 🔧 技术架构
- **API网关**: Kong作为统一入口
- **微服务**: Go语言高并发服务
- **数据库**: PostgreSQL关系型数据库
- **缓存**: Redis分布式缓存
- **容器化**: Docker + Kubernetes部署

## 快速开始

### 1. 环境要求
- Go 1.21+
- Docker & Docker Compose
- PostgreSQL 15+
- Redis 7+
- Kong 3.4+

### 2. 本地开发
```bash
# 克隆项目
git clone <repository-url>
cd kyc-service

# 安装依赖
go mod download

# 启动基础设施
docker-compose up -d postgres redis kong prometheus grafana

# 运行服务
go run cmd/server/main.go
```

### 3. Docker部署
```bash
# 构建镜像
docker build -t kyc-service:latest .

# 启动服务
docker-compose up -d
```

### 4. Kubernetes部署
```bash
# 创建命名空间
kubectl create namespace kyc

# 部署服务
kubectl apply -f k8s-deployment.yaml

# 检查状态
kubectl get pods -n kyc
```

## API文档

### 认证接口

#### 获取访问令牌
```http
POST /api/v1/auth/token
Content-Type: application/json

{
  "client_id": "your-client-id",
  "client_secret": "your-client-secret",
  "grant_type": "client_credentials",
  "scope": "kyc:read kyc:write"
}
```

#### 刷新令牌
```http
POST /api/v1/auth/refresh
Content-Type: application/json

{
  "refresh_token": "your-refresh-token",
  "client_id": "your-client-id"
}
```

### KYC接口

#### OCR识别
```http
POST /api/v1/kyc/ocr
Authorization: Bearer <token>
Idempotency-Key: <unique-key>
Content-Type: multipart/form-data

image: <身份证图片文件>
language: auto
```

#### 人脸识别
```http
POST /api/v1/kyc/face/verify
Authorization: Bearer <token>
Idempotency-Key: <unique-key>
Content-Type: multipart/form-data

image1: <第一张图片>
image2: <第二张图片>
```

#### 活体检测（WebSocket）
```javascript
const ws = new WebSocket('ws://localhost:8000/api/v1/kyc/liveness/ws');
ws.send(JSON.stringify({action: 'blink'}));
```

#### 完整KYC流程
```http
POST /api/v1/kyc/verify
Authorization: Bearer <token>
Idempotency-Key: <unique-key>
Content-Type: multipart/form-data

idcard_image: <身份证图片>
face_image: <人脸图片>
name: <姓名>
idcard: <身份证号>
phone: <手机号>
```

#### 查询KYC状态
```http
GET /api/v1/kyc/status/{request_id}
Authorization: Bearer <token>
```

## 监控和告警

### Prometheus指标
- `http_requests_total`: HTTP请求总数
- `http_request_duration_seconds`: HTTP请求耗时
- `kyc_requests_total`: KYC请求总数
- `kyc_duration_seconds`: KYC处理耗时
- `third_party_requests_total`: 第三方服务调用总数
- `third_party_duration_seconds`: 第三方服务调用耗时

### Grafana仪表板
访问 `http://localhost:3000` 查看监控仪表板，默认用户名/密码：admin/admin

### 告警规则
- 错误率超过10%
- 响应时间P95超过1000ms
- CPU使用率超过85%
- 内存使用率超过80%

## 安全配置

### 数据加密
所有敏感数据（身份证号、姓名、手机号等）都使用AES-256加密存储。

### 数据脱敏
- 身份证号：1234****5678
- 手机号：138****8000
- 姓名：张*

### 审计日志
所有API调用都会记录审计日志，包括：
- 请求ID
- 用户ID
- 操作类型
- 访问资源
- IP地址
- User-Agent
- 响应状态
- 处理时间

## 性能优化

### 限流策略
- 全局：每秒1000请求，突发2000
- KYC服务：每秒100请求，突发200
- 基于IP的限流

### 缓存策略
- Redis缓存热点数据
- 数据库查询缓存
- 第三方服务调用缓存

### 连接池
- 数据库连接池：最大25连接
- Redis连接池：最大10连接

## 部署架构

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Client App    │───▶│   Kong Gateway  │───▶│  KYC Service    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                              │                        │
                              ▼                        ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │  Rate Limiting  │    │   PostgreSQL    │
                       │   Auth & CORS   │    │   Database      │
                       └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                               ┌─────────────────┐
                                               │     Redis       │
                                               │     Cache       │
                                               └─────────────────┘
```

## 开发指南

### 项目结构
```
kyc-service/
├── cmd/
│   └── server/         # 应用入口
├── internal/
│   ├── api/           # API处理器
│   ├── config/        # 配置管理
│   ├── middleware/    # 中间件
│   ├── models/        # 数据模型
│   ├── monitoring/    # 监控组件
│   ├── service/       # 业务逻辑
│   └── storage/       # 数据存储
├── pkg/
│   ├── crypto/        # 加密工具
│   ├── logger/        # 日志工具
│   ├── metrics/       # 指标工具
│   └── utils/         # 通用工具
├── config.yaml        # 配置文件
├── docker-compose.yml # Docker编排
├── k8s-deployment.yaml # K8s部署
└── go.mod            # Go模块
```

### 环境变量
```bash
KYC_PORT=8080
KYC_GIN_MODE=release
KYC_LOG_LEVEL=info
KYC_DATABASE_HOST=localhost
KYC_DATABASE_PORT=5432
KYC_DATABASE_USER=kyc_user
KYC_DATABASE_PASSWORD=password
KYC_REDIS_HOST=localhost
KYC_REDIS_PORT=6379
KYC_SECURITY_JWT_SECRET=your-secret-key
KYC_SECURITY_ENCRYPTION_KEY=your-encryption-key
```

## 贡献指南

1. Fork项目
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建Pull Request

## 许可证

本项目采用MIT许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 支持

如有问题或建议，请提交Issue或联系维护者。

## 更新日志

### v1.0.0 (2024-01-15)
- ✨ 初始版本发布
- 🔧 基础KYC功能实现
- 📊 监控和告警系统
- 🔐 安全特性完整实现
- 🚀 Docker和K8s部署支持