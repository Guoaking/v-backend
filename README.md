# KYC企业级认证服务

这是一个基于Go语言的企业级KYC（Know Your Customer）认证服务，集成了OCR识别、人脸识别、活体检测等功能，通过Kong API Gateway提供统一的API访问入口。

## 知识库

- 开发规范与指南：`docs/BACKEND_GUIDE.md`
- 后端业务实现知识库（AI First 版）：`docs/kb/AI_KNOWLEDGE_BASE.md`
- CI/CD配置指南：`docs/guides/CI_SETUP.md`

## 功能特性

### 🔐 安全特性
- **OAuth 2.0认证**: 基于JWT的访问令牌机制
- **双向认证**: Kong与服务之间的HMAC签名验证
- **mTLS支持**: 证书双向认证
- **数据加密**: 敏感数据AES-256加密存储
- **数据脱敏**: 身份证号、手机号、姓名等敏感信息脱敏处理
- **审计日志**: 完整的操作审计追踪
- **PII保护**: 个人身份信息保护机制
- **IP白名单**: API Key级别的IP访问控制

### 🚀 性能优化
- **限流控制**: 基于Redis的分布式限流
- **幂等性**: 防止重复请求的幂等机制
- **缓存策略**: 多级缓存提升响应速度
- **连接池**: 数据库和Redis连接池优化

### 📊 监控告警
- **Prometheus指标**: 全面的业务和技术指标
- **Grafana仪表板**: 可视化监控面板
- **OpenTelemetry**: 统一的可观测性支持
- **告警机制**: 多维度告警规则配置

### 🔧 技术架构
- **API网关**: Kong作为统一入口
- **微服务**: Go语言高并发服务
- **数据库**: PostgreSQL关系型数据库
- **缓存**: Redis分布式缓存
- **容器化**: Docker部署支持

## 快速开始

### 1. 环境要求
- Go 1.21+
- Docker
- PostgreSQL 15+
- Redis 7+

### 2. 本地开发
```bash
# 启动后端服务 (默认使用 config.local.yaml)
make run
```
或手动：
```bash
./kyc-service -config config.local
```

### 3. Docker部署

```bash
# 构建镜像
docker build -t kyc-service:latest .

# 启动服务
docker-compose up -d
```

## 配置说明

### 配置文件
服务支持通过配置文件和环境变量进行配置，优先级：环境变量 > 配置文件 > 默认值

```bash
# 使用指定配置文件启动
./kyc-service -config config.local

# 使用环境变量覆盖配置
KYC_PORT=8082 ./kyc-service -config config.local
```

### 主要配置项

| 配置项 | 说明 | 默认值 |
|---------|------|---------|
| port | 服务监听端口 | 8082 |
| gin_mode | Gin运行模式 | debug |
| log_level | 日志级别 | info |
| database.host | 数据库地址 | localhost |
| database.port | 数据库端口 | 5432 |
| redis.host | Redis地址 | localhost |
| redis.port | Redis端口 | 6379 |
| security.jwt_secret | JWT密钥 | - |
| security.encryption_key | 加密密钥 | - |

## API文档

### 健康检查

```http
GET /health
```

响应示例：
```json
{
  "kong_verified": true,
  "service": "kyc-service",
  "status": "healthy",
  "timestamp": "2026-02-26T18:31:46+08:00",
  "version": "1.0.0"
}
```

### 认证接口

#### 获取访问令牌

```http
POST /api/v1/oauth/token
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
POST /api/v1/oauth/refresh
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
Content-Type: multipart/form-data

image: <身份证图片文件>
```

#### 人脸识别

```http
POST /api/v1/kyc/face/search
Authorization: Bearer <token>
Content-Type: multipart/form-data

image: <人脸图片文件>
```

#### 活体检测

```http
POST /api/v1/kyc/liveness/silent
Authorization: Bearer <token>
Content-Type: multipart/form-data

image: <活体检测图片>
```

#### 完整KYC流程

```http
POST /api/v1/kyc/verify
Authorization: Bearer <token>
Content-Type: application/json

{
  "id_card_image": "base64_encoded_image",
  "face_image": "base64_encoded_image",
  "name": "张三",
  "id_card": "110101199001011234",
  "phone": "13800138000"
}
```

#### 查询KYC状态

```http
GET /api/v1/kyc/status/{request_id}
Authorization: Bearer <token>
```

## 监控和指标

### Prometheus指标

- `http_requests_total`: HTTP请求总数
- `http_request_duration_seconds`: HTTP请求耗时
- `kyc_requests_total`: KYC请求总数
- `kyc_duration_seconds`: KYC处理耗时

### 访问监控

- **Prometheus**: http://localhost:9090/metrics
- **健康检查**: http://localhost:8082/health

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

## 开发指南

### 项目结构

```
v-backend/
├── cmd/
│   └── server/         # 应用入口
├── internal/
│   ├── api/           # API处理器
│   ├── config/        # 配置管理
│   ├── middleware/    # 中间件
│   ├── models/        # 数据模型
│   ├── monitoring/    # 监控组件
│   ├── service/       # 业务逻辑
│   ├── storage/       # 数据存储
│   └── tasks/         # 后台任务
├── pkg/
│   ├── crypto/        # 加密工具
│   ├── logger/        # 日志工具
│   ├── metrics/       # 指标工具
│   └── utils/         # 通用工具
├── scripts/        # 脚本和工具
├── docs/           # 文档
├── config.yaml        # 配置文件
├── config.local.yaml  # 本地配置文件
└── go.mod            # Go模块
```

### 本地CI测试

项目配置了本地自动化测试脚本：

```bash
# 快速测试（格式检查、静态分析、单元测试）
./scripts/test-quick.sh

# 完整测试（包含构建检查、安全检查）
./scripts/test-all.sh

# 生成覆盖率报告
./scripts/test-coverage.sh
```

### Git Hooks

项目配置了Git hooks自动执行测试：

- **pre-commit**: 运行 `test-quick.sh`
- **pre-push**: 运行 `test-all.sh`

跳过验证：
```bash
git commit --no-verify -m "紧急修复"
git push --no-verify origin main
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

### v1.0.0 (2026-02-26)
- ✨ 完整的服务功能实现
- 🔧 配置系统优化，支持命令行参数指定配置文件
- 📊 OpenTelemetry监控集成
- 🔐 安全特性完整实现（OAuth 2.0、双向认证、mTLS）
- 🚀 性能优化（限流、缓存、连接池）
- 🧪 本地CI测试脚本和Git hooks配置
- 🐳 Docker部署支持
