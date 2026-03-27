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
- 存储系统（Storage Refactor）：
  - 接口：`StorageService` 在 `internal/storage/storage_service.go`
  - 策略路由：基于 `AccessRule`（获取）和 `UploadRule`（上传）配置。
  - Nginx 联动：生产环境通过 `X-Accel-Redirect` 返回 `/_protected/storage/` 开头的内部路径。
  - 本地开发：支持 `Smart Serving`（直接流式下发），无需 Nginx。

## 2. 前端未使用的后端接口（Dead Routes Analysis）

> **注意**: 以下接口在后端 `main.go` 中注册了，但前端没有任何服务调用它们。
> **风险**: 长期维护这些接口可能导致安全风险或代码腐烂。

**1. 认证与令牌相关 (Auth & Token)**
- `POST /api/v1/token/generate`: 后端定义了用于生成 Kong JWT 的接口，但前端目前通过 API Key 流程获取。
- `GET /api/v1/auth/me`: 前端统一使用了 `/api/v1/console/users/me`。
- `POST /api/v1/auth/google`: 虽有 Google 登录后端逻辑，但前端登录页尚未集成。
- `POST /api/v1/oauth/token`, `refresh`, `revoke`, `introspect`: 完整的 OAuth2.0 流程路由，目前前端控制台主要使用基于 JWT 的 Session。

**2. 冗余或旧版接口 (Redundant/Legacy)**
- `GET /api/v1/console/usage`: 前端已转向更细粒度的 `/usage/stats` 或 `/usage/daily`。
- `GET /api/v1/console/me/notifications`: 被更通用的 `/api/v1/users/me/notifications` 替代。
- `PUT /api/v1/console/me/notifications/:id/read`: 同上。
- `POST /api/v1/invitations/accept`: 前端使用带 ID 的版本 `/api/v1/users/me/invitations/:id/accept`。

**3. 管理与配置接口 (Admin & Config)**
- `PUT /api/v1/admin/config/plans/:plan_id/quota`: 计划配额管理接口在前端管理后台暂无对应 UI。
- `PUT /api/v1/admin/config/global`: 全局配置更新接口未使用。
- `POST /api/v1/admin/permissions`: 前端尝试调用 `/meta/permissions`（路径不匹配）。

**4. 系统与文档接口 (System & Docs)**
- `POST /api/v1/notifications/email`: 用于发送邮件的通用接口，前端目前由业务逻辑触发。
- `GET /.well-known/oauth-authorization-server` & `GET /jwks.json`: 发现服务相关的标准接口。
- `GET /api/v1/docs/security` & `GET /api/v1/docs/error-codes`: 静态文档接口。
- `GET /api/v1/orgs/:org_id/usage/summary`: 组织用量汇总接口未使用。

## 3. 潜在的接口不匹配 (Broken Features)

> **注意**: 以下功能在前端调用时会直接报 `404 Not Found`，因为后端没有对应的路由或路径不一致。

| 功能模块 | 前端调用路径 (v-frontend) | 后端实际路径 (v-backend) | 状态 |
| :--- | :--- | :--- | :--- |
| **Webhook集成** | `GET/POST /integrations/webhooks` | **不存在** | ❌ 缺失 |
| **退出组织** | `POST /api/v1/orgs/:id/leave` | **不存在** | ❌ 缺失 |
| **修改密码** | `PUT /users/me/password` | `PUT /console/users/me/password` | ⚠️ 路径不一致 |
| **注销账号** | `DELETE /users/me` | `DELETE /console/users/me` | ⚠️ 路径不一致 |
| **OAuth密钥重置**| `POST .../reset-secret` | `POST .../rotate` | ⚠️ 动作词不一致 |
| **用户状态切换** | `POST /admin/users/:id/toggle-status`| `PUT /admin/users/:id/status` | ⚠️ 方法与路径不一致 |

## 4. AI First 知识库的组织方式（建议）

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
- 架构指南：计费、日志、全链路追踪设计 (`docs/architecture/BILLING_AND_LOGGING.md`)

## 5. 本地启动 Runbook

### 5.1 依赖（PostgreSQL + Redis）

- Docker 方式（示例网络名自行统一；本项目 docker-compose 使用 `shared-network`，见 `docker-compose.yml:78`）

```bash
# PostgreSQL
docker run -d --name database -p 5432:5432 \
  -e POSTGRES_USER=kong -e POSTGRES_PASSWORD=kongpassword -e POSTGRES_DB=kong \
  postgres:15-alpine

# Redis
docker run -d --name redis -p 6379:6379 redis:7
```

### 5.2 构建与启动

```bash
go build -o kyc-service ./cmd/server/main.go

# 使用 config.local.yaml（注意参数不带 .yaml 扩展名）
./kyc-service -config config.local
```

配置加载逻辑参考：`internal/config/config.go:104`。

### 5.3 验证

```bash
curl http://localhost:8082/health
curl http://localhost:8082/metrics
```

健康检查返回结构参考：`internal/middleware/bidirectional_auth.go:200`。

## 6. 配置与优先级

### 6.1 配置文件

- 默认读取 `config.yaml`（`-config config`）
- 本地常用：`config.local.yaml`（`-config config.local`）

配置文件示例：`config.local.yaml:1`。

### 6.2 环境变量覆盖

`viper` 使用 `KYC_` 前缀并将 `.` 替换为 `_`：`internal/config/config.go:126`。

示例：

```bash
KYC_PORT=8082 \
KYC_DATABASE_HOST=localhost \
KYC_DATABASE_PORT=5432 \
KYC_REDIS_HOST=localhost \
./kyc-service -config config.local
```

## 7. 业务域（Domain）与关键模型

### 7.1 核心实体

- `models.Organization`（租户）：`internal/models/models.go:90`
- `models.User`（用户）：`internal/models/models.go:49`
- `models.APIKey`（API Key）：`internal/models/models.go:115`
- `models.OAuthClient`（OAuth 客户端）：`internal/models/models.go:72`
- `models.OAuthToken`（OAuth Token）：`internal/models/models.go:152`
- `models.KYCRequest`（KYC 请求流水）：`internal/models/models.go:11`

### 7.2 KYCRequest 数据保护策略（现状）

`KYCRequest` 中对 PII 字段默认 `json:"-"`，避免直接 API 输出：`internal/models/models.go:17`。

## 8. 认证与鉴权（AuthN / AuthZ）

### 8.1 OAuth2（Client Credentials）

- 路由：`POST /api/v1/oauth/token`：`cmd/server/main.go:995`
- scope 策略：如果请求 scope 为空则使用客户端预设 scopes；否则必须为预设 scopes 子集：`internal/api/auth_handler.go:85`
- token 缓存：Redis（可选）+ DB 命中复用（>5min）：`internal/api/auth_handler.go:106`

### 8.2 双向鉴权（Kong ↔ Service）

- 中间件实现：`internal/middleware/bidirectional_auth.go:39`
- 当前在 `main.go` 中默认注释掉了请求侧校验（仅保留健康检查返回签名头）：`cmd/server/main.go:860`

### 8.3 权限体系（console/admin/org）

- API v1 路由组：`cmd/server/main.go:876`
- Console（JWT + 权限/组织头）：`cmd/server/main.go:903`
- Admin（平台管理员）：`cmd/server/main.go:954`

## 9. API 入口索引（按业务分组）

### 9.1 基础

- `GET /health`：`cmd/server/main.go:865`
- `GET /metrics`：`cmd/server/main.go:873`
- `GET /docs`（Swagger UI）：`cmd/server/main.go:1025`

### 9.2 OAuth2

- `POST /api/v1/oauth/token`：`cmd/server/main.go:995`
- `POST /api/v1/oauth/refresh`：`cmd/server/main.go:1000`
- `POST /api/v1/oauth/revoke`：`cmd/server/main.go:1001`
- `POST /api/v1/oauth/introspect`：`cmd/server/main.go:1002`

### 9.3 KYC 核心

实现集中在：`internal/api/kyc_handler.go:37`。

## 10. 数据库与迁移策略

### 10.1 连接与自动迁移

- 初始化：`internal/storage/storage.go:27`
- AutoMigrate 开关：`config.local.yaml:18`
- 迁移执行：逐模型 AutoMigrate（便于定位问题）：`internal/storage/storage.go:97`

### 10.2 已知坑（已修复）

- 之前手动 `CREATE TABLE kyc_requests` + 后续 AutoMigrate 可能引发 PostgreSQL 元数据冲突；现已移除手动建表，仅保留模型迁移：`internal/storage/storage.go:97`

## 11. 可观测性

- OTel 指标初始化：`cmd/server/main.go:181`
- 指标暴露：`/metrics`：`cmd/server/main.go:873`

## 12. 质量保障（本地 CI）

- 快速校验：`scripts/test-quick.sh:20`（gofmt + go vet + go test）
- 全量校验：`scripts/test-all.sh:44`（含 race/build/敏感信息扫描）
- 覆盖率：`scripts/test-coverage.sh:28`

## 13. 常见问题（Troubleshooting）

### 13.1 `-config config.local.yaml` 为什么不生效？

- 因为参数约定为“不含扩展名”，会在 Load 时做 `TrimSuffix(".yaml")`：`internal/config/config.go:104`
- 正确用法：`./kyc-service -config config.local`

### 13.2 端口被占用

- 先用 `lsof -i :8082` 查占用者，再决定是否停止或改端口。
- 健康检查验证：`curl http://localhost:8082/health`。

## 14. AI First：给模型用的 Prompt 模板（可直接复制）

### 14.1 定位业务实现

```
你是资深Go后端工程师。
请在仓库中定位“<业务能力>”的实现，按以下格式输出：
1) 入口路由与文件行号
2) Handler 主要逻辑（只概述关键分支）
3) Service/Storage/Model 依赖链与文件行号
4) 数据库表/字段（从 models 推断）
5) 常见错误码与返回格式
```

### 14.2 生成可执行 Runbook

```
请基于当前仓库代码，生成“从零启动服务”的Runbook：
- 依赖（Postgres/Redis）启动命令
- 配置文件建议与启动命令
- 健康检查与关键接口验证命令
- 常见失败原因与处理步骤
要求每条步骤都带文件证据（path:line）。
```
