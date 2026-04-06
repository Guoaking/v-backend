# 考勤打卡微应用当前实现说明 (Time & Attendance Current Spec)

> 本文档优先描述当前代码中的客观事实，并在必要处标记“目标态 / 待补齐能力”。
> 若与历史设计文档不一致，以当前实现为准。

## 1. 模块定位

- 考勤模块是后端内的一个 BFF 子模块，代码位于 `internal/apps/attendance/`。
- 该模块复用主 KYC 服务的 OCR、人脸检测、活体、人脸比对、存储和 Redis 能力。
- 当前定位是一个真实客户试点场景下的轻量打卡系统，不是已经完备的 HR / Attendance 平台。

## 2. 当前路由与鉴权

### 2.1 员工端路由

- 路由前缀：`/api/v1/attendance/*`
- 中间件：
  - `RateLimitMiddleware(10)`
  - `MagicLinkAuth(jwtSecret)`
  - `AsyncMediaIngest(svc.GetKYCService())`
- 员工端请求依赖 Magic Link Token，在 Header 中携带 `Authorization: Bearer <attendance_token>`。

### 2.2 管理端路由

- 路由前缀：`/api/v1/console/attendance/*`
- 当前实现说明：
  - attendance 管理端路由已挂入 Console 认证链路；
  - 当前依赖 `JWTAuth + RequireOrganizationHeader + InjectOrgContext` 获取组织上下文；
  - 当前已切换到 attendance 专项权限：`attendance.read / attendance.write / attendance.review / attendance.report`
  - 当前已提供基础主数据管理接口：`groups`、`sites`、`shift-templates`、`shift-assignments`

### 2.3 Magic Link Token 当前行为

- 当前代码会为组织生成带 `scope=attendance_magic_link` 的 JWT。
- `GetActiveAppToken` / `GenerateAppToken` 当前会将 token 缓存在 Redis 中，缓存时长为 365 天。
- 历史文档中“30 天有效”的描述不是当前实现事实。

## 3. 当前数据模型

当前代码中的 attendance 相关模型位于 `internal/apps/attendance/models/models.go`。

### 3.1 已实现实体

- `OrganizationEmployee`
  - 核心字段：`org_id`、`employee_no`、`employee_sn`、`id_number`、`name`、`phone`
  - 用途：组织员工身份信息和注册底库
- `AttendancePunchEvent`
  - 核心字段：`org_id`、`employee_id`、`punch_time`、`punch_type`、`status`
  - 当前额外记录：`liveness_score`、`face_score`、`fallback_image_url`、`latitude`、`longitude`
- `AttendancePolicy`
  - 核心字段：`punch_mode`、`allow_late_punch`、`require_location`
- `DataCollectionDocument`
  - 用途：保留 OCR 原始结果与用户修正后的最终输入
- `DataCollectionFace`
  - 用途：保留人脸比对 / 打卡相关样本，用于算法数据回流

### 3.2 当前模型现状说明

- 当前实现同时存在 `employee_no`、`employee_sn`、`id_number` 三种员工身份字段。
- 当前收口方向是：`employee_no` 作为组织内稳定业务工号，`id_number` 作为实名证件号锚点，`employee_sn` 进入废弃状态，不再作为后续业务设计依赖字段。
- 当前前端设备信任缓存以 `employee_no` 为主；旧的 `id_number` 本地缓存仅作兼容读取，不再作为新的默认持久化字段。
- `AttendancePunchEvent` 已支持经纬度记录，这一点早期设计文档未完全反映。
- 当前不仅存在 `DataCollectionDocument`，还存在 `DataCollectionFace`；因此算法回流已不止 OCR 场景。

## 4. 当前接口契约

### 4.0 管理端主数据接口

### `GET /api/v1/console/attendance/groups`

- 说明：返回当前组织下的 attendance groups 列表。

### `POST /api/v1/console/attendance/groups`

### `PUT /api/v1/console/attendance/groups/:id`

- 当前字段：
  - `code`
  - `name`
  - `description`
  - `parent_group_id`
  - `manager_employee_id`
  - `status`

### `GET /api/v1/console/attendance/sites`

- 说明：返回当前组织下的 attendance sites 列表。

### `POST /api/v1/console/attendance/sites`

### `PUT /api/v1/console/attendance/sites/:id`

- 当前字段：
  - `site_code`
  - `name`
  - `description`
  - `address_line_1`
  - `address_line_2`
  - `city`
  - `state`
  - `country_code`
  - `postal_code`
  - `latitude`
  - `longitude`
  - `radius_meters`
  - `timezone`
  - `status`

### `GET /api/v1/console/attendance/shift-templates`

- 说明：返回当前组织下的班次模板列表。

### `POST /api/v1/console/attendance/shift-templates`

### `PUT /api/v1/console/attendance/shift-templates/:id`

- 当前字段：
  - `shift_code`
  - `name`
  - `description`
  - `start_time`
  - `end_time`
  - `crosses_day_boundary`
  - `check_in_window_before_minutes`
  - `check_in_window_after_minutes`
  - `check_out_window_before_minutes`
  - `check_out_window_after_minutes`
  - `late_grace_minutes`
  - `early_leave_grace_minutes`
  - `work_minutes`
  - `require_location`
  - `default_site_id`
  - `status`

### `GET /api/v1/console/attendance/shift-assignments`

- 可选查询参数：
  - `date=YYYY-MM-DD`
- 说明：返回当前组织下某日或全量的排班实例列表。

### `POST /api/v1/console/attendance/shift-assignments`

### `PUT /api/v1/console/attendance/shift-assignments/:id`

- 当前字段：
  - `assignment_date`
  - `employee_id` 或 `group_id`（必须二选一）
  - `site_id`
  - `shift_template_id`
  - `assignment_source`
  - `status`
  - `notes`

### 4.1 员工注册

### `POST /api/v1/attendance/enroll/ocr`

- 请求体：`multipart/form-data`
- 表单字段：
  - `image`
  - `id_type`
- 说明：调用底层 OCR，返回提取字段、置信度和原始 JSON。

### `POST /api/v1/attendance/enroll/detect`

- 请求体：`multipart/form-data`
- 表单字段：
  - `picture`
- 说明：调用底层 `FaceDetect`，用于人脸质量检测。

### `GET /api/v1/attendance/enroll/check?id_number=...`

- 说明：检查当前组织下该证件号是否已注册。
- 当前返回：
  - 已注册时返回 `enrolled=true` 与 `employee_no`
  - 未注册时返回 `enrolled=false`

### `POST /api/v1/attendance/enroll/submit`

- 请求体：`multipart/form-data`
- 当前必填字段：
  - `id_number`
  - `name`
  - `phone`
  - `face_image`
- 当前可选字段：
  - `session_id`
  - `id_type`
  - `raw_image_url`
  - `raw_ocr_json`
- 当前行为：
  1. 校验员工是否已注册
  2. 生成人脸底库并落员工表
  3. 记录 OCR 数据回流
- 当前特殊返回：
  - 若已注册，接口直接返回冲突状态，并在响应体中带 `employee_no`

### 4.2 员工打卡

### `GET /api/v1/attendance/punch/config`

- 当前实际路由是 `/attendance/punch/config`，不是历史文档中的 `/attendance/config`。
- 返回当前组织打卡配置。

### `POST /api/v1/attendance/punch/identity`

- 请求体：JSON
- 当前字段：
  - `query`
- 说明：根据输入内容匹配员工身份。

### `POST /api/v1/attendance/punch/liveness/session`

- 说明：创建 Attendance 域内的动作活体会话。
- 当前行为：由 Attendance BFF 在后端内部编排底层 Action Liveness 能力，不再要求员工前端直接访问 `/api/v1/kyc/liveness/action/*`。

### `POST /api/v1/attendance/punch/liveness/upload`

- 请求体：`multipart/form-data`
- 当前字段：
  - `session_id`
  - `video`
- 说明：上传动作活体视频并由 Attendance BFF 内部提交到底层能力。

### `POST /api/v1/attendance/punch/liveness/verify`

- 请求体：JSON
- 当前字段：
  - `session_id`
- 说明：轮询或验证动作活体任务结果，返回底层任务状态与详情。

### `POST /api/v1/attendance/punch`

- 请求体：`multipart/form-data`
- 当前字段：
  - `employee_no`
  - `id_number`
  - `punch_type`
  - `fallback_mode`
  - `latitude`
  - `longitude`
  - `liveness_image`
  - `liveness_task_id`
- 当前实现说明：
  - 当前优先按 `employee_no` 匹配员工，未提供时才回退到 `id_number`。
  - 当使用动作活体时，前端会先走 `/api/v1/attendance/punch/liveness/*` 获取和验证 `liveness_task_id`，再提交到本接口。
  - 当使用静默或降级模式时，前端会上传 `liveness_image`。
  - 当前接口最终统一返回成功消息，不会回传“5 分钟内命中的上一条打卡记录详情”。

### 当前防抖行为

- 当前服务层对 5 分钟内重复的相同打卡做防抖，不新增记录。
- 但接口响应仍是通用成功结果，并未返回“上次记录状态详情”。

### 4.3 员工自助查询

### `POST /api/v1/attendance/self/otp`

- 当前状态：关闭
- 当前返回：403，表示在员工临时会话（employee session auth）实现前不开放自助查询入口

### `GET /api/v1/attendance/self/records`

- 当前状态：关闭
- 当前返回：403，表示在员工临时会话（employee session auth）实现前不开放自助查询入口

### 4.4 管理端接口

### `GET /api/v1/console/attendance/magic-link`

- 当前查询参数：
  - `rotate=true`（可选，强制轮换 token）
- 当前返回：
  - `token`
  - `enroll_url`
  - `punch_url`

### `GET /api/v1/console/attendance/records`

- 当前行为：
  - 返回最近 50 条记录
  - 尚未实现后端分页、复杂筛选和导出

### `PUT /api/v1/console/attendance/records/:id/review`

- 请求体：
  - `action`: `approve | reject`
  - `decision_notes`（可选）
  - `review_reason`（可选）
- 当前行为：
  - 更新 `attendance_punch_reviews` 中对应 review 的状态
  - 同步把关联 `attendance_punch_events` 从 `manual_review` 推进到 `success` 或 `failed`
  - 同步刷新对应员工当日的 `attendance_status_snapshots`

### `GET /api/v1/console/attendance/stats`

- 当前行为：返回组织级基础统计数据

### `GET /api/v1/console/attendance/reviews`

- 可选查询参数：
  - `status`
- 当前行为：
  - 返回 review 列表，并带关联 punch event 与 employee 基础信息

### `GET /api/v1/console/attendance/snapshots`

- 可选查询参数：
  - `date=YYYY-MM-DD`
  - `employee_id`
  - `group_id`
- 当前行为：
  - 返回状态快照列表，支持按日期、员工、组过滤

### `GET /api/v1/console/attendance/timeline/:employee_id`

- 当前行为：
  - 返回某员工的 punch events 时间线
  - 返回最近的 status snapshots

### `GET /api/v1/console/attendance/policy`

- 当前行为：
  - 返回组织级默认 AttendancePolicy

### `PUT /api/v1/console/attendance/policy`

- 当前字段：
  - `punch_mode`
  - `allow_late_punch`
  - `require_location`
- 当前行为：
  - 更新组织级默认 AttendancePolicy

### `GET /api/v1/console/attendance/group-memberships`

- 可选查询参数：
  - `group_id`
  - `employee_id`
- 当前行为：
  - 返回 group membership 列表

### `POST /api/v1/console/attendance/group-memberships`

### `PUT /api/v1/console/attendance/group-memberships/:id`

- 当前字段：
  - `group_id`
  - `employee_id`
  - `membership_role`
  - `is_primary`
  - `effective_from`
  - `effective_to`
  - `status`

### `GET /api/v1/console/attendance/reports/daily`

- 必选查询参数：
  - `date=YYYY-MM-DD`
- 可选查询参数：
  - `group_id`
  - `site_id`
- 当前行为：
  - 基于 `attendance_status_snapshots` 构建并返回日报读模型

### `GET /api/v1/console/attendance/reports/monthly`

- 必选查询参数：
  - `month=YYYY-MM`
- 可选查询参数：
  - `group_id`
  - `site_id`
- 当前行为：
  - 基于日报读模型聚合并返回月报读模型

### `GET /api/v1/console/attendance/reports/export/daily`

- 必选查询参数：
  - `date=YYYY-MM-DD`
- 可选查询参数：
  - `group_id`
  - `site_id`
- 当前行为：
  - 导出日报 CSV

## 5. 当前已知差异与待补齐项

- **员工端动作活体已收口**：动作活体当前已通过 Attendance BFF 路由承接，Attendance Magic Link 不再需要直接访问平台 KYC 动作活体路由。
- **管理端权限边界已显式接入**：attendance 管理端当前已接入 Console JWT、组织上下文与 attendance 专项权限链路。
- **报表与导出能力已接通**：当前已提供日报、月报与日报 CSV 导出接口，作为 P1 报表读模型的第一版后端交付。
- **自助查询闭环未完成**：当前已主动关闭 `self/otp` 与 `self/records`，待员工临时会话方案落地后再恢复。
- **异常复核能力已接通**：review 接口已可更新 review、event 与 snapshot，但专属权限点和更完整的审核台仍待继续演进。
- **排班驱动状态已开始接通**：snapshot 在打卡与复核更新时会尝试解析 employee/group 级 `attendance_shift_assignments`，并回填 `shift_assignment_id`、`group_id`、基础迟到/早退分钟数。
- **管理能力仍是 MVP**：记录查询目前是最近 50 条记录的简单接口，前端自行做筛选和分页。

## 6. 目标态与当前态的使用原则

- 若用于测试、联调、客户沟通，应优先以本文“当前接口契约”为准。
- 若用于后续架构演进，可参考 `ATTENDANCE_APP_DESIGN.md` 中的目标方向，但不能将目标态描述当作已实现能力。

## 7. 演进指导原则

- **独立业务域**：Attendance 后续应按独立业务域演进，拥有自己的主数据、规则、状态机和报表口径。
- **共享基础设施**：Attendance 不自行复制 Organization、Console 用户、角色权限、OAuth/STS、全局审计、用量等公共能力，而是复用平台共享基础设施。
- **数据库策略**：
  - 当前阶段优先采用同一 DB 实例内的业务域分表/分前缀模式。
  - Attendance 自有表用于承载员工、门店/用户组、排班、地点、打卡事件、状态快照和报表读模型。
  - 平台公共表继续承载租户、用户、权限、认证、审计、全局计量等横向能力。
- **未来独立条件**：若 Attendance 在流量、查询压力、发布节奏、合规要求或运维 SLA 上明显独立于平台，再考虑拆分独立数据库或独立服务。
- **拆分时的共享原则**：未来即使物理拆分，也应通过共享身份体系、组织上下文、事件同步或只读投影衔接公共能力，避免复制或双写核心公共主数据。
