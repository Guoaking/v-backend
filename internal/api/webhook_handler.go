package api

import (
	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
)

type WebhookHandler struct{ service *service.KYCService }

func NewWebhookHandler(s *service.KYCService) *WebhookHandler { return &WebhookHandler{service: s} }

func (h *WebhookHandler) ListWebhooks(c *gin.Context) {
	JSONSuccess(c, gin.H{"items": []gin.H{}})
}
