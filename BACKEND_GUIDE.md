# 后端项目开发规范与指南 (Backend Development Guidelines)

为确保 Go 后端代码质量、稳定性及可维护性，请严格遵守以下开发规范。

## 1. 核心原则 (Core Principles)

- **架构一致性 (Architectural Consistency)**:
  - **鉴权 (AuthN)**: 遵循 [统一鉴权方案](docs/AUTH_UNIFICATION_PLAN.md)。优先使用 OAuth2/JWT，API Key 视为特殊 Client。
  - **计费 (Billing)**: 所有消耗资源的操作必须通过 `checkAndConsumeQuota`。

- **API 契约优先 (API Contract First)**:
  - 在开发新接口前，必须先定义 Request/Response 结构体。
  - **严禁**使用 `gin.H` 或 `map[string]any` 作为 API 响应体（除非是 `204 No Content`）。所有响应必须有对应的强类型结构体定义。
  - **Swagger 文档**: 所有 Handler 必须包含完整的 Swagger 注释 (`// @Summary`, `// @Success`)，确保文档与代码同步。

- **代码复杂度控制**:
  - **文件行数限制**: 单个 Go 文件不得超过 **1000行**。若逻辑过于复杂，请拆分 Service、Helper 或 Utils。
  - **函数长度**: 单个函数建议控制在 50 行以内，圈复杂度（Cyclomatic Complexity）不宜过高。

- **Mock 与真实环境**:
  - **Mock 不可信**: 单元测试可以使用 Mock，但**必须**包含针对真实数据库/依赖的集成测试（Integration Test）。
  - **环境一致性**: 开发环境（Dev）应尽可能模拟生产环境配置，避免“在我机器上能跑”的问题。

## 2. 代码风格 (Coding Style)

- **项目结构 (Project Layout)**:
  - `internal/api`: HTTP Handler 层，只负责参数解析、校验和响应格式化。**严禁**在 Handler 中写业务逻辑。
  - `internal/service`: 业务逻辑层，负责核心业务处理。
  - `internal/storage` (或 `repository`): 数据访问层，负责 DB/Redis 操作。
  - `internal/config`: 配置管理。

- **错误处理 (Error Handling)**:
  - **Wrap Errors**: 使用 `fmt.Errorf("action failed: %w", err)` 包装错误，保留调用栈上下文。
  - **统一错误码**: API 错误响应必须遵循统一的 Error Code 规范（如 `CodeBusinessError`, `CodeUnauthorized`），禁止随意返回 500。
  - **日志**: 使用结构化日志（如 `slog` 或 `zap`），记录 `request_id`, `user_id` 等关键字段。

- **并发安全**:
  - 在 Handler 中启动 Goroutine 必须处理 `panic` 恢复。
  - 共享变量（如 Map）必须加锁 (`sync.RWMutex`) 或使用并发安全容器。

## 3. 开发流程 (Workflow)

1.  **定义领域模型**: 在 `internal/model` 或 `service` 中定义核心数据结构。
2.  **定义接口**: 编写 Swagger 注释和 Request/Response 结构体。
3.  **实现 Service**: 编写业务逻辑，并编写对应的单元测试。
4.  **实现 Handler**: 调用 Service，处理 HTTP 协议细节。
5.  **集成测试**: 启动真实 Server，使用 Postman 或 E2E 脚本验证接口。

## 4. 提交检查 (Pre-Commit Checklist)

- [ ] 响应体是否使用了强类型结构体？（拒绝 `gin.H`）
- [ ] 文件是否超过 1000 行？
- [ ] 是否添加了 Swagger 注释？
- [ ] 是否处理了 nil 指针和 error？
- [ ] 是否在真实环境下验证过接口？

## 5. 可观测性与审计 (Observability & Auditability)

后端服务必须具备完善的可观测性，以便快速定位问题、分析性能瓶颈及满足合规要求。

- **结构化日志 (Structured Logging)**:
  - **强制使用**结构化日志库（如 `logrus` 或 `zap`），严禁使用 `fmt.Println`。
  - **Context 传递**: 必须在所有层级（Handler -> Service -> Repository）传递 `context.Context`，并在日志中包含 `request_id` (Trace ID)。
  - **关键字段**: 每条日志应包含 `level`, `time`, `msg`, `request_id`, `user_id` (如适用), `error` (如有)。
  - **脱敏**: 严禁打印敏感数据（如密码、密钥、身份证号、人脸图片Base64等）。

- **指标监控 (Metrics)**:
  - **Prometheus**: 关键业务逻辑（如三方调用、核心算法、高频接口）必须埋点。
  - **维度**:
    - **Counter**: 请求次数、错误次数（按 `type`, `code` 标签区分）。
    - **Histogram**: 接口耗时、外部调用耗时（按 `p95`, `p99` 观察）。
  - **命名规范**: `kyc_<subsystem>_<action>_<metric_type>` (e.g., `kyc_liveness_action_verify_duration_seconds`).

- **审计日志 (Audit Logging)**:
  - **关键操作**: 所有**写操作**（增、删、改）及敏感信息的**读操作**必须记录审计日志。
  - **内容**: `who` (User/Org), `when` (Timestamp), `what` (Action/Resource), `result` (Success/Failure), `client_ip`.
  - **存储**: 审计日志应持久化存储（如 DB `audit_logs` 表或发送至专用审计服务），不可仅打印到控制台。

## 6. 新功能开发清单 (Feature Development Checklist)

在提交代码前，请确保新功能满足以下所有要求（以 `ActionLiveness` 为例）：

- [ ] **API 规范**: 定义了 Request/Response 结构体，添加了 Swagger 注释。
- [ ] **鉴权**: 接口已通过中间件保护（`RequireKeyScope` 或统一鉴权）。
- [ ] **计费 (Quota)**: 关键资源消耗点（如 Upload/Verify）是否调用了 `checkAndConsumeQuota`？
- [ ] **审计 (Audit)**:
  - [ ] 创建了 `KYCRequest` 记录？
  - [ ] 敏感数据访问是否调用了 `metrics.RecordSensitiveDataAccess`？
  - [ ] 关键操作是否记录了 `AuditLog`？
- [ ] **可观测性 (Observability)**:
  - [ ] 入口处是否添加了 `tracing.StartSpan`？
  - [ ] 是否记录了业务指标（`metrics.RecordBusinessOperation`）？
  - [ ] 是否记录了成功率/耗时指标？
- [ ] **错误处理**: 是否使用了统一的 `Code*` 错误码？

---

## 7. 参考文档 (References)

- [AI First 知识库](docs/AI_FIRST_BACKEND_KB.md): 快速检索代码实现与运维命令。
- [鉴权统一方案](docs/AUTH_UNIFICATION_PLAN.md): 了解鉴权架构演进方向。
- [STS 架构](docs/STS_ARCHITECTURE.md): 了解临时 Token 机制。

_遵循本指南，构建健壮、高效、可扩展的 Go 后端服务。_
