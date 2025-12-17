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
		if err := autoMigrate(db); err != nil {
			logger.GetLogger().Warnf("数据库迁移失败: %v", err)
			return nil, err
		}
	}

	return db, nil
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
	// 先手动创建 kyc_requests，避免某些环境下对信息模式查询报错
	if err := db.Exec(`
        CREATE TABLE IF NOT EXISTS kyc_requests (
            id VARCHAR(255) PRIMARY KEY,
            user_id VARCHAR(255),
            request_type VARCHAR(50),
            status VARCHAR(50),
            id_card_hash VARCHAR(255),
            id_card TEXT,
            name TEXT,
            phone TEXT,
            face_image TEXT,
            id_card_image TEXT,
            liveness_data TEXT,
            result TEXT,
            error_message TEXT,
            ip_address VARCHAR(45),
            user_agent TEXT,
            created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
            deleted_at TIMESTAMPTZ
        );
        CREATE INDEX IF NOT EXISTS idx_kyc_user ON kyc_requests(user_id);
        CREATE INDEX IF NOT EXISTS idx_kyc_id_card_hash ON kyc_requests(id_card_hash);
    `).Error; err != nil {
		logger.GetLogger().Errorf("手动创建 kyc_requests 失败: %v", err)
		return err
	}

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
