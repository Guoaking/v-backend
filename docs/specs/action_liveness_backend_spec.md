# 视频流活体检测（动作引导版，异步MVP）H5 Spec

## Why

- 提升 KYC 反欺诈能力，支持“点头/摇头/眨眼/张嘴”等随机动作的活体检测。
- 统一“单后端”架构并补齐手机 H5 页面能力，实现端到端的视频采集、模型校验与结果返回。

## What Changes

- 新增手机 H5 页面：引导随机动作、摄像头采集、录制上传与结果展示。
- 后端动作引导与会话管理：生成随机动作序列，返回会话与上传参数。
- 视频上传与帧/片段切割：基于会话元数据在服务端切割模型所需关键帧或动作片段。
- 模型校验与结果返回：调用活体模型进行动作一致性与活体判断，返回置信度与结论。
- 安全与治理：鉴权（JWT/API Key）、速率限制、幂等性、配额、审计与指标。
- 可观测性：Prometheus 指标与日志打点，Grafana 展示。
- 兼容性：复用现有 /api/v1/kyc/liveness/action/\* 接口并补充字段与校验逻辑。
- 异步MVP：前端一次性上传视频，后端创建第三方任务（task_id），后台轮询第三方任务状态，前端用 task_id 查询结果。

## Impact

- Affected specs: 活体检测、动作引导、视频采集与上传、模型校验与结果展示、监控与审计。
- Affected code:
  - 前端：v-frontend H5 页面与路由、媒体采集（getUserMedia/MediaRecorder）、接口适配。
  - 后端：会话接口（Session/Upload/Verify）契约、入库与存储、模型调度、打点与审计。
  - 配置与监控：config.yaml/storage.ingest_dir、Prometheus 指标、Grafana 仪表盘。

## ADDED Requirements

### Requirement: 动作引导会话创建

系统 SHALL 提供创建动作引导会话接口，返回：

- 会话ID、随机动作序列（如 [nod, shake, blink, mouth] 及顺序）、每步时长建议
- 上传限制与最大时长、采样率建议、元数据字段（设备/网络/分辨率）

#### Scenario: 成功

- WHEN 客户端请求创建会话（用户已登录或携带API Key）
- THEN 返回200，提供会话ID与动作序列、上传限制参数

### Requirement: 视频采集与上传

系统 SHALL 接收客户端上传的视频文件（WebM/MP4），并附带动作时间线与设备元数据：

- 接口接受 multipart/form-data，字段包含：session_id、video、timeline（起止时间戳/标记）
- 服务端存储原始视频至 ingest_dir，并记录审计日志与指标
- 系统 SHALL 为第三方任务创建并持久化 task_id，并立即返回接收状态与 task_id

#### Scenario: 成功

- WHEN 客户端在动作引导后上传视频与时间线
- THEN 返回202或200，入库与存储成功，创建第三方任务 task_id，进入异步模型校验阶段

### Requirement: 帧/片段切割与模型校验

系统 SHALL 根据时间线在服务端切割关键帧或动作片段，并调用活体模型进行校验：

- 切割策略：按 timeline 标记截取对应片段或抽帧，保证时序与质量
- 模型返回动作一致性与活体置信度，系统合成最终结论
- 异步轮询：系统 SHALL 以固定策略（退避/最大时长）后台轮询第三方任务状态并更新本地任务记录

#### Scenario: 成功

- WHEN 后端完成模型校验
- THEN 返回包含 result=pass/fail、score、action_consistency、reason（可选）

### Requirement: 结果查询与展示

系统 SHALL 提供验证结果查询接口，并在 H5 页面展示：

- 客户端可轮询/回调获取结果，展示结论与可选提示
- 失败时提供重试建议（重新采集/更好光线/稳定网络）
- 系统 SHALL 支持状态查询：created/accepted/dispatching/processing/done/failed 与 progress

### Requirement: 安全与治理

系统 SHALL 实施鉴权、速率限制、幂等、配额、审计与指标：

- 支持 JWT 或 API Key 验证
- 按 Key/IP/组织实施速率限制与配额扣减
- 记录审计日志（会话创建/上传/校验/查询），指标覆盖成功率、时延等
- 幂等：上传接口支持 X-Idempotency-Key；结果查询按 task_id 幂等
- 可观测：Prometheus 指标（会话创建数、上传字节、第三方提交/轮询耗时、成功率、错误码分布）
- 可审计：扩展 api_request_logs 与专用 liveness_tasks 审计表（task_id、状态、最后错误）
- 可迭代：任务处理器与第三方适配器抽象，允许替换模型服务而不影响接口契约

## MODIFIED Requirements

### Requirement: 现有 /api/v1/kyc/liveness/action/\* 接口契约增强

- LivenessActionSession：返回随机动作序列与采样建议、时长限制
- LivenessActionUpload：接受 timeline 与元数据，创建第三方任务并返回 {status, task_id}
- LivenessActionVerify：支持异步结果查询（字段包含 status、result、score、consistency、progress、details）
- 指标与审计打点增强（接口级、动作级）；速率限制、幂等、配额与使用计量中间件维持一致
- 代码风格：沿用 gin 路由与 middleware 组合（APIOrOAuthAuth、Idempotency、RateLimitWithKey、Quota、KYCUsageMeter），logrus 日志、OTEL tracing、viper 配置

## REMOVED Requirements

（无）

## 工程化设计要点

- 代码组织：后端新增 ActionLivenessService（会话/上传/任务/轮询）、ThirdPartyAdapter（submit/poll），api 层仅协调与校验
- 配置：config.yaml.third_party.liveness_action { submit_url, poll_url, timeout, retry }；storage.ingest_dir 存视频
- 存储：新增 liveness_tasks 表（id, session_id, status, progress, result_json, last_error, created_at, updated_at）
- 任务轮询：后台 goroutine（tasks.StartActionLivenessPoller）可配置并发与退避，保证可迭代扩展
- 指标：Prometheus 命名采用 repo 风格 kyc\_\*，如 kyc_liveness_upload_total、kyc_liveness_poll_duration_seconds
- 审计：api_request_logs 记录接口层；liveness_tasks 记录任务层；统一 logger 埋点并带 trace_id
