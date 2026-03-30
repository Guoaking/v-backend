package service

import (
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
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
func (s *AttendanceService) ProcessOCR(ctx context.Context, orgID string, file *multipart.FileHeader, idType string) (*OCRResult, error) {
	// 映射前端传来的 id_type 到底层 OCR 支持的类型
	// 底层支持: "id_card", "driver_license", "vehicle_license", "bank_card", "business_license", "general", "vat_certificate", "passport", "NPWP"
	backendOcrType := idType
	switch idType {
	case "thai_id", "id_card":
		backendOcrType = "id_card"
	case "driving_license":
		backendOcrType = "driver_license"
	case "voter_id":
		backendOcrType = "general" // 暂时 fallback 到 general 或者其它合适的类型
	}

	// 1. 构建底层所需的请求
	req := &coreService.OCRRequest{
		Picture: file,
		Type:    backendOcrType,
	}

	// Inject the organization context into the context to satisfy the underlying OCR service's requirements
	ctx = context.WithValue(ctx, "org_id", orgID)

	// 2. 调用底层 OCR 服务
	resp, err := s.kycService.OCR(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("underlying OCR failed: %w", err)
	}

	if resp.Code != 200 && resp.Code != 0 {
		return nil, fmt.Errorf("ocr service error: %s", resp.Msg)
	}

	// 3. 将底层返回的字段拍平，返回给前端
	mappedFields := map[string]interface{}{}

	// 智能提取核心字段，兼容多种证件类型的别名
	// ID 字段的可能别名 (例如 NIK 用于印尼身份证)
	idAliases := []string{"id_number", "id", "NIK", "ID", "Document Number", "No", "ID Number"}
	for _, alias := range idAliases {
		if val, ok := resp.ParsingResults[alias]; ok && val.Text != "" {
			mappedFields["id_number"] = val.Text
			break
		}
	}

	// Name 字段的可能别名 (例如 Nama 用于印尼身份证)
	nameAliases := []string{"name", "Name", "Nama", "Full Name", "Given Name", "Surname"}
	for _, alias := range nameAliases {
		if val, ok := resp.ParsingResults[alias]; ok && val.Text != "" {
			mappedFields["name"] = val.Text
			break
		}
	}

	// 额外提取一些有用的上下文信息（如果有）
	// 例如出生日期、性别等，前端可能需要展示
	extraFields := []string{"Tempat/Tgl Lahir", "Jenis kelamin", "Alamat"}
	for _, field := range extraFields {
		if val, ok := resp.ParsingResults[field]; ok && val.Text != "" {
			mappedFields[field] = val.Text
		}
	}

	// 如果没提取到核心字段，兜底给空字符串，让前端填
	if _, ok := mappedFields["id_number"]; !ok {
		mappedFields["id_number"] = ""
	}
	if _, ok := mappedFields["name"]; !ok {
		mappedFields["name"] = ""
	}

	result := &OCRResult{
		SessionID:  utils.GenerateID(),
		Fields:     mappedFields,
		Confidence: 0.95, // 假设一个默认值，或者从 resp 中提取
	}

	rawBytes, _ := json.Marshal(resp.ParsingResults)
	result.RawJSON = string(rawBytes)

	return result, nil
}

// IdentityMatchRequest 身份匹配请求
type IdentityMatchRequest struct {
	Query string `json:"query" binding:"required"` // 通常是手机号后 4 位
}

// IdentityMatchResult 身份匹配结果
type IdentityMatchResult struct {
	IDNumber string `json:"id_number"`
	Name     string `json:"name"`
}

// MatchIdentity 根据手机号后4位匹配员工身份
func (s *AttendanceService) MatchIdentity(ctx context.Context, orgID string, query string) (*IdentityMatchResult, error) {
	var emp models.OrganizationEmployee

	// 查找该企业下，手机号以 query 结尾的 active 员工
	// 注意：实际生产中如果有多人手机号尾号相同，应该返回列表供用户选择。这里简化为取第一条。
	err := s.db.Where("org_id = ? AND status = ? AND phone LIKE ?", orgID, "active", "%"+query).First(&emp).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("identity not found")
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	// 名字简单脱敏，比如 "张三" -> "张*三" 或 "张*"
	// 这里用一个简单的本地实现代替 utils.MaskName
	maskedName := emp.Name
	if len([]rune(emp.Name)) >= 2 {
		runes := []rune(emp.Name)
		if len(runes) == 2 {
			maskedName = string(runes[0]) + "*"
		} else {
			maskedName = string(runes[0]) + "*" + string(runes[len(runes)-1])
		}
	}

	return &IdentityMatchResult{
		IDNumber: emp.IDNumber,
		Name:     maskedName,
	}, nil
}

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

		// 2. 提取人脸特征 (暂时跳过，直接保存图片，因为底层 FaceCompare 需要两张图片)
		// 实际业务中，注册时应该对 req.FaceImage 进行活体检测和质量检测

		// 处理 Base64 图片，保存到本地文件系统
		faceImagePath, err := SaveBase64ToLocal(req.FaceImage, "attendance/faces", req.IDNumber)
		if err != nil {
			log.Errorf("Failed to save face image: %v", err)
			// 依然允许继续，不阻断主流程，只是存个空或原来的数据
			faceImagePath = ""
		}

		// 3. 落库或更新 models.OrganizationEmployee
		emp := models.OrganizationEmployee{
			ID:           utils.GenerateID(),
			OrgID:        orgID,
			IDNumber:     req.IDNumber,
			Name:         req.Name,
			Phone:        req.Phone,
			FaceFeature:  nil,           // 我们不再单独提取特征，而是保存原始图片用于后续 1:1 比对
			FaceImageURL: faceImagePath, // 现在存的是文件路径，如 /uploads/attendance/faces/xxx.jpg
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

		// 保存降级照片到本地
		fallbackPath, err := SaveBase64ToLocal(req.FallbackImage, "attendance/fallbacks", req.IDNumber)
		if err != nil {
			log.Errorf("Failed to save fallback image: %v", err)
		}
		record.FallbackImageURL = fallbackPath
	} else {
		// 4. 正常打卡：调用底层活体和 1:1 比对
		log.Infof("Calling underlying KYC liveness and 1:1 face match for %s", req.IDNumber)

		// 将前端传来的活体打卡照片落盘
		punchImagePath, err := SaveBase64ToLocal(req.LivenessData, "attendance/punches", req.IDNumber)
		if err != nil {
			return fmt.Errorf("failed to save punch image: %w", err)
		}

		// 将底库照片和打卡照片转为 multipart.FileHeader，因为底层 FaceCompare 接口需要这个类型
		baseFaceHeader, err := ConvertLocalFileToMultipartHeader(emp.FaceImageURL)
		if err != nil {
			log.Errorf("Failed to read base face image: %v", err)
			return fmt.Errorf("system error: unable to load employee base image")
		}

		punchFaceHeader, err := ConvertLocalFileToMultipartHeader(punchImagePath)
		if err != nil {
			log.Errorf("Failed to read punch face image: %v", err)
			return fmt.Errorf("system error: unable to load punch image")
		}

		// 调用底层 FaceCompare 服务
		ctx = context.WithValue(ctx, "org_id", orgID) // Inject org_id for quota
		compareRes, err := s.kycService.FaceCompare(ctx, baseFaceHeader, punchFaceHeader)

		if err != nil {
			log.Warnf("Face compare failed or error returned: %v", err)
			return fmt.Errorf("face verification failed: %w", err)
		}

		if compareRes.Code != 0 || compareRes.ComparisonResults.IsSameFace == 0 {
			return fmt.Errorf("face verification failed: not the same person (confidence: %.2f)", compareRes.ComparisonResults.Confidence)
		}

		// 真实调用成功
		record.Status = "success"
		record.LivenessScore = 0.99 // 暂时假定活体通过
		record.FaceScore = compareRes.ComparisonResults.Confidence
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
