package service

import (
	"context"
	"mime/multipart"

	"kyc-service/internal/apps/attendance/models"
	coreService "kyc-service/internal/service"

	goRedis "github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type AttendanceService struct {
	db         *gorm.DB
	kycService *coreService.KYCService
	redis      *goRedis.Client
}

func NewAttendanceService(db *gorm.DB, kycService *coreService.KYCService) *AttendanceService {
	return &AttendanceService{
		db:         db,
		kycService: kycService,
		redis:      kycService.Redis,
	}
}

func (s *AttendanceService) GetConfig() *coreService.KYCService {
	return s.kycService
}

func (s *AttendanceService) GetKYCService() *coreService.KYCService {
	return s.kycService
}

func (s *AttendanceService) buildPlatformContext(ctx context.Context, orgID, requestID, clientIP, userAgent string) context.Context {
	if orgID != "" {
		ctx = context.WithValue(ctx, "org_id", orgID)
	}
	if requestID != "" {
		ctx = context.WithValue(ctx, "request_id", requestID)
	}
	if clientIP != "" {
		ctx = context.WithValue(ctx, "client_ip", clientIP)
	}
	if userAgent != "" {
		ctx = context.WithValue(ctx, "user_agent", userAgent)
	}
	return ctx
}

func (s *AttendanceService) CreateActionLivenessSession(ctx context.Context, orgID, requestID, clientIP, userAgent string) (string, []string, error) {
	return s.kycService.CreateSession(s.buildPlatformContext(ctx, orgID, requestID, clientIP, userAgent))
}

func (s *AttendanceService) UploadActionLivenessVideo(ctx context.Context, orgID, sessionID, requestID, clientIP, userAgent string, file *multipart.FileHeader) error {
	_, err := s.kycService.UploadVideo(s.buildPlatformContext(ctx, orgID, requestID, clientIP, userAgent), sessionID, file)
	return err
}

func (s *AttendanceService) VerifyActionLiveness(ctx context.Context, orgID, sessionID, requestID, clientIP, userAgent string) (*coreService.ActionVerifyResult, error) {
	return s.kycService.Verify(s.buildPlatformContext(ctx, orgID, requestID, clientIP, userAgent), sessionID)
}

func (s *AttendanceService) GetPunchConfig(ctx context.Context, orgID string) (*models.AttendancePolicy, error) {
	var settings models.AttendancePolicy

	err := s.db.Where("org_id = ?", orgID).First(&settings).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			settings = models.AttendancePolicy{
				OrgID:           orgID,
				PunchMode:       "liveness_active",
				AllowLatePunch:  true,
				RequireLocation: false,
			}
			s.db.Create(&settings)
			return &settings, nil
		}
		return nil, err
	}

	return &settings, nil
}
