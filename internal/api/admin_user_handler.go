package api

import (
	"encoding/json"
	"fmt"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
)

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
	al := &models.AuditLog{UserID: c.GetString("userID"), OrgID: user.OrgID, Action: "admin.user_update", Resource: "users", Details: datatypes.JSON(b), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Status: "success", Message: "Admin updated user"}
	_ = h.service.DB.Create(al).Error
	JSONSuccess(c, gin.H{"updated": userID})
}
