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
		return &LocalStorage{
			BaseDir: cfg.Storage.IngestDir,
			BaseURL: cfg.Storage.BaseURL,
		}, nil
	default:
		// 默认为本地
		return &LocalStorage{
			BaseDir: cfg.Storage.IngestDir,
			BaseURL: cfg.Storage.BaseURL,
		}, nil
	}
}

// LocalStorage 本地存储实现
type LocalStorage struct {
	BaseDir string
	BaseURL string
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
	// 确保目录存在
	if err := os.MkdirAll(s.BaseDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create directory: %w", err)
	}

	// 生成安全的文件名（这里简化处理，假设调用者已经处理了文件名的唯一性）
	// 实际生产中可能需要生成 UUID 或 Hash
	fullPath := filepath.Join(s.BaseDir, filename)

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
