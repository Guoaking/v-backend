# KYC Enterprise Authentication Service - Architecture Overview

## Project Overview

KYC Enterprise Authentication Service is a high-performance Go-based microservice providing comprehensive identity verification solutions. The service integrates OCR recognition, facial recognition, and liveness detection through Kong API Gateway, with enterprise-grade security, monitoring, and performance guarantees.

### Core Architecture
- **API Gateway**: Kong Gateway (HTTP: 8000, HTTPS: 8443, Admin: 8001)
- **Backend Service**: Go + Gin framework (Port: 8082)
- **Database**: PostgreSQL 15+ (Port: 5432)
- **Cache**: Redis 7+ (Port: 6379)
- **Monitoring**: Prometheus + Grafana (Port: 9090, 3000)
- **Tracing**: Jaeger (Port: 16686)

### Key Features
- **OAuth 2.0 + JWT**: Token-based authentication
- **Bidirectional Auth**: Mutual verification between Kong and backend
- **mTLS Support**: Certificate-based authentication
- **HMAC Signing**: Request signature verification
- **AES-256 Encryption**: Sensitive data protection
- **PII Data Masking**: Personal information protection
- **Distributed Rate Limiting**: Redis-based throttling
- **WebSocket Liveness Detection**: Real-time biometric verification

## Build & Commands

### Development Setup
```bash
# Install dependencies
go mod download

# Start infrastructure services
docker-compose up -d redis jaeger

# Run service locally
go run cmd/server/main.go

# Build binary
go build -o kyc-service cmd/server/main.go
```

### Testing & Validation
```bash
# Run all tests
go test ./...

# Test with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run CI quick test
./scripts/test-quick.sh

# Run CI full test
./scripts/test-all.sh

# Generate coverage report
./scripts/test-coverage.sh

# Test bidirectional authentication
./scripts/test-bidirectional-auth.sh

# Health check
curl http://localhost:8082/health

# Test with HMAC signature
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
SIGNATURE=$(echo -n "kyc-service:/health:$TIMESTAMP:kong-shared-secret-key-2024" | openssl dgst -sha256 -hmac "kong-shared-secret-key-2024" -binary | base64)
curl -H "X-Kong-Signature: $SIGNATURE" -H "X-Kong-Timestamp: $TIMESTAMP" -H "X-Kong-Service: kyc-service" http://localhost:8082/health
```

### Deployment
```bash
# Full bidirectional auth deployment
./scripts/deploy-bidirectional-auth.sh

# Stop all services
./scripts/stop-services.sh

# Kong OAuth2 + JWT setup
./scripts/kong-oauth2-jwt-setup.sh

# Generate mTLS certificates
./scripts/generate-mtls-certs.sh
```

## Code Style

### Naming Conventions
- **Packages**: lowercase, concise (e.g., `middleware`, `service`)
- **Structs**: PascalCase, nouns (e.g., `SecurityConfig`, `HeartbeatManager`)
- **Interfaces**: end with `er` (e.g., `Authenticator`, `Validator`)
- **Constants**: UPPER_SNAKE_CASE (e.g., `MAX_RETRY_COUNT`)
- **Variables**: camelCase, avoid abbreviations

### Code Organization
- Business logic in `internal/` directory
- Reusable components in `pkg/` directory
- Max 500 lines per file
- Functions ≤ 50 lines, single responsibility
- Explicit error handling, no ignored errors

### Security Guidelines
- All sensitive data encrypted with AES-256
- User input validation and sanitization
- Parameterized queries to prevent SQL injection
- No plaintext sensitive data in logs
- Unified API response format with error codes

## Testing

### Framework & Structure
- **Unit Tests**: Go standard testing framework
- **Integration Tests**: Database and external service testing
- **Coverage Target**: >80% overall, 100% for critical business logic
- **Test Files**: `*_test.go` alongside implementation files

### Test Execution
```bash
# Run specific package tests
go test ./internal/middleware/...

# Run with race detection
go test -race ./...

# Benchmark tests
go test -bench=. ./...

# Verbose output
go test -v ./...
```

### Testing Requirements
- Every public function must have test cases
- Include both success and error scenarios
- Mock external dependencies appropriately
- Test data should be isolated and reproducible

## CI/CD & Automated Testing

### Local CI Scripts
项目配置了本地自动化测试脚本，确保代码质量：

#### Quick Test（快速测试）
```bash
./scripts/test-quick.sh
```
- **触发时机**：开发过程中手动执行
- **执行内容**：
  - 代码格式检查（gofmt）
  - 静态分析（go vet）
  - 单元测试（go test）
- **耗时**：约 10-30 秒
- **用途**：快速验证代码基础质量

#### Full Test（完整测试）
```bash
./scripts/test-all.sh
```
- **触发时机**：推送前自动执行
- **执行内容**：
  - 包含快速测试的所有内容
  - 构建检查（go build）
  - 安全检查（敏感信息泄露）
- **耗时**：约 1-2 分钟
- **用途**：全面验证代码质量和安全性

#### Coverage Report（覆盖率报告）
```bash
./scripts/test-coverage.sh
```
- **执行内容**：
  - 生成测试覆盖率数据
  - 输出文本和 HTML 报告
  - 计算总体覆盖率
- **输出位置**：`coverage/` 目录
- **用途**：分析测试覆盖情况

### Git Hooks（自动化验证）

#### Pre-commit Hook
- **触发时机**：每次 `git commit` 时
- **执行内容**：`./scripts/test-quick.sh`
- **文件位置**：`.git/hooks/pre-commit`
- **作用**：防止提交有基础问题的代码

#### Pre-push Hook
- **触发时机**：每次 `git push` 时
- **执行内容**：`./scripts/test-all.sh`
- **文件位置**：`.git/hooks/pre-push`
- **作用**：防止推送有问题的代码到远程

### Normal Development Workflow
```bash
# 1. 修改代码
vim internal/api/some_handler.go

# 2. 提交（自动运行快速测试）
git add .
git commit -m "feat: 添加新功能"
# ✅ pre-commit hook 自动运行 test-quick.sh

# 3. 推送（自动运行完整测试）
git push origin main
# ✅ pre-push hook 自动运行 test-all.sh
```

### Skip Verification（跳过验证）
```bash
# 跳过提交前验证（紧急情况）
git commit --no-verify -m "紧急修复"

# 跳过推送前验证（紧急情况）
git push --no-verify origin main
```

### CI/CD Requirements
- **测试覆盖率目标**：
  - 整体覆盖率：> 70%
  - 核心业务逻辑：> 90%
  - 认证授权模块：100%
- **代码质量标准**：
  - 所有代码必须通过 gofmt 格式化
  - 所有代码必须通过 go vet 静态分析
  - 所有公共函数必须有测试用例
- **安全检查**：
  - 不得提交明文密码、密钥、token
  - 不得有未处理的错误
  - 不得有 SQL 注入风险

### CI/CD Documentation
详细配置说明请查看：`docs/CI_SETUP.md`

## Security

### Authentication & Authorization
- **OAuth 2.0**: Client credentials flow
- **JWT Tokens**: 24-hour expiration with refresh capability
- **Bidirectional Auth**: HMAC-SHA256 signature verification
- **Rate Limiting**: 100 req/s with 200 burst capacity

### Data Protection
- **Encryption**: AES-256 for sensitive data at rest
- **Data Masking**: 
  - ID Numbers: 1234****5678
  - Phone Numbers: 138****8000
  - Names: Surname + *
  - Bank Cards: First 6 + last 4 digits
- **PII Protection**: Personal identity information safeguards
- **Audit Logging**: Complete operation tracking

### Security Configuration
```yaml
security:
  jwt_secret: "32-byte-secret-key-here"
  jwt_expiration: 24h
  encryption_key: "32-byte-encryption-key"
  rate_limit_per_second: 100
  rate_limit_burst: 200
  kong_shared_secret: "kong-shared-secret-key-2024"
  service_secret_key: "kyc-service-secret-key-2024"
```

### Environment Security
- Use environment variables for secrets in production
- Enable HTTPS with valid SSL certificates
- Configure strict CORS policies
- Implement IP whitelisting for sensitive endpoints
- Regular security audits and penetration testing

## Configuration

### Environment Variables
```bash
# Core Service
KYC_PORT=8082
KYC_GIN_MODE=debug|release|test
KYC_LOG_LEVEL=debug|info|warn|error

# Database
KYC_DATABASE_HOST=localhost
KYC_DATABASE_PORT=5432
KYC_DATABASE_USER=kyc_user
KYC_DATABASE_PASSWORD=password
KYC_DATABASE_NAME=kyc_db

# Redis
KYC_REDIS_HOST=localhost
KYC_REDIS_PORT=6379

# Security
KYC_SECURITY_JWT_SECRET=32-byte-secret
KYC_SECURITY_ENCRYPTION_KEY=32-byte-key

# Monitoring
KYC_JAEGER_ENDPOINT=http://jaeger:14268/api/traces
```

### Configuration Files
- **config.yaml**: Main service configuration
- **docker-compose.yml**: Container orchestration
- **prometheus/**: Alert rules and metrics configuration
- **grafana/**: Dashboard definitions and data sources

### Service Dependencies
- **PostgreSQL**: Primary data storage
- **Redis**: Distributed caching and rate limiting
- **Kong Gateway**: API management and routing
- **Jaeger**: Distributed tracing
- **Prometheus**: Metrics collection
- **Grafana**: Monitoring dashboards

### Monitoring & Observability
- **Metrics**: HTTP requests, KYC processing times, third-party service calls
- **Alerts**: Authentication failures, high error rates, certificate expiration
- **Tracing**: End-to-end request flow visualization
- **Logging**: Structured logs with correlation IDs

Access Points:
- Grafana: http://localhost:3000 (admin/admin123)
- Prometheus: http://localhost:9090
- Jaeger UI: http://localhost:16686

## Advanced Security Features

### Bidirectional Authentication
The service implements a sophisticated bidirectional authentication system that prevents API gateway bypass attacks:

1. **Kong-to-Service Authentication**: Kong signs requests with HMAC-SHA256
2. **Service-to-Kong Authentication**: Service validates Kong signatures and signs responses
3. **Timestamp Validation**: Prevents replay attacks with 5-minute window
4. **Service Whitelist**: Only authorized services can communicate

### mTLS Certificate Management
- **Certificate Generation**: Automated via `./scripts/generate-mtls-certs.sh`
- **Certificate Rotation**: 90-day validity with automated renewal
- **Certificate Validation**: Mutual verification between services
- **Certificate Storage**: Secure key management practices

### Rate Limiting & DDoS Protection
- **Distributed Rate Limiting**: Redis-based cluster-aware throttling
- **Per-Client Limits**: Granular control by client ID and IP
- **Burst Handling**: Temporary capacity increases for traffic spikes
- **Geographic Restrictions**: Optional country-based access controls

### Data Encryption Standards
- **AES-256-GCM**: Industry-standard symmetric encryption
- **Key Rotation**: Automatic key rotation every 30 days
- **Key Derivation**: PBKDF2 with 100,000 iterations
- **Secure Random**: Cryptographically secure random number generation

## Performance Optimization

### Caching Strategy
- **Redis Cache**: Multi-level caching for hot data
- **HTTP Caching**: ETag and Last-Modified headers
- **Database Query Cache**: Prepared statement caching
- **CDN Integration**: Static asset caching

### Connection Pooling
- **Database Pools**: Optimized PostgreSQL connection management
- **Redis Pools**: Efficient Redis connection reuse
- **HTTP Client Pools**: Reusable HTTP connections for external services
- **WebSocket Management**: Efficient real-time connection handling

### Load Balancing
- **Kong Load Balancing**: Intelligent traffic distribution
- **Health Checks**: Automated service health monitoring
- **Circuit Breakers**: Fail-fast mechanisms for external services
- **Graceful Degradation**: Service degradation under high load

## Development Workflow

### Local Development
```bash
# Start minimal development environment
docker-compose up -d redis jaeger

# Run with hot reload (requires air)
air

# Or run directly
go run cmd/server/main.go
```

### Debugging
- **Structured Logging**: JSON-formatted logs with correlation IDs
- **Distributed Tracing**: Full request flow visualization in Jaeger
- **Metrics Dashboard**: Real-time performance monitoring in Grafana
- **Error Tracking**: Centralized error reporting and analysis

### CI/CD Pipeline
- **Automated Testing**: Unit, integration, and security tests
- **Code Quality**: Linting, formatting, and static analysis
- **Security Scanning**: Vulnerability and dependency checks
- **Deployment**: Automated deployment to staging and production

## Troubleshooting

### Common Issues
1. **Service Startup Failures**: Check port availability and dependencies
2. **Database Connection Issues**: Verify PostgreSQL credentials and network
3. **Redis Connection Problems**: Check Redis availability and configuration
4. **Kong Configuration Errors**: Validate Kong routes and plugins
5. **Certificate Issues**: Verify mTLS certificate validity and permissions

### Log Analysis
- **Application Logs**: `/Users/bytedance/Documents/project/go/d/kyc-service.log`
- **Kong Logs**: `docker logs kong`
- **Database Logs**: PostgreSQL container logs
- **Monitoring Logs**: Prometheus and Grafana logs

### Performance Tuning
- **Database Optimization**: Index analysis and query optimization
- **Cache Hit Rates**: Monitor Redis cache performance
- **Response Times**: Track API latency and throughput
- **Resource Usage**: CPU, memory, and network utilization

This architecture overview provides a comprehensive understanding of the KYC Enterprise Authentication Service, enabling effective development, deployment, and maintenance of the system.