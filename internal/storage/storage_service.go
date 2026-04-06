package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kyc-service/internal/config"
)

// StorageService 定义文件存储接口
type ResolvedPath struct {
	Strategy       string
	InternalPath   string // Nginx internal redirect path or S3 URL
	IsLocalAllowed bool   // Whether we can fallback to local file serving
}

// StorageService 定义文件存储接口
type StorageService interface {
	Upload(ctx context.Context, filename string, content io.Reader) (string, string, error)
	ResolveAccess(fullPath string) (*ResolvedPath, error)
	GetPublicURL(fullPath string) string
	GetAbsolutePath(filename string) string
}

// NewStorageService 根据配置创建存储服务
func NewStorageService(cfg *config.Config) (StorageService, error) {
	// 目前仅实现 Local 模式下的 Upload
	return &LocalStorage{
		Config: cfg.Storage,
	}, nil
}

// LocalStorage 本地存储实现
type LocalStorage struct {
	Config config.StorageConfig
}

func (s *LocalStorage) ResolveAccess(fullPath string) (*ResolvedPath, error) {
	// 1. 遍历规则链进行匹配
	for _, rule := range s.Config.Access.Rules {
		if strings.HasPrefix(fullPath, rule.MatchPrefix) {
			rel := strings.TrimPrefix(fullPath, rule.StripPrefix)
			rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")

			return &ResolvedPath{
				Strategy:       rule.Strategy,
				InternalPath:   filepath.Join(rule.InternalPrefix, rel),
				IsLocalAllowed: true,
			}, nil
		}
	}

	// 2. 没匹配到，使用默认策略
	return &ResolvedPath{
		Strategy:       s.Config.Access.DefaultStrategy,
		InternalPath:   fullPath,
		IsLocalAllowed: true,
	}, nil
}

func (s *LocalStorage) GetPublicURL(fullPath string) string {
	resolved, err := s.ResolveAccess(fullPath)
	if err != nil || resolved == nil {
		return ""
	}
	// 如果策略是 redirect，说明 InternalPath 就是公开 URL
	if resolved.Strategy == "redirect" {
		return resolved.InternalPath
	}
	return ""
}

func (s *LocalStorage) Upload(ctx context.Context, filename string, content io.Reader) (string, string, error) {
	// 1. 确定基础目录：遍历上传规则
	baseDir := s.Config.Upload.BaseDir
	finalFilename := filename

	for _, rule := range s.Config.Upload.Rules {
		if strings.HasPrefix(filename, rule.Prefix) {
			baseDir = rule.TargetDir
			// 既然配置了 target_dir=/data，那么 filename(如 videos/...) 原封不动拼上去即可
			// 也就是 /data + videos/... = /data/videos/...
			break
		}
	}

	fullPath := filepath.Join(baseDir, finalFilename)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", "", err
	}

	out, err := os.Create(fullPath)
	if err != nil {
		return "", "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, content); err != nil {
		return "", "", err
	}

	return fullPath, "", nil
}

// GetAbsolutePath returns the final absolute path on disk for a given relative filename
func (s *LocalStorage) GetAbsolutePath(filename string) string {
	baseDir := s.Config.Upload.BaseDir
	finalFilename := filename

	for _, rule := range s.Config.Upload.Rules {
		if strings.HasPrefix(filename, rule.Prefix) {
			baseDir = rule.TargetDir
			break
		}
	}

	return filepath.Join(baseDir, finalFilename)
}
