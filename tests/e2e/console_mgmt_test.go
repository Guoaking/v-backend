package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ConsoleMgmtSuite 专门测试控制台管理功能 (用量、配额、OAuth 客户端)
type ConsoleMgmtSuite struct {
	BaseSuite
}

func TestConsoleMgmtSuite(t *testing.T) {
	suite.Run(t, new(ConsoleMgmtSuite))
}

// TestSmoke_UserProfile [SMOKE] 验证用户资料获取与更新
func (s *ConsoleMgmtSuite) TestSmoke_UserProfile() {
	// 1. Get
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/users/me").
		AsUser().
		ExpectSuccess()

	// 2. Update
	upReq := map[string]string{"name": "Refactored Name"}
	body, _ := json.Marshal(upReq)
	s.Ctx.NewRequest(s.T(), "PUT", "/api/v1/console/users/me").
		AsUser().
		WithBody(bytes.NewReader(body)).
		WithHeader("Content-Type", "application/json").
		ExpectSuccess()

	s.T().Log("✅ Smoke: 用户资料流验证通过")
}

// TestUsageAndQuota 验证用量统计与配额查询
func (s *ConsoleMgmtSuite) TestUsageAndQuota() {
	time.Sleep(500 * time.Millisecond)
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/usage/quota").AsUser().ExpectSuccess()
	s.Ctx.NewRequest(s.T(), "GET", "/api/v1/console/usage?start_date=2020-01-01&end_date=2030-12-31").AsUser().ExpectSuccess()
}

// TestOAuthClientLifecycle 验证 OAuth 客户端完整生命周期
func (s *ConsoleMgmtSuite) TestOAuthClientLifecycle() {
	// 1. Register
	regReq := map[string]string{"name": "Lifecycle Client", "scopes": "ocr:read"}
	body, _ := json.Marshal(regReq)
	var regResp struct {
		Data struct {
			ClientID string `json:"client_id"`
		} `json:"data"`
	}
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/console/oauth/clients/register").
		AsUser().
		WithBody(bytes.NewReader(body)).
		WithHeader("Content-Type", "application/json").
		ExpectJSONWithStatus(http.StatusCreated, &regResp)

	clientID := regResp.Data.ClientID
	require.NotEmpty(s.T(), clientID)

	// 2. Update & Rotate
	s.Ctx.NewRequest(s.T(), "POST", "/api/v1/console/oauth/clients/"+clientID+"/rotate").AsUser().ExpectSuccess()

	// 3. Delete
	s.Ctx.NewRequest(s.T(), "DELETE", "/api/v1/console/oauth/clients/"+clientID).AsUser().ExpectSuccess()

	s.T().Log("✅ OAuth 客户端生命周期验证通过")
}
