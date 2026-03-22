# Verilocale 后端计费与日志架构指南

本文档旨在沉淀 Verilocale 后端关于 **API 计费、统一日志、全链路追踪** 的架构设计与工程实践。这套底座完全支持《Verilocale 账单与结算指引》中定义的“按量付费”与“资源包”商业化模式。

## 1. 架构概览 (The Big Picture)

目前的日志与计费系统采用了 **“统一上下文提取 -> 异步队列解耦 -> 异构存储分发”** 的标准企业级架构。

### 1.1 核心组件

- **UnifiedContextMiddleware**: 衔接 OpenTelemetry，统一提取和注入 `TraceID`。
- **KYCUsageMeter**: 计费中间件。负责在业务请求完成后（`defer` 机制），组装强类型的 `BillingPayload`。
- **RedisLogWorker**: 基于 Redis Stream 的可靠异步日志队列。实现业务主链路与数据库 IO 的完全解耦。
- **Storage Interface**: 抽象的存储层，预留了未来从 Postgres 迁移至 ClickHouse 的扩展点。

### 1.2 异步日志处理与可靠队列 (Redis Stream)

为了不阻塞核心业务流程，并保证日志处理的可靠性与 Exactly-Once 语义，我们采用了基于 **Redis Stream** 的异步消费者组架构：

1.  **API Handler / Middleware** (Producer)
    - 构造强类型的 `LogEnvelope`
    - 通过 `LogWorker.Enqueue()` 写入 Redis Stream (`kyc:logs:billing:stream` / `kyc:logs:audit:stream`)
    - 如果入队失败，则记录错误。
2.  **RedisLogWorker** (Consumer Group)
    - 通过 `XREADGROUP` 阻塞消费流中的数据
    - 对读取到的一批日志进行反序列化
    - **严格 DB 事务**：将明细日志的批量 Insert 和聚合表的增量 Upsert 放入同一个 GORM Transaction 中执行。
    - 只有在事务成功提交后，才会对这批消息执行 `XACK`。如果事务失败，消息会留在 Pending 队列 (PEL) 中，并由恢复协程 (`XCLAIM`) 重新处理，从而保证数据不丢失。

这种模式既避免了 Go Channel 在进程崩溃时的数据丢失，又避免了行级锁争用带来的性能问题。

## 2. 计费数据模型 (Billing Data Model)

### 2.1 Single Source of Truth (SSOT)

- `usage_logs` 表是所有计费和用量统计的唯一事实来源。
- 所有计费点扣减、超额熔断和前端报表，必须基于 `usage_logs` 计算，严禁使用其他可能产生不一致的副表作为主数据源。

### 2.2 混合维度单聚合表 (Hybrid Dimensional Aggregation)

为了兼顾 MVP 阶段的开发效率和未来亿级数据的可扩展性，系统采用**单表维度建模**，彻底废弃早期的 `usage_daily_*` 多表碎片化设计。

- **表名**: `usage_metric_aggs`
- **核心思想**:
  - **高频核心维度**（`org_id`, `metric_name`, `time_unit`, `stat_time`）提取为原生关系型列，建立 B-Tree 索引，用于硬隔离和高效范围查询。
  - **长尾业务维度**（如 `endpoint`, `api_key_id`, `status_code` 等）统一存入 `dimensions` (JSONB) 字段，配合 GIN 索引，实现 Schema-Free 的任意维度扩展。
- **可迁移性**: 该结构完全兼容标准 SQL 规范。未来如需引入 ClickHouse 等专业 OLAP 引擎，此“维度 + 指标”的宽表模型可实现零阻力无缝迁移。

### 2.3 核心计费字段

在 `models.UsageLog` 中，以下字段支撑了商业化的各种玩法：

- **`OrgID`**: 租户维度。所有账单和配额的最终结算实体。
- **`ServiceType`**: 计费项 (SKU)。如 `ocr`, `liveness_action`。系统可根据 URL 自动推断，也允许业务层覆盖。
- **`UsageUnits`**: 消耗量。默认 1 次。如果是视频活体，业务层可设为视频秒数。
- **`Billable`**: 是否纳入计费（布尔值）。
  - `5xx` 服务端错误 -> 自动免单 (`false`)。
  - `402` 欠费 / `429` 超频 -> 自动免单 (`false`)。
  - 业务层可根据置信度等条件主动设置 `c.Set("billable", false)`。
- **`SessionID`**: 会话关联 ID。用于支撑未来“多次 API 调用，一次性打包扣费”的复杂场景。
- **`Metadata` (JSONB)**: 极其灵活的标签维度（如 `platform`, `region`）。业务线无需修改数据库表结构即可增加新的统计维度。

## 3. 商业化场景落地指南

### 3.1 场景 A：按量付费 (Pay-as-you-go)

- **数据产生**：API 正常调用产生 `usage_logs`。
- **出账逻辑**：建议新增每日凌晨的 CronJob（ETL），查出前一天 `Billable=true` 的日志，关联定价表 (Rate Card) 计算金额，汇总至 `monthly_bills`。当累计金额满 5 万时触发扣款流。

### 3.2 场景 B：资源包 (Resource Packages)

- **额度管理**：复用现有的 `OrganizationQuotas` 表。客户购包后增加 `Allocation`。
- **实时扣减**：复用现有的 `Quota` 中间件，请求进入时实时扣减 `Consumed`。
- **预警机制**：需在 `StatsRefresher`（或新建监控任务）中，定期计算 `(Allocation - Consumed) / Allocation`。当达到 20%、10% 时调用 Notification 服务发邮件。
- **超额降级**：若客户选择“超额转按量”，修改 `Quota` 中间件：当余额为 0 时，不返回 429 拦截，而是放行请求（产生按量日志）。

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
