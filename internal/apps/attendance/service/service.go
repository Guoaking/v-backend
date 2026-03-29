package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kyc-service/internal/apps/attendance/models"
	coreService "kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AttendanceService 考勤微应用的核心服务层
// 它内部可以调用核心的 KYCService 来完成活体、OCR、比对等底层能力。
type AttendanceService struct {
	db         *gorm.DB
	kycService *coreService.KYCService
}

func NewAttendanceService(db *gorm.DB, kycService *coreService.KYCService) *AttendanceService {
	return &AttendanceService{
		db:         db,
		kycService: kycService,
	}
}

// ==============================================================================
// B端管理与 Token 生成
// ==============================================================================

// GenerateMagicLinkToken 生成供 C 端 H5 使用的 JWT Token
func (s *AttendanceService) GenerateMagicLinkToken(orgID string, jwtSecret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"org_id": orgID,
		"scope":  "attendance_magic_link",
		// 考勤码不需要长期有效，通常配置为较短时间（比如 30 天），过期后老板需重新生成并打印
		"exp": time.Now().AddDate(0, 1, 0).Unix(),
		"iat": time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// OCRResult 结构体
type OCRResult struct {
	SessionID  string                 `json:"session_id"`
	Fields     map[string]interface{} `json:"fields"`
	Confidence float64                `json:"confidence"`
	RawJSON    string                 `json:"raw_json"` // 用于后续入库比对
}

// ProcessOCR 处理证件 OCR 提取
// 它调用核心的 KYCService，并将结果暂存，返回给前端供用户修改
func (s *AttendanceService) ProcessOCR(ctx context.Context, orgID string, imageBytes []byte, idType string) (*OCRResult, error) {
	// TODO: 真正的 OCR 调用逻辑需要适配 coreService.OCRRequest
	// 目前先返回一个 mock 结果，保证编译通过，后续对接真实的 coreService.OCR

	// 模拟返回
	mockFields := map[string]interface{}{
		"id_number": "mock_12345",
		"name":      "Mock User",
	}

	rawBytes, _ := json.Marshal(mockFields)

	result := &OCRResult{
		SessionID:  utils.GenerateID(),
		Fields:     mockFields,
		Confidence: 0.95,
		RawJSON:    string(rawBytes),
	}

	return result, nil
}

// EnrollRequest 注册请求
type EnrollRequest struct {
	SessionID   string                 `json:"session_id"`
	IDNumber    string                 `json:"id_number" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Phone       string                 `json:"phone"`
	IDType      string                 `json:"id_type"`
	FaceImage   string                 `json:"face_image" binding:"required"` // Base64 or URL
	FinalFields map[string]interface{} `json:"final_fields"`
	RawOCRJSON  string                 `json:"raw_ocr_json"` // 假设前端或者 Redis 传回来的原始 OCR 结果
	RawImageURL string                 `json:"raw_image_url"`
}

// EnrollEmployee 员工注册提交
func (s *AttendanceService) EnrollEmployee(ctx context.Context, orgID string, req *EnrollRequest) error {
	log := logger.GetLogger().WithContext(ctx)

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 查重 (org_id, id_number)
		var existing models.OrganizationEmployee
		err := tx.Where("org_id = ? AND id_number = ?", orgID, req.IDNumber).First(&existing).Error
		if err == nil {
			if existing.Status == "active" {
				return fmt.Errorf("employee already enrolled")
			}
			// 如果是被删除的，我们可以选择复用
			log.Infof("Re-enrolling previously deleted employee: %s", req.IDNumber)
		} else if err != gorm.ErrRecordNotFound {
			return fmt.Errorf("database error during deduplication: %w", err)
		}

		// 2. 提取人脸特征 (调用底层 KYC)
		faceFeature, err := s.extractFaceFeature(ctx, req.FaceImage)
		if err != nil {
			return fmt.Errorf("face extraction failed: %w", err)
		}

		// 3. 落库或更新 models.OrganizationEmployee
		emp := models.OrganizationEmployee{
			ID:           utils.GenerateID(),
			OrgID:        orgID,
			IDNumber:     req.IDNumber,
			Name:         req.Name,
			Phone:        req.Phone,
			FaceFeature:  faceFeature,
			FaceImageURL: req.FaceImage, // 实际应该先上传 OSS
			Status:       "active",
		}

		if existing.ID != "" {
			emp.ID = existing.ID // 复用旧 ID
			if err := tx.Save(&emp).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Create(&emp).Error; err != nil {
				return err
			}
		}

		// 4. 落库 models.DataCollectionDocument (数据飞轮核心！)
		// 计算 is_corrected
		finalBytes, _ := json.Marshal(req.FinalFields)
		isCorrected := string(finalBytes) != req.RawOCRJSON

		doc := models.DataCollectionDocument{
			ID:             utils.GenerateID(),
			OrgID:          orgID,
			IDType:         req.IDType,
			RawImageURL:    req.RawImageURL,
			RawOCRResult:   datatypes.JSON(req.RawOCRJSON),
			FinalUserInput: datatypes.JSON(string(finalBytes)),
			IsCorrected:    isCorrected,
		}

		if err := tx.Create(&doc).Error; err != nil {
			// 这里如果失败，可以选择不回滚主事务，因为收集语料不应该阻塞员工打卡注册
			log.Warnf("Failed to collect data document, but proceeding with enrollment: %v", err)
		}

		return nil
	})
}

// extractFaceFeature 内部辅助方法，调用底层服务提取人脸特征
func (s *AttendanceService) extractFaceFeature(ctx context.Context, faceImageBase64 string) ([]byte, error) {
	// TODO: 调用 s.kycService.FaceCompare() 的单边提取能力
	// 暂作模拟
	return []byte("mock_face_feature_vector"), nil
}

// ==============================================================================
// 考勤打卡相关
// ==============================================================================

type PunchRequest struct {
	IDNumber      string  `json:"id_number" binding:"required"`
	PunchType     string  `json:"punch_type" binding:"required"` // in, out
	LivenessData  string  `json:"liveness_data"`                 // base64 video/image
	FallbackMode  bool    `json:"fallback_mode"`
	FallbackImage string  `json:"fallback_image"`
	Latitude      float64 `json:"latitude"` // 从前端获取的 GPS 坐标
	Longitude     float64 `json:"longitude"`
}

// PunchIn 执行打卡
func (s *AttendanceService) PunchIn(ctx context.Context, orgID string, req *PunchRequest) error {
	log := logger.GetLogger().WithContext(ctx)

	// 1. 查缓存，防抖 (Debounce)
	// TODO: 使用 Redis 检查 `attendance:punch:debounce:{orgID}:{idNumber}:{punchType}` 是否存在
	// 如果存在，直接 return nil (视为成功)

	// 2. 查员工是否存在
	var emp models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND id_number = ? AND status = ?", orgID, req.IDNumber, "active").First(&emp).Error; err != nil {
		return fmt.Errorf("employee not found or inactive")
	}

	record := models.AttendanceRecord{
		ID:         utils.GenerateID(),
		OrgID:      orgID,
		EmployeeID: emp.ID,
		PunchTime:  time.Now(),
		PunchType:  req.PunchType,
		Latitude:   req.Latitude,
		Longitude:  req.Longitude,
	}

	// 3. 处理降级模式 (Fallback)
	if req.FallbackMode {
		log.Infof("Processing punch-in in fallback mode for %s", req.IDNumber)
		record.Status = "manual_review"
		record.FallbackImageURL = req.FallbackImage // 实际工程中这里应该先转存 OSS
	} else {
		// 4. 正常打卡：调用底层活体和 1:1 比对
		log.Infof("Calling underlying KYC liveness and 1:1 face match for %s", req.IDNumber)

		// TODO: 构建核心 KYCRequest 并调用 s.kycService.SubmitKYCRequest()
		// 传入 req.LivenessData 和 emp.FaceFeature 进行比对
		// 注入系统内部的 req.AppKey = "system_attendance_app_key" 以完成计费隔离

		// 模拟调用成功
		record.Status = "success"
		record.LivenessScore = 0.98
		record.FaceScore = 0.99
	}

	// 5. 落库流水
	if err := s.db.Create(&record).Error; err != nil {
		return fmt.Errorf("failed to save attendance record: %w", err)
	}

	// 6. 写入防抖缓存
	// TODO: redis.Set(`attendance:punch:debounce:{orgID}:{idNumber}:{punchType}`, "1", 5*time.Minute)

	return nil
}

// GetPunchConfig 获取打卡配置
func (s *AttendanceService) GetPunchConfig(ctx context.Context, orgID string) (interface{}, error) {
	// TODO: 从 Org 的配置中读取，或者暂时写死默认配置
	return map[string]interface{}{
		"punch_mode":       "liveness_active",
		"allow_late_punch": true,
		"require_location": false,
	}, nil
}
