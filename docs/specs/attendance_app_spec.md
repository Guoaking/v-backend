# 考勤打卡微应用技术方案设计 (Time & Attendance Tech Spec)

## 1. 架构定位 (BFF 层设计)

为了不污染现有的 API Key 鉴权体系和核心服务的纯粹性，考勤应用作为“第一方应用”接入，采用 **BFF (Backend for Frontend)** 架构。

### 1.1 路由前缀
所有前端 H5 发起的请求均通过 `/api/v1/attendance/*` 路由。

### 1.2 鉴权与上下文签名 (The "Magic Link")
员工端不需要登录，但请求必须携带合法的 `OrgID` 以确保数据隔离。
- **机制**：老板在控制台生成“打卡二维码”时，后端使用全局 JWT Secret 签发一个特殊的短效或长效 Token（如 `attendance_token`），该 Token 内部仅包含 `org_id`。
- **前端携带**：员工扫码后，H5 页面从 URL 参数中提取该 Token，并在后续所有 `/api/v1/attendance/*` 请求的 Header 中携带：`Authorization: Bearer <attendance_token>`。
- **BFF 拦截器**：拦截该路由，解析 Token，向 Context 中注入 `OrgID`，但不注入 `UserID`。

---

## 2. 数据库设计 (Database Schema)

在 `v-backend/internal/models` 中新增以下三个实体。所有表必须以 `org_id` 为核心隔离字段。

### 2.1 员工表 (`OrganizationEmployee`)
记录员工身份和用于比对的底库。
```go
type OrganizationEmployee struct {
    ID             string    `gorm:"primaryKey" json:"id"`
    OrgID          string    `gorm:"index;not null" json:"org_id"`
    EmployeeSN     string    `gorm:"index" json:"employee_sn"` // 客户内部工号(可选)
    IDNumber       string    `gorm:"index;not null" json:"id_number"` // 核心锚点：证件号
    Name           string    `gorm:"not null" json:"name"`
    Phone          string    `json:"phone"`
    FaceFeature    []byte    `gorm:"type:bytea" json:"-"` // 提取出的人脸特征向量，不对外暴露
    FaceImageURL   string    `json:"face_image_url"`      // 原始人脸图(用于HR查看)
    Status         string    `gorm:"default:'active'" json:"status"` // active, inactive, deleted
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
// 联合唯一索引：同一个租户下，证件号必须唯一
// gorm:"uniqueIndex:idx_org_id_number"
```

### 2.2 考勤记录表 (`AttendanceRecord`)
记录每一次打卡行为。
```go
type AttendanceRecord struct {
    ID               string    `gorm:"primaryKey" json:"id"`
    OrgID            string    `gorm:"index;not null" json:"org_id"`
    EmployeeID       string    `gorm:"index;not null" json:"employee_id"`
    PunchTime        time.Time `gorm:"index;not null" json:"punch_time"`
    PunchType        string    `json:"punch_type"` // "in", "out"
    LivenessScore    float64   `json:"liveness_score"`
    FaceScore        float64   `json:"face_score"`
    Status           string    `json:"status"` // "success", "manual_review" (需人工复核)
    FallbackImageURL string    `json:"fallback_image_url,omitempty"` // 失败降级时的现场照片
    CreatedAt        time.Time `json:"created_at"`
}
```

### 2.3 数据反哺表 (`DataCollectionDocument`)
专为算法团队优化模型设计的“Ground Truth”数据池。
```go
type DataCollectionDocument struct {
    ID             string         `gorm:"primaryKey" json:"id"`
    OrgID          string         `gorm:"index;not null" json:"org_id"`
    IDType         string         `json:"id_type"` // passport, thai_id 等
    RawImageURL    string         `json:"raw_image_url"`
    RawOCRResult   datatypes.JSON `gorm:"type:jsonb" json:"raw_ocr_result"` // 模型原始输出
    FinalUserInput datatypes.JSON `gorm:"type:jsonb" json:"final_user_input"` // 员工手动修正后的最终结果
    IsCorrected    bool           `gorm:"index" json:"is_corrected"` // 如果 Final != Raw，则为 true
    CreatedAt      time.Time      `json:"created_at"`
}
```

---

## 3. API 接口契约 (API Specification)

### 3.1 员工注册与信息采集

#### 3.1.1 `POST /api/v1/attendance/enroll/ocr`
- **功能**：上传证件，调用底层 OCR 提取信息。
- **请求体**：`multipart/form-data` (包含图片文件 `image` 和 `id_type`)
- **响应体**：
  ```json
  {
      "code": 0,
      "data": {
          "session_id": "req_12345", // 用于后续追踪
          "fields": {
              "id_number": "A12345678",
              "name": "John Doe",
              "dob": "1990-01-01"
          },
          "confidence": 0.98
      }
  }
  ```

#### 3.1.2 `POST /api/v1/attendance/enroll/submit`
- **功能**：提交员工核对后的最终信息及人脸照片，完成注册。
- **逻辑**：
  1. 校验 `OrgID` + `id_number` 是否已存在。若存在，返回状态码提示前端直接跳转打卡。
  2. 调用内部 `service.FaceExtract()` 提取人脸特征并进行质量检测。
  3. 质量不合格则阻断，返回要求重拍。
  4. 质量合格则落库 `OrganizationEmployee`。
  5. 异步落库 `DataCollectionDocument`（对比 OCR 结果与当前提交结果，计算 `is_corrected`）。
- **请求体**：
  ```json
  {
      "session_id": "req_12345", // 关联之前的 OCR 请求
      "id_number": "A12345678",
      "name": "John Doe",
      "phone": "13800138000",
      "face_image": "<base64_or_url>"
  }
  ```

### 3.2 员工打卡 (Punch)

#### 3.2.1 `GET /api/v1/attendance/config`
- **功能**：前端渲染打卡页前，拉取当前租户的打卡策略配置。
- **响应体**：
  ```json
  {
      "code": 0,
      "data": {
          "punch_mode": "liveness_active", // 枚举：photo_only, liveness_silent, liveness_active
          "allow_late_punch": true,
          "require_location": false
      }
  }
  ```

#### 3.2.2 `POST /api/v1/attendance/punch`
- **功能**：执行打卡操作（依据 `punch_mode` 决定是否调用底层模型）。
- **去重防抖逻辑**：如果在 5 分钟内同一 `id_number` 重复提交相同的 `punch_type`，直接返回 HTTP 200 和上一次的记录状态，不扣减 Quota，不在 `attendance_records` 新增记录。
- **降级逻辑**：前端在连续 2 次识别失败后，将 `fallback_mode` 设为 `true`，此时仅上传现场照片，跳过严格活体和比对，直接标记为 `manual_review`。
- **请求体**：
  ```json
  {
      "id_number": "A12345678",      // 核心锚点 (1:1 比对前提)
      "punch_type": "in",            // in/out
      "liveness_data": "<payload>",  // 活体数据 (动作视频或静默照片)
      "fallback_mode": false,        // 是否为降级模式
      "fallback_image": "<base64>"   // 仅在 fallback_mode=true 时有效
  }
  ```
- **响应体**：
  ```json
  {
      "code": 0,
      "data": {
          "status": "success", // 或 "manual_review"
          "punch_time": "2023-10-27T08:55:00Z",
          "message": "打卡成功"
      }
  }
  ```

### 3.3 员工自助查询 (Self-Service)

#### 3.3.1 `POST /api/v1/attendance/self/otp`
- **功能**：请求查看个人信息的验证码（通过手机号或企业内通知）。

#### 3.3.2 `GET /api/v1/attendance/self/records`
- **功能**：员工凭验证码换取的临时 Session 查询自己当周/当月的打卡记录。

---

## 4. 管理端接口 (Admin API)

这些接口供企业老板/HR在 Console 控制台使用（复用现有的 Console JWT 鉴权）。

#### 4.1 `GET /api/v1/console/attendance/records`
- **功能**：分页查询考勤记录，支持按员工、日期、状态（如 `manual_review`）过滤。

#### 4.2 `PUT /api/v1/console/attendance/records/:id/review`
- **功能**：处理降级的异常打卡。
- **请求体**：`{ "action": "approve" | "reject" }`

#### 4.3 `GET /api/v1/console/attendance/stats`
- **功能**：考勤大盘聚合数据（今日迟到人数、当月工时统计等）。

---

## 5. 关键技术点对齐 (Alignment Required)

在正式编码前，请确认以下技术决策：

### 4.1 计费耦合点对齐
- **方案**：在 `attendance/punch` 接口内部，我们会硬编码一个属于我们自己平台（Service Provider）的 `SystemAppKey` 去调用 `service.Liveness()` 和 `service.FaceCompare()`，并显式将这笔账记在请求上下文中的 `OrgID` 头上。
- **确认点**：客户的 `Org` 必须有一个免费的 `Plan`，否则内部调用会因为 Quota 不足而失败。

### 4.2 数据存储合规对齐
- **方案**：`face_image_url` 和 `fallback_image_url` 将存储在系统的标准 OSS/S3 中。
- **确认点**：考虑到这部分是高度敏感的 C 端生物数据，是否需要为其单独开辟一个 Bucket 并设置严格的生命周期（如 30 天后自动转入冷存储或删除）？目前暂定复用现有 Storage 策略，由租户自己负责删除。

### 4.3 并发与限流对齐
- **方案**：考勤打卡具有极强的时效性和集中性（如早上 8:55-9:00）。
- **确认点**：BFF 层的打卡接口需要挂载限流中间件（Rate Limiter），如单 IP 10qps，单 Org 50qps。若触发限流，直接指导前端进入 `fallback_mode`（拍摄水印照片），保护底层 GPU 资源不被打挂。