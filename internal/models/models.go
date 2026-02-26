package models

import (
	"time"

	pq "github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// KYCRequest KYC请求
type KYCRequest struct {
	ID           string         `gorm:"primaryKey" json:"id"`
	UserID       string         `gorm:"index" json:"user_id"`
	RequestType  string         `json:"request_type"`   // ocr, face, liveness, complete
	Status       string         `json:"status"`         // pending, processing, success, failed
	IDCardHash   string         `gorm:"index" json:"-"` // 身份证号哈希，用于索引
	IDCard       string         `json:"-"`              // 加密的身份证号
	Name         string         `json:"-"`              // 加密的姓名
	Phone        string         `json:"-"`              // 加密的手机号
	FaceImage    string         `json:"-"`              // 人脸图片URL
	IDCardImage  string         `json:"-"`              // 身份证图片URL
	LivenessData string         `json:"-"`              // 活体检测数据
	Result       string         `json:"result,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	IPAddress    string         `json:"ip_address"`
	UserAgent    string         `json:"user_agent"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// AuditLog 审计日志
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RequestID string    `gorm:"index" json:"request_id"`
	UserID    string    `gorm:"index" json:"user_id"`
	OrgID     string    `gorm:"index" json:"org_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Details   string    `json:"details"` // JSON格式的详细信息
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// User 用户
type User struct {
	ID              string         `gorm:"primaryKey" json:"id"`
	Email           string         `gorm:"uniqueIndex" json:"email"`
	Password        string         `json:"-"`
	Name            string         `json:"name"`
	FullName        string         `json:"full_name"`
	AvatarURL       string         `json:"avatar_url"`
	Company         string         `json:"company"`
	Role            string         `json:"role"` // user, admin
	OrgID           string         `json:"org_id"`
	OrgRole         string         `json:"org_role"` // owner, admin, developer, viewer
	CurrentOrgID    string         `json:"currentOrgId"`
	LastActiveOrgID string         `json:"last_active_org_id"`
	Status          string         `json:"status"` // active, suspended
	LastLoginAt     *time.Time     `json:"last_login_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Organization    Organization   `gorm:"foreignKey:OrgID" json:"organization,omitempty"`
	IsPlatformAdmin bool           `gorm:"default:false" json:"is_platform_admin"`
}

// OAuthClient OAuth客户端
type OAuthClient struct {
	ID              string         `gorm:"primaryKey" json:"id"`
	Secret          string         `json:"-"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	RedirectURI     string         `json:"redirect_uri"`
	Scopes          string         `json:"scopes"`
	Status          string         `json:"status"`
	OrgID           string         `json:"org_id"`
	OwnerID         string         `json:"owner_id"`
	TokenTTLSeconds int            `json:"token_ttl_seconds"`
	IPWhitelist     pq.StringArray `gorm:"type:text[]" json:"ip_whitelist"`
	RateLimitPerSec int            `json:"rate_limit_per_sec"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// Organization 组织/租户表
type Organization struct {
	ID               string         `gorm:"primaryKey" json:"id"`
	Name             string         `json:"name"`
	PlanID           string         `json:"plan_id"` // starter, growth, scale
	BillingEmail     string         `json:"billing_email"`
	StripeCustomerID string         `json:"stripe_customer_id,omitempty"`
	Status           string         `json:"status"`
	OwnerID          string         `json:"owner_id"`
	UsageSummary     datatypes.JSON `gorm:"type:jsonb" json:"usage_summary,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// OrganizationMember 组织成员关联表
type OrganizationMember struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OrganizationID string    `gorm:"index" json:"org_id"`
	UserID         string    `gorm:"index" json:"user_id"`
	Role           string    `json:"role"`   // owner, admin, developer, viewer
	Status         string    `json:"status"` // active, pending
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// APIKey API密钥表
type APIKey struct {
	ID              string         `gorm:"primaryKey" json:"id"`
	UserID          string         `gorm:"index" json:"user_id"`
	OrgID           string         `gorm:"index" json:"org_id"`
	Name            string         `json:"name"`
	SecretHash      string         `json:"-"`
	SecretEnc       string         `json:"-"`
	Prefix          string         `json:"prefix"`
	Scopes          string         `json:"scopes"` // JSON array: ["ocr:read", "face:write"]
	Status          string         `json:"status"` // active, revoked, rate_limited, quota_exceeded
	IPWhitelist     pq.StringArray `gorm:"type:text[]" json:"ip_whitelist"`
	LastUsedAt      *time.Time     `json:"last_used_at,omitempty"`
	LastUsedIP      string         `json:"last_used_ip,omitempty"`
	CreatedByUserID string         `json:"created_by_user_id"`
	// Stats (24h rolling window)
	TotalRequests24h int        `json:"total_requests_24h"`
	SuccessRate24h   float64    `json:"success_rate_24h"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// UsageLog 用量日志表
type UsageLog struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	OrgID         string    `gorm:"index" json:"org_id"`
	APIKeyID      string    `gorm:"index" json:"api_key_id"`
	UserID        string    `gorm:"index" json:"user_id"`
	OAuthClientID *string   `gorm:"index" json:"oauth_client_id,omitempty"`
	Endpoint      string    `json:"endpoint"`
	StatusCode    int       `json:"status_code"`
	RequestID     string    `json:"request_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// OAuthToken OAuth令牌
type OAuthToken struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	AccessToken  string    `gorm:"uniqueIndex" json:"-"`
	RefreshToken string    `gorm:"uniqueIndex" json:"-"`
	UserID       string    `json:"user_id"`
	ClientID     string    `json:"client_id"`
	Scopes       string    `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// PasswordReset 密码重置表
type PasswordReset struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	UserID    string    `gorm:"index" json:"user_id"`
	Token     string    `gorm:"uniqueIndex" json:"-"`
	Status    string    `json:"status"` // pending, used, expired
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// OrganizationInvitation 组织邀请表
type OrganizationInvitation struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	OrgID     string    `gorm:"index" json:"org_id"`
	Email     string    `gorm:"index" json:"email"`
	Role      string    `json:"role"` // admin, developer, viewer
	InvitedBy string    `json:"invited_by"`
	Token     string    `gorm:"uniqueIndex" json:"-"`
	Status    string    `json:"status"` // pending, accepted, rejected, expired
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Permission 原子权限定义
type Permission struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	Category    string    `json:"category"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Role 角色定义
type Role struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `gorm:"default:false" json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RolePermission 角色-权限关联
type RolePermission struct {
	RoleID       string `gorm:"primaryKey" json:"role_id"`
	PermissionID string `gorm:"primaryKey" json:"permission_id"`
}

// APIRequestLog API请求日志表
type APIRequestLog struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID         string         `gorm:"index" json:"org_id"`
	UserID        *string        `gorm:"index" json:"user_id,omitempty"`
	APIKeyID      *string        `gorm:"index" json:"api_key_id,omitempty"`
	APIKeyName    string         `json:"api_key_name,omitempty"`
	APIKeyOwnerID *string        `gorm:"index" json:"api_key_owner_id,omitempty"`
	Method        string         `gorm:"not null" json:"method"`
	Path          string         `gorm:"not null" json:"path"`
	StatusCode    int            `gorm:"not null" json:"status_code"`
	LatencyMs     int            `gorm:"not null" json:"latency_ms"`
	ClientIP      string         `gorm:"index" json:"client_ip"`
	RequestBody   datatypes.JSON `gorm:"type:jsonb" json:"request_body,omitempty"`
	ResponseBody  datatypes.JSON `gorm:"type:jsonb" json:"response_body,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Invitation 邀请记录
type Invitation struct {
	ID         string     `gorm:"primaryKey" json:"id"`
	OrgID      string     `gorm:"index" json:"org_id"`
	InviterID  string     `gorm:"index" json:"inviter_id"`
	Email      string     `gorm:"index" json:"email"`
	Role       string     `json:"role"` // admin, viewer
	Token      string     `gorm:"uniqueIndex" json:"token"`
	Status     string     `json:"status"` // pending, accepted, declined, expired
	ExpiresAt  time.Time  `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

// Notification 站内信
type Notification struct {
	ID        string         `gorm:"primaryKey" json:"id"`
	UserID    string         `gorm:"index" json:"user_id"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	Payload   datatypes.JSON `gorm:"type:jsonb" json:"payload"`
	IsRead    bool           `json:"is_read"`
	CreatedAt time.Time      `json:"created_at"`
}

type AuditAction struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"created_at"`
}

type Plan struct {
	ID          string         `gorm:"primaryKey" json:"id"`
	Name        string         `json:"name"`
	QuotaConfig datatypes.JSON `gorm:"type:jsonb" json:"quota_config"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type GlobalConfig struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OrganizationQuotas struct {
	ID             string     `gorm:"primaryKey" json:"id"`
	OrganizationID string     `gorm:"index" json:"organization_id"`
	ServiceType    string     `gorm:"index" json:"service_type"`
	Allocation     int        `json:"allocation"`
	Consumed       int        `json:"consumed"`
	ResetAt        *time.Time `json:"reset_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ImageAsset struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OrganizationID string    `gorm:"index" json:"organization_id"`
	Hash           string    `gorm:"uniqueIndex" json:"hash"`
	FilePath       string    `json:"file_path"`
	SafeFilename   string    `json:"safe_filename"`
	ContentType    string    `json:"content_type"`
	SizeBytes      int64     `json:"size_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

type VideoAsset struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OrganizationID string    `gorm:"index" json:"organization_id"`
	Hash           string    `gorm:"uniqueIndex" json:"hash"`
	FilePath       string    `json:"file_path"`
	SafeFilename   string    `json:"safe_filename"`
	ContentType    string    `json:"content_type"`
	SizeBytes      int64     `json:"size_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

type FaceImageRef struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OrganizationID string    `gorm:"index" json:"organization_id"`
	FilePath       string    `json:"file_path"`
	SafeFilename   string    `json:"safe_filename"`
	CreatedAt      time.Time `json:"created_at"`
}
