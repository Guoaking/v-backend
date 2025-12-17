package api

import (
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "kyc-service/internal/middleware"
    "kyc-service/internal/models"
    "kyc-service/internal/service"
    "kyc-service/pkg/logger"
    "kyc-service/pkg/metrics"
    "kyc-service/pkg/utils"

    "github.com/gin-gonic/gin"
    "golang.org/x/crypto/bcrypt"
    "gorm.io/datatypes"
    "gorm.io/gorm"
)

// OrganizationHandler 组织管理处理器
type OrganizationHandler struct {
	service *service.KYCService
}

// NewOrganizationHandler 创建组织管理处理器
func NewOrganizationHandler(svc *service.KYCService) *OrganizationHandler {
	return &OrganizationHandler{service: svc}
}

// OrgMemberResponse 组织成员响应
type OrgMemberResponse struct {
	ID           string `json:"id"`
	UserID       string `json:"userId"`
	Email        string `json:"email"`
	FullName     string `json:"name"`
	AvatarURL    string `json:"avatar"`
	Role         string `json:"role"`
	Status       string `json:"status"`
	JoinedAt     string `json:"joinedAt"`
	LastActiveAt string `json:"last_active_at,omitempty"`
}

// OrganizationResponse 组织信息响应
type OrganizationResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PlanID       string    `json:"plan_id"`
	BillingEmail string    `json:"billing_email"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	MemberCount  int       `json:"member_count"`
}

// InviteMemberRequest 邀请成员请求
type InviteMemberRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required"`
}

// UpdatePlanRequest 更新套餐请求
type UpdatePlanRequest struct {
	PlanID string `json:"plan_id" binding:"required,oneof=starter growth scale"`
}

// @Summary 获取当前组织信息
// @Description 组织管理员获取当前组织的基本信息
// @Tags Organization
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/orgs/current [get]
func (h *OrganizationHandler) GetCurrentOrganization(c *gin.Context) {
	start := time.Now()

	// 能力验证由路由中间件负责

	orgID := c.GetString("orgID")
    if orgID == "" {
        metrics.RecordBusinessOperation(c.Request.Context(), "get_org_info", false, time.Since(start), "org_not_found")
        JSONError(c, CodeInvalidParameter, "组织信息错误")
        return
    }

	// 查询组织信息
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", orgID).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            metrics.RecordBusinessOperation(c.Request.Context(), "get_org_info", false, time.Since(start), "org_not_found")
            JSONError(c, CodeNotFound, "组织不存在")
            return
        }
        logger.GetLogger().WithError(err).Error("查询组织失败")
        metrics.RecordBusinessOperation(c.Request.Context(), "get_org_info", false, time.Since(start), "database_error")
        JSONError(c, CodeDatabaseError, "系统错误")
        return
    }

	// 查询成员数量
	var memberCount int64
	if err := h.service.DB.Model(&models.User{}).Where("org_id = ? AND status = ?", orgID, "active").Count(&memberCount).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询成员数量失败")
		memberCount = 0
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    c.GetString("userID"),
		OrgID:     orgID,
		Action:    "view_organization",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Viewed organization: %s", org.Name),
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("get_org_info", true, time.Since(start), "")

	JSONSuccess(c, OrganizationResponse{
		ID:           org.ID,
		Name:         org.Name,
		PlanID:       org.PlanID,
		BillingEmail: org.BillingEmail,
		Status:       org.Status,
		CreatedAt:    org.CreatedAt,
		MemberCount:  int(memberCount),
	})
}

// @Summary 获取组织成员列表
// @Description 组织管理员获取组织成员列表
// @Tags Organization
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/orgs/members [get]
func (h *OrganizationHandler) GetOrganizationMembers(c *gin.Context) {
	start := time.Now()

	// 能力验证由路由中间件负责

	orgID := c.GetString("orgID")
	if orgID == "" {
		middleware.RecordBusinessOperation("get_org_members", false, time.Since(start), "org_not_found")
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}

	// 查询组织成员（通过关系表 Join 用户）
	type row struct {
		ID           string
		UserID       string
		Email        string
		FullName     string
		AvatarURL    string
		Role         string
		Status       string
		CreatedAt    time.Time
		LastActiveAt *time.Time
	}
	var rows []row
	if err := h.service.DB.Table("organization_members om").
		Select("om.id as id, om.user_id as user_id, u.email as email, COALESCE(u.full_name,u.name) as full_name, u.avatar_url as avatar_url, om.role as role, om.status as status, om.created_at as created_at, om.last_active_at as last_active_at").
		Joins("LEFT JOIN users u ON u.id = om.user_id").
		Where("om.organization_id = ?", orgID).
		Order("om.created_at DESC").
		Find(&rows).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织成员失败")
		middleware.RecordBusinessOperation("get_org_members", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 构建响应
	memberResponses := make([]OrgMemberResponse, len(rows))
	for i, r := range rows {
		la := ""
		if r.LastActiveAt != nil {
			la = r.LastActiveAt.Format("2006-01-02 15:04:05")
		}
		memberResponses[i] = OrgMemberResponse{ID: r.ID, UserID: r.UserID, Email: r.Email, FullName: r.FullName, AvatarURL: r.AvatarURL, Role: r.Role, Status: r.Status, JoinedAt: r.CreatedAt.Format("2006-01-02 15:04:05"), LastActiveAt: la}
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    c.GetString("userID"),
		OrgID:     orgID,
		Action:    "view_organization_members",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Viewed organization members: %d members", len(memberResponses)),
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("get_org_members", true, time.Since(start), "")

	JSONSuccess(c, gin.H{"items": memberResponses, "total": len(memberResponses)})
}

// @Summary 邀请组织成员
// @Description 组织管理员邀请新成员加入组织
// @Tags Organization
// @Accept json
// @Produce json
// @Param request body InviteMemberRequest true "邀请成员请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/orgs/members [post]
func (h *OrganizationHandler) InviteOrganizationMember(c *gin.Context) {
	start := time.Now()

	// 能力验证由路由中间件负责

	orgID := c.GetString("orgID")
	if orgID == "" {
		middleware.RecordBusinessOperation("invite_org_member", false, time.Since(start), "org_not_found")
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}

	var req InviteMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RecordBusinessOperation("invite_org_member", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 检查被邀请用户是否已存在
	var existingUser models.User
	err := h.service.DB.Where("email = ?", req.Email).First(&existingUser).Error
	if err == nil {
		// 已注册用户：直接加入组织为active
		member := models.OrganizationMember{
			ID:             utils.GenerateID(),
			OrganizationID: orgID,
			UserID:         existingUser.ID,
			Role:           req.Role,
			Status:         "active",
		}
		if err := h.service.DB.Create(&member).Error; err != nil {
			logger.GetLogger().WithError(err).Error("创建组织成员失败")
			middleware.RecordBusinessOperation("invite_org_member", false, time.Since(start), "database_error")
			JSONError(c, CodeDatabaseError, "创建成员失败")
			return
		}
		middleware.RecordBusinessOperation("invite_org_member", true, time.Since(start), "")
		JSONSuccess(c, OrgMemberResponse{ID: member.ID, UserID: existingUser.ID, Email: existingUser.Email, FullName: existingUser.Name, AvatarURL: existingUser.AvatarURL, Role: member.Role, Status: member.Status, JoinedAt: member.CreatedAt.Format("2006-01-02 15:04:05")})
		return
	}

	// 未注册邮箱：创建邀请记录并标记invited
	invitation := models.OrganizationInvitation{
		ID:        utils.GenerateID(),
		OrgID:     orgID,
		Email:     req.Email,
		Role:      req.Role,
		InvitedBy: c.GetString("userID"),
		Token:     utils.GenerateID(),
		Status:    "invited",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}

	if err := h.service.DB.Create(&invitation).Error; err != nil {
		logger.GetLogger().WithError(err).Error("创建邀请记录失败")
		middleware.RecordBusinessOperation("invite_org_member", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "邀请失败")
		return
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    c.GetString("userID"),
		OrgID:     orgID,
		Action:    "invite_organization_member",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Invited member: %s with role %s", req.Email, req.Role),
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("invite_org_member", true, time.Since(start), "")

	JSONSuccess(c, gin.H{"id": invitation.ID, "email": req.Email, "role": req.Role, "status": "invited"})
}

// DeleteOrganizationMember 移除成员
func (h *OrganizationHandler) DeleteOrganizationMember(c *gin.Context) {
	start := time.Now()
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	memberID := c.Param("id")
	if memberID == "" {
		JSONError(c, CodeInvalidParameter, "成员ID不能为空")
		return
	}
	var member models.OrganizationMember
	if err := h.service.DB.Where("id = ? AND organization_id = ?", memberID, orgID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			JSONError(c, CodeNotFound, "成员不存在")
			return
		}
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	if member.Role == "owner" {
		JSONError(c, CodeForbidden, "不允许移除所有者")
		return
	}
	if err := h.service.DB.Delete(&member).Error; err != nil {
		JSONError(c, CodeDatabaseError, "移除失败")
		return
	}
	middleware.RecordBusinessOperation("remove_org_member", true, time.Since(start), "")
	JSONSuccess(c, gin.H{"deleted": memberID})
}

type BillingResponse struct {
	Plan struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Price         int    `json:"price"`
		RequestsLimit int    `json:"requestsLimit"`
	} `json:"plan"`
	UsageSummary struct {
		TotalRequests int64   `json:"totalRequests"`
		Limit         int     `json:"limit"`
		PercentUsed   float64 `json:"percentUsed"`
		Period        string  `json:"period"`
	} `json:"usageSummary"`
	Invoices []struct {
		ID     string `json:"id"`
		Amount int    `json:"amount"`
		Status string `json:"status"`
		Date   string `json:"date"`
	} `json:"invoices"`
}

// GetBilling 获取账单与用量
func (h *OrganizationHandler) GetBilling(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", orgID).Error; err != nil {
		JSONError(c, CodeNotFound, "组织不存在")
		return
	}
	planMap := map[string]struct {
		Name  string
		Price int
		Limit int
	}{
		"starter": {Name: "Starter", Price: 0, Limit: 1000},
		"growth":  {Name: "Growth", Price: 299, Limit: 50000},
		"scale":   {Name: "Scale", Price: 999, Limit: 1000000},
	}
	pm := planMap[org.PlanID]
	var total int64
	_ = h.service.DB.Table("usage_logs").Where("org_id = ? AND created_at >= date_trunc('month', now())", orgID).Count(&total).Error
	percent := float64(0)
	if pm.Limit > 0 {
		percent = float64(total) / float64(pm.Limit) * 100
	}
	resp := BillingResponse{}
	resp.Plan.ID = org.PlanID
	resp.Plan.Name = pm.Name
	resp.Plan.Price = pm.Price
	resp.Plan.RequestsLimit = pm.Limit
	resp.UsageSummary.TotalRequests = total
	resp.UsageSummary.Limit = pm.Limit
	resp.UsageSummary.PercentUsed = percent
	resp.UsageSummary.Period = time.Now().Format("2006-01")
	resp.Invoices = []struct {
		ID     string `json:"id"`
		Amount int    `json:"amount"`
		Status string `json:"status"`
		Date   string `json:"date"`
	}{}
	JSONSuccess(c, resp)
}

type UsageDailyResponse struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Errors   int64  `json:"errors"`
}

// GetUsageDaily 获取日统计
func (h *OrganizationHandler) GetUsageDaily(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	period := c.Query("period")
	days := 30
	if period == "7d" {
		days = 7
	} else if period == "90d" {
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	var rows []struct {
		Day   time.Time
		Total int64
		Errs  int64
	}
	_ = h.service.DB.Raw(`SELECT date(created_at) AS day, COUNT(*) AS total, SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS errs FROM api_request_logs WHERE org_id = ? AND created_at >= ? GROUP BY date(created_at) ORDER BY day`, orgID, since).Scan(&rows).Error
	resp := make([]UsageDailyResponse, len(rows))
	for i, r := range rows {
		resp[i] = UsageDailyResponse{Date: r.Day.Format("2006-01-02"), Requests: r.Total, Errors: r.Errs}
	}
	JSONSuccess(c, resp)
}

// @Summary 更新组织套餐
// @Description 组织所有者更新组织套餐
// @Tags Organization
// @Accept json
// @Produce json
// @Param request body UpdatePlanRequest true "更新套餐请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/orgs/plan [put]
func (h *OrganizationHandler) UpdatePlan(c *gin.Context) {
	start := time.Now()

	// 能力验证由路由中间件负责

	orgID := c.GetString("orgID")
	if orgID == "" {
		middleware.RecordBusinessOperation("update_org_plan", false, time.Since(start), "org_not_found")
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}

	var req UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RecordBusinessOperation("update_org_plan", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 更新组织套餐
	if err := h.service.DB.Model(&models.Organization{}).Where("id = ?", orgID).Update("plan_id", req.PlanID).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新组织套餐失败")
		middleware.RecordBusinessOperation("update_org_plan", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}

	// 同步配额
	if err := h.service.SyncOrganizationQuotas(orgID, req.PlanID); err != nil {
		logger.GetLogger().WithError(err).Error("同步组织配额失败")
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    c.GetString("userID"),
		OrgID:     orgID,
		Action:    "update_organization_plan",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Updated organization plan to: %s", req.PlanID),
	}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}

	// 记录业务操作成功
	middleware.RecordBusinessOperation("update_org_plan", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"message": "套餐更新成功",
	})
}

// UpdateMemberRole 修改成员角色
type UpdateMemberRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=admin developer viewer"`
}

type ResetMemberPasswordRequest struct {
	Password string `json:"password" binding:"required,min=6"`
}

// ResetMemberPassword 组织管理员重置成员密码
func (h *OrganizationHandler) ResetMemberPassword(c *gin.Context) {
	orgID := c.GetString("orgID")
	operatorRole := c.GetString("orgRole")
	memberID := c.Param("id")
	if orgID == "" || memberID == "" {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	var req ResetMemberPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	// 查找成员关系
	var member models.OrganizationMember
	if err := h.service.DB.Where("id = ? AND organization_id = ?", memberID, orgID).First(&member).Error; err != nil {
		JSONError(c, CodeNotFound, "成员不存在")
		return
	}
	// 不允许非所有者重置所有者密码
	if member.Role == "owner" && operatorRole != "owner" {
		JSONError(c, CodeForbidden, "无权修改所有者密码")
		return
	}
	// 加载用户并重置密码
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", member.UserID).Error; err != nil {
		JSONError(c, CodeNotFound, "用户不存在")
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		JSONError(c, CodeInternalError, "密码处理失败")
		return
	}
	if err := h.service.DB.Model(&models.User{}).Where("id = ?", user.ID).Update("password", string(hashed)).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	// 会话撤销
	_ = h.service.DB.Where("user_id = ?", user.ID).Delete(&models.OAuthToken{}).Error
	// 审计
	details := map[string]interface{}{"target_user_id": user.ID, "member_id": memberID}
	b, _ := json.Marshal(details)
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: "user.update_password", Resource: "users", Details: string(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Reset member password"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"updated": user.ID})
}

func (h *OrganizationHandler) UpdateMemberRole(c *gin.Context) {
	start := time.Now()
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	memberID := c.Param("id")
	if memberID == "" {
		JSONError(c, CodeInvalidParameter, "成员ID不能为空")
		return
	}
	var req UpdateMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	// 角色必须存在
	var roleCount int64
	if err := h.service.DB.Model(&models.Role{}).Where("id = ?", req.Role).Count(&roleCount).Error; err != nil || roleCount == 0 {
		JSONError(c, CodeInvalidParameter, "角色不存在")
		return
	}
	var member models.OrganizationMember
	if err := h.service.DB.Where("id = ? AND organization_id = ?", memberID, orgID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			JSONError(c, CodeNotFound, "成员不存在")
			return
		}
		logger.GetLogger().WithError(err).Error("查询成员失败")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}
	if member.Role == "owner" || req.Role == "owner" {
		JSONError(c, CodeForbidden, "不允许修改所有者角色")
		return
	}
	if err := h.service.DB.Model(&member).Update("role", req.Role).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新成员角色失败")
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	middleware.RecordBusinessOperation("update_member_role", true, time.Since(start), "")
	JSONSuccess(c, gin.H{"message": "角色更新成功"})
}

// GetUsageSummary 组织用量汇总
func (h *OrganizationHandler) GetUsageSummary(c *gin.Context) {
	orgID := c.Param("org_id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织ID不能为空")
		return
	}
	var org models.Organization
	if err := h.service.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			JSONError(c, CodeNotFound, "组织不存在")
			return
		}
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}
	now := time.Now()
	period := now.Format("2006-01")
	var total int64
	if err := h.service.DB.Raw(`SELECT COUNT(*) FROM api_request_logs WHERE organization_id = ? AND created_at >= date_trunc('month', now()) AND created_at < date_trunc('month', now()) + interval '1 month'`, orgID).Scan(&total).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	planLimits := map[string]int{"starter": 50000, "growth": 200000, "scale": 1000000}
	limit := planLimits[org.PlanID]
	percent := float64(0)
	status := "healthy"
	if limit > 0 {
		percent = float64(total) / float64(limit) * 100
		if percent >= 100 {
			status = "exceeded"
		} else if percent >= 80 {
			status = "warning"
		}
	}
	JSONSuccess(c, gin.H{"period": period, "total_requests": total, "plan_limit": limit, "usage_percent": percent, "status": status})
}

type UpdateMemberStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active suspended"`
}

func (h *OrganizationHandler) UpdateMemberStatus(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	memberID := c.Param("id")
	if memberID == "" {
		JSONError(c, CodeInvalidParameter, "成员ID不能为空")
		return
	}
	var req UpdateMemberStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	var member models.OrganizationMember
	if err := h.service.DB.Where("id = ? AND organization_id = ?", memberID, orgID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			JSONError(c, CodeNotFound, "成员不存在")
			return
		}
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	if member.Role == "owner" && req.Status == "suspended" {
		JSONError(c, CodeForbidden, "不允许停用所有者")
		return
	}
	if err := h.service.DB.Model(&member).Update("status", req.Status).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	action := "member.activate"
	if req.Status == "suspended" {
		action = "member.suspend"
	}
	auditLog := &models.AuditLog{UserID: c.GetString("userID"), OrgID: orgID, Action: action, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: fmt.Sprintf("Member %s status -> %s", memberID, req.Status)}
	if err := h.service.DB.Create(auditLog).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}
	JSONSuccess(c, gin.H{"id": memberID, "status": req.Status})
}

// GetOrgAuditLogs 获取组织审计日志
func (h *OrganizationHandler) GetOrgAuditLogs(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	role := c.GetString("orgRole")
	if role != "owner" && role != "admin" {
		JSONError(c, CodeForbidden, "权限不足")
		return
	}
	page := 1
	pageSize := 20
	if v := c.Query("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := c.Query("page_size"); v != "" {
		fmt.Sscanf(v, "%d", &pageSize)
	} else if v := c.Query("limit"); v != "" {
		fmt.Sscanf(v, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	action := c.Query("action")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	type Row struct {
		ID        uint      `json:"id"`
		OrgID     string    `json:"org_id"`
		UserID    string    `json:"user_id"`
		UserName  string    `json:"user_name"`
		Action    string    `json:"action"`
		Message   string    `json:"message"`
		Resource  string    `json:"resource"`
		IP        string    `json:"ip"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	qb := h.service.DB.Table("audit_logs al").Select("al.id, al.org_id, al.user_id, COALESCE(u.full_name,u.name) as user_name, al.action, al.message, al.resource, al.ip, al.status, al.created_at").Joins("LEFT JOIN users u ON u.id = al.user_id").Where("al.org_id = ?", orgID)
	if action != "" {
		qb = qb.Where("al.action = ?", action)
	}
	if startDate != "" {
		qb = qb.Where("al.created_at >= ?::date", startDate)
	}
	if endDate != "" {
		qb = qb.Where("al.created_at < ?::date + interval '1 day'", endDate)
	}
	var total int64
	if err := qb.Count(&total).Error; err != nil {
		JSONError(c, CodeDatabaseError, "统计失败")
		return
	}
	var rows []Row
	if err := qb.Order("al.created_at DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}

	totalPage := (int(total) + pageSize - 1) / pageSize
	JSONSuccess(c, gin.H{"items": rows, "pagination": Pagination{
		Page:      page,
		PageSize:  pageSize,
		Total:     int(total),
		TotalPage: totalPage,
	}})

}

func (h *OrganizationHandler) ExportOrgAuditLogs(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	_ = c.DefaultQuery("format", "csv")
	action := c.Query("action")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	userID := c.Query("user_id")
	qb := h.service.DB.Table("audit_logs al").Select("al.id, al.org_id, al.user_id, COALESCE(u.full_name,u.name) as user_name, al.action, al.message, al.resource, al.ip, al.status, al.created_at").Joins("LEFT JOIN users u ON u.id = al.user_id").Where("al.org_id = ?", orgID)
	if action != "" {
		qb = qb.Where("al.action = ?", action)
	}
	if startDate != "" {
		qb = qb.Where("al.created_at >= ?::date", startDate)
	}
	if endDate != "" {
		qb = qb.Where("al.created_at < ?::date + interval '1 day'", endDate)
	}
	if userID != "" {
		qb = qb.Where("al.user_id = ?", userID)
	}
	type Row struct {
		ID                                                             uint
		OrgID, UserID, UserName, Action, Message, Resource, IP, Status string
		CreatedAt                                                      time.Time
	}
	var rows []Row
	if err := qb.Order("al.created_at DESC").Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	filename := "audit_logs.csv"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	w := c.Writer
	_, _ = w.Write([]byte("id,org_id,user_id,user_name,action,message,resource,ip,status,created_at\n"))
	for _, r := range rows {
		ts := r.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
		esc := func(s string) string {
			if strings.ContainsAny(s, ",\n\r\"") {
				return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
			}
			return s
		}
		line := fmt.Sprintf("%d,%s,%s,%s,%s,%s,%s,%s,%s,%s\n", r.ID, esc(r.OrgID), esc(r.UserID), esc(r.UserName), esc(r.Action), esc(r.Message), esc(r.Resource), esc(r.IP), esc(r.Status), ts)
		_, _ = w.Write([]byte(line))
	}
}

// CreateInvitation 发送邀请
type CreateInvitationRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required,oneof=admin viewer developer"`
}

func (h *OrganizationHandler) CreateInvitation(c *gin.Context) {
	orgID := c.GetString("orgID")
	userID := c.GetString("userID")
	if orgID == "" || userID == "" {
		JSONError(c, CodeInvalidParameter, "组织或用户信息错误")
		return
	}
	// 权限由路由中间件校验 team.invite
	var req CreateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	var dup int64
	_ = h.service.DB.Model(&models.Invitation{}).Where("org_id = ? AND email = ? AND status = ?", orgID, req.Email, "pending").Count(&dup).Error
	if dup > 0 {
		JSONError(c, CodeConflict, "已存在待处理邀请")
		return
	}
	var exists int64
	_ = h.service.DB.Table("organization_members om").Joins("INNER JOIN users u ON u.id = om.user_id").Where("om.organization_id = ? AND u.email = ?", orgID, req.Email).Count(&exists).Error
	if exists > 0 {
		JSONError(c, CodeConflict, "已是成员")
		return
	}
	token := utils.GenerateID()
	inv := models.Invitation{ID: utils.GenerateID(), OrgID: orgID, InviterID: userID, Email: req.Email, Role: req.Role, Token: token, Status: "pending", ExpiresAt: time.Now().Add(7 * 24 * time.Hour), CreatedAt: time.Now()}
	if err := h.service.DB.Create(&inv).Error; err != nil {
		JSONError(c, CodeDatabaseError, "创建邀请失败")
		return
	}
	// 站内信
	var target models.User
	if err := h.service.DB.Where("email = ?", req.Email).First(&target).Error; err == nil {
		payload := datatypes.JSON([]byte(fmt.Sprintf(`{"invitation_id":"%s","token":"%s"}`, inv.ID, token)))
		notif := models.Notification{ID: utils.GenerateID(), UserID: target.ID, Type: "invitation", Title: "组织邀请", Message: "您收到加入组织的邀请", Payload: payload, IsRead: false, CreatedAt: time.Now()}
		_ = h.service.DB.Create(&notif).Error
	}
	JSONSuccess(c, gin.H{"id": inv.ID, "token": inv.Token})
}

// ListInvitations 获取待处理邀请
func (h *OrganizationHandler) ListInvitations(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	type item struct {
		ID, Email, Role, Status, InviterName string
		SentAt                               time.Time
	}
	var rows []item
	if err := h.service.DB.Table("invitations i").Select("i.id, i.email, i.role, i.status, i.created_at as sent_at, COALESCE(u.full_name,u.name) as inviter_name").Joins("LEFT JOIN users u ON u.id = i.inviter_id").Where("i.org_id = ? AND i.status = ?", orgID, "pending").Order("i.created_at DESC").Scan(&rows).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, rows)
}

// RevokeInvitation 撤销邀请
func (h *OrganizationHandler) RevokeInvitation(c *gin.Context) {
	orgID := c.GetString("orgID")
	id := c.Param("id")
	if orgID == "" || id == "" {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	if err := h.service.DB.Model(&models.Invitation{}).Where("id = ? AND org_id = ?", id, orgID).Update("status", "revoked").Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"revoked": id})
}

type AcceptInvitationRequest struct {
	Token string `json:"token" binding:"required"`
}

// AcceptInvitation 接受邀请（登录用户）
func (h *OrganizationHandler) AcceptInvitation(c *gin.Context) {
	userID := c.GetString("userID")
	var req AcceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	var inv models.Invitation
	if err := h.service.DB.Where("token = ? AND status = ?", req.Token, "pending").First(&inv).Error; err != nil {
		JSONError(c, CodeNotFound, "邀请不存在或已处理")
		return
	}
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeUnauthorized, "未授权")
		return
	}
	if strings.ToLower(user.Email) != strings.ToLower(inv.Email) {
		JSONError(c, CodeForbidden, "邮箱不匹配")
		return
	}
	if time.Now().After(inv.ExpiresAt) {
		JSONError(c, CodeConflict, "邀请已过期")
		return
	}
	tx := h.service.DB.Begin()
	if err := tx.Model(&inv).Updates(map[string]interface{}{"status": "accepted", "accepted_at": time.Now()}).Error; err != nil {
		tx.Rollback()
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	var exist int64
	_ = tx.Model(&models.OrganizationMember{}).Where("organization_id = ? AND user_id = ?", inv.OrgID, userID).Count(&exist).Error
	if exist == 0 {
		mem := models.OrganizationMember{ID: utils.GenerateID(), OrganizationID: inv.OrgID, UserID: userID, Role: inv.Role, Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := tx.Create(&mem).Error; err != nil {
			tx.Rollback()
			JSONError(c, CodeDatabaseError, "加入失败")
			return
		}
	} else {
		_ = tx.Table("organization_members").Where("organization_id = ? AND user_id = ?", inv.OrgID, userID).Update("status", "active").Error
	}
	_ = tx.Model(&models.User{}).Where("id = ?", userID).Update("current_org_id", inv.OrgID).Error
	if err := tx.Commit().Error; err != nil {
		JSONError(c, CodeDatabaseError, "事务失败")
		return
	}
	// 审计
	audit := &models.AuditLog{UserID: userID, OrgID: inv.OrgID, Action: "member.join", IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Accepted invitation"}
	_ = h.service.DB.Create(audit).Error
	JSONSuccess(c, gin.H{"status": "accepted"})
}

func (h *OrganizationHandler) AcceptInvitationByID(c *gin.Context) {
	userID := c.GetString("userID")
	id := c.Param("id")
	var inv models.Invitation
	if err := h.service.DB.Where("id = ? AND status = ?", id, "pending").First(&inv).Error; err != nil {
		JSONError(c, CodeNotFound, "邀请不存在或已处理")
		return
	}
	var user models.User
	_ = h.service.DB.First(&user, "id = ?", userID).Error
	if strings.ToLower(user.Email) != strings.ToLower(inv.Email) {
		JSONError(c, CodeNotFound, "邀请不存在或已处理")
		return
	}
	var req AcceptInvitationRequest
	req.Token = inv.Token
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Token = inv.Token
	}
	h.AcceptInvitation(c)
}

func (h *OrganizationHandler) DeclineInvitationByID(c *gin.Context) {
	userID := c.GetString("userID")
	id := c.Param("id")
	var inv models.Invitation
	if err := h.service.DB.Where("id = ? AND status = ?", id, "pending").First(&inv).Error; err != nil {
		JSONError(c, CodeNotFound, "邀请不存在或已处理")
		return
	}
	var user models.User
	_ = h.service.DB.First(&user, "id = ?", userID).Error
	if strings.ToLower(user.Email) != strings.ToLower(inv.Email) {
		JSONError(c, CodeNotFound, "邀请不存在或已处理")
		return
	}
	if err := h.service.DB.Model(&inv).Update("status", "rejected").Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}
	JSONSuccess(c, gin.H{"status": "rejected"})
}

// RespondInvitation 处理邀请
func (h *OrganizationHandler) RespondInvitation(c *gin.Context) {
	orgID := c.GetString("orgID")
	userID := c.GetString("userID")
	token := c.Param("token")
	action := c.Param("action")
	if orgID == "" || userID == "" || token == "" {
		JSONError(c, CodeInvalidParameter, "参数错误")
		return
	}
	var inv models.Invitation
	if err := h.service.DB.Where("token = ? AND org_id = ?", token, orgID).First(&inv).Error; err != nil {
		JSONError(c, CodeNotFound, "邀请不存在")
		return
	}
	if time.Now().After(inv.ExpiresAt) {
		JSONError(c, CodeConflict, "邀请已过期")
		return
	}
	if action == "accept" {
		tx := h.service.DB.Begin()
		if err := tx.Model(&inv).Updates(map[string]interface{}{"status": "accepted", "accepted_at": time.Now()}).Error; err != nil {
			tx.Rollback()
			JSONError(c, CodeDatabaseError, "更新失败")
			return
		}
		mem := models.OrganizationMember{ID: utils.GenerateID(), OrganizationID: orgID, UserID: userID, Role: inv.Role, Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := tx.Create(&mem).Error; err != nil {
			tx.Rollback()
			JSONError(c, CodeDatabaseError, "加入失败")
			return
		}
		if err := tx.Commit().Error; err != nil {
			JSONError(c, CodeDatabaseError, "事务失败")
			return
		}
		JSONSuccess(c, gin.H{"status": "accepted"})
		return
	}
	if action == "decline" {
		_ = h.service.DB.Model(&inv).Update("status", "declined").Error
		JSONSuccess(c, gin.H{"status": "declined"})
		return
	}
	JSONError(c, CodeInvalidParameter, "非法操作")
}
func (h *OrganizationHandler) GetAuditActions(c *gin.Context) {
	orgID := c.GetString("orgID")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "组织信息错误")
		return
	}
	var actions []string
	var rows []struct{ ID string }
	if err := h.service.DB.Table("audit_actions").Select("id").Order("id ASC").Scan(&rows).Error; err == nil {
		for _, r := range rows {
			actions = append(actions, r.ID)
		}
	}
	if len(actions) == 0 {
		var al []struct{ Action string }
		_ = h.service.DB.Table("audit_logs").Select("DISTINCT action").Where("org_id = ?", orgID).Order("action").Scan(&al).Error
		for _, a := range al {
			actions = append(actions, a.Action)
		}
	}
	JSONSuccess(c, actions)
}
