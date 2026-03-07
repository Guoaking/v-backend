# 仓库概览与更新记录

本文件基于自动扫描结果整理，概述项目入口、目录结构、配置、脚本与测试，以及本次更新记录，便于快速理解与维护。

## 项目入口

- 服务入口：[main.go](file:///Users/bytedance/Documents/project/go/v/v-backend/cmd/server/main.go)
- 负载测试入口：[main.go](file:///Users/bytedance/Documents/project/go/v/v-backend/cmd/kyc-loadtest/main.go)
- 邮件工具入口：[mail.go](file:///Users/bytedance/Documents/project/go/v/v-backend/cmd/mail/mail.go)

## 目录结构（关键路径）

- 接口层：[internal/api](file:///Users/bytedance/Documents/project/go/v/v-backend/internal/api/)
- 中间件：[internal/middleware](file:///Users/bytedance/Documents/project/go/v/v-backend/internal/middleware/)
- 业务服务：[internal/service](file:///Users/bytedance/Documents/project/go/v/v-backend/internal/service/)
- 任务与后台作业：[internal/tasks](file:///Users/bytedance/Documents/project/go/v/v-backend/internal/tasks/)
- 配置与初始化：[internal/config](file:///Users/bytedance/Documents/project/go/v/v-backend/internal/config/)
- 公共组件：[pkg](file:///Users/bytedance/Documents/project/go/v/v-backend/pkg/)

## 配置与文档

- 主配置：[config.yaml](file:///Users/bytedance/Documents/project/go/v/v-backend/config.yaml)（含 dev/prod 变体）
- 编排配置：[docker-compose.yml](file:///Users/bytedance/Documents/project/go/v/v-backend/docker-compose.yml)
- 监控与告警：Prometheus/Grafana 配置位于 [prometheus/](file:///Users/bytedance/Documents/project/go/v/v-backend/prometheus/) 与 [grafana/](file:///Users/bytedance/Documents/project/go/v/v-backend/grafana/)
- API 文档：Swagger 定义位于 [docs/swagger.yaml](file:///Users/bytedance/Documents/project/go/v/v-backend/docs/swagger.yaml)
- 参考文档：[
  README.md](file:///Users/bytedance/Documents/project/go/v/v-backend/README.md)、[
  AGENTS.md](file:///Users/bytedance/Documents/project/go/v/v-backend/AGENTS.md)、[
  CI_SETUP.md](file:///Users/bytedance/Documents/project/go/v/v-backend/docs/CI_SETUP.md)

## 脚本与工具（选摘）

- 测试与验证：[
  scripts/test-quick.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/test-quick.sh)、[
  test-all.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/test-all.sh)、[
  test-coverage.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/test-coverage.sh)
- 部署与运维：[
  scripts/deploy-bidirectional-auth.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/deploy-bidirectional-auth.sh)、[
  stop-services.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/stop-services.sh)
- 安全与证书：[
  scripts/generate-mtls-certs.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/generate-mtls-certs.sh)
- Kong 管理：[
  scripts/kong-oauth2-jwt-setup.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/scripts/kong-oauth2-jwt-setup.sh)
- 负载测试：[
  cmd/kyc-loadtest/run-tests.sh](file:///Users/bytedance/Documents/project/go/v/v-backend/cmd/kyc-loadtest/run-tests.sh)

## 运行与验证

```bash
# 安装依赖
go mod download

# 启动基础服务（示例）
docker-compose up -d redis jaeger

# 本地运行服务
go run cmd/server/main.go

# 运行测试
go test ./...

# 生成覆盖率报告（本仓库脚本）
./scripts/test-coverage.sh
```

## 更新记录

- 2026-03-07：新增本概览文档，整理仓库入口、结构、配置、脚本与验证指令；仅文档更新，未改动业务代码。

## 项目定位与目标

### 定位

- 企业级 KYC 身份认证微服务，面向 B2B 与平台型业务的统一身份核验中心。
- 安全优先，通过 Kong 网关统一接入，对外提供标准化 API。
- 支撑身份证件 OCR、人脸识别、活体检测等合规核验流程，强调可审计与可观测。

### 目标

- 安全与合规：OAuth2/JWT、mTLS、HMAC 双向签名、AES-256 加密、PII 脱敏与审计。
- 可靠与高性能：Redis 分布式限流与突发流量处理，支撑高并发。
- 可观测与可维护：Prometheus/Grafana 指标与告警，Jaeger 全链路追踪，统一健康检查。
- 工程效率：完善脚本与 CI 流程，单元/集成测试覆盖，预提交/预推送自动校验。
- 网关防绕过：双向鉴权与时间戳校验，确保所有流量经授权入口到达后端。

### 核心能力

- 认证授权：OAuth2 客户端凭证 + JWT（24h 过期、可刷新）、RBAC 中间件。
- 双向鉴权与防重放：Kong → 服务 HMAC 校验，服务 → Kong 签名响应，5 分钟时间窗。
- 生物核验：OCR、人脸识别、WebSocket 活体检测的实时通道与回传。
- 数据保护：AES-256-GCM 加密、PII 数据掩码、密钥轮转与安全随机。
- 流量治理：Redis 限流、突发流量处理、可选地理/白名单策略。
- 可观测：系统/业务指标、告警规则、追踪与结构化日志。

### 典型场景

- 新用户开户/入驻（KYC Onboarding）与高风险操作的二次校验。
- 企业客户或内部微服务通过 Kong 接入身份核验能力。
- 合规审计与事后追踪，基于指标与日志复盘核验链路。

### 技术架构

- 入口：Kong Gateway（HTTP/HTTPS）路由到后端 Gin 服务（8082）。
- 存储与缓存：PostgreSQL（主数据）+ Redis（限流与缓存）。
- 可观测：Prometheus + Grafana（指标/告警），Jaeger（分布式追踪）。
- 代码分层：接口层、中间件、服务、任务、公共库；入口位于 [cmd/server/main.go](file:///Users/bytedance/Documents/project/go/v/v-backend/cmd/server/main.go)。

### 边界与非目标

- 不提供前端页面或复杂人工审核工作流，聚焦后端核验能力与接口。
- 不存储明文敏感数据；生产环境使用环境变量与安全管理。
- 长周期合规模型与文档管理不在核心范围，可通过接口与外部系统协作。

### 关键指标

- 安全：零明文敏感信息、密钥安全、双向鉴权有效、重放防护命中率。
- 性能：限流下稳定吞吐、P95/P99 延迟、错误率与重试率。
- 可观测：核心业务与系统指标齐备、关键链路具备追踪、告警覆盖关键事件。
- 质量：测试覆盖率达标、提交/推送阶段自动化校验通过率。

### 差异化价值

- 双向鉴权与网关防绕过的体系化设计，降低越权与旁路风险。
- 全链路安全与合规实践（mTLS、HMAC、AES、掩码），满足企业级要求。
- 运行即观测的工程化落地，易于上线与运维，降低集成与排障成本。
- 脚本与文档完善，快速启动与验证，缩短接入与试运行周期。

### 演进方向

- 风险与策略引擎：自适应核验策略、ABAC 与细粒度审计。
- 多区域与灾备：跨区部署、密钥管理与证书轮转自动化增强。
- 测试与压测：扩大单元/集成覆盖面，完善负载与故障注入场景。
- 服务网格与零信任：更细致的服务间认证与策略控制。
- 合规扩展：对接更多监管要求与审计模板，强化数据生命周期管理。

参考入口与文档：

- 架构综述：[AGENTS.md](file:///Users/bytedance/Documents/project/go/v/v-backend/AGENTS.md)
- 概览文档：[REPO_OVERVIEW.md](file:///Users/bytedance/Documents/project/go/v/v-backend/docs/REPO_OVERVIEW.md)

## 前端项目参考与集成建议

本后端项目存在配套的前端项目，建议以下几种常见联动方式，便于偶尔参考与协同开发：

### 推荐：兄弟目录 + 多根工作区（不使用软链接）

- 将两个项目置于同一父目录下：
  ```
  /path/to/workspace/
    frontend/
    v-backend/
  ```
- 使用 IDE 的多根工作区（VS Code 示例，保存为 `workspace.code-workspace`）：
  ```json
  {
    "folders": [{ "path": "v-backend" }, { "path": "frontend" }]
  }
  ```
- 优点：无平台兼容性问题；避免软链接导致的工具索引、监控循环与打包问题；清晰的职责边界。

### 备选：Git Submodule（前端作为子模块）

- 在后端中引入前端子模块（需前端独立仓库）：
  ```bash
  git submodule add <frontend_git_url> frontend
  git submodule update --init --recursive
  ```
- 优点：版本绑定清晰，适合一体化发布流程；缺点：对子模块的操作有学习成本。

### 仅本地参考：单向软链接（避免双向与循环）

- 若确需在后端内偶尔浏览前端代码，可在本地创建单向软链接，并忽略提交：
  ```bash
  ln -s /absolute/path/to/frontend ./_frontend
  ```
- 将软链接加入忽略列表，避免提交到仓库：
  ```
  # .gitignore
  _frontend
  ```
- 注意：
  - 避免双向软链接（前端指向后端、后端指向前端），易引发工具递归扫描与循环观察。
  - Windows 对软链接支持与权限不一致，跨平台团队建议使用上方两种方案。

### 联动脚本（可选）

- 可在根目录新增简单脚本，联动启动前后端（示例）：
  ```bash
  # scripts/dev-run.sh
  (cd cmd/server && go run main.go) &
  (cd ../frontend && npm run dev)
  ```
- 脚本仅供本地开发参考，避免在生产工作流中强绑定两个项目的生命周期。
