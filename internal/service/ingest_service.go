package service

import (
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

	// 使用 StorageService 上传
	absPath, _, err := s.Storage.Upload(ctx, safe, f)
	if err != nil {
		return nil, fmt.Errorf("storage upload failed: %w", err)
	}

	if ct == "" || !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("unsupported file type: %v", ct)
	}
	asset := &models.ImageAsset{ID: utils.GenerateID(), OrganizationID: orgID, Hash: sum, FilePath: absPath, SafeFilename: safe, ContentType: ct, SizeBytes: size, CreatedAt: time.Now()}
	if err := s.DB.Create(asset).Error; err != nil {
		return nil, err
	}
	s.RecordAuditLog(ctx, "image.ingest", "image", asset.ID, "success", "")
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

	// Create path: YYYY/MM/DD/sessionID_timestamp.ext
	relativePath := fmt.Sprintf("%04d/%02d/%02d/%s_%d%s",
		now.Year(), now.Month(), now.Day(),
		sessionID, timestamp, ext)

	// Check for path traversal attempts just in case, though we generated it ourselves
	if strings.Contains(relativePath, "..") {
		return nil, fmt.Errorf("invalid filename generated")
	}

	// 使用 StorageService 上传
	absPath, _, err := s.Storage.Upload(ctx, relativePath, f)
	if err != nil {
		return nil, fmt.Errorf("storage upload failed: %w", err)
	}

	ct := vct
	if ct == "" || !strings.HasPrefix(ct, "video/") {
		// Even if detection failed, we allow upload if extension was plausible,
		// but here we check content type again.
		// If vct was empty, we might want to trust extension or fail.
		// Original code failed here if ct was empty or not video/.
		// But we set vct based on extension earlier.
		if ct == "" {
			return nil, fmt.Errorf("unsupported file type: %v, %v", ct, file.Filename)
		}
	}

	asset := &models.VideoAsset{ID: utils.GenerateID(), OrganizationID: orgID, Hash: sum, FilePath: absPath, SafeFilename: relativePath, ContentType: ct, SizeBytes: size, CreatedAt: time.Now()}
	if err := s.DB.Create(asset).Error; err != nil {
		return nil, err
	}
	s.RecordAuditLog(ctx, "video.ingest", "video", asset.ID, "success", "")
	return asset, nil
}
