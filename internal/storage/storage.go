package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"kyc-service/internal/config"
	"kyc-service/internal/models"

	"kyc-service/pkg/logger"

	"github.com/go-redis/redis/v8"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	db          *gorm.DB
	redisClient *redis.Client
)

// InitDB 初始化数据库连接
func InitDB(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode)

	var err error

	newLogger := gormlogger.New(
		// 使用标准输出避免 Writer() 与 fmt 格式化冲突导致的日志错误
		log.New(os.Stdout, "", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormlogger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newLogger,
	})
	db = db.Debug()
	if err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 连接池配置
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// 自动迁移表结构（可配置）
	if cfg.AutoMigrateEnabled {
		// 1. 执行预迁移处理（处理破坏性变更）
		if err := preAutoMigrate(db); err != nil {
			logger.GetLogger().Warnf("数据库预迁移失败: %v", err)
			// 不阻断，尝试继续
		}

		// 2. 执行 GORM AutoMigrate
		if err := autoMigrate(db); err != nil {
			logger.GetLogger().Warnf("数据库迁移失败: %v", err)
			return nil, err
		}
	}

	return db, nil
}

// preAutoMigrate 处理 GORM 无法自动处理的破坏性变更
func preAutoMigrate(db *gorm.DB) error {
	// 1. 处理 audit_logs.details 从 text 到 jsonb 的变更
	// 为了数据安全，我们将旧的 text 列重命名为 details_text，让 GORM 创建新的 details (jsonb) 列
	// 这样既保留了历史数据，又解决了类型冲突
	err := db.Exec(`
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 
				FROM information_schema.columns 
				WHERE table_name = 'audit_logs' 
				AND column_name = 'details' 
				AND data_type = 'text'
			) THEN
				RAISE NOTICE 'Renaming audit_logs.details to details_text to allow type migration';
				ALTER TABLE audit_logs RENAME COLUMN details TO details_text;
			END IF;
		END $$;
	`).Error
	if err != nil {
		return fmt.Errorf("audit_logs migration failed: %w", err)
	}
	return nil
}

// InitRedis 初始化Redis连接
func InitRedis(cfg config.RedisConfig) (*redis.Client, error) {
	redisClient = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		MaxRetries:   cfg.MaxRetries,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis连接失败: %w", err)
	}

	return redisClient, nil
}

// autoMigrate 自动迁移数据库表（逐模型执行，便于定位错误）
func autoMigrate(db *gorm.DB) error {
	// 移除手动创建 kyc_requests 表的代码，避免与 GORM AutoMigrate 冲突
	// 直接让 GORM 自动管理表结构

	modelsToMigrate := []interface{}{
		&models.AuditLog{},
		&models.User{},
		&models.OAuthClient{},
		&models.OAuthToken{},
		&models.Organization{},
		&models.OrganizationMember{},
		&models.APIKey{},
		&models.UsageLog{},
		&models.PasswordReset{},
		&models.OrganizationInvitation{},
		&models.Invitation{},
		&models.Notification{},
		&models.AuditAction{},
		&models.Plan{},
		&models.GlobalConfig{},
		&models.APIRequestLog{},
		&models.OrganizationQuotas{},
		&models.FaceImageRef{},
		&models.ImageAsset{},
		&models.VideoAsset{},
		&models.LivenessTask{},
		&models.KYCRequest{}, // 确保 KYCRequest 模型被包含在自动迁移中
	}

	for _, m := range modelsToMigrate {
		if err := db.AutoMigrate(m); err != nil {
			// 记录失败的模型类型，帮助排查具体错误来源
			logger.GetLogger().WithField("model", fmt.Sprintf("%T", m)).Errorf("AutoMigrate失败: %v", err)
			return err
		}
		// 记录成功迁移的模型，便于观察进度
		logger.GetLogger().WithField("model", fmt.Sprintf("%T", m)).Info("AutoMigrate完成")
	}
	return nil
}

// GetDB 获取数据库连接
func GetDB() *gorm.DB {
	return db
}

// GetRedis 获取Redis连接
func GetRedis() *redis.Client {
	return redisClient
}
