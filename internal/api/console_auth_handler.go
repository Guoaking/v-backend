package api

import (
	"context"
	"encoding/json"
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
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ConsoleAuthHandler 控制台认证处理器
type ConsoleAuthHandler struct {
	service *service.KYCService
}

// NewConsoleAuthHandler 创建控制台认证处理器
func NewConsoleAuthHandler(svc *service.KYCService) *ConsoleAuthHandler {
	return &ConsoleAuthHandler{service: svc}
}

// ConsoleLoginRequest 登录请求
type ConsoleLoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// ConsoleLoginResponse 登录响应
type ConsoleLoginResponse struct {
	AccessToken string              `json:"access_token"`
	User        *ConsoleUserProfile `json:"user"`
	//Orgs        []OrganizationLite  `json:"orgs,omitempty"`
}

// ConsoleUserProfile 用户档案
type ConsoleUserProfile struct {
	AccessToken     string              `json:"access_token,omitempty"`
	ID              string              `json:"id"`
	Email           string              `json:"email"`
	FullName        string              `json:"full_name"`
	AvatarURL       string              `json:"avatar,omitempty"`
	Role            string              `json:"role"`
	OrgRole         string              `json:"org_role"`
	OrgID           string              `json:"org_id"`
	LastActiveOrgID string              `json:"last_active_org_id"`
	PlanID          string              `json:"plan_id"`
	Status          string              `json:"status"`
	Organization    models.Organization `json:"organization,omitempty"`
	Orgs            []OrganizationLite  `json:"orgs,omitempty"`
	Permissions     []string            `json:"permissions,omitempty"`
}

type OrganizationLite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConsoleRegisterRequest 注册请求
type ConsoleRegisterRequest struct {
	FullName string `json:"full_name" binding:"required,min=2"`
	Email    string `json:"email" binding:"required,email"`
	Company  string `json:"company" binding:"required,min=2"`
	Password string `json:"password" binding:"required,min=6"`
	Avatar   string `json:"avatar"`
}

// ConsoleRegisterResponse 注册响应
type ConsoleRegisterResponse struct {
	AccessToken string              `json:"access_token,omitempty"`
	User        *ConsoleUserProfile `json:"user,omitempty"`
	Message     string              `json:"message"`
}

// Login 用户登录
// @Summary 用户登录
// @Description 用户使用邮箱和密码登录控制台
// @Tags Console Auth
// @Accept json
// @Produce json
// @Param request body ConsoleLoginRequest true "登录请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/v1/auth/login [post]
func (h *ConsoleAuthHandler) Login(c *gin.Context) { // ignore_security_alert
	start := time.Now()

	var req ConsoleLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		metrics.RecordBusinessOperation(c.Request.Context(), "console_login", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		Action:    "login_attempt",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "pending",
		Message:   fmt.Sprintf("Login attempt for email: %s", req.Email),
	}

	// 查找用户
	var user models.User
	if err := h.service.DB.Where("email = ? AND status = ?", req.Email, "active").First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			auditLog.Status = "failed"
			auditLog.Message = "User not found or inactive"
			h.service.LogWorker.RecordAuditLog(auditLog)
			metrics.RecordBusinessOperation(c.Request.Context(), "console_login", false, time.Since(start), "user_not_found")
			JSONError(c, CodeUnauthorized, "邮箱或密码错误")
			return
		}
		logger.GetLogger().WithError(err).Error("查询用户失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "console_login", false, time.Since(start), "database_error")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		auditLog.UserID = user.ID
		auditLog.Status = "failed"
		auditLog.Message = "Invalid password"
		h.service.LogWorker.RecordAuditLog(auditLog)
		metrics.RecordBusinessOperation(c.Request.Context(), "console_login", false, time.Since(start), "invalid_password")
		JSONError(c, CodeUnauthorized, "邮箱或密码错误")
		return
	}

	// 延后到解析组织上下文与角色后再生成Token

	// 更新最后登录时间
	now := time.Now()
	user.LastLoginAt = &now
	// 设置当前组织上下文
	if user.CurrentOrgID == "" {
		user.CurrentOrgID = user.OrgID
	}
	if err := h.service.DB.Save(&user).Error; err != nil {
		logger.GetLogger().WithError(err).Error("更新登录时间失败")
	}

	// 记录成功的审计日志
	auditLog.UserID = user.ID
	auditLog.OrgID = user.OrgID
	auditLog.Status = "success"
	auditLog.Message = "Login successful"
	h.service.LogWorker.RecordAuditLog(auditLog)

	// 记录业务操作成功
	metrics.RecordBusinessOperation(c.Request.Context(), "console_login", true, time.Since(start), "")

	roleToUse := user.OrgRole
	orgIDToUse := user.CurrentOrgID
	if orgIDToUse == "" {
		orgIDToUse = user.OrgID
	}
	var member models.OrganizationMember
	if err := h.service.DB.Where("organization_id = ? AND user_id = ?", orgIDToUse, user.ID).First(&member).Error; err == nil && member.Role != "" {
		roleToUse = member.Role
	}
	var permIDs []string
	var rows []struct{ PermissionID string }
	if err := h.service.DB.Table("role_permissions").Select("permission_id").Where("role_id = ?", roleToUse).Scan(&rows).Error; err == nil {
		for _, r := range rows {
			permIDs = append(permIDs, r.PermissionID)
		}
	}

	// 获取组织信息（以选定的 orgID 为准）
	var org models.Organization
	if err := h.service.DB.First(&org, "id = ?", orgIDToUse).Error; err != nil {
		logger.GetLogger().WithError(err).Error("查询组织失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "console_login", false, time.Since(start), "org_not_found")
		JSONError(c, CodeInternalError, "组织信息错误")
		return
	}
	// 临时设置用户上下文用于生成Token
	user.CurrentOrgID = orgIDToUse
	user.OrgRole = roleToUse
	user.OrgID = orgIDToUse
	// 生成JWT令牌（绑定当前选定组织）
	accessToken, err := h.generateUserJWT(&user, &org, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		logger.GetLogger().WithError(err).Error("生成JWT失败")
		metrics.RecordBusinessOperation(c.Request.Context(), "console_login", false, time.Since(start), "jwt_generation_failed")
		JSONError(c, CodeInternalError, "令牌生成失败")
		return
	}

	// 返回用户信息
	// 更新活跃组织
	user.LastActiveOrgID = orgIDToUse
	_ = h.service.DB.Model(&models.User{}).Where("id = ?", user.ID).Update("last_active_org_id", orgIDToUse).Error

	var orgsOut []OrganizationLite
	var memberships []models.OrganizationMember
	_ = h.service.DB.Where("user_id = ?", user.ID).Find(&memberships).Error

	if len(memberships) > 0 {
		var ids []string
		for _, m := range memberships {
			ids = append(ids, m.OrganizationID)
		}
		var orgs []models.Organization
		_ = h.service.DB.Where("id IN ?", ids).Find(&orgs).Error
		for _, o := range orgs {
			orgsOut = append(orgsOut, OrganizationLite{ID: o.ID, Name: o.Name})
		}
	}

	userProfile := &ConsoleUserProfile{
		AccessToken:     accessToken,
		ID:              user.ID,
		Email:           user.Email,
		FullName:        user.Name,
		AvatarURL:       user.AvatarURL,
		Role:            user.Role,
		OrgRole:         roleToUse,
		OrgID:           orgIDToUse,
		LastActiveOrgID: user.LastActiveOrgID,
		PlanID:          org.PlanID,
		Status:          user.Status,
		Organization:    org,
		Orgs:            orgsOut,
		Permissions:     permIDs,
	}

	//JSONSuccess(c, userProfile)
	JSONSuccess(c, ConsoleLoginResponse{AccessToken: accessToken, User: userProfile})
}

func (h *ConsoleAuthHandler) Me(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	var user models.User
	if err := h.service.DB.First(&user, "id = ?", userID).Error; err != nil {
		JSONError(c, CodeNotFound, "用户不存在")
		return
	}
	var org models.Organization
	orgIDToUse := user.CurrentOrgID
	if orgIDToUse == "" {
		orgIDToUse = user.OrgID
	}
	_ = h.service.DB.First(&org, "id = ?", orgIDToUse).Error
	roleToUse := user.OrgRole
	var member models.OrganizationMember
	if err := h.service.DB.Where("organization_id = ? AND user_id = ?", orgIDToUse, user.ID).First(&member).Error; err == nil && member.Role != "" {
		roleToUse = member.Role
	}
	var permIDs []string
	var rows []struct{ PermissionID string }
	if err := h.service.DB.Table("role_permissions").Select("permission_id").Where("role_id = ?", roleToUse).Scan(&rows).Error; err == nil {
		for _, r := range rows {
			permIDs = append(permIDs, r.PermissionID)
		}
	}
	resp := &ConsoleUserProfile{
		ID:              user.ID,
		Email:           user.Email,
		FullName:        user.Name,
		AvatarURL:       user.AvatarURL,
		Role:            user.Role,
		OrgRole:         roleToUse,
		OrgID:           orgIDToUse,
		LastActiveOrgID: user.LastActiveOrgID,
		PlanID:          org.PlanID,
		Status:          user.Status,
		Permissions:     permIDs,
	}
	JSONSuccess(c, resp)
}

// SandboxTokenRequest 沙盒Token请求
type SandboxTokenRequest struct {
	FeatureID string `json:"feature_id"` // 可选，用于细粒度审计
}

// SandboxTokenResponse 沙盒Token响应
type SandboxTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // 单位：秒
}

// GenerateSandboxToken 为 Playground 生成短期测试 JWT (STS 机制)
func (h *ConsoleAuthHandler) GenerateSandboxToken(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		JSONError(c, CodeUnauthorized, "未授权访问")
		return
	}
	claims := userClaims.(jwt.MapClaims)
	userID := claims["user_id"].(string)
	orgID := c.GetString("orgID") // 依赖于前面的 InjectOrgContext 中间件
	if orgID == "" {
		// 降级尝试从 User JWT 中提取
		if v, ok := claims["org_id"].(string); ok && v != "" {
			orgID = v
		} else {
			JSONError(c, CodeUnauthorized, "无法确定所属组织")
			return
		}
	}

	// 查找系统内置客户端
	var sysClient models.OAuthClient
	if err := h.service.DB.Where("id = ?", "sys_web_console_playground").First(&sysClient).Error; err != nil {
		logger.GetLogger().Errorf("无法找到系统内置客户端 sys_web_console_playground: %v", err)
		JSONError(c, CodeInternalError, "系统配置错误，无法生成测试令牌")
		return
	}

	// 生成包含全部测试权限的短期 JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": sysClient.ID,
		"org_id":    orgID,
		"user_id":   userID,
		"scope":     sysClient.Scopes, // 赋予全量测试权限
		"source":    "playground",
		"exp":       time.Now().Add(time.Duration(sysClient.TokenTTLSeconds) * time.Second).Unix(),
		"iat":       time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(h.service.Config.Security.JWTSecret))
	if err != nil {
		logger.GetLogger().Errorf("签名 Sandbox JWT 失败: %v", err)
		JSONError(c, CodeInternalError, "令牌生成失败")
		return
	}

	h.service.RecordAuditLog(c, "playground.token.generate", "oauth_client", sysClient.ID, "success", "")

	JSONSuccess(c, SandboxTokenResponse{
		Token:     tokenString,
		ExpiresIn: sysClient.TokenTTLSeconds,
	})
}

// Register 用户注册
// @Summary 用户注册
// @Description 新用户注册并创建组织
// @Tags Console Auth
// @Accept json
// @Produce json
// @Param request body ConsoleRegisterRequest true "注册请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /api/v1/auth/register [post]
func (h *ConsoleAuthHandler) Register(c *gin.Context) { // ignore_security_alert
	start := time.Now()

	var req ConsoleRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "invalid_request")
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}

	var capVal string
	_ = h.service.DB.Raw("SELECT value FROM global_configs WHERE key = 'daily_registration_cap'").Scan(&capVal).Error
	capNum := 1000
	if capVal != "" {
		fmt.Sscanf(capVal, "%d", &capNum)
	}
	var todayCount int64
	_ = h.service.DB.Model(&models.User{}).Where("created_at >= date_trunc('day', now())").Count(&todayCount).Error
	if int(todayCount) >= capNum {
		JSONError(c, CodeForbidden, "Daily registration limit reached")
		return
	}

	// 开始数据库事务
	tx := h.service.DB.Begin()
	if tx.Error != nil {
		logger.GetLogger().WithError(tx.Error).Error("开启事务失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "transaction_failed")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// 检查邮箱是否已存在
	var existingUser models.User
	if err := tx.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		tx.Rollback()
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "email_exists")
		JSONError(c, CodeConflict, "邮箱已被注册")
		return
	}

	// 创建组织
	org := models.Organization{
		ID:           utils.GenerateID(),
		Name:         req.Company,
		PlanID:       "starter",
		BillingEmail: req.Email,
		Status:       "active",
	}

	if err := tx.Create(&org).Error; err != nil {
		tx.Rollback()
		logger.GetLogger().WithError(err).Error("创建组织失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "org_creation_failed")
		JSONError(c, CodeDatabaseError, "组织创建失败")
		return
	}

	var raw string
	if err := tx.Raw("SELECT quota_config::text FROM plans WHERE id = ?", org.PlanID).Scan(&raw).Error; err != nil {
		JSONError(c, CodeDatabaseError, "组织创建失败")
		return
	}
	if raw == "" {
		JSONError(c, CodeDatabaseError, "组织创建失败")
		return
	}

	var m map[string]map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &m)
	for svc, v := range m {
		alloc := 0
		if l, ok := v["limit"].(float64); ok {
			alloc = int(l)
		}
		var reset interface{}
		if p, ok := v["period"].(string); ok && p == "monthly" {
			nm := time.Date(time.Now().Year(), time.Now().Month()+1, 1, 0, 0, 0, 0, time.Now().Location())
			reset = nm
		} else {
			reset = nil
		}

		tx.Exec("INSERT INTO organization_quotas(id, organization_id, service_type, allocation, consumed, reset_at, updated_at) VALUES(?, ?, ?, ?, 0, ?, NOW()) ON CONFLICT (organization_id, service_type) DO UPDATE SET allocation = EXCLUDED.allocation, consumed = LEAST(organization_quotas.consumed, EXCLUDED.allocation), reset_at = EXCLUDED.reset_at, updated_at = NOW()", utils.GenerateID(), org.ID, svc, alloc, reset)
		metrics.SetOrgQuotaLimit(context.Background(), org.ID, svc, alloc)
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		tx.Rollback()
		logger.GetLogger().WithError(err).Error("密码加密失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "password_hash_failed")
		JSONError(c, CodeInternalError, "密码处理失败")
		return
	}

	// 创建用户
	user := models.User{
		ID:       utils.GenerateID(),
		Email:    req.Email,
		Password: string(hashedPassword),
		Name:     req.FullName,
		FullName: req.FullName,
		//AvatarURL: fmt.Sprintf("https://api.dicebear.com/7.x/avataaars/svg?seed=%s", req.Email),
		AvatarURL: req.Avatar,
		Role:      "user",
		OrgID:     org.ID,
		OrgRole:   "owner",
		Status:    "active",
	}

	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		logger.GetLogger().WithError(err).Error("创建用户失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "user_creation_failed")
		JSONError(c, CodeDatabaseError, "用户创建失败")
		return
	}

	// 创建组织成员关系（owner）
	member := models.OrganizationMember{
		ID:             utils.GenerateID(),
		OrganizationID: org.ID,
		UserID:         user.ID,
		Role:           "owner",
		Status:         "active",
	}
	if err := tx.Create(&member).Error; err != nil {
		tx.Rollback()
		logger.GetLogger().WithError(err).Error("创建组织成员关系失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "org_member_creation_failed")
		JSONError(c, CodeDatabaseError, "组织成员创建失败")
		return
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		logger.GetLogger().WithError(err).Error("提交事务失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "transaction_commit_failed")
		JSONError(c, CodeDatabaseError, "系统错误")
		return
	}

	// 记录审计日志
	auditLog := &models.AuditLog{
		UserID:    user.ID,
		OrgID:     org.ID,
		Action:    "user_registered",
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    "success",
		Message:   fmt.Sprintf("User registered: %s", req.Email),
	}
	h.service.LogWorker.RecordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("console_register", true, time.Since(start), "")

	// 生成 JWT
	accessToken, err := h.generateUserJWT(&user, &org, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		logger.GetLogger().WithError(err).Error("生成JWT失败")
		JSONSuccess(c, ConsoleRegisterResponse{Message: "注册成功，但令牌生成失败，请手动登录"})
		return
	}

	// 构造用户档案用于自动登录
	userProfile := &ConsoleUserProfile{
		AccessToken:     accessToken,
		ID:              user.ID,
		Email:           user.Email,
		FullName:        user.Name,
		AvatarURL:       user.AvatarURL,
		Role:            user.Role,
		OrgRole:         user.OrgRole,
		OrgID:           org.ID,
		LastActiveOrgID: org.ID,
		PlanID:          org.PlanID,
		Status:          user.Status,
		Organization:    org,
		Orgs:            []OrganizationLite{{ID: org.ID, Name: org.Name}},
		Permissions:     []string{"org.read", "org.update", "team.read", "team.invite", "team.write", "billing.read", "logs.read", "org.usage.read", "org.audit"}, // Owner 初始权限
	}

	JSONSuccess(c, ConsoleRegisterResponse{
		AccessToken: accessToken,
		User:        userProfile,
		Message:     "注册成功",
	})
}

func (s *ConsoleAuthHandler) SyncOrganizationQuotasWithPolicy(orgID string, planID string, resetUsage bool) error {

	return nil
}

// generateUserJWT 生成用户JWT令牌并记录Session
func (h *ConsoleAuthHandler) generateUserJWT(user *models.User, org *models.Organization, userAgent string, ip string) (string, error) {
	jti := utils.GenerateID()

	claims := jwt.MapClaims{
		"jti":      jti,
		"user_id":  user.ID,
		"email":    user.Email,
		"role":     user.Role,
		"org_id":   user.OrgID,
		"org_role": user.OrgRole,
		"plan_id":  org.PlanID,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}

	// 将会话信息存入 Redis (如果配置了 Redis)
	if h.service.Redis != nil {
		sessionData := map[string]interface{}{
			"id":         jti,
			"user_id":    user.ID,
			"user_agent": userAgent,
			"ip":         ip,
			"created_at": time.Now().Unix(),
			"last_seen":  time.Now().Unix(),
		}
		sessionJSON, _ := json.Marshal(sessionData)
		sessionKey := fmt.Sprintf("session:%s:%s", user.ID, jti)
		h.service.Redis.Set(context.Background(), sessionKey, sessionJSON, 24*time.Hour)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.service.Config.Security.JWTSecret))
}

// recordAuditLog 记录审计日志
func (h *ConsoleAuthHandler) recordAuditLog(log *models.AuditLog) {
	h.service.LogWorker.RecordAuditLog(log)
}
