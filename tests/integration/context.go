package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"kyc-service/internal/bootstrap"
	"kyc-service/internal/models"
	"kyc-service/pkg/utils"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// E2EContext 封装 E2E 测试所需的全局上下文
type E2EContext struct {
	App     *bootstrap.App
	UserID  string
	AdminID string
	OrgID   string
	Email   string
}

// RequestBuilder 用于构建流式 HTTP 请求
type RequestBuilder struct {
	ctx    *E2EContext
	t      *testing.T
	method string
	path   string
	body   io.Reader
	header http.Header
}

// BaseSuite 基础测试套件
type BaseSuite struct {
	suite.Suite
	Ctx *E2EContext
}

func (s *BaseSuite) SetupSuite() {
	ctx := context.Background()
	_ = os.MkdirAll("./test_storage/videos", 0755)
	_ = os.MkdirAll("./test_storage/pic", 0755)
	// 启动应用
	app, cleanup, err := bootstrap.SetupApp(ctx, "../../config.test")
	require.NoError(s.T(), err)

	s.Ctx = &E2EContext{
		App:     app,
		UserID:  "550e8400-e29b-41d4-a716-446655440000",
		AdminID: "550e8400-e29b-41d4-a716-446655440002",
		OrgID:   "550e8400-e29b-41d4-a716-446655440001",
		Email:   "e2e@test.com",
	}

	// 预置数据
	s.seedData()

	// 注册清理函数
	s.T().Cleanup(func() {
		cleanup()
		if s.Ctx.App.LogWorker != nil {
			s.Ctx.App.LogWorker.Stop()
		}
		if s.Ctx.App.HeartbeatManager != nil {
			s.Ctx.App.HeartbeatManager.Stop()
		}
	})
}

func (s *BaseSuite) seedData() {
	db := s.Ctx.App.DB
	// 1. 组织
	org := models.Organization{ID: s.Ctx.OrgID, Name: "E2E Test Org", PlanID: "starter", Status: "active"}
	db.Save(&org)
	// 2. 普通用户
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	user := models.User{
		ID:           s.Ctx.UserID,
		Email:        s.Ctx.Email,
		Password:     string(hashedPassword),
		Name:         "E2E Tester",
		OrgID:        s.Ctx.OrgID,
		OrgRole:      "owner",
		Status:       "active",
		CurrentOrgID: s.Ctx.OrgID,
	}
	db.Save(&user)
	// 3. 平台管理员
	admin := models.User{
		ID:              s.Ctx.AdminID,
		Email:           "admin@test.com",
		Name:            "E2E Admin",
		Role:            "admin",
		IsPlatformAdmin: true,
		OrgID:           s.Ctx.OrgID, // 即使是管理员，也关联一个测试组织以满足约束
		CurrentOrgID:    s.Ctx.OrgID,
		Status:          "active",
	}
	db.Save(&admin)
	// 4. 成员
	member := models.OrganizationMember{
		ID:             utils.GenerateID(),
		OrganizationID: s.Ctx.OrgID,
		UserID:         s.Ctx.UserID,
		Role:           "owner",
		Status:         "active",
	}
	db.Save(&member)
	// 5. 平台管理员成员关系
	adminMember := models.OrganizationMember{
		ID:             utils.GenerateID(),
		OrganizationID: s.Ctx.OrgID,
		UserID:         s.Ctx.AdminID,
		Role:           "admin",
		Status:         "active",
	}
	db.Save(&adminMember)
	// 6. 初始配额与计划 (核心计费项)
	db.Exec("DELETE FROM organization_quotas WHERE organization_id = ?", s.Ctx.OrgID)
	services := []string{"ocr", "face", "liveness", "kyc"}
	for _, srv := range services {
		db.Exec("INSERT INTO organization_quotas (id, organization_id, service_type, allocation, consumed, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			utils.GenerateID(), s.Ctx.OrgID, srv, 1000, 0, time.Now())
	}
	// 7. OAuth 客户端 (用于 AsApp)
	sysClient := models.OAuthClient{
		ID:              "sys_web_console_playground",
		Secret:          "sys_web_console_playground_secret",
		Name:            "Playground",
		Status:          "active",
		Scopes:          `["ocr:read", "face:read", "liveness:read", "kyc:verify"]`,
		TokenTTLSeconds: 900,
	}
	db.Save(&sysClient) // Use Save to ensure secret is updated if ID exists
}

// NewRequest 开始构建一个新请求
func (c *E2EContext) NewRequest(t *testing.T, method, path string) *RequestBuilder {
	return &RequestBuilder{
		ctx:    c,
		t:      t,
		method: method,
		path:   path,
		header: make(http.Header),
	}
}

// --- 鉴权身份注入 ---

// AsUser 模拟控制台普通用户登录
func (b *RequestBuilder) AsUser() *RequestBuilder {
	return b.AsSpecificUser(b.ctx.UserID, b.ctx.OrgID)
}

// AsSpecificUser 模拟特定用户登录
func (b *RequestBuilder) AsSpecificUser(userID string, orgID string) *RequestBuilder {
	token, err := b.ctx.generateToken(jwt.MapClaims{
		"user_id": userID,
		"org_id":  orgID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})
	require.NoError(b.t, err)
	b.header.Set("Authorization", "Bearer "+token)
	b.header.Set("X-Organization-ID", orgID)
	return b
}

// AsAdmin 模拟平台超级管理员
func (b *RequestBuilder) AsAdmin() *RequestBuilder {
	token, err := b.ctx.generateToken(jwt.MapClaims{
		"user_id": b.ctx.AdminID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})
	require.NoError(b.t, err)
	b.header.Set("Authorization", "Bearer "+token)
	return b
}

// AsApp 模拟第三方 App 通过 OAuth 访问 API (Client Credentials 流)
func (b *RequestBuilder) AsApp(scopes ...string) *RequestBuilder {
	scopeStr := `["ocr:read", "face:read", "liveness:read", "kyc:verify"]`
	if len(scopes) > 0 {
		scopeStr = `["` + strings.Join(scopes, `","`) + `"]`
	}

	token, err := b.ctx.generateToken(jwt.MapClaims{
		"client_id": "sys_web_console_playground",
		"org_id":    b.ctx.OrgID,
		"scope":     scopeStr,
		"exp":       time.Now().Add(1 * time.Hour).Unix(),
	})
	require.NoError(b.t, err)
	b.header.Set("Authorization", "Bearer "+token)
	// API 模式下 X-Organization-ID 是可选的，因为 Token 里通常带了 org_id
	return b
}

// AsPlayground 模拟 Playground 演示模式 (User + App 双重身份)
func (b *RequestBuilder) AsPlayground() *RequestBuilder {
	token, err := b.ctx.generateToken(jwt.MapClaims{
		"client_id": "sys_web_console_playground",
		"user_id":   b.ctx.UserID,
		"org_id":    b.ctx.OrgID,
		"scope":     `["ocr:read", "face:read", "liveness:read", "kyc:verify"]`,
		"source":    "playground",
		"exp":       time.Now().Add(15 * time.Minute).Unix(),
	})
	require.NoError(b.t, err)
	b.header.Set("Authorization", "Bearer "+token)
	b.header.Set("X-Organization-ID", b.ctx.OrgID)
	return b
}

// --- 请求构建助手 ---

// WithBody 设置请求体
func (b *RequestBuilder) WithBody(body io.Reader) *RequestBuilder {
	b.body = body
	return b
}

// WithJSON 序列化并设置请求体，自动添加 Content-Type: application/json
func (b *RequestBuilder) WithJSON(v interface{}) *RequestBuilder {
	payload, err := json.Marshal(v)
	require.NoError(b.t, err)
	b.body = bytes.NewReader(payload)
	b.header.Set("Content-Type", "application/json")
	return b
}

// WithHeader 设置 Header
func (b *RequestBuilder) WithHeader(key, value string) *RequestBuilder {
	b.header.Set(key, value)
	return b
}

// Do 发送请求并返回响应记录器
func (b *RequestBuilder) Do() *httptest.ResponseRecorder {
	req, err := http.NewRequest(b.method, b.path, b.body)
	require.NoError(b.t, err)
	req.Header = b.header

	w := httptest.NewRecorder()
	b.ctx.App.Engine.ServeHTTP(w, req)
	return w
}

// --- 断言助手 (Assertions) ---

// ExpectSuccess 断言 200 OK
func (b *RequestBuilder) ExpectSuccess() *httptest.ResponseRecorder {
	w := b.Do()
	require.Equal(b.t, http.StatusOK, w.Code, "Expected 200 OK, but got %d. Body: %s", w.Code, w.Body.String())
	return w
}

// ExpectStatus 断言特定状态码
func (b *RequestBuilder) ExpectStatus(code int) *httptest.ResponseRecorder {
	w := b.Do()
	require.Equal(b.t, code, w.Code, "Expected status %d, but got %d. Body: %s", code, w.Code, w.Body.String())
	return w
}

// ExpectForbidden 断言 403 Forbidden (权限不足)
func (b *RequestBuilder) ExpectForbidden() {
	w := b.Do()
	require.Equal(b.t, http.StatusForbidden, w.Code, "Should be forbidden, but got %d. Body: %s", w.Code, w.Body.String())
}

// ExpectUnauthorized 断言 401 Unauthorized (未登录)
func (b *RequestBuilder) ExpectUnauthorized() {
	w := b.Do()
	require.Equal(b.t, http.StatusUnauthorized, w.Code, "Should be unauthorized, but got %d. Body: %s", w.Code, w.Body.String())
}

// ExpectJSON 发送请求并断言 200 OK，然后解析 JSON 到目标对象
func (b *RequestBuilder) ExpectJSON(target any) *httptest.ResponseRecorder {
	return b.ExpectJSONWithStatus(http.StatusOK, target)
}

// ExpectJSONWithStatus 发送请求并断言特定状态码，然后解析 JSON 到目标对象
func (b *RequestBuilder) ExpectJSONWithStatus(status int, target any) *httptest.ResponseRecorder {
	w := b.ExpectStatus(status)
	err := json.Unmarshal(w.Body.Bytes(), target)
	require.NoError(b.t, err, "Response should be valid JSON. Body: %s", w.Body.String())
	return w
}

// ExpectErrorCode 发送请求并断言特定的业务错误码
func (b *RequestBuilder) ExpectErrorCode(code int) *httptest.ResponseRecorder {
	w := b.Do()
	var resp struct {
		Code int `json:"code"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(b.t, err, "Response should be valid JSON")
	require.Equal(b.t, code, resp.Code, "Expected business error code %d, but got %d", code, resp.Code)
	return w
}

// --- 副作用验证器 (Verifiers) ---

// AssertBusinessReach 断言请求到达了业务逻辑 (即不是 401/403 等基础架构错误)
func (s *BaseSuite) AssertBusinessReach(t *testing.T, w *httptest.ResponseRecorder) {
	require.NotEqual(t, http.StatusUnauthorized, w.Code, "不应返回 401 Unauthorized. Body: %s", w.Body.String())
	require.NotEqual(t, http.StatusForbidden, w.Code, "不应返回 403 Forbidden. Body: %s", w.Body.String())
	// 即使是 404 或 400，只要返回了 JSON 且包含 request_id，说明到达了业务层
	var resp struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NotEmpty(t, resp.RequestID, "响应应包含 RequestID，证明到达了业务框架. Body: %s", w.Body.String())
}

// VerifyUsageLogged 验证数据库中是否产生了计费日志
func (s *BaseSuite) VerifyUsageLogged(serviceType string) {
	var count int64
	err := s.Ctx.App.DB.Model(&models.UsageLog{}).
		Where("org_id = ? AND service_type = ?", s.Ctx.OrgID, serviceType).
		Count(&count).Error
	require.NoError(s.T(), err)
	require.Greater(s.T(), count, int64(0), "No usage log found for service: %s", serviceType)
}

// --- 内部辅助方法 ---

func (c *E2EContext) generateToken(claims jwt.MapClaims) (string, error) {
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(c.App.Config.Security.JWTSecret))
}

// MultipartBody 辅助函数用于构建文件上传请求体
func (c *E2EContext) MultipartBody(files map[string][]byte, fields map[string]string) (*bytes.Buffer, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for name, content := range files {
		part, _ := writer.CreateFormFile(name, "test.jpg")
		part.Write(content)
	}
	for k, v := range fields {
		writer.WriteField(k, v)
	}
	writer.Close()
	return body, writer.FormDataContentType()
}
