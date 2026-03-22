package api

import (
	"fmt"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

type SwitchOrgRequest struct {
	OrgID string `json:"org_id" binding:"required"`
}

type CreateOrgRequest struct {
	Name string `json:"name" binding:"required"`
}

// @Summary 切换当前组织
// @Description 登录用户在多组织间切换当前组织上下文
// @Tags Organization
// @Accept json
// @Produce json
// @Param request body SwitchOrgRequest true "切换组织请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Router /api/v1/orgs/switch [post]
func (h *OrganizationHandler) SwitchOrganization(c *gin.Context) {
	var req SwitchOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	userID := c.GetString("userID")
	if userID == "" {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	// 校验成员资格
	var member models.OrganizationMember
	if err := h.service.DB.Where("organization_id = ? AND user_id = ? AND status = ?", req.OrgID, userID, "active").First(&member).Error; err != nil {
		JSONError(c, CodeForbidden, "非组织成员或未激活")
		return
	}
	// 更新用户当前组织
	if err := h.service.DB.Model(&models.User{}).Where("id = ?", userID).Update("current_org_id", req.OrgID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "切换失败")
		return
	}

	// 获取目标组织信息（用于生成Token）
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", req.OrgID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "获取组织信息失败")
		return
	}

	// 获取用户在目标组织的角色
	// 注意：member 已经在前面校验成员资格时获取到了

	// 获取用户基本信息
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeDatabaseError, "获取用户信息失败")
		return
	}
	// 临时更新内存对象以生成Token（不影响DB，DB已更新）
	user.CurrentOrgID = req.OrgID
	user.OrgRole = member.Role
	user.OrgID = req.OrgID // 注意：有些Token逻辑可能用OrgID字段，有些用CurrentOrgID，这里统一下

	// 生成新Token
	newToken, err := h.generateTokenForSwitch(&user, &org)
	if err != nil {
		logger.GetLogger().WithError(err).Error("生成切换Token失败")
		// 降级处理：仅返回ID，前端可能需要重新登录或容忍旧Token（不推荐）
		// 但为了健壮性，这里报错
		JSONError(c, CodeInternalError, "Token生成失败")
		return
	}

	// 获取新权限列表
	var permIDs []string
	var rows []struct{ PermissionID string }
	if err := h.service.DB.Table("role_permissions").Select("permission_id").Where("role_id = ?", member.Role).Scan(&rows).Error; err == nil {
		for _, r := range rows {
			permIDs = append(permIDs, r.PermissionID)
		}
	}

	metrics.RecordAuditEvent(c.Request.Context(), "org.switch", "organization", "success")

	JSONSuccess(c, gin.H{
		"current_org_id": req.OrgID,
		"access_token":   newToken,
		"permissions":    permIDs,
		"org_role":       member.Role,
	})
}

// 更新组织
func (h *OrganizationHandler) UpdateOrganization(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		JSONError(c, CodeInvalidParameter, "缺少组织ID")
		return
	}

	// Check if current user has permission (must be owner or have update perm, simplifying to just checking orgID matches token)
	tokenOrgID := c.GetString("orgID")
	if tokenOrgID != orgID {
		JSONError(c, CodeForbidden, "无权修改此组织")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数格式错误")
		return
	}

	if err := h.service.DB.Model(&models.Organization{}).Where("id = ?", orgID).Update("name", req.Name).Error; err != nil {
		JSONError(c, CodeDatabaseError, "更新失败")
		return
	}

	JSONSuccess(c, "更新成功")
}

// 辅助方法：生成Token (复用 ConsoleAuthHandler 逻辑)
func (h *OrganizationHandler) generateTokenForSwitch(user *models.User, org *models.Organization) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"email":    user.Email,
		"role":     user.Role,
		"org_id":   user.CurrentOrgID, // 切换后，Token应当绑定当前选中的Org
		"org_role": user.OrgRole,
		"plan_id":  org.PlanID,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.service.Config.Security.JWTSecret))
}

// 创建组织
func (h *OrganizationHandler) CreateOrganization(c *gin.Context) {
	var req CreateOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	userID := c.GetString("userID")
	org := &models.Organization{ID: utils.GenerateID(), Name: req.Name, Status: "active", OwnerID: userID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := h.service.DB.Create(org).Error; err != nil {
		JSONError(c, CodeDatabaseError, "创建失败")
		return
	}
	member := &models.OrganizationMember{ID: utils.GenerateID(), OrganizationID: org.ID, UserID: userID, Role: "owner", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = h.service.DB.Create(member).Error
	metrics.RecordAuditEvent(c.Request.Context(), "org.create", "organization", "success")
	JSONSuccess(c, gin.H{"org_id": org.ID})
}

// 注销组织
func (h *OrganizationHandler) DeleteOrganization(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	var org models.Organization
	if err := h.service.DB.Where("id = ?", id).First(&org).Error; err != nil {
		JSONError(c, CodeNotFound, "组织不存在")
		return
	}
	if org.OwnerID != userID {
		JSONError(c, CodeForbidden, "仅所有者可注销组织")
		return
	}
	var cnt int64
	_ = h.service.DB.Model(&models.OrganizationMember{}).Where("organization_id = ? AND status = ?", id, "active").Count(&cnt).Error
	if cnt > 1 {
		JSONError(c, CodeForbidden, "存在其他活跃成员，无法注销")
		return
	}
	org.Status = "deleted"
	org.UpdatedAt = time.Now()
	if err := h.service.DB.Save(&org).Error; err != nil {
		JSONError(c, CodeDatabaseError, "注销失败")
		return
	}
	_ = h.service.DB.Where("organization_id = ?", id).Delete(&models.OrganizationMember{}).Error
	metrics.RecordAuditEvent(c.Request.Context(), "org.delete", "organization", "success")
	JSONSuccess(c, gin.H{"deleted": id})
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
	h.service.LogWorker.RecordAuditLog(auditLog)

	// 记录审计日志
	auditLog = &models.AuditLog{
		UserID:    c.GetString("userID"),
		OrgID:     org.ID,
		Action:    "create_organization",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("Created organization: %s", org.Name),
	}
	h.service.LogWorker.RecordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("create_org", true, time.Since(start), "")

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
	h.service.LogWorker.RecordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("update_org_plan", true, time.Since(start), "")

	JSONSuccess(c, gin.H{
		"message": "套餐更新成功",
	})
}
