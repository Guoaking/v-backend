package api

import (
	"kyc-service/internal/service"
)

// ConsoleHandler 控制台处理器
type ConsoleHandler struct {
	service *service.KYCService
}

// NewConsoleHandler 创建控制台处理器
func NewConsoleHandler(svc *service.KYCService) *ConsoleHandler {
	return &ConsoleHandler{service: svc}
}

// ConsoleCreateAPIKeyRequest 创建API密钥请求

// ConsoleAPIKeyResponse API密钥响应

type UpdateKeyScopesRequest struct {
	Scopes []string `json:"scopes" binding:"required,min=1"`
}

// UserMeResponse 用户信息响应
type UserMeResponse struct {
	ID              string                  `json:"id"`
	Email           string                  `json:"email"`
	FullName        string                  `json:"full_name"`
	Name            string                  `json:"name"`
	AvatarURL       string                  `json:"avatar,omitempty"`
	Role            string                  `json:"role"`
	OrgRole         string                  `json:"org_role"`
	CurrentOrgID    string                  `json:"currentOrgId"`
	Permissions     []string                `json:"permissions"`
	Company         string                  `json:"company"`
	APIKeys         []ConsoleAPIKeyResponse `json:"apiKeys"`
	IsPlatformAdmin bool                    `json:"is_platform_admin"`
}

// ConsoleUpdateUserRequest 更新用户请求

// GetCurrentUser 获取当前用户信息
// @Summary 获取当前用户信息
// @Description 获取当前登录用户的详细信息
// @Tags Console
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/users/me [get]

// [DEPRECATED] API Key related methods have been moved to console_apikey_handler.go and marked for removal.

// UpdateUserProfile 更新用户资料
// @Summary 更新用户资料
// @Description 更新当前用户的个人资料
// @Tags Console
// @Accept json
// @Produce json
// @Param request body ConsoleUpdateUserRequest true "更新用户资料请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/users/me [put]

// CreateAPIKey 创建API密钥
// @Summary 创建API密钥
// @Description 创建新的API密钥
// @Tags Console
// @Accept json
// @Produce json
// @Param request body ConsoleCreateAPIKeyRequest true "创建API密钥请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/keys [post]

// GetAPIKeySecret 返回明文密钥（加密存储，按权限解密）

// ListAPIKeys 控制台获取API密钥列表（含统计） [DEPRECATED]

// RevokeAPIKey 撤销API密钥 [DEPRECATED]
// @Summary 撤销API密钥
// @Description 撤销指定的API密钥
// @Tags Console
// @Accept json
// @Produce json
// @Param id path string true "API密钥ID"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/keys/{id} [delete]

// generateAPIKeySecret 生成API密钥

// recordAuditLog 记录审计日志

// UsageItem 用量聚合项
type UsageItem struct {
	Date    string `json:"date"`
	Success int64  `json:"success"`
	Failed  int64  `json:"failed"`
}

// LogItem 日志项（不含敏感体）
type LogItem struct {
	ID           string `json:"id"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	StatusCode   int    `json:"statusCode"`
	LatencyMs    int    `json:"latency"`
	ClientIP     string `json:"clientIp"`
	CreatedAt    string `json:"created_at"`
	TimeStamp    string `json:"timestamp"`
	RequestBody  string `json:"requestBody"`
	ResponseBody string `json:"responseBody"`
	KeyID        string `json:"key_id,omitempty"`
	KeyName      string `json:"key_name,omitempty"`
	KeyOwnerID   string `json:"keyOwner,omitempty"`
}

// GetUsage 聚合用量

// GetLogs 分页查询日志

func summarizeJSON(s string) string {
	if len(s) == 0 {
		return ""
	}
	if len(s) > 256 {
		return s[:256]
	}
	return s
}

// DeleteMe 删除当前账户（软删除）

// GetNotifications 获取站内通知

// MarkNotificationRead 标记通知为已读

type QuotaStatusItem struct {
	Limit     int     `json:"limit"`
	Used      int     `json:"used"`
	Remaining int     `json:"remaining"`
	ResetAt   *string `json:"reset_at"`
}
