# Verilocale 后端计费与日志架构指南

本文档旨在沉淀 Verilocale 后端关于 **API 计费、统一日志、全链路追踪** 的架构设计与工程实践。这套底座完全支持《Verilocale 账单与结算指引》中定义的“按量付费”与“资源包”商业化模式。

## 1. 架构概览 (The Big Picture)

目前的日志与计费系统采用了 **“统一上下文提取 -> 异步队列解耦 -> 异构存储分发”** 的标准企业级架构。

### 1.1 核心组件
*   **UnifiedContextMiddleware**: 衔接 OpenTelemetry，统一提取和注入 `TraceID`。
*   **KYCUsageMeter**: 计费中间件。负责在业务请求完成后（`defer` 机制），组装强类型的 `BillingPayload`。
*   **AsyncLogWorker**: 纯内存异步日志队列（Go Channel）。实现业务主链路与数据库 IO 的完全解耦。
*   **Storage Interface**: 抽象的存储层，预留了未来从 Postgres 迁移至 ClickHouse 的扩展点。

## 2. 计费数据模型 (Billing Data Model)

我们遵循 **Event Sourcing (事件溯源)** 的设计理念。`usage_logs` 表不仅是日志，更是计费的 **Single Source of Truth (唯一事实来源)**。

### 2.1 核心计费字段
在 `models.UsageLog` 中，以下字段支撑了商业化的各种玩法：
*   **`OrgID`**: 租户维度。所有账单和配额的最终结算实体。
*   **`ServiceType`**: 计费项 (SKU)。如 `ocr`, `liveness_action`。系统可根据 URL 自动推断，也允许业务层覆盖。
*   **`UsageUnits`**: 消耗量。默认 1 次。如果是视频活体，业务层可设为视频秒数。
*   **`Billable`**: 是否纳入计费（布尔值）。
    *   `5xx` 服务端错误 -> 自动免单 (`false`)。
    *   `402` 欠费 / `429` 超频 -> 自动免单 (`false`)。
    *   业务层可根据置信度等条件主动设置 `c.Set("billable", false)`。
*   **`SessionID`**: 会话关联 ID。用于支撑未来“多次 API 调用，一次性打包扣费”的复杂场景。
*   **`Metadata` (JSONB)**: 极其灵活的标签维度（如 `platform`, `region`）。业务线无需修改数据库表结构即可增加新的统计维度。

## 3. 商业化场景落地指南

### 3.1 场景 A：按量付费 (Pay-as-you-go)
*   **数据产生**：API 正常调用产生 `usage_logs`。
*   **出账逻辑**：建议新增每日凌晨的 CronJob（ETL），查出前一天 `Billable=true` 的日志，关联定价表 (Rate Card) 计算金额，汇总至 `monthly_bills`。当累计金额满 5 万时触发扣款流。

### 3.2 场景 B：资源包 (Resource Packages)
*   **额度管理**：复用现有的 `OrganizationQuotas` 表。客户购包后增加 `Allocation`。
*   **实时扣减**：复用现有的 `Quota` 中间件，请求进入时实时扣减 `Consumed`。
*   **预警机制**：需在 `StatsRefresher`（或新建监控任务）中，定期计算 `(Allocation - Consumed) / Allocation`。当达到 20%、10% 时调用 Notification 服务发邮件。
*   **超额降级**：若客户选择“超额转按量”，修改 `Quota` 中间件：当余额为 0 时，不返回 429 拦截，而是放行请求（产生按量日志）。

### 3.3 场景 C：复合能力打包计费 (Bundling)
如果推出 `/bundle/ocr-liveness` 接口：
1.  该入口 API 生成一个唯一的 `SessionID`。
2.  内部调用 OCR 和 Liveness 时，带上这个 `SessionID`，并设置 `Billable=false`（避免原子能力重复扣费）。
3.  入口 API 结束时，抛出一个 `ServiceType=bundle_ocr_live`, `UsageUnits=1`, `Billable=true` 的计费事件。

## 4. 全链路追踪 (Tracing)

系统已实现与 OpenTelemetry (OTel) 的完全闭环：
1.  在 Gin 路由的最外层（`Recovery` 之后），挂载了 `otelgin.Middleware`。它会自动生成符合 W3C 标准的 Root Span 和 TraceID，或继承来自 Kong 网关的 Trace Context。
2.  后续的 `UnifiedContextMiddleware` 会主动从 OTel Context 中提取 `TraceID`。
3.  **最终效果**：您在 SRE 系统日志、`usage_logs`、`audit_logs` 中看到的 `request_id`，可以直接粘贴到 Jaeger / Grafana 中，查询到精确到函数级别的调用火焰图。

## 5. 最佳实践与避坑指南

1.  **绝不在日志中存储 PII 数据**：`usage_logs` 和 `audit_logs` 严禁记录 Request Body 和 Response Body。调试信息请依赖 OTel Span Attributes 或输出到 stdout 供 Loki 采集。
2.  **Panic 安全**：计费事件的产生必须使用 `defer` 机制。即使业务代码 `panic`，`KYCUsageMeter` 依然能捕获并记录一条 `Billable=false` 的日志，防止数据断层。
3.  **数据库迁移安全**：修改数据库结构（如字段类型变更）必须在 `storage.go` 的 `preAutoMigrate` 中进行安全处理（如重命名旧列），严禁暴力删除或忽略 GORM 报错。
