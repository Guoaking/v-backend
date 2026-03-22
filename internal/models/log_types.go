package models

import "time"

// StandardContextKey 上下文键名常量
const (
	ContextKeyRequestID = "request_id"
	ContextKeyTraceID   = "trace_id"
	ContextKeySpanID    = "span_id"
	ContextKeyTenantID  = "tenant_id" // 对应 orgID
	ContextKeyUserID    = "user_id"
	ContextKeyClientIP  = "client_ip"
)

// LogType 日志类型枚举
type LogType string

const (
	LogTypeSRE      LogType = "SRE"
	LogTypeBilling  LogType = "BILLING"
	LogTypeAudit    LogType = "AUDIT"
	LogTypeSecurity LogType = "SECURITY"
)

// LogEnvelope 标准日志信封结构
type LogEnvelope struct {
	Timestamp   time.Time    `json:"timestamp"`
	LogType     LogType      `json:"log_type"`
	Level       string       `json:"level"`
	Trace       TraceInfo    `json:"trace"`
	Identity    IdentityInfo `json:"identity"`
	EventAction string       `json:"event_action,omitempty"`
	Payload     interface{}  `json:"payload"`
	Message     string       `json:"message,omitempty"` // 用于简短描述
}

type TraceInfo struct {
	TraceID   string `json:"trace_id"`
	RequestID string `json:"request_id"`
	SpanID    string `json:"span_id,omitempty"`
}

type IdentityInfo struct {
	TenantID string `json:"tenant_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	ClientIP string `json:"client_ip"`
}

// BillingPayload 强类型的计费负载
type BillingPayload struct {
	ID            string                 `json:"id"`
	ActorUserID   string                 `json:"actor_user_id,omitempty"`
	OAuthClientID string                 `json:"oauth_client_id,omitempty"`
	Endpoint      string                 `json:"endpoint"`
	StatusCode    int                    `json:"status_code"`
	ServiceType   string                 `json:"service_type"`         // 核心计费项 (SKU)，例如: ocr, face_compare
	UsageUnits    int                    `json:"usage_units"`          // 计费单位，默认为 1 (次)，视频可能是时长(秒)
	Billable      bool                   `json:"billable"`             // 本次请求是否纳入计费 (例如某些内部调用或完全缓存命中可为 false)
	SessionID     string                 `json:"session_id,omitempty"` // 会话ID，用于关联多个子服务的计费事件 (e.g. 复合能力打包计费)
	Metadata      map[string]interface{} `json:"metadata,omitempty"`   // 扩展元数据 (如 1级/2级分类, 地区, 平台等)
}
