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
	"path/filepath"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"
)

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
	// 为了不阻塞，这里先给一个预期的相对路径
	uploadName := "images/" + safe
	// Assuming local base path or storage specific path.
	// We will update FilePath later if needed, but for now we can put a placeholder or the expected uploadName.
	asset.FilePath = "pending_upload"
	if err := s.DB.Create(asset).Error; err != nil {
		return nil, err
	}

	// Read all file data into memory (or a temp file) to pass to the goroutine
	// because the multipart.FileHeader's underlying temp file might be deleted when the request ends
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	fileData, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read file data for async upload: %w", err)
	}

	// Start goroutine for actual storage upload and DB update
	go func(assetID string, oID string, fileName string, data []byte) {
		// Create a new context for the goroutine since the request ctx will be cancelled
		bgCtx := context.Background()

		// Create a reader from the in-memory data
		reader := bytes.NewReader(data)

		absPath, _, err := s.Storage.Upload(bgCtx, fileName, reader)
		if err != nil {
			logger.GetLogger().WithError(err).Warnf("Async storage upload failed for asset %s", assetID)
			return
		}

		// Update the asset with the actual absolute path
		if err := s.DB.Model(&models.ImageAsset{}).Where("id = ?", assetID).Update("file_path", absPath).Error; err != nil {
			logger.GetLogger().WithError(err).Warnf("Failed to update asset file_path for %s", assetID)
		}

		s.RecordAuditLog(bgCtx, "image.ingest.async", "image", assetID, "success", "")
	}(asset.ID, orgID, uploadName, fileData)

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

	return asset, nil
}
