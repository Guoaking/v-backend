# AI First 知识库（v-backend / kyc-service）

> 目标：把“项目现状”沉淀为可被人类与AI共同检索/复用的后端业务实现知识库。
>
> 说明：我无法直接访问公网进行“aifirst 知识库”检索；以下内容基于本仓库代码与常见 AI-First 知识库组织方式抽象而来。

## 1. 现状快照（可运行的事实）

- 服务入口：`cmd/server/main.go:159`
- 配置加载：`internal/config/config.go:104`
  - 支持 `-config <name>`（不含 `.yaml`）选择配置文件名（例如 `config.local`）
- 健康检查：`/health`（支持双向签名返回头）
  - 路由注册：`cmd/server/main.go:865`
  - 处理器：`internal/middleware/bidirectional_auth.go:187`
- 监控指标：`/metrics`（Prometheus handler）
  - 路由注册：`cmd/server/main.go:873`
- OAuth2（client_credentials）Token：`POST /api/v1/oauth/token`
  - 路由注册：`cmd/server/main.go:995`
  - 业务实现：`internal/api/auth_handler.go:60`
- KYC 业务接口示例：
  - OCR：`POST /api/v1/kyc/ocr`，实现：`internal/api/kyc_handler.go:37`
  - 人脸搜索：`POST /api/v1/kyc/face/search`，实现：`internal/api/kyc_handler.go:138`

## 2. AI First 知识库的组织方式（建议）

把知识拆成“可检索的最小单元”，每个条目满足：

1) **问题/意图**（一句话）
2) **结论**（一段话）
3) **证据**（代码引用到具体文件行号）
4) **操作步骤**（可复制的命令/API 示例）
5) **风险/边界**（会踩坑的点）
6) **下一步**（TODO / 改进方向）

后端项目最常用的条目类型：

- Runbook：如何启动、如何排障
- 业务流程：鉴权、配额、KYC 请求链路
- 数据模型：核心表与索引、迁移策略
- API 契约：入口、鉴权方式、错误码
- 质量保障：本地 CI、Git hooks、发布前清单

## 3. 本地启动 Runbook

### 3.1 依赖（PostgreSQL + Redis）

- Docker 方式（示例网络名自行统一；本项目 docker-compose 使用 `shared-network`，见 `docker-compose.yml:78`）

```bash
# PostgreSQL
docker run -d --name database -p 5432:5432 \
  -e POSTGRES_USER=kong -e POSTGRES_PASSWORD=kongpassword -e POSTGRES_DB=kong \
  postgres:15-alpine

# Redis
docker run -d --name redis -p 6379:6379 redis:7
```

### 3.2 构建与启动

```bash
go build -o kyc-service ./cmd/server/main.go

# 使用 config.local.yaml（注意参数不带 .yaml 扩展名）
./kyc-service -config config.local
```

配置加载逻辑参考：`internal/config/config.go:104`。

### 3.3 验证

```bash
curl http://localhost:8082/health
curl http://localhost:8082/metrics
```

健康检查返回结构参考：`internal/middleware/bidirectional_auth.go:200`。

## 4. 配置与优先级

### 4.1 配置文件

- 默认读取 `config.yaml`（`-config config`）
- 本地常用：`config.local.yaml`（`-config config.local`）

配置文件示例：`config.local.yaml:1`。

### 4.2 环境变量覆盖

`viper` 使用 `KYC_` 前缀并将 `.` 替换为 `_`：`internal/config/config.go:126`。

示例：

```bash
KYC_PORT=8082 \
KYC_DATABASE_HOST=localhost \
KYC_DATABASE_PORT=5432 \
KYC_REDIS_HOST=localhost \
./kyc-service -config config.local
```

## 5. 业务域（Domain）与关键模型

### 5.1 核心实体

- `models.Organization`（租户）：`internal/models/models.go:90`
- `models.User`（用户）：`internal/models/models.go:49`
- `models.APIKey`（API Key）：`internal/models/models.go:115`
- `models.OAuthClient`（OAuth 客户端）：`internal/models/models.go:72`
- `models.OAuthToken`（OAuth Token）：`internal/models/models.go:152`
- `models.KYCRequest`（KYC 请求流水）：`internal/models/models.go:11`

### 5.2 KYCRequest 数据保护策略（现状）

`KYCRequest` 中对 PII 字段默认 `json:"-"`，避免直接 API 输出：`internal/models/models.go:17`。

## 6. 认证与鉴权（AuthN / AuthZ）

### 6.1 OAuth2（Client Credentials）

- 路由：`POST /api/v1/oauth/token`：`cmd/server/main.go:995`
- scope 策略：如果请求 scope 为空则使用客户端预设 scopes；否则必须为预设 scopes 子集：`internal/api/auth_handler.go:85`
- token 缓存：Redis（可选）+ DB 命中复用（>5min）：`internal/api/auth_handler.go:106`

### 6.2 双向鉴权（Kong ↔ Service）

- 中间件实现：`internal/middleware/bidirectional_auth.go:39`
- 当前在 `main.go` 中默认注释掉了请求侧校验（仅保留健康检查返回签名头）：`cmd/server/main.go:860`

### 6.3 权限体系（console/admin/org）

- API v1 路由组：`cmd/server/main.go:876`
- Console（JWT + 权限/组织头）：`cmd/server/main.go:903`
- Admin（平台管理员）：`cmd/server/main.go:954`

## 7. API 入口索引（按业务分组）

### 7.1 基础

- `GET /health`：`cmd/server/main.go:865`
- `GET /metrics`：`cmd/server/main.go:873`
- `GET /docs`（Swagger UI）：`cmd/server/main.go:1025`

### 7.2 OAuth2

- `POST /api/v1/oauth/token`：`cmd/server/main.go:995`
- `POST /api/v1/oauth/refresh`：`cmd/server/main.go:1000`
- `POST /api/v1/oauth/revoke`：`cmd/server/main.go:1001`
- `POST /api/v1/oauth/introspect`：`cmd/server/main.go:1002`

### 7.3 KYC 核心

实现集中在：`internal/api/kyc_handler.go:37`。

## 8. 数据库与迁移策略

### 8.1 连接与自动迁移

- 初始化：`internal/storage/storage.go:27`
- AutoMigrate 开关：`config.local.yaml:18`
- 迁移执行：逐模型 AutoMigrate（便于定位问题）：`internal/storage/storage.go:97`

### 8.2 已知坑（已修复）

- 之前手动 `CREATE TABLE kyc_requests` + 后续 AutoMigrate 可能引发 PostgreSQL 元数据冲突；现已移除手动建表，仅保留模型迁移：`internal/storage/storage.go:97`

## 9. 可观测性

- OTel 指标初始化：`cmd/server/main.go:181`
- 指标暴露：`/metrics`：`cmd/server/main.go:873`

## 10. 质量保障（本地 CI）

- 快速校验：`scripts/test-quick.sh:20`（gofmt + go vet + go test）
- 全量校验：`scripts/test-all.sh:44`（含 race/build/敏感信息扫描）
- 覆盖率：`scripts/test-coverage.sh:28`

## 11. 常见问题（Troubleshooting）

### 11.1 `-config config.local.yaml` 为什么不生效？

- 因为参数约定为“不含扩展名”，会在 Load 时做 `TrimSuffix(".yaml")`：`internal/config/config.go:104`
- 正确用法：`./kyc-service -config config.local`

### 11.2 端口被占用

- 先用 `lsof -i :8082` 查占用者，再决定是否停止或改端口。
- 健康检查验证：`curl http://localhost:8082/health`。

## 12. AI First：给模型用的 Prompt 模板（可直接复制）

### 12.1 定位业务实现

```
你是资深Go后端工程师。
请在仓库中定位“<业务能力>”的实现，按以下格式输出：
1) 入口路由与文件行号
2) Handler 主要逻辑（只概述关键分支）
3) Service/Storage/Model 依赖链与文件行号
4) 数据库表/字段（从 models 推断）
5) 常见错误码与返回格式
```

### 12.2 生成可执行 Runbook

```
请基于当前仓库代码，生成“从零启动服务”的Runbook：
- 依赖（Postgres/Redis）启动命令
- 配置文件建议与启动命令
- 健康检查与关键接口验证命令
- 常见失败原因与处理步骤
要求每条步骤都带文件证据（path:line）。
```

