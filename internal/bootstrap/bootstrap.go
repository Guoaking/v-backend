package bootstrap

import (
	"context"
	"time"

	"kyc-service/internal/config"
	"kyc-service/internal/middleware"
	"kyc-service/internal/migration"
	"kyc-service/internal/models"
	"kyc-service/internal/router"
	"kyc-service/internal/service"
	"kyc-service/internal/storage"
	"kyc-service/internal/tasks"
	"kyc-service/internal/worker"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/tracing"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// App holds the application dependencies
type App struct {
	Config           *config.Config
	DB               *gorm.DB
	RedisClient      *redis.Client
	KYCService       *service.KYCService
	LogWorker        worker.LogWorker
	Engine           *gin.Engine
	HeartbeatManager *middleware.HeartbeatManager
}

// SetupApp initializes the application for server or testing
func SetupApp(ctx context.Context, configFile string) (*App, func(), error) {
	app, tracerCleanup, err := Init(ctx, configFile)
	if err != nil {
		return nil, nil, err
	}

	// 2. Migration: 执行数据库迁移
	if err := migration.Run(app.DB); err != nil {
		tracerCleanup()
		return nil, nil, err
	}

	// 3. Router: 初始化路由和中间件
	r, heartbeatManager := router.New(app.Config, app.KYCService, app.RedisClient)
	app.Engine = r
	app.HeartbeatManager = heartbeatManager

	return app, tracerCleanup, nil
}

// LoadConfigOnly just loads the config without initializing other dependencies
// Useful for pre-flight checks like port availability
func LoadConfigOnly(configFile string) (*config.Config, error) {
	return config.Load(configFile), nil
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

	// 初始化异步日志Worker (带DB用于聚合)
	// billingStore := storage.NewPostgresBillingStorage(db)
	// auditStore := storage.NewPostgresAuditStorage(db)
	// logWorker := worker.NewAsyncLogWorker(db, billingStore, auditStore)
	logWorker := worker.NewRedisLogWorker(db, redisClient)
	logWorker.Start()

	// 初始化服务
	kycService := service.NewKYCService(db, redisClient, cfg, logWorker)
	tasks.StartStatsRefresher(kycService, 5*time.Minute)
	tasks.StartInvitationCleaner(kycService, time.Hour)
	tasks.StartAuditActionsSync(kycService, 10*time.Minute)
	tasks.StartQuotaResetter(db, time.Hour)
	// tasks.StartUsageMeterConsumer(kycService, 100, time.Second) // 已废弃，改用 AsyncLogWorker

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
		LogWorker:   logWorker,
	}

	return app, tracerCleanup, nil
}
