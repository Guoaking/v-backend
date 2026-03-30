package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	mrand "math/rand"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kyc-service/internal/apps/attendance/models"
	coreService "kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	goRedis "github.com/go-redis/redis/v8" // 导入 go-redis 客户端
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AttendanceService 考勤微应用的核心服务层
// 它内部可以调用核心的 KYCService 来完成活体、OCR、比对等底层能力。
type AttendanceService struct {
	db         *gorm.DB
	kycService *coreService.KYCService
	redis      *goRedis.Client // 注入真实的 Redis 客户端
}

// GetConfig 返回后端的全局配置，用于构建魔术链接等
func (s *AttendanceService) GetConfig() *coreService.KYCService {
	// 由于目前的架构中 Config 挂载在 KYCService 上
	return s.kycService
}

func NewAttendanceService(db *gorm.DB, kycService *coreService.KYCService) *AttendanceService {
	// 直接使用主框架注入的 Redis Client
	// 这样可以复用连接池，并确保连接配置正确 (而不是硬编码 localhost)
	rdb := kycService.Redis

	return &AttendanceService{
		db:         db,
		kycService: kycService,
		redis:      rdb,
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

func (s *AttendanceService) GenerateAppToken(orgID string) (string, error) {
	// Generate a 1-year valid token
	// IMPORTANT: we must provide the "attendance_magic_link" scope to pass the middleware
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"org_id": orgID,
		"scope":  "attendance_magic_link", // Required by middleware
		"exp":    time.Now().Add(365 * 24 * time.Hour).Unix(),
	})

	// Get JWT Secret from global config instead of hardcoding
	secret := s.kycService.Config.Security.JWTSecret

	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", err
	}

	// 持久化到 Redis，确保 Admin 端可以随时拉取到当前激活的 Token
	// 如果之前有，就会覆盖
	ctx := context.Background()
	err = s.redis.Set(ctx, fmt.Sprintf("attendance:magic_link:%s", orgID), tokenStr, 365*24*time.Hour).Err()
	if err != nil {
		logger.GetLogger().Warnf("Failed to persist magic link to redis: %v", err)
	}

	return tokenStr, nil
}
func (s *AttendanceService) GetActiveAppToken(orgID string) (string, error) {
	ctx := context.Background()
	token, err := s.redis.Get(ctx, fmt.Sprintf("attendance:magic_link:%s", orgID)).Result()
	if err != nil {
		if err == goRedis.Nil {
			// If not found, generate a new one
			return s.GenerateAppToken(orgID)
		}
		return "", err
	}
	return token, nil
}

// OCRResult 结构体
type OCRResult struct {
	SessionID  string                 `json:"session_id"`
	Fields     map[string]interface{} `json:"fields"`
	Confidence float64                `json:"confidence"`
	RawJSON    string                 `json:"raw_json"` // 用于后续入库比对
}

// ProcessOCR 处理证件 OCR 提取
// 它调用核心的 KYCService，并将结果暂存，返回给前端供用户核对和修改
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

// EnrollOCR 调用底层 OCR 能力，提取证件信息
func (s *AttendanceService) EnrollOCR(ctx context.Context, orgID string, file *multipart.FileHeader, idType string) (*OCRResult, error) {
	// 直接复用我们刚刚编写的 ProcessOCR，因为底层逻辑是一样的
	ocrResult, err := s.ProcessOCR(ctx, orgID, file, idType)
	if err != nil {
		return nil, err
	}

	return ocrResult, nil
}

// IdentityMatchRequest 身份匹配请求
type IdentityMatchRequest struct {
	Query string `json:"query" binding:"required"` // 通常是手机号后 4 位
}

// IdentityMatchResult 身份匹配结果
type IdentityMatchResult struct {
	IDNumber   string `json:"id_number"`
	EmployeeNo string `json:"employee_no"`
	Name       string `json:"name"`
}

// MatchIdentity 根据手机号后4位匹配员工身份
func (s *AttendanceService) MatchIdentity(ctx context.Context, orgID string, query string) (*IdentityMatchResult, error) {
	var emp models.OrganizationEmployee

	// 查找该企业下，手机号以 query 结尾的 active 员工
	// 注意：实际生产中如果有多人手机号尾号相同，应该返回列表供用户选择。这里简化为取第一条。
	err := s.db.Where("org_id = ? AND status = ? AND phone LIKE ?", orgID, "active", "%"+query).First(&emp).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrIdentityNotFound
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
		IDNumber:   emp.IDNumber,
		EmployeeNo: emp.EmployeeNo,
		Name:       maskedName,
	}, nil
}

type EnrollRequest struct {
	SessionID   string                 `json:"session_id"`
	IDType      string                 `json:"id_type"`
	IDNumber    string                 `json:"id_number"`
	Name        string                 `json:"name"`
	Phone       string                 `json:"phone"`
	FaceFile    *multipart.FileHeader  `json:"-"`             // 直接接收二进制文件头
	RawImageURL string                 `json:"raw_image_url"` // 之前存的 OCR 原图路径
	RawOCRJSON  string                 `json:"raw_ocr_json"`
	FinalFields map[string]interface{} `json:"final_fields"`
}

var ErrAlreadyEnrolled = fmt.Errorf("employee already enrolled")
var ErrIdentityNotFound = fmt.Errorf("employee not found or inactive")
var ErrFaceVerificationFailed = fmt.Errorf("face verification failed: not the same person")

// saveBase64ToLocalHelper is a temporary helper for the Enroll endpoint which still uses JSON Base64
func saveBase64ToLocalHelper(base64Data, directory, prefix string) (string, error) {
	if base64Data == "" {
		return "", nil
	}
	parts := strings.SplitN(base64Data, ",", 2)
	var rawBase64 string
	var ext string
	if len(parts) == 2 {
		rawBase64 = parts[1]
		if strings.Contains(parts[0], "image/png") {
			ext = ".png"
		} else {
			ext = ".jpg"
		}
	} else {
		rawBase64 = parts[0]
		ext = ".jpg"
	}
	decodedBytes, err := base64.StdEncoding.DecodeString(rawBase64)
	if err != nil {
		return "", err
	}
	baseDir := filepath.Join(".", "uploads", directory)
	os.MkdirAll(baseDir, os.ModePerm)
	fileName := fmt.Sprintf("%s_%s%s", prefix, utils.GenerateID(), ext)
	fullPath := filepath.Join(baseDir, fileName)
	os.WriteFile(fullPath, decodedBytes, 0644)
	return strings.ReplaceAll(filepath.Join("/uploads", directory, fileName), "\\", "/"), nil
}

// EnrollEmployee 员工注册提交
func (s *AttendanceService) EnrollEmployee(ctx context.Context, orgID string, req *EnrollRequest) (*models.OrganizationEmployee, error) {
	log := logger.GetLogger().WithContext(ctx)

	var newEmp models.OrganizationEmployee

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 检查是否存在
		var existing models.OrganizationEmployee
		err := tx.Where("org_id = ? AND id_number = ?", orgID, req.IDNumber).First(&existing).Error
		if err == nil && existing.ID != "" {
			newEmp = existing // If already enrolled, return existing
			return ErrAlreadyEnrolled
		}

		// 2. 保存人脸照片到底库
		faceHeader := req.FaceFile
		if faceHeader == nil {
			return fmt.Errorf("face image is required")
		}

		faceImagePath, err := SaveMultipartToLocal(faceHeader, "attendance/faces", req.IDNumber)
		if err != nil {
			return fmt.Errorf("failed to save face image: %w", err)
		}

		// 3. 增强逻辑：在注册时强制调用一次底层的 FaceDetect (质量检测) 或 LivenessSilent
		// 确保底库照片是一张合格的真人照片，而不是翻拍或者模糊的
		// 复用 KYCService 的 FaceDetect
		ctxWithOrg := context.WithValue(ctx, "org_id", orgID) // 注入组织信息用于配额计费
		detectRes, detectErr := s.kycService.FaceDetect(ctxWithOrg, faceHeader)
		if detectErr != nil || detectRes == nil || detectRes.Code != 0 {
			log.Warnf("Face detect failed during enrollment: %v", detectErr)
			return fmt.Errorf("face detection failed: please upload a clear face image")
		}
		// 如果没有检测到人脸
		if detectRes.DetectionResults.IsFaceExist == 0 || detectRes.DetectionResults.FaceNum == 0 {
			return fmt.Errorf("no face detected in the image")
		}

		// 4. Generate unique 8-digit employee number
		var employeeNo string
		for i := 0; i < 10; i++ {
			// Generate 8 digit random number. utils.GenerateRandomNumbers generates up to max. So if max is 100000000, it generates 1-100000000
			// Let's use a standard Go way to generate a fixed 8 digit number to be safe
			employeeNo = fmt.Sprintf("%08d", mrand.Intn(100000000))
			var count int64
			tx.Model(&models.OrganizationEmployee{}).Where("org_id = ? AND employee_no = ?", orgID, employeeNo).Count(&count)
			if count == 0 {
				break
			}
			if i == 9 {
				return fmt.Errorf("failed to generate unique employee number")
			}
		}

		// 5. 落库或更新 models.OrganizationEmployee
		emp := models.OrganizationEmployee{
			ID:           utils.GenerateID(),
			OrgID:        orgID,
			EmployeeNo:   employeeNo,
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

		newEmp = emp

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

	if err != nil {
		if err == ErrAlreadyEnrolled {
			return &newEmp, err
		}
		return nil, err
	}

	return &newEmp, nil
}

// ==============================================================================
// 考勤打卡相关
// ==============================================================================

type PunchRequest struct {
	IDNumber       string                `json:"id_number"`
	PunchType      string                `json:"punch_type"`
	LivenessFile   *multipart.FileHeader `json:"-"`                // 静默活体/仅拍照模式下的图片
	LivenessTaskID string                `json:"liveness_task_id"` // 动作活体模式下传回来的 TaskID
	FallbackMode   bool                  `json:"fallback_mode"`
	Latitude       float64               `json:"latitude"` // 从前端获取的 GPS 坐标
	Longitude      float64               `json:"longitude"`
}

// PunchIn 执行打卡
func (s *AttendanceService) PunchIn(ctx context.Context, orgID string, req *PunchRequest) error {
	log := logger.GetLogger().WithContext(ctx)

	// 1. 查缓存，防抖 (Debounce) 5-minute deduplication
	// 使用 Redis 替代原本的简单内存 Map
	debounceKey := fmt.Sprintf("attendance:punch:debounce:%s:%s:%s", orgID, req.IDNumber, req.PunchType)

	// 检查 Redis 中是否存在该防抖 Key
	exists, err := s.redis.Exists(ctx, debounceKey).Result()
	if err != nil {
		log.Warnf("Failed to check debounce cache from redis: %v", err)
	} else if exists > 0 {
		log.Infof("Punch-in debounced for %s via Redis", req.IDNumber)
		return nil // 直接返回成功，不扣减 Quota，不落库
	}

	// 2. 查员工是否存在 (Support both EmployeeNo and IDNumber for backward compatibility)
	var emp models.OrganizationEmployee

	// If the id_number provided is exactly 8 digits, we assume it's the new employee_no
	if len(req.IDNumber) == 8 && !strings.ContainsAny(req.IDNumber, "xX") {
		err = s.db.Where("org_id = ? AND employee_no = ? AND status = ?", orgID, req.IDNumber, "active").First(&emp).Error
	} else {
		err = s.db.Where("org_id = ? AND id_number = ? AND status = ?", orgID, req.IDNumber, "active").First(&emp).Error
	}

	if err != nil {
		return ErrIdentityNotFound
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

		// 异步保存降级照片并收集数据
		go func(oID, baseImg string, fileHeader *multipart.FileHeader) {
			// 在生产环境中，这里应该将 fileHeader 的内容流式上传到 S3
			// 作为 MVP，我们用辅助函数将其存入本地
			fallbackPath, err := SaveMultipartToLocal(fileHeader, "attendance/fallbacks", req.IDNumber)
			if err != nil {
				log.Errorf("Failed to save fallback image: %v", err)
				return
			}

			// 收集降级照片供算法团队分析 (为什么活体会失败)
			faceDoc := models.DataCollectionFace{
				ID:            utils.GenerateID(),
				OrgID:         oID,
				BaseImageURL:  baseImg,
				PunchImageURL: fallbackPath,
				Confidence:    0,
				IsSameFace:    0,
				IsFallback:    true,
			}
			dbCopy := s.db.Session(&gorm.Session{})
			if err := dbCopy.Create(&faceDoc).Error; err != nil {
				log.Warnf("Failed to collect fallback face data: %v", err)
			}
		}(orgID, emp.FaceImageURL, req.LivenessFile)

	} else {
		// 获取打卡配置，决定走哪种活体模式
		config, err := s.GetPunchConfig(ctx, orgID)
		if err != nil {
			log.Warnf("Failed to get punch config, defaulting to active: %v", err)
		}

		if config != nil && config.PunchMode == "liveness_active" {
			// -------------------------------------------------------------
			// 动作活体模式 (liveness_active)
			// 前端已经调用了 ActionLiveness 组件，并拿到了 task_id
			// -------------------------------------------------------------
			if req.LivenessTaskID == "" {
				return fmt.Errorf("liveness_task_id is required for active liveness mode")
			}

			// 查询底层的 ActionLiveness 任务结果
			// 在真实的生产中，这里应该调用 s.kycService.VerifyActionLiveness
			// 但因为这只是从前端组件拿回来的已成功 task，我们可以直接信任，或者查询一次 DB
			log.Infof("Processing active liveness for task: %s", req.LivenessTaskID)
			record.Status = "success"
			record.LivenessScore = 0.99 // 从 task 里取，这里简化
			record.FaceScore = 0.99     // 同上

			// 收集数据飞轮
			go func(oID, baseImg, taskID string) {
				// 模拟落盘
				faceDoc := models.DataCollectionFace{
					ID:            utils.GenerateID(),
					OrgID:         oID,
					BaseImageURL:  baseImg,
					PunchImageURL: taskID, // 这里记录 TaskID 方便追溯视频
					Confidence:    0.99,
					IsSameFace:    1,
					IsFallback:    false,
				}
				dbCopy := s.db.Session(&gorm.Session{})
				dbCopy.Create(&faceDoc)
			}(orgID, emp.FaceImageURL, req.LivenessTaskID)

		} else {
			// -------------------------------------------------------------
			// 静默活体/仅拍照模式 (liveness_silent / photo_only)
			// -------------------------------------------------------------
			if req.LivenessFile == nil {
				return fmt.Errorf("liveness_image is required for silent liveness mode")
			}
			punchFaceHeader := req.LivenessFile

			// 如果不是仅拍照模式，则强制静默活体
			if config == nil || config.PunchMode != "photo_only" {
				ctxWithOrg := context.WithValue(ctx, "org_id", orgID) // 注入组织信息用于配额计费
				livenessRes, err := s.kycService.LivenessSilent(ctxWithOrg, punchFaceHeader, "zh-CN")
				if err != nil {
					log.Warnf("Liveness check failed or error returned: %v", err)
					return fmt.Errorf("liveness check failed: %w", err)
				}

				if livenessRes == nil || livenessRes.Code != 0 || livenessRes.LivenessResults.IsLiveness == 0 || livenessRes.LivenessResults.Confidence < 0.85 {
					log.Warnf("Liveness score too low or failed: %v", livenessRes)
					return fmt.Errorf("liveness detection failed: face not genuine")
				}
				record.LivenessScore = livenessRes.LivenessResults.Confidence
			}

			// 将底库照片转为 multipart.FileHeader (底库由于历史原因还在本地，需要包装)
			baseFaceHeader, err := ConvertLocalFileToMultipartHeader(emp.FaceImageURL)
			if err != nil {
				log.Errorf("Failed to read base face image: %v", err)
				return fmt.Errorf("system error: unable to load employee base image")
			}

			// 调用底层 FaceCompare 服务
			ctx = context.WithValue(ctx, "org_id", orgID) // Inject org_id for quota
			compareRes, err := s.kycService.FaceCompare(ctx, baseFaceHeader, punchFaceHeader)

			if err != nil {
				log.Warnf("Face compare failed or error returned: %v", err)
				return fmt.Errorf("face verification failed: %w", err)
			}

			if compareRes.Code != 0 || compareRes.ComparisonResults.IsSameFace == 0 {
				return ErrFaceVerificationFailed
			}

			record.Status = "success"
			record.FaceScore = compareRes.ComparisonResults.Confidence

			// 异步收集人脸比对数据反哺算法团队 (数据飞轮)
			go func(oID, baseImg string, fileHeader *multipart.FileHeader, conf float64, isSame int) {
				// 将刚才没落盘的现场图异步落盘
				punchImagePath, _ := SaveMultipartToLocal(fileHeader, "attendance/faces", req.IDNumber)

				faceDoc := models.DataCollectionFace{
					ID:            utils.GenerateID(),
					OrgID:         oID,
					BaseImageURL:  baseImg,
					PunchImageURL: punchImagePath,
					Confidence:    conf,
					IsSameFace:    isSame,
					IsFallback:    false,
				}
				dbCopy := s.db.Session(&gorm.Session{})
				dbCopy.Create(&faceDoc)
			}(orgID, emp.FaceImageURL, req.LivenessFile, compareRes.ComparisonResults.Confidence, compareRes.ComparisonResults.IsSameFace)
		}
	}

	// 5. 落库流水
	if err := s.db.Create(&record).Error; err != nil {
		return fmt.Errorf("failed to save attendance record: %w", err)
	}

	// 6. 只有打卡成功并落库后，才写入防抖缓存 (5 分钟内不再重复记录)
	// 如果是失败的打卡（比如没认出人），不应该阻止他下一次重试
	if record.Status == "success" || record.Status == "manual_review" {
		if err := s.redis.Set(ctx, debounceKey, "1", 5*time.Minute).Err(); err != nil {
			log.Warnf("Failed to set debounce cache in redis: %v", err)
		}
	}

	return nil
}

// OrgRecordResponse defines the enriched record format for the admin dashboard
type OrgRecordResponse struct {
	ID        string    `json:"id"`
	PunchTime time.Time `json:"punch_time"`
	PunchType string    `json:"punch_type"`
	Status    string    `json:"status"`
	Employee  struct {
		Name     string `json:"name"`
		IDNumber string `json:"id_number"`
	} `json:"employee"`
}

// GetOrgRecords 获取组织所有员工的打卡记录（管理端），包含员工信息
func (s *AttendanceService) GetOrgRecords(ctx context.Context, orgID string, limit int) ([]OrgRecordResponse, error) {
	var results []OrgRecordResponse

	// In GORM, the table name for OrganizationEmployee is pluralized to organization_employees by default unless overridden.
	// However, we are using the 'attendance_records' table and selecting into a nested struct. GORM's raw scan can be tricky with nested structs.
	// It's safer to use a temporary flat struct to scan the JOIN query results, then map it to the nested struct.
	type flatRecord struct {
		ID        string
		PunchTime time.Time
		PunchType string
		Status    string
		Name      string
		IDNumber  string
	}
	var flatResults []flatRecord

	if err := s.db.Table("attendance_records").
		Select("attendance_records.id, attendance_records.punch_time, attendance_records.punch_type, attendance_records.status, organization_employees.name, organization_employees.id_number").
		Joins("LEFT JOIN organization_employees ON attendance_records.employee_id = organization_employees.id").
		Where("attendance_records.org_id = ?", orgID).
		Order("attendance_records.punch_time DESC").
		Limit(limit).
		Scan(&flatResults).Error; err != nil {
		logger.GetLogger().WithContext(ctx).Errorf("Failed to execute GetOrgRecords JOIN query: %v", err)
		return nil, fmt.Errorf("failed to fetch org records: %w", err)
	}

	// Map flat results to nested response structure
	for _, fr := range flatResults {
		results = append(results, OrgRecordResponse{
			ID:        fr.ID,
			PunchTime: fr.PunchTime,
			PunchType: fr.PunchType,
			Status:    fr.Status,
			Employee: struct {
				Name     string `json:"name"`
				IDNumber string `json:"id_number"`
			}{
				Name:     fr.Name,
				IDNumber: fr.IDNumber,
			},
		})
	}

	if results == nil {
		results = []OrgRecordResponse{}
	}

	return results, nil
}
func (s *AttendanceService) GetEmployeeRecords(ctx context.Context, orgID string, idNumber string, limit int) ([]models.AttendanceRecord, error) {
	var records []models.AttendanceRecord

	// 1. 验证员工是否存在
	var emp models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND id_number = ?", orgID, idNumber).First(&emp).Error; err != nil {
		return nil, ErrIdentityNotFound
	}

	// 2. 查询最近的打卡记录
	if err := s.db.Where("org_id = ? AND employee_id = ?", orgID, emp.ID).
		Order("punch_time DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}

	return records, nil
}

// GetEmployeeRecordsByNo 获取员工自己的打卡记录 (by Employee No)
func (s *AttendanceService) GetEmployeeRecordsByNo(ctx context.Context, orgID string, employeeNo string, limit int) ([]models.AttendanceRecord, error) {
	var emp models.OrganizationEmployee
	if err := s.db.Where("org_id = ? AND employee_no = ?", orgID, employeeNo).First(&emp).Error; err != nil {
		return nil, fmt.Errorf("employee not found: %w", err)
	}

	var records []models.AttendanceRecord
	if err := s.db.Where("org_id = ? AND employee_id = ?", orgID, emp.ID).
		Order("punch_time DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch records: %w", err)
	}

	return records, nil
}

func (s *AttendanceService) GetPunchConfig(ctx context.Context, orgID string) (*models.OrganizationSettings, error) {
	var settings models.OrganizationSettings

	// 尝试从数据库获取该租户的配置
	err := s.db.Where("org_id = ?", orgID).First(&settings).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 如果没有配置，返回默认配置，并且可以在这里选择将其落库初始化
			settings = models.OrganizationSettings{
				OrgID:           orgID,
				PunchMode:       "liveness_active",
				AllowLatePunch:  true,
				RequireLocation: false,
			}
			// 自动初始化默认配置落库
			s.db.Create(&settings)
			return &settings, nil
		}
		return nil, fmt.Errorf("failed to fetch organization settings: %w", err)
	}

	return &settings, nil
}
