package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pq "github.com/lib/pq"
)

type ClientHandler struct {
	service *service.KYCService
}

func NewClientHandler(svc *service.KYCService) *ClientHandler {
	return &ClientHandler{service: svc}
}

// ClientRegistrationRequest 客户端注册请求
type ClientRegistrationRequest struct {
	Name            string   `json:"name" binding:"required"`
	Description     string   `json:"description"`
	RedirectURI     string   `json:"redirect_uri"`
	Scopes          string   `json:"scopes" binding:"required"`
	TokenTTLSeconds int      `json:"token_ttl_seconds"`
	OwnerID         string   `json:"owner_id"`
	IPWhitelist     []string `json:"ip_whitelist"`
	RateLimitPerSec int      `json:"rate_limit_per_sec"`
}

// ClientRegistrationResponse 客户端注册响应
type ClientRegistrationResponse struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	RedirectURI  string    `json:"redirect_uri"`
	Scopes       string    `json:"scopes"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	OwnerID      string    `json:"owner_id"`
}

// ClientOwnerInfo 客户端负责人信息
type ClientOwnerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar"`
}

// ClientListResponse 客户端列表响应
type ClientListResponse struct {
	ID          string           `json:"client_id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	RedirectURI string           `json:"redirect_uri"`
	Scopes      string           `json:"scopes"`
	Status      string           `json:"status"`
	CreatedAt   time.Time        `json:"created_at"`
	Owner       *ClientOwnerInfo `json:"owner,omitempty"`
}

// RegisterClient 注册OAuth客户端
// @Summary 注册OAuth客户端
// @Description 注册新的OAuth客户端，获取client_id和client_secret
// @Tags Client Management
// @Accept json
// @Produce json
// @Param request body ClientRegistrationRequest true "Client registration request"
// @Success 201 {object} ClientRegistrationResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /clients/register [post]
func (h *ClientHandler) RegisterClient(c *gin.Context) {
	var req ClientRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}

	// 组织策略校验
	orgID := c.GetString("orgID")
	policy := h.service.GetOrgPolicy(orgID)
	scopesArr := func(s string) []string {
		ss := strings.Fields(strings.TrimSpace(s))
		out := make([]string, 0, len(ss))
		for _, x := range ss {
			if x != "" {
				out = append(out, x)
			}
		}
		return out
	}(req.Scopes)
	if !h.service.ValidateScopesSubset(policy.AllowedScopes, scopesArr) {
		JSONError(c, CodeForbidden, "Scopes not allowed by organization policy")
		return
	}
	if req.RateLimitPerSec <= 0 || req.RateLimitPerSec > policy.MaxRatePerSec {
		req.RateLimitPerSec = policy.MaxRatePerSec
	}
	if !h.service.ValidateIPWhitelistSubset(policy.IPWhitelist, req.IPWhitelist) {
		JSONError(c, CodeForbidden, "IP whitelist not allowed by organization policy")
		return
	}
	if req.TokenTTLSeconds <= 0 || req.TokenTTLSeconds > policy.MaxTokenTTLSec {
		req.TokenTTLSeconds = policy.MaxTokenTTLSec
	}

	// 生成客户端凭证
	clientID := uuid.New().String()
	clientSecret := uuid.New().String()

	// 创建客户端记录
	ownerID := c.GetString("userID")
	if req.OwnerID != "" && req.OwnerID != ownerID {
		role := c.GetString("orgRole")
		if role != "owner" && role != "admin" {
			JSONError(c, CodeForbidden, "Insufficient permissions to assign owner")
			return
		}
		var member models.OrganizationMember
		if err := h.service.DB.Where("organization_id = ? AND user_id = ? AND status = ?", orgID, req.OwnerID, "active").First(&member).Error; err != nil {
			JSONError(c, CodeForbidden, "Owner must be an active organization member")
			return
		}
		ownerID = req.OwnerID
	}
	client := &models.OAuthClient{
		ID:          clientID,
		Secret:      clientSecret,
		Name:        req.Name,
		Description: req.Description,
		RedirectURI: req.RedirectURI,
		Scopes:      req.Scopes,
		//Status:          map[bool]string{true: "pending", false: "active"}[policy.RequireApproval],
		Status:          map[bool]string{true: "active", false: "active"}[policy.RequireApproval],
		OrgID:           orgID,
		TokenTTLSeconds: req.TokenTTLSeconds,
		OwnerID:         ownerID,
		IPWhitelist:     pq.StringArray(req.IPWhitelist),
		RateLimitPerSec: req.RateLimitPerSec,
	}

	if err := h.service.DB.Create(client).Error; err != nil {
		logger.GetLogger().WithError(err).Error("create client failed")
		JSONError(c, CodeInternalError, "Failed to create client")
		return
	}

	// 记录客户端注册审计日志
	h.service.RecordAuditLog(c, "client.register", "client", clientID, "success", "client registered")

	JSONSuccessWithStatus(c, http.StatusCreated, ClientRegistrationResponse{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Name:         req.Name,
		Description:  req.Description,
		RedirectURI:  req.RedirectURI,
		Scopes:       req.Scopes,
		Status:       client.Status,
		CreatedAt:    client.CreatedAt,
		OwnerID:      client.OwnerID,
	})
}

// ListClients
// @Summary List OAuth clients
// @Description List OAuth clients in current organization
// @Tags Client Management
// @Accept json
// @Produce json
// @Success 200 {array} ClientListResponse
// @Failure 500 {object} map[string]string
// @Router /clients [get]
func (h *ClientHandler) ListClients(c *gin.Context) {
	orgID := c.GetString("orgID")
	page := 1
	pageSize := 20
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	search := strings.TrimSpace(c.Query("q"))

	// Hide system clients and apply role-based filtering
	q := h.service.DB.Table("oauth_clients").
		Select("oauth_clients.*, users.id as user_id, users.full_name as user_name, users.email as user_email, users.avatar_url as user_avatar").
		Joins("LEFT JOIN users ON users.id = oauth_clients.owner_id").
		Where("oauth_clients.status = ? AND oauth_clients.is_system = ?", "active", false)

	if orgID != "" {
		q = q.Where("oauth_clients.org_id = ?", orgID)
	}

	// Check dynamic permissions
	perms, _ := c.Get("permissions")
	permList, _ := perms.([]string)
	canRead := false
	for _, p := range permList {
		if p == "oauth.read" || p == "*" {
			canRead = true
			break
		}
	}

	if !canRead {
		JSONSuccess(c, []ClientListResponse{})
		return
	}

	role := c.GetString("orgRole")
	if role == "editor" || role == "developer" {
		// Editors can only see their own clients
		userID := c.GetString("userID")
		q = q.Where("oauth_clients.owner_id = ?", userID)
	}

	if search != "" {
		q = q.Where("oauth_clients.name ILIKE ? OR oauth_clients.redirect_uri ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// Define a custom struct to scan JOIN results
	type ClientWithUser struct {
		models.OAuthClient
		UserID     string
		UserName   string
		UserEmail  string
		UserAvatar string
	}

	var results []ClientWithUser
	if err := q.Order("oauth_clients.created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&results).Error; err != nil {
		logger.GetLogger().WithError(err).Error("list clients failed")
		JSONError(c, CodeInternalError, "Failed to list clients")
		return
	}

	response := make([]ClientListResponse, len(results))
	for i, r := range results {
		response[i] = ClientListResponse{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			RedirectURI: r.RedirectURI,
			Scopes:      r.Scopes,
			Status:      r.Status,
			CreatedAt:   r.CreatedAt,
		}
		if r.UserID != "" {
			response[i].Owner = &ClientOwnerInfo{
				ID:        r.UserID,
				Name:      r.UserName,
				Email:     r.UserEmail,
				AvatarURL: r.UserAvatar,
			}
		}
	}

	JSONSuccess(c, response)
}

// DeleteClient
// @Summary Delete OAuth client
// @Description Delete specified OAuth client
// @Tags Client Management
// @Accept json
// @Produce json
// @Param client_id path string true "Client ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /clients/{client_id} [delete]
func (h *ClientHandler) DeleteClient(c *gin.Context) {
	clientID := c.Param("client_id")
	if clientID == "" {
		JSONError(c, CodeInvalidParameter, "Client ID must not be empty")
		return
	}

	// 删除客户端
	orgID := c.GetString("orgID")
	q := h.service.DB.Where("id = ? AND is_system = ?", clientID, false)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}

	// RBAC Check: Developer/Editor can only delete their own clients
	role := c.GetString("orgRole")
	if role == "editor" || role == "developer" {
		userID := c.GetString("userID")
		q = q.Where("owner_id = ?", userID)
	}

	result := q.Delete(&models.OAuthClient{})
	if result.Error != nil {
		logger.GetLogger().WithError(result.Error).Error("delete client failed")
		JSONError(c, CodeInternalError, "Failed to delete client")
		return
	}
	if result.RowsAffected == 0 {
		JSONError(c, CodeNotFound, "Client not found or you don't have permission to delete it")
		return
	}

	// 记录删除审计日志
	h.service.RecordAuditLog(c, "client.delete", "client", clientID, "success", "client deleted")
	JSONSuccess(c, gin.H{"message": "Client deleted"})
}

type RotateSecretResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type TransferClientRequest struct {
	NewOwnerID string `json:"new_owner_id" binding:"required"`
}

func (h *ClientHandler) TransferClientOwnership(c *gin.Context) {
	clientID := c.Param("id")
	if clientID == "" {
		JSONError(c, CodeInvalidParameter, "Client ID must not be empty")
		return
	}

	var req TransferClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}

	orgID := c.GetString("orgID")

	// Only Admin or Owner can transfer clients
	role := c.GetString("orgRole")
	if role != "admin" && role != "owner" {
		JSONError(c, CodeForbidden, "Only administrators can transfer client ownership")
		return
	}

	// 1. Verify that the new owner is an active member of this organization
	var member models.OrganizationMember
	if err := h.service.DB.Where("organization_id = ? AND user_id = ? AND status = ?", orgID, req.NewOwnerID, "active").First(&member).Error; err != nil {
		JSONError(c, CodeInvalidParameter, "The new owner must be an active member of this organization")
		return
	}

	// 2. Perform the transfer
	result := h.service.DB.Model(&models.OAuthClient{}).
		Where("id = ? AND org_id = ? AND is_system = ?", clientID, orgID, false).
		Update("owner_id", req.NewOwnerID)

	if result.Error != nil {
		logger.GetLogger().WithError(result.Error).Error("transfer client failed")
		JSONError(c, CodeInternalError, "Failed to transfer client ownership")
		return
	}

	if result.RowsAffected == 0 {
		JSONError(c, CodeNotFound, "Client not found in this organization")
		return
	}

	h.service.RecordAuditLog(c, "client.transfer", "client", clientID, "success", fmt.Sprintf("transferred to user %s", req.NewOwnerID))
	JSONSuccess(c, gin.H{"message": "Client ownership transferred successfully"})
}

// RotateClientSecret
// @Summary Rotate OAuth client secret
// @Description Generate a new client secret and optionally invalidate the old one immediately
// @Tags Client Management
// @Accept json
// @Produce json
// @Param id path string true "Client ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /clients/{id}/rotate [post]
func (h *ClientHandler) RotateClientSecret(c *gin.Context) {
	clientID := c.Param("id")
	if clientID == "" {
		JSONError(c, CodeInvalidParameter, "Client ID must not be empty")
		return
	}

	orgID := c.GetString("orgID")
	q := h.service.DB.Model(&models.OAuthClient{}).Where("id = ? AND is_system = ?", clientID, false)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}

	// RBAC Check: Developer/Editor can only rotate their own clients
	role := c.GetString("orgRole")
	if role == "editor" || role == "developer" {
		userID := c.GetString("userID")
		q = q.Where("owner_id = ?", userID)
	}

	// check if client exists
	var client models.OAuthClient
	if err := q.First(&client).Error; err != nil {
		JSONError(c, CodeNotFound, "Client not found or you don't have permission to modify it")
		return
	}

	newSecret := uuid.New().String()
	if err := h.service.DB.Model(&client).Update("secret", newSecret).Error; err != nil {
		logger.GetLogger().WithError(err).Error("rotate client secret failed")
		JSONError(c, CodeInternalError, "Failed to rotate client secret")
		return
	}

	h.service.RecordAuditLog(c, "client.rotate_secret", "client", clientID, "success", "client secret rotated")
	JSONSuccess(c, gin.H{
		"client_id":     clientID,
		"client_secret": newSecret,
		"message":       "Client secret rotated successfully",
	})
}

type UpdateClientRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RedirectURI string   `json:"redirect_uri"`
	Scopes      string   `json:"scopes"`
	IPWhitelist []string `json:"ip_whitelist"`
}

func (h *ClientHandler) UpdateClient(c *gin.Context) {
	clientID := c.Param("id")
	if clientID == "" {
		JSONError(c, CodeInvalidParameter, "Client ID must not be empty")
		return
	}

	var req UpdateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}

	orgID := c.GetString("orgID")
	q := h.service.DB.Model(&models.OAuthClient{}).Where("id = ? AND is_system = ?", clientID, false)

	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}

	// RBAC Check: Developer/Editor can only update their own clients
	role := c.GetString("orgRole")
	if role == "editor" || role == "developer" {
		userID := c.GetString("userID")
		q = q.Where("owner_id = ?", userID)
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.RedirectURI != "" {
		updates["redirect_uri"] = req.RedirectURI
	}
	if req.Scopes != "" {
		// Verify scopes against org policy
		policy := h.service.GetOrgPolicy(orgID)
		scopesArr := strings.Fields(strings.TrimSpace(req.Scopes))
		if !h.service.ValidateScopesSubset(policy.AllowedScopes, scopesArr) {
			JSONError(c, CodeForbidden, "Scopes not allowed by organization policy")
			return
		}
		updates["scopes"] = req.Scopes
	}
	if req.IPWhitelist != nil {
		policy := h.service.GetOrgPolicy(orgID)
		if !h.service.ValidateIPWhitelistSubset(policy.IPWhitelist, req.IPWhitelist) {
			JSONError(c, CodeForbidden, "IP whitelist not allowed by organization policy")
			return
		}
		updates["ip_whitelist"] = pq.StringArray(req.IPWhitelist)
	}

	if len(updates) == 0 {
		JSONSuccess(c, gin.H{"message": "no changes provided"})
		return
	}

	result := q.Updates(updates)
	if result.Error != nil {
		logger.GetLogger().WithError(result.Error).Error("update client failed")
		JSONError(c, CodeInternalError, "Failed to update client")
		return
	}
	if result.RowsAffected == 0 {
		JSONError(c, CodeNotFound, "Client not found or you don't have permission to update it")
		return
	}

	h.service.RecordAuditLog(c, "client.update", "client", clientID, "success", "client metadata updated")
	JSONSuccess(c, gin.H{"message": "Client updated successfully"})
}

type UpdateStatusRequest struct {
	Status string `json:"status"`
}

func (h *ClientHandler) UpdateClientStatus(c *gin.Context) {
	clientID := c.Param("id")
	if clientID == "" {
		JSONError(c, CodeInvalidParameter, "Client ID must not be empty")
		return
	}

	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "Invalid request body")
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		JSONError(c, CodeInvalidParameter, "Status must be active or disabled")
		return
	}

	orgID := c.GetString("orgID")
	q := h.service.DB.Model(&models.OAuthClient{}).Where("id = ? AND is_system = ?", clientID, false)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}

	// RBAC Check: Developer/Editor can only update their own clients
	role := c.GetString("orgRole")
	if role == "editor" || role == "developer" {
		userID := c.GetString("userID")
		q = q.Where("owner_id = ?", userID)
	}

	result := q.Update("status", req.Status)
	if result.Error != nil {
		logger.GetLogger().WithError(result.Error).Error("update client status failed")
		JSONError(c, CodeInternalError, "Failed to update client status")
		return
	}
	if result.RowsAffected == 0 {
		JSONError(c, CodeNotFound, "Client not found or you don't have permission to modify it")
		return
	}

	h.service.RecordAuditLog(c, "client.update_status", "client", clientID, "success", fmt.Sprintf("status changed to %s", req.Status))
	JSONSuccess(c, gin.H{"message": "Client status updated successfully"})
}

func (h *ClientHandler) GetClientSecret(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		JSONError(c, CodeMissingParameter, "Missing client id")
		return
	}
	var client models.OAuthClient
	if err := h.service.DB.Where("id = ?", id).First(&client).Error; err != nil {
		JSONError(c, CodeNotFound, "Client not found")
		return
	}
	orgID := c.GetString("orgID")
	if !c.GetBool("isPlatformAdmin") && client.OrgID != orgID {
		JSONError(c, CodeForbidden, "Access denied")
		return
	}
	h.service.RecordAuditLog(c, "oauth.client.secret.read", "oauth_client", client.ID, "success", "")
	JSONSuccess(c, gin.H{"id": client.ID, "secret": client.Secret})
}
