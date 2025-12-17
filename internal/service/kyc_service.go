package service

import (
    "context"
    "fmt"
    "mime/multipart"
    "net/http"
    "time"

    "kyc-service/internal/config"
    "kyc-service/internal/models"
    "kyc-service/pkg/crypto"
    "kyc-service/pkg/httpclient"
    "kyc-service/pkg/logger"
    "kyc-service/pkg/metrics"
    "kyc-service/pkg/tracing"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
)

type KYCService struct {
	DB         *gorm.DB
	Redis      *redis.Client
	Config     *config.Config
	Encryptor  *crypto.Encryptor
	Upgrader   websocket.Upgrader
	HTTPClient *httpclient.Client // 新增HTTP客户端

	// OTel指标
	ocrSuccessRate        metric.Float64Gauge
	faceVerifySuccessRate metric.Float64Gauge
	livenessSuccessRate   metric.Float64Gauge
	kycSuccessRate        metric.Float64Gauge
	kycProcessingTime     metric.Float64Histogram
}

func NewKYCService(db *gorm.DB, redis *redis.Client, cfg *config.Config) *KYCService {
	encryptor, _ := crypto.NewEncryptor(cfg.Security.EncryptionKey)

	// 配置HTTP客户端
	httpClient := httpclient.New(httpclient.Config{
		Timeout: 120 * time.Second,
		//RetryCount:    1,
		RetryInterval: 3 * time.Second,
		Logger:        logger.GetLogger(),
	})

	service := &KYCService{
		DB:         db,
		Redis:      redis,
		Config:     cfg,
		Encryptor:  encryptor,
		HTTPClient: httpClient, // 初始化HTTP客户端
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 生产环境需要配置具体的域名
			},
		},
	}

	// 初始化OTel指标
	if cfg.Monitoring.Metrics.Enabled {
		meter := tracing.GetMeter()

		// OCR成功率
		ocrSuccessRate, _ := meter.Float64Gauge("business_ocr_success_rate",
			metric.WithDescription("OCR success rate"),
			metric.WithUnit("1"),
		)
		service.ocrSuccessRate = ocrSuccessRate

		// 人脸识别成功率
		faceVerifySuccessRate, _ := meter.Float64Gauge("business_face_verify_success_rate",
			metric.WithDescription("Face verification success rate"),
			metric.WithUnit("1"),
		)
		service.faceVerifySuccessRate = faceVerifySuccessRate

		// 活体检测成功率
		livenessSuccessRate, _ := meter.Float64Gauge("business_liveness_success_rate",
			metric.WithDescription("Liveness detection success rate"),
			metric.WithUnit("1"),
		)
		service.livenessSuccessRate = livenessSuccessRate

		// KYC成功率
		kycSuccessRate, _ := meter.Float64Gauge("business_kyc_success_rate",
			metric.WithDescription("KYC success rate"),
			metric.WithUnit("1"),
		)
		service.kycSuccessRate = kycSuccessRate

		// KYC处理时间
		kycProcessingTime, _ := meter.Float64Histogram("business_kyc_processing_time_seconds",
			metric.WithDescription("KYC processing time in seconds"),
			metric.WithUnit("s"),
		)
		service.kycProcessingTime = kycProcessingTime
	}

	return service
}

// OCR 类型已迁移至 ocr_service.go

// Face 类型已迁移至 face_service.go

// LivenessRequest 活体检测请求
type LivenessRequest struct {
	Action string `json:"action"` // blink, nod, smile
}

// LivenessResponse 活体检测响应
type LivenessResponse struct {
	Success bool    `json:"success"`
	Score   float64 `json:"score,omitempty"`
	Error   string  `json:"error,omitempty"`
}

// StandardLivenessResponse 标准活体检测响应
type StandardLivenessResponse struct {
	Success        bool    `json:"success"`
	Score          float64 `json:"score"`
	Action         string  `json:"action"`
	Confidence     float64 `json:"confidence"`
	ProcessingTime int64   `json:"processing_time_ms"`
	IsLive         bool    `json:"is_live"`
}

// CompleteKYCRequest 完整KYC请求
type CompleteKYCRequest struct {
	IDCardImage *multipart.FileHeader `form:"idcard_image" binding:"required"`
	FaceImage   *multipart.FileHeader `form:"face_image" binding:"required"`
	Name        string                `form:"name" binding:"required"`
	IDCard      string                `form:"idcard" binding:"required"`
	Phone       string                `form:"phone"`
}

// CompleteKYCResponse 完整KYC响应
type CompleteKYCResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// StandardCompleteKYCResponse 标准完整KYC响应
type StandardCompleteKYCResponse struct {
	RequestID       string    `json:"request_id"`
	Status          string    `json:"status"`
	Message         string    `json:"message"`
	OCRScore        float64   `json:"ocr_score"`
	FaceVerifyScore float64   `json:"face_verify_score"`
	Confidence      float64   `json:"confidence"`
	ProcessingTime  int64     `json:"processing_time_ms"`
	CompletedAt     time.Time `json:"completed_at"`
}

// OCR 方法已迁移至 ocr_service.go

// FaceVerify 执行人脸识别
// FaceVerify 已迁移至 face_service.go

// FaceSearch 已迁移至 face_service.go

// FaceCompare 已迁移至 face_service.go

// FaceDetect 已迁移至 face_service.go

// LivenessWebSocket 处理活体检测WebSocket连接
func (s *KYCService) LivenessWebSocket(ctx context.Context, conn *websocket.Conn) error {
	ctx, span := tracing.StartSpan(ctx, "KYCService.LivenessWebSocket")
	defer span.End()

	defer conn.Close()

	// 创建KYC请求记录
	kycRequest := &models.KYCRequest{
		ID:          uuid.New().String(),
		UserID:      getUserID(ctx),
		RequestType: "liveness",
		Status:      "processing",
		IPAddress:   getClientIP(ctx),
		UserAgent:   getUserAgent(ctx),
	}

	if err := s.DB.Create(kycRequest).Error; err != nil {
		return err
	}

	// WebSocket消息处理循环
	for {
		var req LivenessRequest
		if err := conn.ReadJSON(&req); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.GetLogger().WithError(err).Error("WebSocket连接异常关闭")
			}
			break
		}

		start := time.Now()

		// 处理活体检测请求
		result, err := s.processLiveness(ctx, &req)
		if err != nil {
			conn.WriteJSON(LivenessResponse{
				Success: false,
				Error:   err.Error(),
			})
			continue
		}

		if err := conn.WriteJSON(result); err != nil {
			logger.GetLogger().WithError(err).Error("WebSocket写入失败")
			break
		}

		// 如果检测成功，更新状态
		if result.Success {
			kycRequest.Status = "success"
			kycRequest.Result = fmt.Sprintf("score:%f", result.Score)
			s.DB.Save(kycRequest)

			// 记录成功指标
			if s.livenessSuccessRate != nil {
				s.livenessSuccessRate.Record(ctx, 1.0)
			}
			if s.kycProcessingTime != nil {
				s.kycProcessingTime.Record(ctx, time.Since(start).Seconds())
			}
		} else {
			// 记录失败指标
			if s.livenessSuccessRate != nil {
				s.livenessSuccessRate.Record(ctx, 0.0)
			}
		}
	}

	return nil
}

// CompleteKYC 执行完整KYC流程
func (s *KYCService) CompleteKYC(ctx context.Context, req *CompleteKYCRequest) (*CompleteKYCResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.CompleteKYC")
	defer span.End()

	start := time.Now()
	defer func() {
		if s.kycProcessingTime != nil {
			s.kycProcessingTime.Record(ctx, time.Since(start).Seconds())
		}
	}()

	// 创建KYC请求记录
	kycRequest := &models.KYCRequest{
		ID:          uuid.New().String(),
		UserID:      getUserID(ctx),
		RequestType: "complete",
		Status:      "processing",
		IPAddress:   getClientIP(ctx),
		UserAgent:   getUserAgent(ctx),
	}

	// 加密敏感数据
	encryptedIDCard, _ := s.Encryptor.Encrypt(req.IDCard)
	encryptedName, _ := s.Encryptor.Encrypt(req.Name)
	encryptedPhone, _ := s.Encryptor.Encrypt(req.Phone)

	kycRequest.IDCard = encryptedIDCard
	kycRequest.Name = encryptedName
	kycRequest.Phone = encryptedPhone
	kycRequest.IDCardHash = crypto.HashIDCard(req.IDCard)

	if err := s.DB.Create(kycRequest).Error; err != nil {
		return nil, fmt.Errorf("创建请求失败")
	}

	// 执行OCR识别
	ocrResult, err := s.callOCRService(ctx, &OCRRequest{Picture: req.IDCardImage})
	if err != nil || ocrResult.Code != 0 {
		kycRequest.Status = "failed"
		kycRequest.ErrorMessage = "ocr task error"
		s.DB.Save(kycRequest)

		// 记录失败指标
		if s.kycSuccessRate != nil {
			s.kycSuccessRate.Record(ctx, 0.0)
		}

		return &CompleteKYCResponse{
			RequestID: kycRequest.ID,
			Status:    "failed",
			Message:   "身份证识别失败",
		}, nil
	}

	// 验证身份信息
	if ocrResult.Filename != req.Name {
		kycRequest.Status = "failed"
		kycRequest.ErrorMessage = "身份信息不匹配"
		s.DB.Save(kycRequest)

		// 记录失败指标
		if s.kycSuccessRate != nil {
			s.kycSuccessRate.Record(ctx, 0.0)
		}

		return &CompleteKYCResponse{
			RequestID: kycRequest.ID,
			Status:    "failed",
			Message:   "身份信息不匹配",
		}, nil
	}

	// 执行人脸识别
	//faceResult, err := s.callFaceService(ctx, req.IDCardImage, req.FaceImage)
	//if err != nil || !faceResult.Success || faceResult.Score < 0.8 {
	//	kycRequest.Status = "failed"
	//	kycRequest.ErrorMessage = "人脸识别失败"
	//	s.DB.Save(kycRequest)
	//
	//	// 记录失败指标
	//	if s.kycSuccessRate != nil {
	//		s.kycSuccessRate.Record(ctx, 0.0)
	//	}
	//
	//	return &CompleteKYCResponse{
	//		RequestID: kycRequest.ID,
	//		Status:    "failed",
	//		Message:   "人脸识别失败",
	//	}, nil
	//}

	// 更新状态为成功
	kycRequest.Status = "success"
	//kycRequest.Result = fmt.Sprintf("score:%f", faceResult.Score)
	s.DB.Save(kycRequest)

	// 记录成功指标
	if s.kycSuccessRate != nil {
		s.kycSuccessRate.Record(ctx, 1.0)
	}

	return &CompleteKYCResponse{
		RequestID: kycRequest.ID,
		Status:    "success",
		Message:   "KYC认证成功",
	}, nil
}

// GetKYCStatus 获取KYC状态
func (s *KYCService) GetKYCStatus(ctx context.Context, requestID string) (*models.KYCRequest, error) {
	var kycRequest models.KYCRequest
	if err := s.DB.Where("id = ?", requestID).First(&kycRequest).Error; err != nil {
		return nil, fmt.Errorf("请求不存在")
	}

	return &kycRequest, nil
}

// callOCRService 已迁移至 ocr_service.go

// 调用第三方人脸识别服务
// callFaceService 已迁移至 face_service.go

// 处理活体检测
func (s *KYCService) processLiveness(ctx context.Context, req *LivenessRequest) (*LivenessResponse, error) {
	// 模拟活体检测处理
	// 实际应该调用第三方服务
	return &LivenessResponse{
		Success: true,
		Score:   0.98,
	}, nil
}

// 辅助函数
func getUserID(ctx context.Context) string {
	if userID, ok := ctx.Value("user_id").(string); ok {
		return userID
	}
	return "unknown"
}

func getOrgID(ctx context.Context) string {
	if v, ok := ctx.Value("org_id").(string); ok {
		return v
	}
	return ""
}

func getClientIP(ctx context.Context) string {
	if ip, ok := ctx.Value("client_ip").(string); ok {
		return ip
	}
	return "unknown"
}

func getUserAgent(ctx context.Context) string {
	if ua, ok := ctx.Value("user_agent").(string); ok {
		return ua
	}
	return "unknown"
}

// RecordAuditLog 记录审计日志
func (s *KYCService) RecordAuditLog(ctx context.Context, action, resource, resourceID, status, message string) {
	userID := getUserID(ctx)
	ip := getClientIP(ctx)
	userAgent := getUserAgent(ctx)
	requestID := ""
	if reqID, ok := ctx.Value("request_id").(string); ok {
		requestID = reqID
	}

	auditLog := &models.AuditLog{
		RequestID: requestID,
		UserID:    userID,
		Action:    action,
		Resource:  resource,
		IP:        ip,
		UserAgent: userAgent,
		Status:    status,
		Message:   message,
	}

    if err := s.DB.Create(auditLog).Error; err != nil {
        logger.GetLogger().WithError(err).Error("记录审计日志失败")
    }
    metrics.RecordAuditEvent(ctx, action, resource, status)
}

// 用户管理相关方法

// GetUserByEmail 根据邮箱获取用户
func (s *KYCService) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	if err := s.DB.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByID 根据ID获取用户
func (s *KYCService) GetUserByID(id string) (*models.User, error) {
	var user models.User
	if err := s.DB.Where("id = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateUser 创建用户
func (s *KYCService) CreateUser(user *models.User) error {
	return s.DB.Create(user).Error
}

// CreateAuditLog 创建审计日志
func (s *KYCService) CreateAuditLog(auditLog *models.AuditLog) error {
	return s.DB.Create(auditLog).Error
}

// 组织管理相关方法

// CreateOrganization 创建组织
func (s *KYCService) CreateOrganization(org *models.Organization) error {
	if err := s.DB.Create(org).Error; err != nil {
		return err
	}
	_ = s.SyncOrganizationQuotasWithPolicy(org.ID, org.PlanID, true)
	return nil
}

// GetOrganizationByID 根据ID获取组织
func (s *KYCService) GetOrganizationByID(id string) (*models.Organization, error) {
	var org models.Organization
	if err := s.DB.Where("id = ?", id).First(&org).Error; err != nil {
		return nil, err
	}
	return &org, nil
}

// UpdateOrganization 更新组织
func (s *KYCService) UpdateOrganization(org *models.Organization) error {
	return s.DB.Save(org).Error
}

// CreateOrganizationMember 创建组织成员
func (s *KYCService) CreateOrganizationMember(member *models.OrganizationMember) error {
	return s.DB.Create(member).Error
}

// GetOrganizationMemberByUserID 根据用户ID获取组织成员关系
func (s *KYCService) GetOrganizationMemberByUserID(userID string) (*models.OrganizationMember, error) {
	var member models.OrganizationMember
	if err := s.DB.Where("user_id = ? AND status = ?", userID, "active").First(&member).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

// GetOrganizationMemberByID 根据ID获取组织成员
func (s *KYCService) GetOrganizationMemberByID(id string) (*models.OrganizationMember, error) {
	var member models.OrganizationMember
	if err := s.DB.Where("id = ?", id).First(&member).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

// GetOrganizationMembers 获取组织成员列表
func (s *KYCService) GetOrganizationMembers(orgID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	if err := s.DB.Where("organization_id = ?", orgID).Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// DeleteOrganizationMember 删除组织成员
func (s *KYCService) DeleteOrganizationMember(member *models.OrganizationMember) error {
	return s.DB.Delete(member).Error
}

// UpdateOrganizationMember 更新组织成员
func (s *KYCService) UpdateOrganizationMember(member *models.OrganizationMember) error {
	return s.DB.Save(member).Error
}

// API密钥管理相关方法

// GetAPIKeysByUserID 根据用户ID获取API密钥列表
func (s *KYCService) GetAPIKeysByUserID(userID string) ([]models.APIKey, error) {
	var keys []models.APIKey
	if err := s.DB.Where("user_id = ? AND status = ?", userID, "active").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

// GetAPIKeyByID 根据ID获取API密钥
func (s *KYCService) GetAPIKeyByID(id string) (*models.APIKey, error) {
	var key models.APIKey
	if err := s.DB.Where("id = ?", id).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// CreateAPIKey 创建API密钥
func (s *KYCService) CreateAPIKey(key *models.APIKey) error {
	return s.DB.Create(key).Error
}

// UpdateAPIKey 更新API密钥
func (s *KYCService) UpdateAPIKey(key *models.APIKey) error {
	return s.DB.Save(key).Error
}

// GetAPIKeyBySecretHash 根据密钥哈希获取API密钥
func (s *KYCService) GetAPIKeyBySecretHash(hash string) (*models.APIKey, error) {
	var key models.APIKey
	if err := s.DB.Where("secret_hash = ?", hash).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// LivenessVideo migrated to liveness_service.go

// LivenessSilent migrated to liveness_service.go
