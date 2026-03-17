package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kyc-service/internal/config"
)

// StorageService 定义文件存储接口
type StorageService interface {
	// Upload 上传文件
	// filename: 可以包含子目录 (e.g. "images/abc.jpg" or "videos/2026/01/01/video.mp4")
	// 返回: internalPath (用于本地处理或记录), publicURL (用于外部访问), error
	Upload(ctx context.Context, filename string, content io.Reader) (internalPath string, publicURL string, err error)
	// GetPublicURL 根据内部路径获取公共URL
	GetPublicURL(internalPath string) string
}

// NewStorageService 根据配置创建存储服务
func NewStorageService(cfg *config.Config) (StorageService, error) {
	switch cfg.Storage.Mode {
	case "remote":
		// 这里将来可以添加 S3 实现
		return nil, fmt.Errorf("remote storage not implemented yet")
	case "local":
		// 如果未配置 ImageDir，默认使用 IngestDir
		imageDir := cfg.Storage.ImageDir
		if imageDir == "" {
			imageDir = cfg.Storage.IngestDir
		}
		
		return &LocalStorage{
			BaseDir:  cfg.Storage.IngestDir,
			ImageDir: imageDir,
			BaseURL:  cfg.Storage.BaseURL,
		}, nil
	default:
		// 默认为本地
		return &LocalStorage{
			BaseDir:  cfg.Storage.IngestDir,
			ImageDir: cfg.Storage.IngestDir, // Default to IngestDir if not set
			BaseURL:  cfg.Storage.BaseURL,
		}, nil
	}
}

// LocalStorage 本地存储实现
type LocalStorage struct {
	BaseDir  string
	ImageDir string
	BaseURL  string
}

func (s *LocalStorage) GetPublicURL(internalPath string) string {
	if s.BaseURL == "" {
		return ""
	}
	// 假设 internalPath 是绝对路径，我们需要提取文件名
	filename := filepath.Base(internalPath)
	return fmt.Sprintf("%s/%s", strings.TrimRight(s.BaseURL, "/"), filename)
}

func (s *LocalStorage) Upload(ctx context.Context, filename string, content io.Reader) (string, string, error) {
	// Determine target directory based on filename prefix or type
	// 约定: 如果 filename 以 "images/" 开头，使用 ImageDir
	// 否则使用 BaseDir (默认用于 videos)
	targetDir := s.BaseDir
	relativePath := filename

	if strings.HasPrefix(filename, "images/") && s.ImageDir != "" {
		targetDir = s.ImageDir
		relativePath = strings.TrimPrefix(filename, "images/")
	} else if strings.HasPrefix(filename, "videos/") {
		// Optional: could have VideoDir, but currently BaseDir is used for videos
		relativePath = strings.TrimPrefix(filename, "videos/")
	}

	// 确保目录存在
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create directory: %w", err)
	}

	// 生成安全的文件名
	fullPath := filepath.Join(targetDir, relativePath)

	// 确保父目录存在
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create directory: %w", err)
	}

	// 创建文件
	out, err := os.Create(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// 写入内容
	if _, err := io.Copy(out, content); err != nil {
		return "", "", fmt.Errorf("failed to write file content: %w", err)
	}

	// 构造 URL
	var publicURL string
	if s.BaseURL != "" {
		publicURL = fmt.Sprintf("%s/%s", strings.TrimRight(s.BaseURL, "/"), filename)
	}

	return fullPath, publicURL, nil
}
