package api

import (
	"encoding/json"
	"fmt"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

// AdminHandler 管理员处理器
type AdminHandler struct {
	service *service.KYCService
}

// NewAdminHandler 创建管理员处理器
func NewAdminHandler(svc *service.KYCService) *AdminHandler {
	return &AdminHandler{service: svc}
}

// PaginationRequest 通用分页请求参数
type PaginationRequest struct {
	Page   int    `form:"page,default=1"`   // 页码，从1开始
	Limit  int    `form:"limit,default=10"` // 每页数量，默认10
	Offset int    `form:"offset"`           // 可选的偏移量，如果提供则优先使用
	Search string `form:"search"`           // 搜索关键词
	Status string `form:"status"`           // 状态筛选
}

// AdminUserListResponse 管理员用户列表响应

// AdminOrganizationResponse 管理员组织列表响应
type AdminOrganizationResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	BillingEmail string    `json:"billing_email"`
	Plan         PlanInfo  `json:"plan"`
	MemberCount  int       `json:"member_count"`
	TotalUsage   int       `json:"total_usage"`
	CreatedAt    time.Time `json:"created_at"`
	Status       string    `json:"status"`
	UsageSummary struct {
		TotalRequests int     `json:"totalRequests"`
		Limit         int     `json:"limit"`
		PercentUsed   float64 `json:"percentUsed"`
		Period        string  `json:"period"`
	} `json:"usageSummary"`
}

// PlanInfo 套餐信息
type PlanInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AdminPlan struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Price         int             `json:"price"`
	Currency      string          `json:"currency"`
	RequestsLimit int             `json:"requestsLimit"`
	Features      json.RawMessage `json:"features"`
	QuotaConfig   json.RawMessage `json:"quotaConfig"`
	IsActive      bool            `json:"isActive"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type PlanUpdateRequest struct {
	Price         *int             `json:"price"`
	Currency      *string          `json:"currency"`
	RequestsLimit *int             `json:"requestsLimit"`
	Features      *json.RawMessage `json:"features"`
	QuotaConfig   *json.RawMessage `json:"quotaConfig"`
	IsActive      *bool            `json:"isActive"`
}

// AdminUserListRequest 用户列表请求
type AdminUserListRequest struct {
	PaginationRequest
	Role   string `form:"role"` // 用户角色筛选
	SortBy string `form:"sort_by"`
	Order  string `form:"order"`
	Q      string `form:"q"`
}

// @Summary 管理员获取用户列表
// @Description 管理员获取所有用户列表，包含组织信息和用量统计
// @Tags Admin
// @Accept json
// @Produce json
// @Param page query int false "页码，从1开始" default(1)
// @Param limit query int false "每页数量，默认10" default(10)
// @Param offset query int false "偏移量，可选，如果提供则优先使用"
// @Param search query string false "搜索关键词"
// @Param status query string false "用户状态"
// @Param role query string false "用户角色"
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/admin/users [get]

// @Summary 管理员获取组织列表
// @Description 超级管理员获取所有组织列表，包含成员数量和用量统计
// @Tags Admin
// @Accept json
// @Produce json
// @Param page query int false "页码，从1开始" default(1)
// @Param limit query int false "每页数量，默认10" default(10)
// @Param offset query int false "偏移量，可选，如果提供则优先使用"
// @Param search query string false "搜索关键词"
// @Param status query string false "组织状态"
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/admin/organizations [get]

// GetPlans 管理端获取计划列表

// UpdatePlan 管理端更新计划基础属性

// GetOrganizationQuotas 获取组织配额

type QuotaAdjustRequest struct {
	ServiceType string `json:"service_type" binding:"required,oneof=ocr face liveness"`
	Adjustment  int    `json:"adjustment" binding:"required"`
	Reason      string `json:"reason"`
}

// AdjustOrganizationQuota 调整组织配额（原子更新+审计）

type UpdateOrgPlanRequest struct {
	PlanID     string `json:"plan_id" binding:"required"`
	Immediate  *bool  `json:"immediate"`
	ResetUsage *bool  `json:"reset_usage"`
}

// @Summary 管理员更新用户状态
// @Description 管理员更新用户状态（激活/禁用）
// @Tags Admin
// @Accept json
// @Produce json
// @Param id path string true "用户ID"
// @Param request body UpdateUserStatusRequest true "状态更新请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/admin/users/{id}/status [put]

// UpdateUserStatusRequest 更新用户状态请求
type UpdateUserStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active suspended"`
}

type AdminUpdateUserRequest struct {
	Role     *string `json:"role"`     // admin | user
	Status   *string `json:"status"`   // active | suspended
	Password *string `json:"password"` // new password
}

// UpdateUserAdmin 平台管理员更新用户信息（角色、状态、密码）

// AdminAuditLogResponse 管理员审计日志响应

type AdminOverviewStats struct {
	TotalUsers    int64 `json:"total_users"`
	ActiveClients int64 `json:"active_clients"`
	TodayRequests int64 `json:"today_requests"`
}

// AdminAuditLogRequest 审计日志请求
type AdminAuditLogRequest struct {
	PaginationRequest
	UserID   string `form:"user_id"` // 用户ID筛选
	Action   string `form:"action"`  // 操作类型筛选
	DateFrom string `form:"date_from"`
	DateTo   string `form:"date_to"`
	FromDate string `form:"from_date"`
	ToDate   string `form:"to_date"`
}

// @Summary 管理员获取审计日志
// @Description 超级管理员获取全平台审计日志，支持多种筛选条件
// @Tags Admin
// @Accept json
// @Produce json
// @Param page query int false "页码，从1开始" default(1)
// @Param limit query int false "每页数量，默认50" default(50)
// @Param offset query int false "偏移量，可选，如果提供则优先使用"
// @Param user_id query string false "用户ID筛选"
// @Param action query string false "操作类型筛选"
// @Param status query string false "状态筛选"
// @Param date_from query string false "开始日期 (YYYY-MM-DD)"
// @Param date_to query string false "结束日期 (YYYY-MM-DD)"  // ignore_security_alert
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/admin/audit-logs [get]

// recordAuditLog 记录审计日志

type UpdatePlanQuotaRequest struct {
	OCRLimit   int    `json:"ocr_limit"`
	OCRPeriod  string `json:"ocr_period"`
	FaceLimit  int    `json:"face_limit"`
	FacePeriod string `json:"face_period"`
}

type UpdateGlobalConfigRequest struct {
	DailyRegistrationCap int `json:"daily_registration_cap"`
}

// CreatePermissionRequest 创建权限请求
type CreatePermissionRequest struct {
	ID          string `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Category    string `json:"category" binding:"required"`
	Description string `json:"description"`
}

// CreatePermission 管理员创建新权限定义
func (h *AdminHandler) CreatePermission(c *gin.Context) {
	var req CreatePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	perm := models.Permission{
		ID:          req.ID,
		Name:        req.Name,
		Category:    req.Category,
		Description: req.Description,
		CreatedAt:   time.Now(),
	}

	if err := h.service.DB.Create(&perm).Error; err != nil {
		logger.GetLogger().WithError(err).Error("创建权限失败")
		JSONError(c, CodeDatabaseError, "创建失败，可能ID已存在")
		return
	}

	// 审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin.create_permission", "success", fmt.Sprintf("Created permission: %s", req.ID))
	JSONSuccess(c, perm)
}

// DeletePermission 管理员删除权限定义
func (h *AdminHandler) DeletePermission(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		JSONError(c, CodeInvalidParameter, "缺少权限ID")
		return
	}

	// 开启事务
	tx := h.service.DB.Begin()

	// 1. 删除 role_permissions 中的关联
	if err := tx.Exec("DELETE FROM role_permissions WHERE permission_id = ?", id).Error; err != nil {
		tx.Rollback()
		JSONError(c, CodeDatabaseError, "清理角色关联失败")
		return
	}

	// 2. 删除 permissions 表中的记录
	if err := tx.Delete(&models.Permission{}, "id = ?", id).Error; err != nil {
		tx.Rollback()
		JSONError(c, CodeDatabaseError, "删除权限失败")
		return
	}

	tx.Commit()

	// 审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin.delete_permission", "success", fmt.Sprintf("Deleted permission: %s", id))
	JSONSuccess(c, gin.H{"deleted": id})
}
