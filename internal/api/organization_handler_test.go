package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"kyc-service/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestOrganizationHandler_Create(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewOrganizationHandler(svc)
	r := setupRouter()

	userID := "u1"

	r.POST("/orgs", func(c *gin.Context) {
		c.Set("userID", userID)
		handler.CreateOrganization(c)
	})

	reqBody := CreateOrgRequest{Name: "New Org"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/orgs", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var org models.Organization
	db.First(&org, "name = ?", "New Org")
	assert.NotEmpty(t, org.ID)
	assert.Equal(t, userID, org.OwnerID)
}

func TestOrganizationHandler_InviteOrganizationMember(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewOrgMemberHandler(svc)
	r := setupRouter()

	ownerID := "u1"
	orgID := "org1"
	db.Create(&models.Organization{ID: orgID, OwnerID: ownerID})

	r.POST("/orgs/members", func(c *gin.Context) {
		c.Set("userID", ownerID)
		c.Set("orgID", orgID)
		handler.InviteOrganizationMember(c)
	})

	t.Run("Invite New User", func(t *testing.T) {
		reqBody := InviteMemberRequest{Email: "new@example.com", Role: "viewer"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/orgs/members", bytes.NewBuffer(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var inv models.OrganizationInvitation
		db.First(&inv, "email = ?", "new@example.com")
		assert.Equal(t, "invited", inv.Status)
	})
}

func TestOrganizationHandler_DeleteOrganizationMember(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewOrgMemberHandler(svc)
	r := setupRouter()

	orgID := "org1"
	memID := "mem1"
	db.Create(&models.OrganizationMember{ID: memID, OrganizationID: orgID, Role: "viewer"})

	r.DELETE("/orgs/members/:id", func(c *gin.Context) {
		c.Set("orgID", orgID)
		handler.DeleteOrganizationMember(c)
	})

	req := httptest.NewRequest("DELETE", "/orgs/members/"+memID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var count int64
	db.Model(&models.OrganizationMember{}).Where("id = ?", memID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestOrganizationHandler_GetUsageDetailedV2(t *testing.T) {
	svc, db := setupTestService(t)
	handler := NewOrgUsageHandler(svc)
	r := setupRouter()

	orgID := "org1"
	db.Create(&models.Organization{ID: orgID})
	// Seed Usage Data (using raw SQL because models might not match table exactly or simplified)
	// We need 'usage_daily' table which GORM auto migrate might not handle if it's a view or manual table.
	// In setupTestService, we didn't migrate Usage tables. Let's create dummy table for test.
	db.Exec("CREATE TABLE IF NOT EXISTS usage_daily (org_id text, date timestamp, total bigint, failed bigint)")
	db.Exec("INSERT INTO usage_daily (org_id, date, total, failed) VALUES (?, ?, ?, ?)", orgID, time.Now(), 100, 5)

	db.Exec("CREATE TABLE IF NOT EXISTS usage_daily_service (org_id text, date timestamp, service_id text, total bigint)")
	db.Exec("CREATE TABLE IF NOT EXISTS usage_daily_endpoint (org_id text, date timestamp, endpoint text, total bigint)")
	db.Exec("CREATE TABLE IF NOT EXISTS usage_daily_key (org_id text, date timestamp, api_key_id text, total bigint)")

	// Also need quotas
	db.Create(&models.OrganizationQuotas{
		OrganizationID: orgID,
		Allocation:     1000,
		Consumed:       100,
	})

	r.GET("/usage", func(c *gin.Context) {
		c.Set("orgID", orgID)
		handler.GetUsageDetailedV2(c)
	})

	req := httptest.NewRequest("GET", "/usage?period=30d", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"totalRequests":100`)
}
