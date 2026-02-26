package api

import (
    "encoding/json"
    "fmt"
    "time"

    "kyc-service/internal/middleware"
    "kyc-service/internal/models"
    "kyc-service/internal/service"
    "kyc-service/pkg/logger"
    "kyc-service/pkg/metrics"

    "github.com/gin-gonic/gin"
    "golang.org/x/crypto/bcrypt"
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
type AdminUserListResponse struct {
	ID         string    `json:"id"`
	FullName   string    `json:"full_name"`
	Email      string    `json:"email"`
	Role       string    `json:"role"`
	Status     string    `json:"status"`
	AvatarURL  string    `json:"avatar"`
	OrgName    string    `json:"org_name"`
	PlanID     string    `json:"plan_id"`
	TotalUsage int       `json:"total_usage"`
	CreatedAt  time.Time `json:"created_at"`
}

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
func (h *AdminHandler) GetUserList(c *gin.Context) {
	start := time.Now()

	// 权限检查 - 确保是管理员
	//userRole, exists := c.Get("userRole")
	//if !exists || userRole != "admin" {
	//	middleware.RecordBusinessOperation("admin_user_list", false, time.Since(start), "permission_denied")
	//	JSONError(c, CodeForbidden, "权限不足")
	//	return
	//}

	var req AdminUserListRequest
    if err := c.ShouldBindQuery(&req); err != nil {
        metrics.RecordBusinessOperation(c.Request.Context(), "admin_user_list", false, time.Since(start), "invalid_request")
        JSONError(c, CodeInvalidParameter, "参数验证失败")
        return
    }

	// 设置默认值并计算分页参数
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 10 // 默认每页10条
	}

	// 如果提供了offset，优先使用offset；否则根据page和limit计算
	var offset int
	if req.Offset > 0 {
		offset = req.Offset
	} else {
		offset = (req.Page - 1) * req.Limit
	}

	// 构建查询条件
	query := h.service.DB.Table("users u").
		Select(`u.id, u.full_name, u.email, u.role, u.status, u.avatar_url, 
            o.name as org_name, o.plan_id, u.created_at,
            COALESCE(SUM(um.request_count), 0) as total_usage`).
		Joins("LEFT JOIN organizations o ON o.id::text = u.org_id::text").
		Joins("LEFT JOIN usage_metrics um ON o.id::text = um.org_id::text").
		Group("u.id, u.full_name, u.email, u.role, u.status, u.avatar_url, o.name, o.plan_id, u.created_at")

	// 应用过滤条件
	if req.Search != "" {
		searchPattern := fmt.Sprintf("%%%s%%", req.Search)
		query = query.Where("u.email LIKE ? OR u.full_name LIKE ? OR o.name LIKE ?",
			searchPattern, searchPattern, searchPattern)
	}
	if req.Q != "" {
		qp := fmt.Sprintf("%%%s%%", req.Q)
		query = query.Where("u.email LIKE ? OR u.full_name LIKE ?", qp, qp)
	}

	if req.Status != "" {
		query = query.Where("u.status = ?", req.Status)
	}

	if req.Role != "" {
		query = query.Where("u.role = ?", req.Role)
	}

	// 计算总数
	var total int64
	countQuery := h.service.DB.Table("users u").
		Joins("LEFT JOIN organizations o ON o.id::text = u.org_id::text").
		Joins("LEFT JOIN usage_metrics um ON o.id::text = um.org_id::text")

	// 应用相同的过滤条件
	if req.Search != "" {
		searchPattern := fmt.Sprintf("%%%s%%", req.Search)
		countQuery = countQuery.Where("u.email LIKE ? OR u.full_name LIKE ? OR o.name LIKE ?",
			searchPattern, searchPattern, searchPattern)
	}
	if req.Q != "" {
		qp := fmt.Sprintf("%%%s%%", req.Q)
		countQuery = countQuery.Where("u.email LIKE ? OR u.full_name LIKE ?", qp, qp)
	}
	if req.Status != "" {
		countQuery = countQuery.Where("u.status = ?", req.Status)
	}
	if req.Role != "" {
		countQuery = countQuery.Where("u.role = ?", req.Role)
	}

	if err := countQuery.Count(&total).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询用户总数失败")
		middleware.RecordBusinessOperation("admin_user_list", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 查询用户列表
	sortCol := map[string]string{"created_at": "u.created_at", "name": "u.full_name", "role": "u.role"}[req.SortBy]
	if sortCol == "" {
		sortCol = "u.created_at"
	}
	sortOrder := "ASC"
	if req.Order == "desc" {
		sortOrder = "DESC"
	}
	var users []AdminUserListResponse
	if err := query.Order(sortCol + " " + sortOrder).Offset(offset).Limit(req.Limit).Find(&users).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询用户列表失败")
		middleware.RecordBusinessOperation("admin_user_list", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 记录审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_user_list", "success",
		fmt.Sprintf("Admin viewed user list: page %d, limit %d", req.Page, req.Limit))

	// 记录业务操作成功
	middleware.RecordBusinessOperation("admin_user_list", true, time.Since(start), "")

	// 使用现有的分页响应函数
	JSONPaginated(c, users, req.Page, req.Limit, int(total))
}

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
func (h *AdminHandler) GetOrganizationList(c *gin.Context) {
	start := time.Now()

	// 权限检查 - 确保是超级管理员
	userRole, exists := c.Get("userRole")
	if !exists || userRole != "admin" {
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "permission_denied")
		JSONError(c, CodeForbidden, "权限不足")
		return
	}

	var req PaginationRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 设置默认值并计算分页参数
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 10 // 默认每页10条
	}

	// 如果提供了offset，优先使用offset；否则根据page和limit计算
	var offset int
	if req.Offset > 0 {
		offset = req.Offset
	} else {
		offset = (req.Page - 1) * req.Limit
	}

	// 构建查询 - 修改字段映射以匹配结构体
	query := h.service.DB.Table("organizations o").
		Select(`o.id, o.name, o.billing_email, o.plan_id, o.status, o.created_at,
            COUNT(DISTINCT u.id) as member_count,
            COALESCE(SUM(um.request_count), 0) as total_usage,
            COALESCE((SELECT requests_limit FROM plans WHERE id = o.plan_id), 0) as requests_limit`).
		Joins("LEFT JOIN users u ON o.id::text = u.org_id::text").
		Joins("LEFT JOIN usage_metrics um ON o.id::text = um.org_id::text").
		Group("o.id, o.name, o.billing_email, o.plan_id, o.status, o.created_at")
	sortBy := c.Query("sort_by")
	order := c.Query("order")
	q := c.Query("q")

	// 应用过滤条件
	if req.Search != "" {
		searchPattern := fmt.Sprintf("%%%s%%", req.Search)
		query = query.Where("o.name LIKE ? OR o.billing_email LIKE ?", searchPattern, searchPattern)
	}
	if q != "" {
		qp := fmt.Sprintf("%%%s%%", q)
		query = query.Where("o.name LIKE ?", qp)
	}

	if req.Status != "" {
		query = query.Where("o.status = ?", req.Status)
	}

	// 计算总数
	var total int64
	countQuery := h.service.DB.Table("organizations o")
	if req.Search != "" {
		searchPattern := fmt.Sprintf("%%%s%%", req.Search)
		countQuery = countQuery.Where("o.name LIKE ? OR o.billing_email LIKE ?", searchPattern, searchPattern)
	}
	if req.Status != "" {
		countQuery = countQuery.Where("o.status = ?", req.Status)
	}

	if err := countQuery.Count(&total).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织总数失败")
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 查询组织列表 - 使用临时结构体接收查询结果
	type tempOrgResult struct {
		ID            string    `json:"id"`
		Name          string    `json:"name"`
		BillingEmail  string    `json:"billing_email"`
		PlanID        string    `json:"plan_id"`
		Status        string    `json:"status"`
		CreatedAt     time.Time `json:"created_at"`
		MemberCount   int       `json:"member_count"`
		TotalUsage    int       `json:"total_usage"`
		RequestsLimit int       `json:"requests_limit"`
	}

	orgSortCol := map[string]string{"created_at": "o.created_at", "name": "o.name", "total_usage": "total_usage"}[sortBy]
	if orgSortCol == "" {
		orgSortCol = "o.created_at"
	}
	orgSortOrder := "ASC"
	if order == "desc" {
		orgSortOrder = "DESC"
	}
	var tempResults []tempOrgResult
	if err := query.Order(orgSortCol + " " + orgSortOrder).Offset(offset).Limit(req.Limit).Find(&tempResults).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织列表失败")
		middleware.RecordBusinessOperation("admin_org_list", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 转换为响应结构体
	organizations := make([]AdminOrganizationResponse, len(tempResults))
	planNames := map[string]string{
		"starter": "Starter Plan",
		"growth":  "Growth Plan",
		"scale":   "Scale Plan",
	}

	for i, result := range tempResults {
		organizations[i] = AdminOrganizationResponse{
			ID:           result.ID,
			Name:         result.Name,
			BillingEmail: result.BillingEmail,
			Plan: PlanInfo{
				ID:   result.PlanID,
				Name: result.PlanID,
			},
			MemberCount: result.MemberCount,
			TotalUsage:  result.TotalUsage,
			CreatedAt:   result.CreatedAt,
			Status:      result.Status,
		}
		limit := result.RequestsLimit
		percent := 0.0
		if limit > 0 {
			percent = float64(result.TotalUsage) / float64(limit) * 100
		}
		organizations[i].UsageSummary.TotalRequests = result.TotalUsage
		organizations[i].UsageSummary.Limit = limit
		organizations[i].UsageSummary.PercentUsed = percent
		organizations[i].UsageSummary.Period = time.Now().Format("2006-01")

		// 设置套餐名称
		if name, ok := planNames[result.PlanID]; ok {
			organizations[i].Plan.Name = name
		}
	}

	// 记录审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_org_list", "success",
		fmt.Sprintf("Admin viewed organization list: page %d, limit %d", req.Page, req.Limit))

	// 记录业务操作成功
	middleware.RecordBusinessOperation("admin_org_list", true, time.Since(start), "")

	// 使用现有的分页响应函数
	JSONPaginated(c, organizations, req.Page, req.Limit, int(total))
}

// GetPlans 管理端获取计划列表
func (h *AdminHandler) GetPlans(c *gin.Context) {
	var rows []AdminPlan
	err := h.service.DB.Raw(`SELECT id, name, COALESCE(price,0) AS price, COALESCE(currency,'USD') AS currency, COALESCE(requests_limit,0) AS requests_limit, COALESCE(features,'[]') AS features, COALESCE(quota_config,'{}') AS quota_config, COALESCE(is_active, true) AS is_active, updated_at FROM plans`).Scan(&rows).Error
	if err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, rows)
}

// UpdatePlan 管理端更新计划基础属性
func (h *AdminHandler) UpdatePlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		JSONError(c, CodeInvalidParameter, "缺少计划ID")
		return
	}
	var req PlanUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	updates := map[string]interface{}{}
	if req.Price != nil {
		updates["price"] = *req.Price
	}
	if req.Currency != nil {
		updates["currency"] = *req.Currency
	}
	if req.RequestsLimit != nil {
		updates["requests_limit"] = *req.RequestsLimit
	}
	if req.Features != nil {
		updates["features"] = *req.Features
	}
	if req.QuotaConfig != nil {
		updates["quota_config"] = *req.QuotaConfig
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if len(updates) == 0 {
		JSONError(c, CodeInvalidParameter, "无更新内容")
		return
	}
	if err := h.service.DB.Table("plans").Where("id = ?", id).Updates(updates).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	// 审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_update_plan", "success", fmt.Sprintf("Update plan %s: %+v", id, updates))
	JSONSuccess(c, gin.H{"updated": id})
}

// GetOrganizationQuotas 获取组织配额
func (h *AdminHandler) GetOrganizationQuotas(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}
	type item struct {
		ServiceType string
		Allocation  int
		Consumed    int
		ResetAt     *time.Time
	}
	var qs []item
	if err := h.service.DB.Raw("SELECT service_type, allocation, consumed, reset_at FROM organization_quotas WHERE organization_id = ?", orgID).Scan(&qs).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	type view struct {
		Limit     int     `json:"limit"`
		Used      int     `json:"used"`
		Remaining int     `json:"remaining"`
		ResetAt   *string `json:"resetAt"`
	}
	resp := map[string]view{}
	for _, q := range qs {
		var ra *string
		if q.ResetAt != nil {
			s := q.ResetAt.UTC().Format("2006-01-02T15:04:00Z")
			ra = &s
		}
		resp[q.ServiceType] = view{Limit: q.Allocation, Used: q.Consumed, Remaining: q.Allocation - q.Consumed, ResetAt: ra}
	}
	JSONSuccess(c, resp)
}

type QuotaAdjustRequest struct {
	ServiceType string `json:"service_type" binding:"required,oneof=ocr face liveness"`
	Adjustment  int    `json:"adjustment" binding:"required"`
	Reason      string `json:"reason"`
}

// AdjustOrganizationQuota 调整组织配额（原子更新+审计）
func (h *AdminHandler) AdjustOrganizationQuota(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}
	var req QuotaAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	// 原子调整，避免负数
	type rid struct{ ID string }
	var r rid
	err := h.service.DB.Raw(`UPDATE organization_quotas SET allocation = CASE WHEN allocation + ? < 0 THEN 0 ELSE allocation + ? END, updated_at = NOW() WHERE organization_id = ? AND service_type = ? RETURNING id`, req.Adjustment, req.Adjustment, orgID, req.ServiceType).Scan(&r).Error
	if err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	if r.ID == "" {
		JSONError(c, CodeNotFound, "记录不存在")
		return
	}
	// 审计日志
	details := map[string]interface{}{"org_id": orgID, "service_type": req.ServiceType, "adjustment": req.Adjustment, "reason": req.Reason}
	b, _ := json.Marshal(details)
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: "quota.adjust", Resource: "organization_quotas", Details: string(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Admin adjusted quota"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"adjusted": r.ID})
}

type UpdateOrgPlanRequest struct {
	PlanID     string `json:"plan_id" binding:"required"`
	Immediate  *bool  `json:"immediate"`
	ResetUsage *bool  `json:"reset_usage"`
}

func (h *AdminHandler) UpdateOrganizationPlan(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}
	var req UpdateOrgPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	imm := true
	if req.Immediate != nil {
		imm = *req.Immediate
	}
	reset := false
	if req.ResetUsage != nil {
		reset = *req.ResetUsage
	}
	if err := h.service.DB.Model(&models.Organization{}).Where("id = ?", orgID).Update("plan_id", req.PlanID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	if imm {
		_ = h.service.SyncOrganizationQuotasWithPolicy(orgID, req.PlanID, reset)
	}
	type item struct {
		ServiceType string
		Allocation  int
		Consumed    int
	}
	var qs []item
	_ = h.service.DB.Raw("SELECT service_type, allocation, consumed FROM organization_quotas WHERE organization_id = ?", orgID).Scan(&qs).Error
	resp := map[string]map[string]int{}
	for _, q := range qs {
		resp[q.ServiceType] = map[string]int{"limit": q.Allocation, "used": q.Consumed, "remaining": q.Allocation - q.Consumed}
	}
	details := map[string]interface{}{"org_id": orgID, "plan_id": req.PlanID, "immediate": imm, "reset_usage": reset}
	b, _ := json.Marshal(details)
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: "admin.update_org_plan", Resource: "organizations", Details: string(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Update organization plan"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"org_id": orgID, "new_plan": req.PlanID, "quotas": resp})
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
func (h *AdminHandler) UpdateUserStatus(c *gin.Context) {
	start := time.Now()

	// 权限检查
	userRole, exists := c.Get("userRole")
	if !exists || userRole != "admin" {
		middleware.RecordBusinessOperation("admin_update_user_status", false, time.Since(start), "permission_denied")
		JSONError(c, CodeForbidden, "权限不足")
		return
	}

	userID := c.Param("id")
	if userID == "" {
		middleware.RecordBusinessOperation("admin_update_user_status", false, time.Since(start), "invalid_user_id")
		JSONError(c, CodeInvalidParameter, "用户ID不能为空")
		return
	}

	var req UpdateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RecordBusinessOperation("admin_update_user_status", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 更新用户状态
	if err := h.service.DB.Model(&models.User{}).Where("id = ?", userID).Update("status", req.Status).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新用户状态失败")
		middleware.RecordBusinessOperation("admin_update_user_status", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}

	// 记录审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_user_status_updated", "success",
		fmt.Sprintf("Admin updated user %s status to %s", userID, req.Status))

	// 记录业务操作成功
	middleware.RecordBusinessOperation("admin_update_user_status", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"message": "用户状态更新成功",
	})
}

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
func (h *AdminHandler) UpdateUserAdmin(c *gin.Context) {
	// 平台管理员权限由路由中间件 RequirePlatformAdmin 保证
	userID := c.Param("id")
	if userID == "" {
		JSONError(c, CodeInvalidParameter, "缺少用户ID")
		return
	}
	var req AdminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	// 加载用户
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeNotFound, "用户不存在")
		return
	}
	updates := map[string]interface{}{}
	if req.Role != nil {
		if *req.Role != "admin" && *req.Role != "user" {
			JSONError(c, CodeInvalidParameter, "非法角色")
			return
		}
		updates["role"] = *req.Role
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "suspended" {
			JSONError(c, CodeInvalidParameter, "非法状态")
			return
		}
		updates["status"] = *req.Status
	}
	// 密码重置
	if req.Password != nil && *req.Password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			JSONError(c, CodeInternalError, "密码处理失败")
			return
		}
		updates["password"] = string(hashed)
	}
	if len(updates) > 0 {
		if err := h.service.DB.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
			JSONError(c, CodeDatabaseError, "更新失败")
			return
		}
	}
	// 会话撤销：删除该用户的OAuthToken（如使用）
	_ = h.service.DB.Where("user_id = ?", userID).Delete(&models.OAuthToken{}).Error
	// 审计日志
	details := map[string]interface{}{"target_user_id": userID, "updated_fields": updates}
	b, _ := json.Marshal(details)
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: user.OrgID, Action: "admin.user_update", Resource: "users", Details: string(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Admin updated user"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"updated": userID})
}

// AdminAuditLogResponse 管理员审计日志响应
type AdminAuditLogResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	IP        string    `json:"ip"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

type AdminOverviewStats struct {
	TotalUsers    int64 `json:"total_users"`
	ActiveKeys    int64 `json:"active_keys"`
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
func (h *AdminHandler) GetAuditLogs(c *gin.Context) {
	start := time.Now()

	// 权限检查 - 确保是超级管理员
	userRole, exists := c.Get("userRole")
	if !exists || userRole != "admin" {
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "permission_denied")
		JSONError(c, CodeForbidden, "权限不足")
		return
	}

	var req AdminAuditLogRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 设置默认值并计算分页参数
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit <= 0 || req.Limit > 1000 {
		req.Limit = 50 // 默认每页50条
	}

	// 如果提供了offset，优先使用offset；否则根据page和limit计算
	var offset int
	if req.Offset > 0 {
		offset = req.Offset
	} else {
		offset = (req.Page - 1) * req.Limit
	}

	// 构建查询
	query := h.service.DB.Table("audit_logs al").
		Select(`al.id, al.user_id, al.action, al.resource as target, al.ip, al.created_at as timestamp, al.status,
			COALESCE(u.full_name, u.name, 'System') as user_name`).
		Joins("LEFT JOIN users u ON u.id = al.user_id").
		Order("al.created_at DESC")

	// 应用筛选条件
	if req.UserID != "" {
		query = query.Where("al.user_id = ?", req.UserID)
	}

	if req.Action != "" {
		query = query.Where("al.action = ?", req.Action)
	}

	if req.Status != "" {
		query = query.Where("al.status = ?", req.Status)
	}

	// 日期范围筛选
	if req.DateFrom != "" || req.FromDate != "" {
		if fromDate, err := time.Parse("2006-01-02", req.DateFrom); err == nil {
			query = query.Where("al.created_at >= ?", fromDate)
		}
		if req.FromDate != "" {
			if fd, err := time.Parse("2006-01-02", req.FromDate); err == nil {
				query = query.Where("al.created_at >= ?", fd)
			}
		}
	}

	if req.DateTo != "" || req.ToDate != "" {
		if toDate, err := time.Parse("2006-01-02", req.DateTo); err == nil {
			// 包含指定日期的整天
			toDate = toDate.Add(24 * time.Hour)
			query = query.Where("al.created_at < ?", toDate)
		}
		if req.ToDate != "" {
			if td, err := time.Parse("2006-01-02", req.ToDate); err == nil {
				td = td.Add(24 * time.Hour)
				query = query.Where("al.created_at < ?", td)
			}
		}
	}

	// 计算总数 - 重新构建计数查询，避免ORDER BY导致的GROUP BY错误
	var total int64
	countQuery := h.service.DB.Table("audit_logs al").
		Joins("LEFT JOIN users u ON u.id = al.user_id")

	// 应用相同的筛选条件
	if req.UserID != "" {
		countQuery = countQuery.Where("al.user_id = ?", req.UserID)
	}
	if req.Action != "" {
		countQuery = countQuery.Where("al.action = ?", req.Action)
	}
	if req.Status != "" {
		countQuery = countQuery.Where("al.status = ?", req.Status)
	}
	if req.DateFrom != "" {
		if fromDate, err := time.Parse("2006-01-02", req.DateFrom); err == nil {
			countQuery = countQuery.Where("al.created_at >= ?", fromDate)
		}
	}
	if req.DateTo != "" {
		if toDate, err := time.Parse("2006-01-02", req.DateTo); err == nil {
			toDate = toDate.Add(24 * time.Hour)
			countQuery = countQuery.Where("al.created_at < ?", toDate)
		}
	}

	if err := countQuery.Count(&total).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询审计日志总数失败")
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 查询审计日志列表
	var logs []AdminAuditLogResponse
	if err := query.Offset(offset).Limit(req.Limit).Find(&logs).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询审计日志列表失败")
		middleware.RecordBusinessOperation("admin_audit_logs", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	// 记录审计日志
	h.recordAuditLog(c, c.GetString("userID"), "admin_audit_logs", "success",
		fmt.Sprintf("Admin viewed audit logs: page %d, limit %d", req.Page, req.Limit))

	// 记录业务操作成功
	middleware.RecordBusinessOperation("admin_audit_logs", true, time.Since(start), "")

	// 审计日志保持原有的数据结构，但使用分页响应格式
	JSONSuccess(c, gin.H{
		"logs":   logs,
		"total":  total,
		"limit":  req.Limit,
		"offset": offset,
		"page":   req.Page,
	})
}

func (h *AdminHandler) GetOverviewStats(c *gin.Context) {
	var totalUsers int64
	_ = h.service.DB.Model(&models.User{}).Count(&totalUsers).Error
	var activeKeys int64
	_ = h.service.DB.Model(&models.APIKey{}).Where("status = ?", "active").Count(&activeKeys).Error
	var todayRequests int64
	_ = h.service.DB.Model(&models.APIRequestLog{}).Where("DATE(created_at) = CURRENT_DATE").Count(&todayRequests).Error
	JSONSuccess(c, AdminOverviewStats{TotalUsers: totalUsers, ActiveKeys: activeKeys, TodayRequests: todayRequests})
}

// recordAuditLog 记录审计日志
func (h *AdminHandler) recordAuditLog(c *gin.Context, userID, action, status, message string) {
	go func() {
		auditLog := &models.AuditLog{
			UserID:    userID,
			Action:    action,
			IP:        c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
			Status:    status,
			Message:   message,
		}
		if err := h.service.DB.Create(auditLog).Error; err != nil {
			logger.GetLogger().WithError(err).Error("记录审计日志失败")
		}
	}()
}

type UpdatePlanQuotaRequest struct {
	OCRLimit   int    `json:"ocr_limit"`
	OCRPeriod  string `json:"ocr_period"`
	FaceLimit  int    `json:"face_limit"`
	FacePeriod string `json:"face_period"`
}

func (h *AdminHandler) UpdatePlanQuota(c *gin.Context) { // ignore_security_alert
	planID := c.Param("plan_id")
	var req UpdatePlanQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	cfg := map[string]map[string]interface{}{
		"ocr":  {"limit": req.OCRLimit, "period": req.OCRPeriod},
		"face": {"limit": req.FaceLimit, "period": req.FacePeriod},
	}
	b, _ := json.Marshal(cfg)
	if err := h.service.DB.Exec("UPDATE plans SET quota_config = ?, updated_at = NOW() WHERE id = ?", string(b), planID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"updated": planID})
}

type UpdateGlobalConfigRequest struct {
	DailyRegistrationCap int `json:"daily_registration_cap"`
}

func (h *AdminHandler) UpdateGlobalConfig(c *gin.Context) {
	var req UpdateGlobalConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	if err := h.service.DB.Exec("INSERT INTO global_configs(key,value,updated_at) VALUES('daily_registration_cap', ?, NOW()) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at", fmt.Sprintf("%d", req.DailyRegistrationCap)).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"daily_registration_cap": req.DailyRegistrationCap})
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
