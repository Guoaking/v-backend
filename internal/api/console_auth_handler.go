package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/crypto"
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
	Company         string              `json:"company,omitempty"`
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
	Success bool   `json:"success"`
	Message string `json:"message"`
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
			h.recordAuditLog(auditLog)
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
		h.recordAuditLog(auditLog)
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
	h.recordAuditLog(auditLog)

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
	accessToken, err := h.generateUserJWT(&user, &org)
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
	if len(memberships) > 1 {
		var orgs []models.Organization
		var ids []string
		for _, m := range memberships {
			ids = append(ids, m.OrganizationID)
		}
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
		Company:         org.Name,
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

	// 创建默认API密钥
	_, err = h.createDefaultAPIKey(tx, &user, &org)
	if err != nil {
		tx.Rollback()
		logger.GetLogger().WithError(err).Error("创建默认API密钥失败")
		middleware.RecordBusinessOperation("console_register", false, time.Since(start), "api_key_creation_failed")
		JSONError(c, CodeConflict, "API密钥创建失败")
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
	h.recordAuditLog(auditLog)

	// 记录业务操作成功
	middleware.RecordBusinessOperation("console_register", true, time.Since(start), "")

	JSONSuccess(c, ConsoleRegisterResponse{Success: true, Message: "注册成功，请登录"})
}

func (s *ConsoleAuthHandler) SyncOrganizationQuotasWithPolicy(orgID string, planID string, resetUsage bool) error {

	return nil
}

// generateUserJWT 生成用户JWT令牌
func (h *ConsoleAuthHandler) generateUserJWT(user *models.User, org *models.Organization) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"email":    user.Email,
		"role":     user.Role,
		"org_id":   user.OrgID,
		"org_role": user.OrgRole,
		"plan_id":  org.PlanID,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.service.Config.Security.JWTSecret))
}

// createDefaultAPIKey 创建默认API密钥
func (h *ConsoleAuthHandler) createDefaultAPIKey(tx *gorm.DB, user *models.User, org *models.Organization) (*models.APIKey, error) {
	// 生成密钥
	secretKey := h.generateAPIKeySecret()
	// 计算前缀
	prefix := ""
	if idx := strings.Index(secretKey, "_"); idx != -1 {
		if j := strings.Index(secretKey[idx+1:], "_"); j != -1 {
			prefix = secretKey[:idx+1+j+1]
			if k := strings.LastIndex(prefix, "_"); k != -1 {
				prefix = prefix[:k]
			}
		}
	}

	// 哈希密钥用于存储
	secretHash, err := crypto.HashString(secretKey)
	if err != nil {
		return nil, err
	}

	encSecret := ""
	if h.service.Encryptor != nil {
		if es, err := h.service.Encryptor.Encrypt(secretKey); err == nil {
			encSecret = es
		} else {
			logger.GetLogger().WithError(err).Warn("加密API密钥失败，改为不保存明文密钥")
		}
	} else {
		logger.GetLogger().Warn("未配置加密密钥（EncryptionKey），不保存明文密钥副本")
	}

	apiKey := models.APIKey{
		ID:              utils.GenerateID(),
		UserID:          user.ID,
		OrgID:           org.ID,
		Name:            "Default Key",
		SecretHash:      secretHash,
		SecretEnc:       encSecret,
		Prefix:          prefix,
		Scopes:          `["ocr:read", "face:read", "liveness:read"]`,
		Status:          "active",
		CreatedByUserID: user.ID,
	}

	if err := tx.Create(&apiKey).Error; err != nil {
		return nil, err
	}

	return &apiKey, nil
}

// generateAPIKeySecret 生成API密钥
func (h *ConsoleAuthHandler) generateAPIKeySecret() string {
	// 生成32字节的随机数据
	bytes := make([]byte, 32)
	rand.Read(bytes)

	// 编码为base64
	encoded := base64.URLEncoding.EncodeToString(bytes)

	// 添加前缀并返回
	return "sk_live_" + strings.ToLower(encoded)
}

// recordAuditLog 记录审计日志
func (h *ConsoleAuthHandler) recordAuditLog(log *models.AuditLog) {
	if err := h.service.DB.Create(log).Error; err != nil {
		logger.GetLogger().WithError(err).Error("记录审计日志失败")
	}
}
