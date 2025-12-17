package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ClientHandler struct {
	service *service.KYCService
}

func NewClientHandler(svc *service.KYCService) *ClientHandler {
	return &ClientHandler{service: svc}
}

// ClientRegistrationRequest 客户端注册请求
type ClientRegistrationRequest struct {
	Name            string `json:"name" binding:"required"`
	Description     string `json:"description"`
	RedirectURI     string `json:"redirect_uri" binding:"required"`
	Scopes          string `json:"scopes" binding:"required"`
	TokenTTLSeconds int    `json:"token_ttl_seconds"`
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
}

// ClientListResponse 客户端列表响应
type ClientListResponse struct {
	ID          string    `json:"client_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	RedirectURI string    `json:"redirect_uri"`
	Scopes      string    `json:"scopes"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// 生成客户端凭证
	clientID := uuid.New().String()
	clientSecret := uuid.New().String()

	// 创建客户端记录
	client := &models.OAuthClient{
		ID:              clientID,
		Secret:          clientSecret,
		Name:            req.Name,
		Description:     req.Description,
		RedirectURI:     req.RedirectURI,
		Scopes:          req.Scopes,
		Status:          "active",
		OrgID:           c.GetString("orgID"),
		TokenTTLSeconds: req.TokenTTLSeconds,
	}

	if err := h.service.DB.Create(client).Error; err != nil {
		logger.GetLogger().WithError(err).Error("create client failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create client"})
		return
	}

	// 记录客户端注册审计日志
	h.service.RecordAuditLog(c, "client.register", "client", clientID, "success", "client registered")

	c.JSON(http.StatusCreated, ClientRegistrationResponse{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Name:         req.Name,
		Description:  req.Description,
		RedirectURI:  req.RedirectURI,
		Scopes:       req.Scopes,
		Status:       "active",
		CreatedAt:    client.CreatedAt,
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
	var clients []models.OAuthClient
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
	q := h.service.DB.Where("status = ?", "active")
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if search != "" {
		q = q.Where("name ILIKE ? OR redirect_uri ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if err := q.Order("created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&clients).Error; err != nil {
		logger.GetLogger().WithError(err).Error("list clients failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list clients"})
		return
	}

	response := make([]ClientListResponse, len(clients))
	for i, client := range clients {
		response[i] = ClientListResponse{
			ID:          client.ID,
			Name:        client.Name,
			Description: client.Description,
			RedirectURI: client.RedirectURI,
			Scopes:      client.Scopes,
			Status:      client.Status,
			CreatedAt:   client.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, response)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID must not be empty"})
		return
	}

	// 删除客户端
	orgID := c.GetString("orgID")
	q := h.service.DB.Where("id = ?", clientID)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if err := q.Delete(&models.OAuthClient{}).Error; err != nil {
		logger.GetLogger().WithError(err).Error("delete client failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete client"})
		return
	}

	// 记录删除审计日志
	h.service.RecordAuditLog(c, "client.delete", "client", clientID, "success", "client deleted")
	c.JSON(http.StatusOK, gin.H{"message": "Client deleted"})
}

type RotateSecretResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (h *ClientHandler) RotateClientSecret(c *gin.Context) {
	clientID := c.Param("id")
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID must not be empty"})
		return
	}
	orgID := c.GetString("orgID")
	var client models.OAuthClient
	q := h.service.DB.Where("id = ?", clientID)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if err := q.First(&client).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		return
	}
	newSecret := uuid.New().String()
	client.Secret = newSecret
	if err := h.service.DB.Save(&client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Secret rotation failed"})
		return
	}
	h.service.RecordAuditLog(c, "client.rotate_secret", "client", clientID, "success", "")
	c.JSON(http.StatusOK, RotateSecretResponse{ClientID: clientID, ClientSecret: newSecret})
}

type UpdateStatusRequest struct {
	Status string `json:"status"`
}

func (h *ClientHandler) UpdateClientStatus(c *gin.Context) {
	clientID := c.Param("id")
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID must not be empty"})
		return
	}
	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	orgID := c.GetString("orgID")
	q := h.service.DB.Model(&models.OAuthClient{}).Where("id = ?", clientID)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if err := q.Update("status", req.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update status"})
		return
	}
	h.service.RecordAuditLog(c, "client.update_status", "client", clientID, "success", req.Status)
	c.JSON(http.StatusOK, gin.H{"client_id": clientID, "status": req.Status})
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
