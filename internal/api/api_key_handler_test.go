package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"kyc-service/internal/config"
	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Helper to setup isolated test environment
func setupTestService(t *testing.T) (*service.KYCService, *gorm.DB) {
	// Use distinct in-memory DB for each test to avoid conflicts
	// Use t.Name() to ensure isolation but also persistence within the same test
	dbName := fmt.Sprintf("file:memdb_%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	// Migrate schema
	db.AutoMigrate(
		&models.Organization{},
		&models.User{},
		&models.OrganizationMember{},
		&models.RolePermission{},
		&models.APIKey{},
		&models.GlobalConfig{},
		&models.AuditLog{},
		&models.OrganizationInvitation{},
		&models.OrganizationQuotas{},
		&models.Notification{},
	)

	cfg := &config.Config{
		Security: config.SecurityConfig{
			JWTSecret: "test-secret-key-must-be-32-bytes-long",
		},
	}

	return &service.KYCService{DB: db, Config: cfg}, db
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func TestAPIKeyHandler_CreateAPIKey(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewAPIKeyHandler(svc)
	r := setupRouter()
	r.Use(middleware.JWTAuth(svc))

	// Seed User & Org & Policy
	userID := "u1"
	orgID := "org1"
	db.Create(&models.User{
		ID:           userID,
		Status:       "active",
		OrgID:        orgID,
		CurrentOrgID: orgID,
		OrgRole:      "admin",
	})
	db.Create(&models.Organization{ID: orgID, Status: "active"})
	db.Create(&models.OrganizationMember{
		ID:             fmt.Sprintf("mem_%s_%s", orgID, userID),
		OrganizationID: orgID,
		UserID:         userID,
		Role:           "admin",
		Status:         "active",
	})

	// Policy: allowed_scopes=["ocr:read"]
	db.Create(&models.GlobalConfig{
		Key:   "org_policy:" + orgID,
		Value: `{"allowed_scopes": ["ocr:read"], "require_approval": false}`,
	})

	r.POST("/keys", handler.CreateAPIKey)

	t.Run("Success", func(t *testing.T) {
		reqBody := CreateAPIKeyRequest{
			Name:   "Test Key",
			Scopes: []string{"ocr:read"},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/keys", bytes.NewBuffer(body))
		
		token := GenerateTestToken(t, userID, orgID, "admin", svc.Config.Security.JWTSecret)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		var resp SuccessResponse
		json.Unmarshal(w.Body.Bytes(), &resp)

		data := resp.Data.(map[string]interface{})
		assert.Equal(t, "Test Key", data["name"])
		assert.NotEmpty(t, data["secret"])
		assert.Equal(t, "active", data["status"])
	})

	t.Run("Policy Reject", func(t *testing.T) {
		reqBody := CreateAPIKeyRequest{
			Name:   "Bad Key",
			Scopes: []string{"admin:write"}, // Not allowed
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/keys", bytes.NewBuffer(body))

		token := GenerateTestToken(t, userID, orgID, "admin", svc.Config.Security.JWTSecret)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 403, w.Code)
	})
}

func TestAPIKeyHandler_GetAPIKeys(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewAPIKeyHandler(svc)
	r := setupRouter()
	r.Use(middleware.JWTAuth(svc))

	userID := "u1"
	orgID := "org1"
	db.Create(&models.User{
		ID:           userID,
		Status:       "active",
		OrgID:        orgID,
		CurrentOrgID: orgID,
		OrgRole:      "admin",
	})
	db.Create(&models.Organization{ID: orgID, Status: "active"})
	db.Create(&models.OrganizationMember{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           "admin",
		Status:         "active",
	})

	db.Create(&models.APIKey{
		ID:          "kyc_key1",
		UserID:      userID,
		OrgID:       orgID,
		Name:        "Key 1",
		Status:      "active",
		Scopes:      `["ocr:read"]`,
	})

	r.GET("/keys", handler.GetAPIKeys)

	req := httptest.NewRequest("GET", "/keys", nil)
	token := GenerateTestToken(t, userID, orgID, "admin", svc.Config.Security.JWTSecret)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Key 1")
}

func TestAPIKeyHandler_DeleteAPIKey(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewAPIKeyHandler(svc)
	r := setupRouter()
	r.Use(middleware.JWTAuth(svc))

	userID := "u1"
	orgID := "org1"
	db.Create(&models.User{
		ID:           userID,
		Status:       "active",
		OrgID:        orgID,
		CurrentOrgID: orgID,
		OrgRole:      "admin",
	})
	db.Create(&models.Organization{ID: orgID, Status: "active"})
	db.Create(&models.OrganizationMember{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           "admin",
		Status:         "active",
	})

	keyID := "kyc_del"
	db.Create(&models.APIKey{ID: keyID, UserID: userID, OrgID: orgID, Status: "active"})

	r.DELETE("/keys/:id", handler.DeleteAPIKey)

	req := httptest.NewRequest("DELETE", "/keys/"+keyID, nil)
	token := GenerateTestToken(t, userID, orgID, "admin", svc.Config.Security.JWTSecret)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	// Verify DB status
	var key models.APIKey
	db.First(&key, "id = ?", keyID)
	assert.Equal(t, "revoked", key.Status)
}
