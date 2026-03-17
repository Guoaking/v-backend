package bootstrap

import (
	"context"
	"time"

	"kyc-service/internal/config"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/internal/storage"
	"kyc-service/internal/tasks"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/tracing"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// App holds the application dependencies
type App struct {
	Config      *config.Config
	DB          *gorm.DB
	RedisClient *redis.Client
	KYCService  *service.KYCService
}

// Init initializes the application dependencies
func Init(ctx context.Context, configFile string) (*App, func(), error) {
	// 加载配置
	cfg := config.Load(configFile)

	// 初始化日志
	logger.Init(cfg.LogLevel)
	log := logger.GetLogger()

	// 初始化链路追踪
	tracerCleanup, err := tracing.Init(cfg)
	if err != nil {
		log.Fatalf("链路追踪初始化失败: %v", err)
	}

	// 初始化OpenTelemetry指标
	if cfg.Monitoring.Metrics.Enabled {
		if err := metrics.InitOTelMetrics(); err != nil {
			log.Fatalf("OpenTelemetry指标初始化失败: %v", err)
		}

		// 启动双向鉴权指标收集器
		metricsCollector := metrics.NewBidirectionalAuthMetricsCollector(ctx)
		metricsCollector.Start()
	}

	// 初始化存储
	db, err := storage.InitDB(cfg.Database)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	{
		var curdb string
		_ = db.Raw("select current_database()").Scan(&curdb).Error
		log.Infof("已连接数据库: %s", curdb)
	}

	redisClient, err := storage.InitRedis(cfg.Redis)
	if err != nil {
		log.Fatalf("Redis初始化失败: %v", err)
	}

	// 初始化服务
	kycService := service.NewKYCService(db, redisClient, cfg)
	tasks.StartStatsRefresher(kycService, 5*time.Minute)
	tasks.StartInvitationCleaner(kycService, time.Hour)
	tasks.StartAuditActionsSync(kycService, 10*time.Minute)
	tasks.StartQuotaResetter(db, time.Hour)
	tasks.StartUsageMeterConsumer(kycService, 100, time.Second)

	// 启动后同步现有组织的配额（Plans -> OrganizationQuotas）
	{
		var orgs []models.Organization
		_ = db.Find(&orgs).Error
		for _, o := range orgs {

			var orgCount int64
			if err = db.Raw("SELECT COUNT(*) FROM organization_quotas WHERE organization_id = ?", o.ID).Scan(&orgCount).Error; err != nil {
				log.Warnf("组织配额同步失败: org=%s plan=%s err=%v", o.ID, o.PlanID, err)
				continue
			}

			if orgCount != 0 {
				continue
			}

			if err = kycService.SyncOrganizationQuotas(o.ID, o.PlanID); err != nil {
				log.Warnf("组织配额同步失败: org=%s plan=%s err=%v", o.ID, o.PlanID, err)
			} else {
				log.Infof("✅ 组织配额已同步: org=%s plan=%s", o.ID, o.PlanID)
			}
		}
	}

	app := &App{
		Config:      cfg,
		DB:          db,
		RedisClient: redisClient,
		KYCService:  kycService,
	}

	return app, tracerCleanup, nil
}
