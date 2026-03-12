package service

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"kyc-service/internal/config"
	"kyc-service/internal/models"
	
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// MockStorage implements storage.StorageService
type MockStorage struct {
	UploadFunc       func(ctx context.Context, filename string, content io.Reader) (string, string, error)
	GetPublicURLFunc func(internalPath string) string
}

func (m *MockStorage) Upload(ctx context.Context, filename string, content io.Reader) (string, string, error) {
	if m.UploadFunc != nil {
		return m.UploadFunc(ctx, filename, content)
	}
	return "/tmp/" + filename, "http://localhost/files/" + filename, nil
}

func (m *MockStorage) GetPublicURL(internalPath string) string {
	if m.GetPublicURLFunc != nil {
		return m.GetPublicURLFunc(internalPath)
	}
	return "http://localhost/files/" + internalPath
}

func setupTestDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	// Migrate schema
	err = db.AutoMigrate(
		&models.Organization{},
		&models.Plan{},
		&models.OrganizationQuotas{},
		&models.LivenessTask{},
		&models.VideoAsset{},
		&models.KYCRequest{},
		&models.AuditLog{},
	)
	if err != nil {
		panic(err)
	}
	return db
}

func setupTestConfig(serverURL string) *config.Config {
	cfg := &config.Config{}
	cfg.Storage.Mode = "local"
	cfg.ThirdParty.LivenessAction.SubmitURL = serverURL
	cfg.ThirdParty.LivenessAction.CallbackURL = "http://localhost:8080/callback"
	cfg.Security.ServiceSecretKey = "test-secret"
	return cfg
}

// Helper to create multipart file header
func createMultipartFileHeader(t *testing.T, content []byte, filename string) *multipart.FileHeader {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		part, _ := writer.CreateFormFile("file", filename)
		part.Write(content)
		writer.Close()
		pw.Close()
	}()

	req := httptest.NewRequest("POST", "/", pr)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	
	err := req.ParseMultipartForm(1024 * 1024) // 1MB
	assert.NoError(t, err)

	return req.MultipartForm.File["file"][0]
}

func TestActionLivenessFlow(t *testing.T) {
	// 1. Setup DB
	db := setupTestDB()

	// 2. Setup Mock ThirdParty Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"message":"success"}`))
	}))
	defer ts.Close()

	// 3. Setup Config
	cfg := setupTestConfig(ts.URL)

	// 4. Initialize Service
	svc := &KYCService{
		DB:         db,
		Config:     cfg,
		Storage:    &MockStorage{},
		ThirdParty: NewThirdPartyService(cfg),
		Redis:      nil, // Use DB fallback for quotas
	}

	// 5. Seed Data (Org, Plan, Quota)
	orgID := "org_test_123"
	db.Create(&models.Organization{
		ID:     orgID,
		Name:   "Test Org",
		Status: "active",
	})
	db.Create(&models.OrganizationQuotas{
		ID:             "quota_1",
		OrganizationID: orgID,
		ServiceType:    "liveness",
		Allocation:     100,
		Consumed:       0,
	})

	// 6. Test Context
	ctx := context.Background()
	ctx = context.WithValue(ctx, "org_id", orgID)
	ctx = context.WithValue(ctx, "user_id", "user_123")

	// --- Test Step 1: Create Session ---
	var sid string
	t.Run("CreateSession", func(t *testing.T) {
		var actions []string
		var err error
		sid, actions, err = svc.CreateSession(ctx)
		assert.NoError(t, err)
		assert.NotEmpty(t, sid)
		assert.Len(t, actions, 2)

		// Verify DB
		var task models.LivenessTask
		err = db.Where("session_id = ?", sid).First(&task).Error
		assert.NoError(t, err)
		assert.Equal(t, "created", task.Status)
	})

	// --- Test Step 2: Upload Video ---
	t.Run("UploadVideo", func(t *testing.T) {
		// Create FileHeader
		fileHeader := createMultipartFileHeader(t, []byte("fake mp4 data"), "test.mp4")

		// Upload
		task, err := svc.UploadVideo(ctx, sid, fileHeader)
		if assert.NoError(t, err) {
			assert.NotNil(t, task)
			assert.Equal(t, "submitted", task.Status) 
		}

		// Verify Quota Consumed
		var q models.OrganizationQuotas
		db.Where("organization_id = ? AND service_type = ?", orgID, "liveness").First(&q)
		assert.Equal(t, 1, q.Consumed)
	})
}
