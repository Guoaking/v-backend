package service

import (
	"context"
	"io"
	"testing"

	"kyc-service/internal/models"
	"kyc-service/internal/storage"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- Standard Mocks using testify/mock ---

// SyncLogWorker is a synchronous implementation for tests
type SyncLogWorker struct {
	db *gorm.DB
}

func (s *SyncLogWorker) Start()                         {}
func (s *SyncLogWorker) Stop()                          {}
func (s *SyncLogWorker) Enqueue(log models.LogEnvelope) {}
func (s *SyncLogWorker) RecordAuditLog(log *models.AuditLog) {
	s.db.Create(log)
}

// MockStorageService is a mock implementation of storage.StorageService
type MockStorageService struct {
	mock.Mock
}

func (m *MockStorageService) Upload(ctx context.Context, filename string, content io.Reader) (string, string, error) {
	args := m.Called(ctx, filename, content)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockStorageService) GetPublicURL(internalPath string) string {
	args := m.Called(internalPath)
	return args.String(0)
}

func (m *MockStorageService) ResolveAccess(fullPath string) (*storage.ResolvedPath, error) {
	return nil, nil
}

func (m *MockStorageService) GetAbsolutePath(filename string) string {
	return filename
}

// --- Test Setup Helpers ---

func setupTestContext(orgID string) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, "org_id", orgID)
	ctx = context.WithValue(ctx, "user_id", "test-user-123")
	return ctx
}

func initTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	// AutoMigrate all needed models
	err = db.AutoMigrate(
		&models.Organization{},
		&models.OrganizationQuotas{},
		&models.GlobalConfig{},
		&models.KYCRequest{},
		&models.AuditLog{},
	)
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
	return db
}

func initTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to run miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	return s, rdb
}

// --- Table-Driven Tests for KYCService ---

func TestKYCService_GetOrgPolicy(t *testing.T) {
	db := initTestDB(t)
	svc := &KYCService{DB: db, LogWorker: &SyncLogWorker{db: db}}
	orgID := "test-org"

	tests := []struct {
		name     string
		setup    func()
		wantRate int
	}{
		{
			name:     "Default Policy",
			setup:    func() {},
			wantRate: 100, // Default from code
		},
		{
			name: "Custom Policy from DB",
			setup: func() {
				policyJSON := `{"max_rate_per_sec": 500, "allowed_scopes": ["ocr:read"]}`
				db.Create(&models.GlobalConfig{
					Key:   "org_policy:" + orgID,
					Value: policyJSON,
				})
			},
			wantRate: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			policy := svc.GetOrgPolicy(orgID)
			assert.Equal(t, tt.wantRate, policy.MaxRatePerSec)
		})
	}
}

func TestKYCService_QuotaConsumption(t *testing.T) {
	// Setup DB & Redis
	db := initTestDB(t)
	mr, rdb := initTestRedis(t)
	defer mr.Close()

	orgID := "org-1"
	svcType := "ocr"
	ctx := setupTestContext(orgID)

	svc := &KYCService{
		DB:    db,
		Redis: rdb,
	}

	// 1. Seed Quota in DB
	db.Create(&models.OrganizationQuotas{
		ID:             "q1",
		OrganizationID: orgID,
		ServiceType:    svcType,
		Allocation:     2,
		Consumed:       0,
	})

	// Test Case: Successful consumption (using Redis fallback to DB init)
	t.Run("Consume within limit", func(t *testing.T) {
		executed := false
		err := svc.checkAndConsumeQuota(ctx, orgID, svcType, func() error {
			executed = true
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, executed)

		// Verify Redis state (Lua script increments it)
		val, _ := rdb.Get(ctx, "quota:consumed:"+orgID+":"+svcType).Int()
		assert.Equal(t, 1, val)
	})

	t.Run("Quota Exceeded", func(t *testing.T) {
		// Consume second token
		_ = svc.checkAndConsumeQuota(ctx, orgID, svcType, func() error { return nil })

		// Attempt third (limit is 2)
		err := svc.checkAndConsumeQuota(ctx, orgID, svcType, func() error {
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "QUOTA_EXCEEDED")
	})
}

func TestKYCService_AuditLogging(t *testing.T) {
	db := initTestDB(t)
	svc := &KYCService{DB: db, LogWorker: &SyncLogWorker{db: db}}
	orgID := "org-audit"
	ctx := setupTestContext(orgID)

	t.Run("Record Audit Log Success", func(t *testing.T) {
		svc.RecordAuditLog(ctx, "test.action", "test.resource", "res-123", "success", "all good")

		var log models.AuditLog
		err := db.Where("action = ?", "test.action").First(&log).Error
		assert.NoError(t, err)
		assert.Equal(t, "test.action", log.Action)
		assert.Equal(t, "test-user-123", log.UserID)
	})
}
