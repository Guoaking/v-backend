package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// AdminOrgSuite 专门测试管理员与组织管理功能
type AdminOrgSuite struct {
	BaseSuite
}

func TestAdminOrgSuite(t *testing.T) {
	suite.Run(t, new(AdminOrgSuite))
}

// TestSmoke_OrgManagement [SMOKE] 验证组织创建、切换与成员列表
func (s *AdminOrgSuite) TestSmoke_OrgManagement() {
	// 1. Create Organization
	createReq := map[string]string{"name": "Smoke Org"}
	createBody, _ := json.Marshal(createReq)
	var createResp struct {
		Data struct {
			OrgID string `json:"org_id"`
		} `json:"data"`
	}
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/orgs").
		AsUser().
		WithBody(bytes.NewReader(createBody)).
		WithHeader("Content-Type", "application/json").
		ExpectJSON(&createResp)

	newOrgID := createResp.Data.OrgID
	require.NotEmpty(s.T(), newOrgID)

	// 2. Switch & Token
	switchReq := map[string]string{"org_id": newOrgID}
	switchBody, _ := json.Marshal(switchReq)
	var switchResp struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/orgs/switch").
		AsUser().
		WithBody(bytes.NewReader(switchBody)).
		WithHeader("Content-Type", "application/json").
		ExpectJSON(&switchResp)

	// 3. Current Org
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/orgs/current").
		WithHeader("Authorization", "Bearer "+switchResp.Data.AccessToken).
		WithHeader("X-Organization-ID", newOrgID).
		ExpectSuccess()

	s.T().Log("✅ Smoke: 组织管理全链路验证通过")
}

// TestAdminStats 验证管理员统计报表
func (s *AdminOrgSuite) TestAdminStats() {
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/admin/stats/overview").AsAdmin().ExpectSuccess()
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/admin/users").AsAdmin().ExpectSuccess()
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/admin/organizations").AsAdmin().ExpectSuccess()
}

// TestAdminWriteActions 验证管理员写操作 (状态更新、配额调整)
func (s *AdminOrgSuite) TestAdminWriteActions() {
	// 1. Update User Status
	s.Ctx.NewRequest(s.T(), "PUT", fmt.Sprintf("/api/v1/admin/users/%s/status", s.Ctx.UserID)).
		AsAdmin().
		WithBody(strings.NewReader(`{"status":"suspended"}`)).
		WithHeader("Content-Type", "application/json").
		ExpectSuccess()

	// 2. Restore user
	s.Ctx.NewRequest(s.T(), "PUT", fmt.Sprintf("/api/v1/admin/users/%s/status", s.Ctx.UserID)).
		AsAdmin().
		WithBody(strings.NewReader(`{"status":"active"}`)).
		WithHeader("Content-Type", "application/json").
		ExpectSuccess()

	// 3. Adjust Quota
	adjustReq := map[string]interface{}{
		"service_type": "ocr",
		"adjustment":   100,
		"reason":       "Smoke Test Adjustment",
	}
	adjustBody, _ := json.Marshal(adjustReq)
	s.Ctx.NewRequest(s.T(), "POST", fmt.Sprintf("/api/v1/admin/organizations/%s/quotas/adjust", s.Ctx.OrgID)).
		AsAdmin().
		WithHeader("X-Organization-ID", s.Ctx.OrgID).
		WithBody(bytes.NewReader(adjustBody)).
		WithHeader("Content-Type", "application/json").
		ExpectSuccess()

	s.T().Log("✅ 管理员高级写操作验证通过")
}
