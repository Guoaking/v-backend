package api

import (
	"github.com/gin-gonic/gin"
)

type DiscoveryHandler struct{}

func NewDiscoveryHandler() *DiscoveryHandler { return &DiscoveryHandler{} }

func (h *DiscoveryHandler) WellKnown(c *gin.Context) {
	base := "/api/v1"
	c.JSON(200, gin.H{
		"issuer":                   "kyc-service",
		"token_endpoint":           base + "/oauth/token",
		"revocation_endpoint":      base + "/oauth/revoke",
		"introspection_endpoint":   base + "/oauth/introspect",
		"jwks_uri":                 "/jwks.json",
		"grant_types_supported":    []string{"client_credentials"},
		"response_types_supported": []string{"token"},
		"scopes_supported":         []string{"ocr:read", "face:read", "liveness:read", "kyc:verify"},
	})
}

func (h *DiscoveryHandler) JWKS(c *gin.Context) {
	c.JSON(200, gin.H{"keys": []interface{}{}})
}
