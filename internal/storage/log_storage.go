package storage

import (
	"context"

	"kyc-service/internal/models"

	"gorm.io/gorm"
)

// BillingLogStorage 计费日志存储接口
// 未来可替换为 ClickHouse 或 TimescaleDB 实现
type BillingLogStorage interface {
	Save(ctx context.Context, logs []models.UsageLog) error
}

// AuditLogStorage 审计日志存储接口
// 未来可替换为 Elasticsearch 或其他文档型数据库实现
type AuditLogStorage interface {
	Save(ctx context.Context, logs []models.AuditLog) error
}

// PostgresLogStorage 基于 Postgres (GORM) 的日志存储实现
type PostgresLogStorage struct {
	db *gorm.DB
}

func NewPostgresLogStorage(db *gorm.DB) *PostgresLogStorage {
	return &PostgresLogStorage{db: db}
}

// Save 实现 BillingLogStorage 接口
func (s *PostgresLogStorage) Save(ctx context.Context, logs []models.UsageLog) error {
	if len(logs) == 0 {
		return nil
	}
	// 使用 CreateInBatches 批量插入
	return s.db.WithContext(ctx).CreateInBatches(logs, len(logs)).Error
}

// SaveAudit 实现 AuditLogStorage 接口
// 注意：Go 接口匹配是隐式的，虽然这里方法名不同，但在 Worker 中我们将分别注入
// 为了清晰起见，我们可以让 PostgresLogStorage 同时实现两个接口，或者拆分
func (s *PostgresLogStorage) SaveAudit(ctx context.Context, logs []models.AuditLog) error {
	if len(logs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).CreateInBatches(logs, len(logs)).Error
}

// 为了让 PostgresLogStorage 显式实现接口，我们可以定义如下：
// 实际使用时，LogWorker 会持有两个接口类型的字段

// 辅助：显式分离实现，避免方法名冲突（虽然 Save 参数不同已经重载，但在接口中可能混淆）
type PostgresBillingStorage struct {
	db *gorm.DB
}

func NewPostgresBillingStorage(db *gorm.DB) *PostgresBillingStorage {
	return &PostgresBillingStorage{db: db}
}

func (s *PostgresBillingStorage) Save(ctx context.Context, logs []models.UsageLog) error {
	if len(logs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).CreateInBatches(logs, 100).Error
}

type PostgresAuditStorage struct {
	db *gorm.DB
}

func NewPostgresAuditStorage(db *gorm.DB) *PostgresAuditStorage {
	return &PostgresAuditStorage{db: db}
}

func (s *PostgresAuditStorage) Save(ctx context.Context, logs []models.AuditLog) error {
	if len(logs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).CreateInBatches(logs, 100).Error
}
