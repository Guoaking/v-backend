package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"
)

// AsyncUploadTask represents a task for background ingestion
type AsyncUploadTask struct {
	AssetID   string
	OrgID     string
	RequestID string
	FileName  string
	TempPath  string
	IsVideo   bool
	Service   *KYCService
}

var (
	asyncUploadQueue chan AsyncUploadTask
	workersStarted   bool
)

// InitAsyncUploadWorkers explicitly initializes and starts the worker pool
// for background storage uploads. It should be called during service initialization.
func InitAsyncUploadWorkers() {
	if workersStarted {
		return
	}

	// Initialize a buffered channel for async uploads
	asyncUploadQueue = make(chan AsyncUploadTask, 1000)

	// Start a bounded number of worker goroutines (e.g., 5)
	for i := 0; i < 5; i++ {
		go asyncUploadWorker()
	}

	workersStarted = true
	logger.GetLogger().Info("Initialized Async Upload Worker Pool with 5 workers")
}

func asyncUploadWorker() {
	for task := range asyncUploadQueue {
		processAsyncUpload(task)
	}
}

func processAsyncUpload(task AsyncUploadTask) {
	bgCtx := context.Background()

	// Open the persistent temp file
	f, err := os.Open(task.TempPath)
	if err != nil {
		logger.GetLogger().WithError(err).Warnf("Async storage worker failed to open temp file for %s", task.AssetID)
		return
	}

	absPath, _, err := task.Service.Storage.Upload(bgCtx, task.FileName, f)
	f.Close()

	// Clean up the temp file
	os.Remove(task.TempPath)

	if err != nil {
		logger.GetLogger().WithError(err).Warnf("Async storage upload failed for asset %s", task.AssetID)
		return
	}

	// Update the asset with the actual absolute path
	if task.IsVideo {
		if err := task.Service.DB.Model(&models.VideoAsset{}).Where("id = ?", task.AssetID).Update("file_path", absPath).Error; err != nil {
			logger.GetLogger().WithError(err).Warnf("Failed to update video asset file_path for %s", task.AssetID)
		}
		task.Service.RecordAuditLog(bgCtx, "video.ingest.async", "video", task.AssetID, "success", "")
	} else {
		if err := task.Service.DB.Model(&models.ImageAsset{}).Where("id = ?", task.AssetID).Update("file_path", absPath).Error; err != nil {
			logger.GetLogger().WithError(err).Warnf("Failed to update asset file_path for %s", task.AssetID)
		}
		task.Service.RecordAuditLog(bgCtx, "image.ingest.async", "image", task.AssetID, "success", "")
	}
}

func (s *KYCService) IngestImage(ctx context.Context, orgID string, file *multipart.FileHeader) (*models.ImageAsset, error) {
	f, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return nil, fmt.Errorf("read file failed: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	var exist models.ImageAsset
	if err := s.DB.Where("organization_id = ? AND hash = ?", orgID, sum).First(&exist).Error; err == nil {
		return &exist, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	ct := ""
	if n > 0 {
		ct = http.DetectContentType(buf[:n])
	}
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		ext := strings.ToLower(filepath.Ext(file.Filename))
		switch ext {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".png":
			ct = "image/png"
		case ".gif":
			ct = "image/gif"
		case ".webp":
			ct = "image/webp"
		case ".bmp":
			ct = "image/bmp"
		case ".tif", ".tiff":
			ct = "image/tiff"
		default:
			if v := mime.TypeByExtension(ext); v != "" {
				ct = v
			}
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	safe := sum + filepath.Ext(file.Filename)
	if strings.Contains(safe, "..") {
		return nil, fmt.Errorf("invalid filename")
	}

	if ct == "" || !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("unsupported file type: %v", ct)
	}

	// === 异步化落盘处理 ===
	// 生成临时的 asset ID 和结构
	asset := &models.ImageAsset{
		ID:             utils.GenerateID(),
		OrganizationID: orgID,
		Hash:           sum,
		SafeFilename:   safe,
		ContentType:    ct,
		SizeBytes:      size,
		CreatedAt:      time.Now(),
	}

	// 提前创建一条记录，状态可以是 pending 或者由具体的 file_path 决定
	uploadName := "images/" + safe
	asset.FilePath = "pending_upload"
	if err := s.DB.Create(asset).Error; err != nil {
		return nil, err
	}

	// Try to get the persistent temp path from the context (injected by AsyncMediaIngest middleware)
	reqID := ""
	if reqIDCtx := ctx.Value("request_id"); reqIDCtx != nil {
		if sReqID, ok := reqIDCtx.(string); ok {
			reqID = sReqID
		}
	}
	// For traceability, include request_id in the safe filename if available
	if reqID != "" {
		asset.SafeFilename = reqID + "_" + asset.SafeFilename
		uploadName = "images/" + asset.SafeFilename
	}

	// Try to get the pre-saved temp path from context
	tempPathCtxKey := "async_temp_path_picture" // Assuming field is usually 'picture'
	// The exact field name might vary, but in most KYC/Face APIs it's 'picture', 'image', 'source_image', etc.
	// This is a simplified lookup. A more robust way would be mapping by filename.
	var tempPath string

	// Fast lookup in context (gin.Context passes values if set)
	if val := ctx.Value(tempPathCtxKey); val != nil {
		tempPath = val.(string)
	}

	// If the middleware successfully saved it to disk, queue the task
	if tempPath != "" {
		asyncUploadQueue <- AsyncUploadTask{
			AssetID:   asset.ID,
			OrgID:     orgID,
			RequestID: reqID,
			FileName:  uploadName,
			TempPath:  tempPath,
			IsVideo:   false,
			Service:   s,
		}
	} else {
		// Fallback: If the middleware didn't run or field name mismatched, do synchronous upload
		// Read all file data into memory to pass to the goroutine
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		fileData, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read file data for async upload: %w", err)
		}

		// Start ad-hoc goroutine for actual storage upload and DB update
		go func(assetID string, oID string, fileName string, data []byte) {
			bgCtx := context.Background()
			reader := bytes.NewReader(data)

			absPath, _, err := s.Storage.Upload(bgCtx, fileName, reader)
			if err != nil {
				logger.GetLogger().WithError(err).Warnf("Async storage upload failed for asset %s", assetID)
				return
			}

			if err := s.DB.Model(&models.ImageAsset{}).Where("id = ?", assetID).Update("file_path", absPath).Error; err != nil {
				logger.GetLogger().WithError(err).Warnf("Failed to update asset file_path for %s", assetID)
			}

			s.RecordAuditLog(bgCtx, "image.ingest.async", "image", assetID, "success", "")
		}(asset.ID, orgID, uploadName, fileData)
	}

	return asset, nil
}

func (s *KYCService) IngestVideo(ctx context.Context, orgID string, sessionID string, file *multipart.FileHeader) (*models.VideoAsset, error) {
	f, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return nil, fmt.Errorf("read file failed: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	var exist models.VideoAsset
	if err := s.DB.Where("organization_id = ? AND hash = ?", orgID, sum).First(&exist).Error; err == nil {
		return &exist, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	vct := ""
	if n > 0 {
		vct = http.DetectContentType(buf[:n])
	}
	if vct == "" || !strings.HasPrefix(vct, "video/") {
		ext := strings.ToLower(filepath.Ext(file.Filename))
		switch ext {
		case ".mp4", ".m4v":
			vct = "video/mp4"
		case ".mov":
			vct = "video/quicktime"
		case ".webm":
			vct = "video/webm"
		case ".mkv":
			vct = "video/x-matroska"
		case ".avi":
			vct = "video/x-msvideo"
		case ".flv":
			vct = "video/x-flv"
		case ".3gp":
			vct = "video/3gpp"
		default:
			if v := mime.TypeByExtension(ext); v != "" {
				vct = v
			}
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// Generate hierarchical path: YYYY/MM/DD/{SessionID}_{Timestamp}.ext
	now := time.Now()
	timestamp := now.Unix()
	if sessionID == "" {
		sessionID = utils.GenerateID()
	}

	ext := filepath.Ext(file.Filename)
	if ext == "" {
		ext = ".webm" // Default to webm if unknown
	}

	// Create path: videos/YYYY/MM/DD/sessionID_timestamp.ext
	// Note: We use "videos/" prefix which StorageService will strip before saving to BaseDir
	relativePath := fmt.Sprintf("videos/%04d/%02d/%02d/%s_%d%s",
		now.Year(), now.Month(), now.Day(),
		sessionID, timestamp, ext)

	// Check for path traversal attempts just in case, though we generated it ourselves
	if strings.Contains(relativePath, "..") {
		return nil, fmt.Errorf("invalid filename generated")
	}

	ct := vct
	if ct == "" || !strings.HasPrefix(ct, "video/") {
		if ct == "" {
			return nil, fmt.Errorf("unsupported file type: %v, %v", ct, file.Filename)
		}
	}

	// === 异步化落盘处理 ===
	asset := &models.VideoAsset{
		ID:             utils.GenerateID(),
		OrganizationID: orgID,
		Hash:           sum,
		FilePath:       "pending_upload", // Placeholder
		SafeFilename:   relativePath,
		ContentType:    ct,
		SizeBytes:      size,
		CreatedAt:      time.Now(),
	}

	if err := s.DB.Create(asset).Error; err != nil {
		return nil, err
	}

	reqID := ""
	if reqIDCtx := ctx.Value("request_id"); reqIDCtx != nil {
		if sReqID, ok := reqIDCtx.(string); ok {
			reqID = sReqID
		}
	}
	if reqID != "" {
		asset.SafeFilename = reqID + "_" + asset.SafeFilename
		relativePath = reqID + "_" + relativePath
	}

	tempPathCtxKey := "async_temp_path_video" // Assuming field is usually 'video' or 'liveness_file'
	var tempPath string
	if val := ctx.Value(tempPathCtxKey); val != nil {
		tempPath = val.(string)
	}
	if val := ctx.Value("async_temp_path_liveness_file"); val != nil && tempPath == "" {
		tempPath = val.(string)
	}

	if tempPath != "" {
		asyncUploadQueue <- AsyncUploadTask{
			AssetID:   asset.ID,
			OrgID:     orgID,
			RequestID: reqID,
			FileName:  relativePath,
			TempPath:  tempPath,
			IsVideo:   true,
			Service:   s,
		}
	} else {
		// Read all file data into memory
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		fileData, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read file data for async upload: %w", err)
		}

		// Start goroutine for actual storage upload and DB update
		go func(assetID string, oID string, fileName string, data []byte) {
			bgCtx := context.Background()
			reader := bytes.NewReader(data)

			absPath, _, err := s.Storage.Upload(bgCtx, fileName, reader)
			if err != nil {
				logger.GetLogger().WithError(err).Warnf("Async storage upload failed for video asset %s", assetID)
				return
			}

			if err := s.DB.Model(&models.VideoAsset{}).Where("id = ?", assetID).Update("file_path", absPath).Error; err != nil {
				logger.GetLogger().WithError(err).Warnf("Failed to update video asset file_path for %s", assetID)
			}

			s.RecordAuditLog(bgCtx, "video.ingest.async", "video", assetID, "success", "")
		}(asset.ID, orgID, relativePath, fileData)
	}

	return asset, nil
}
