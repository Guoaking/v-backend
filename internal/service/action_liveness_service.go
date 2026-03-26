package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"mime/multipart"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/utils"

	"gorm.io/datatypes"
)

type ActionSubmitResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		ExternalID string `json:"external_id"`
	} `json:"data"`
}

type ActionVerifyResult struct {
	SessionID string              `json:"session_id"`
	Status    string              `json:"status"`
	Details   *ActionLivenessData `json:"details,omitempty"`
}

func (s *KYCService) CreateSession(ctx context.Context) (string, []string, error) {
	orgID := getOrgID(ctx)
	if orgID == "" {
		return "", nil, fmt.Errorf("缺少组织信息")
	}
	sid := utils.GenerateID()

	// Generate random actions
	pool := []string{"blink", "mouth_open", "nod", "nod_down", "nod_up", "shake_head", "turn_left", "turn_right"}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	actions := pool[:2]

	actionsJSON, _ := json.Marshal(actions)

	task := &models.LivenessTask{
		ID:             utils.GenerateID(),
		OrganizationID: orgID,
		SessionID:      sid,
		Status:         "created",
		Actions:        datatypes.JSON(actionsJSON),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := s.DB.Create(task).Error; err != nil {
		return "", nil, err
	}

	// Create Audit/KYC Request Record
	kycRequest := &models.KYCRequest{
		ID:          utils.GenerateID(),
		UserID:      getUserID(ctx), // May be empty if API key used, logic inside getUserID handles it
		RequestType: "action_liveness",
		Status:      "pending",
		IPAddress:   getClientIP(ctx),
		UserAgent:   getUserAgent(ctx),
		Result:      fmt.Sprintf(`{"session_id": "%s"}`, sid),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	// Best effort to save KYC request, log error if fails but don't block flow
	if err := s.DB.Create(kycRequest).Error; err != nil {
		logger.GetLogger().WithError(err).Warn("Failed to create KYC request record for action liveness")
	}

	logger.GetLogger().WithField("session_id", sid).Info("Action liveness session created")
	s.RecordAuditLog(ctx, "liveness.action.session.create", "liveness_action", task.ID, "success", "")
	return sid, actions, nil
}

func (s *KYCService) UploadVideo(ctx context.Context, sessionID string, file *multipart.FileHeader) (*models.LivenessTask, error) {
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("缺少组织信息")
	}
	var task models.LivenessTask
	if err := s.DB.Where("organization_id = ? AND session_id = ?", orgID, sessionID).First(&task).Error; err != nil {
		return nil, fmt.Errorf("会话不存在")
	}

	// 1. Quota Check & Consumption
	// Using "action_liveness" service type. Ensure this is configured in DB/Plan.
	serviceType := "liveness"
	err := s.checkAndConsumeQuota(ctx, orgID, serviceType, func() error {
		// 2. Ingest Video
		asset, err := s.IngestVideo(ctx, orgID, sessionID, file)
		if err != nil {
			return err
		}
		task.VideoAssetID = &asset.ID
		task.Status = "uploaded"
		task.UpdatedAt = time.Now()
		if err := s.DB.Save(&task).Error; err != nil {
			return err
		}

		// 3. Submit to ThirdParty (Within Quota Transaction)
		updatedTask, err := s.SubmitThirdParty(ctx, sessionID)
		if err != nil {
			return err
		}
		task = *updatedTask

		// 4. Sensitive Data Audit (Video contains face)
		metrics.RecordSensitiveDataAccess(ctx, "liveness_video", getOrgID(ctx), true, "UploadVideo")

		// 5. Update KYCRequest Status
		go func() {
			s.DB.Model(&models.KYCRequest{}).
				Where("result LIKE ?", fmt.Sprintf("%%%s%%", sessionID)).
				Update("status", "processing")
		}()

		logger.GetLogger().WithFields(map[string]interface{}{"session_id": sessionID, "video_asset_id": asset.ID}).Info("Action liveness video uploaded and submitted")
		return nil
	})

	if err != nil {
		// 统一错误处理，确保Quota中间件能识别
		if strings.Contains(err.Error(), "QUOTA_EXCEEDED") {
			return nil, fmt.Errorf("Quota exceeded. Please upgrade your plan.")
		}
		return nil, err
	}

	metrics.RecordBusinessOperation(ctx, "action_liveness_upload", true, 0, "")
	s.RecordAuditLog(ctx, "liveness.action.upload", "liveness_action", task.ID, "success", "")
	return &task, nil
}

func (s *KYCService) SubmitThirdParty(ctx context.Context, sessionID string) (*models.LivenessTask, error) {
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("缺少组织信息")
	}
	var task models.LivenessTask
	if err := s.DB.Where("organization_id = ? AND session_id = ?", orgID, sessionID).First(&task).Error; err != nil {
		return nil, fmt.Errorf("会话不存在")
	}
	if task.VideoAssetID == nil || *task.VideoAssetID == "" {
		return nil, fmt.Errorf("未上传视频")
	}
	var v models.VideoAsset
	if err := s.DB.Where("id = ?", *task.VideoAssetID).First(&v).Error; err != nil {
		return nil, fmt.Errorf("视频资源不存在")
	}

	var actions []string
	if len(task.Actions) > 0 {
		_ = json.Unmarshal(task.Actions, &actions)
	}

	publicURL := s.Storage.GetPublicURL(v.FilePath)

	req := &ActionLivenessRequest{
		RequestID: utils.GenerateID(),
		TaskID:    sessionID,
		VideoPath: v.FilePath,
		Actions:   actions,
		ThresholdConfig: &ThresholdConfig{
			LivenessThreshold: 0.95,
			ActionThreshold:   0.85,
		},
		ActionConfig: &ActionConfig{
			MaxVideoDuration: 6,
			PerActionTimeout: 2,
		},
		CallBackURL: s.Config.ThirdParty.LivenessAction.CallbackURL,
	}

	if s.Config.Storage.Upload.Mode == "remote" && publicURL != "" {
		// If we had a VideoURL field in request, we'd set it.
		// But current struct reuses VideoPath or we need to add VideoURL to ActionLivenessRequest if needed.
		// Checking ThirdPartyService.SubmitActionLivenessV2 implementation...
		// It just marshals the request.
		// The user JSON example has "video_path". Let's stick to "video_path" for now.
		// If remote, maybe we send URL in video_path?
		// User example: "video_path": "/data/..." (looks local).
		// Let's assume for now we send what we have.
		// If remote, we might need to send URL.
		// Let's check ActionLivenessRequest struct again. It has VideoPath.
		// I will send publicURL if remote, else FilePath.
		if s.Config.Storage.Upload.Mode == "remote" {
			req.VideoPath = publicURL
		}
	}

	err := s.ThirdParty.SubmitActionLivenessV2(ctx, req)
	if err != nil {
		return nil, err
	}

	task.Status = "submitted"
	task.UpdatedAt = time.Now()
	if err := s.DB.Save(&task).Error; err != nil {
		return nil, err
	}
	logger.GetLogger().WithFields(map[string]interface{}{"session_id": sessionID}).Info("Action liveness submitted to third-party (async)")
	s.RecordAuditLog(ctx, "liveness.action.submit", "liveness_action", task.ID, "success", "")
	return &task, nil
}

func (s *KYCService) ProcessActionLivenessCallback(ctx context.Context, req *ActionLivenessCallback) error {
	var task models.LivenessTask
	taskID := fmt.Sprintf("%v", req.TaskID)
	logger.GetLogger().WithFields(map[string]interface{}{
		"task_id": taskID,
		"code":    req.Code,
		"msg":     req.Msg,
	}).Info("Processing action liveness callback service logic")

	if err := s.DB.Where("session_id = ?", taskID).First(&task).Error; err != nil {
		logger.GetLogger().WithError(err).Warnf("Callback received for unknown task: %s", taskID)
		return fmt.Errorf("task not found: %s", taskID)
	}

	if req.Code != 0 {
		task.Status = "failed"
		task.ErrorMessage = req.Msg
	} else {
		task.Status = "succeeded"
		resultJSON, _ := json.Marshal(req.Data)
		task.Result = datatypes.JSON(resultJSON)
	}
	task.UpdatedAt = time.Now()

	if err := s.DB.Save(&task).Error; err != nil {
		return err
	}

	status := "success"
	if req.Code != 0 {
		status = "failed"
	}

	score := 0.0
	if req.Data != nil {
		score = req.Data.LivenessConfidence
	}

	logger.GetLogger().WithFields(map[string]interface{}{
		"session_id": taskID,
		"status":     status,
		"score":      score,
	}).Info("Processed action liveness callback")

	// Update KYCRequest Result
	go func() {
		updateMap := map[string]interface{}{
			"status": status,
			"result": fmt.Sprintf(`{"score": %f, "code": %d}`, score, req.Code),
		}
		if req.Code != 0 {
			updateMap["error_message"] = req.Msg
		}
		s.DB.Model(&models.KYCRequest{}).
			Where("result LIKE ?", fmt.Sprintf("%%%s%%", taskID)).
			Updates(updateMap)
	}()

	// Record Metrics
	if s.livenessSuccessRate != nil {
		val := 0.0
		if status == "success" {
			val = 1.0
		}
		s.livenessSuccessRate.Record(ctx, val)
	}
	metrics.RecordBusinessOperation(ctx, "action_liveness_callback", status == "success", 0, req.Msg)

	// Publish to Redis channel for long polling
	if s.Redis != nil {
		channel := fmt.Sprintf("liveness:result:%s", taskID)
		if err := s.Redis.Publish(ctx, channel, "done").Err(); err != nil {
			logger.GetLogger().Warnf("Failed to publish redis message: %v", err)
		}
	}

	return nil
}

func (s *KYCService) Verify(ctx context.Context, sessionID string) (*ActionVerifyResult, error) {
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("缺少组织信息")
	}

	// Long Polling Logic with Redis Pub/Sub
	// 1. Check current status from DB (fast path if already done)
	var task models.LivenessTask
	if err := s.DB.Where("organization_id = ? AND session_id = ?", orgID, sessionID).First(&task).Error; err != nil {
		return nil, fmt.Errorf("会话不存在")
	}

	// If already done, return immediately
	if task.Status == "succeeded" || task.Status == "failed" {
		if task.Status == "succeeded" {
			metrics.RecordSensitiveDataAccess(ctx, "liveness_result", getOrgID(ctx), true, "Verify")
		}
		return s.buildVerifyResult(&task), nil
	}

	// 2. If processing, wait for Redis notification
	if s.Redis != nil {
		channel := fmt.Sprintf("liveness:result:%s", sessionID)
		pubsub := s.Redis.Subscribe(ctx, channel)
		defer pubsub.Close()

		// Wait with timeout (e.g., 10 seconds, client should have longer timeout)
		// Or wait until context deadline (client controls timeout)
		// Let's use a safe internal timeout to avoid hanging forever
		timeout := 10 * time.Second

		select {
		case <-pubsub.Channel():
			// Received notification, query DB again for latest result
			if err := s.DB.Where("organization_id = ? AND session_id = ?", orgID, sessionID).First(&task).Error; err == nil {
				if task.Status == "succeeded" {
					metrics.RecordSensitiveDataAccess(ctx, "liveness_result", getOrgID(ctx), true, "Verify")
				}
				return s.buildVerifyResult(&task), nil
			}
		case <-time.After(timeout):
			// Timeout, return current status (likely still processing)
		case <-ctx.Done():
			// Request cancelled
			return nil, ctx.Err()
		}
	}

	// Fallback or timeout reached: return current status
	return s.buildVerifyResult(&task), nil
}

func (s *KYCService) buildVerifyResult(task *models.LivenessTask) *ActionVerifyResult {
	result := &ActionVerifyResult{
		SessionID: task.SessionID,
		Status:    task.Status,
	}

	if task.Status == "succeeded" && len(task.Result) > 0 {
		var data ActionLivenessData
		if err := json.Unmarshal(task.Result, &data); err == nil {
			result.Details = &data // Pass full details to frontend
		}
	} else if task.Status == "failed" && task.ErrorMessage != "" {
		// If task failed (e.g. system error), we might want to convey that in details or just status
		// For now status=failed is enough, but maybe details can hold error msg?
		// Let's stick to minimal structure. Frontend checks status.
	}
	return result
}
