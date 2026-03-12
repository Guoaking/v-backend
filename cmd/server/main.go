package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kyc-service/internal/api"
	"kyc-service/internal/config"
	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/internal/storage"
	"kyc-service/internal/tasks"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/tracing"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// @title KYC Service API
// @version 1.0
// @description 企业级KYC认证服务
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /api/v1

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @securityDefinitions.oauth2 OAuth2Application
// @tokenUrl /api/v1/oauth/token
// @scope ocr:read "OCR read access"
// @scope face:read "Face read access"
// @scope liveness:read "Liveness read access"
// @scope kyc:verify "KYC verify access"
// executeSecurityAuditMigration 执行安全审计数据库迁移
func executeSecurityAuditMigration(db *gorm.DB) error {
	log := logger.GetLogger()
	log.Info("开始执行安全审计数据库迁移...")

	// 1. 增强API Key表，添加IP白名单字段
	if err := db.Exec(`
		ALTER TABLE api_keys 
		ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[] DEFAULT '{}'
	`).Error; err != nil {
		log.Warnf("添加IP白名单字段失败: %v", err)
	} else {
		log.Info("✅ API Key表已添加IP白名单字段")
	}

	// 2. 创建API请求日志表
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS api_request_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id VARCHAR(255),
			user_id VARCHAR(255) REFERENCES users(id) ON DELETE SET NULL,
			api_key_id VARCHAR(255) REFERENCES api_keys(id) ON DELETE SET NULL,
			method VARCHAR(10) NOT NULL,
			path VARCHAR(255) NOT NULL,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			client_ip VARCHAR(45),
			request_body JSONB,
			response_body JSONB,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		log.Fatalf("创建API请求日志表失败: %v", err)
	} else {
		log.Info("✅ API请求日志表创建成功")
	}

	// 2.1 兼容已有表，确保 org_id 列存在
	if err := db.Exec(`ALTER TABLE api_request_logs ADD COLUMN IF NOT EXISTS org_id VARCHAR(255)`).Error; err != nil {
		log.Fatalf("添加 org_id 列失败: %v", err)
	}

	// 3. 创建索引
	indexes := []struct {
		name string
		sql  string
	}{
		{
			name: "idx_req_logs_user_date",
			sql:  "CREATE INDEX IF NOT EXISTS idx_req_logs_user_date ON api_request_logs (user_id, created_at DESC)",
		},
		{
			name: "idx_req_logs_org_date",
			sql:  "CREATE INDEX IF NOT EXISTS idx_req_logs_org_date ON api_request_logs (org_id, created_at DESC)",
		},
		{
			name: "idx_req_logs_api_key",
			sql:  "CREATE INDEX IF NOT EXISTS idx_req_logs_api_key ON api_request_logs (api_key_id, created_at DESC)",
		},
		{
			name: "idx_req_logs_created_at",
			sql:  "CREATE INDEX IF NOT EXISTS idx_req_logs_created_at ON api_request_logs (created_at DESC)",
		},
		{
			name: "idx_req_logs_client_ip",
			sql:  "CREATE INDEX IF NOT EXISTS idx_req_logs_client_ip ON api_request_logs (client_ip)",
		},
		{
			name: "idx_req_logs_status",
			sql:  "CREATE INDEX IF NOT EXISTS idx_req_logs_status ON api_request_logs (status_code)",
		},
		{
			name: "idx_api_keys_ip_whitelist",
			sql:  "CREATE INDEX IF NOT EXISTS idx_api_keys_ip_whitelist ON api_keys USING GIN (ip_whitelist)",
		},
	}

	for _, idx := range indexes {
		if err := db.Exec(idx.sql).Error; err != nil {
			log.Fatalf("创建索引 %s 失败: %v", idx.name, err)
		} else {
			log.Infof("✅ 索引 %s 创建成功", idx.name)
		}
	}

	// 4. 为现有API密钥添加默认空IP白名单
	if err := db.Exec(`
		UPDATE api_keys SET ip_whitelist = '{}' WHERE ip_whitelist IS NULL
	`).Error; err != nil {
		log.Warnf("更新现有API密钥IP白名单失败: %v", err)
	} else {
		log.Info("✅ 现有API密钥IP白名单已更新")
	}

	log.Info("🎉 数据库安全审计增强脚本执行完成！")
	log.Info("主要变更：")
	log.Info("1. ✅ API Key表新增ip_whitelist字段")
	log.Info("2. ✅ 创建api_request_logs请求日志表")
	log.Info("3. ✅ 创建相关性能索引")
	log.Info("4. ✅ 更新现有API密钥默认设置")

	return nil
}

func main() {
	ctx := context.Background()

	// 解析命令行参数
	var configFile string
	flag.StringVar(&configFile, "config", "config", "配置文件路径 (不包含 .yaml 扩展名)")
	flag.Parse()

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
	defer tracerCleanup()

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

	// 执行安全审计数据库迁移
	log.Info("执行安全审计数据库迁移...")
	// 确保pgcrypto扩展可用以支持gen_random_uuid()
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		log.Warnf("创建pgcrypto扩展失败: %v", err)
	}
	if err := executeSecurityAuditMigration(db); err != nil {
		log.Fatalf("安全审计数据库迁移失败: %v", err)
	} else {
		log.Info("安全审计数据库迁移完成")
	}

	// 确保权限与角色相关表存在
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS permissions (
			id VARCHAR(50) PRIMARY KEY,
			category VARCHAR(50),
			name VARCHAR(100),
			description TEXT
		);
	`).Error; err != nil {
		log.Warnf("创建permissions表失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE permissions ADD COLUMN IF NOT EXISTS name VARCHAR(100)`).Error; err != nil {
		log.Warnf("permissions.name 列创建失败: %v", err)
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS roles (
			id VARCHAR(50) PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			description TEXT,
			is_system BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
	`).Error; err != nil {
		log.Warnf("创建roles表失败: %v", err)
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS role_permissions (
			role_id VARCHAR(50) REFERENCES roles(id) ON DELETE CASCADE,
			permission_id VARCHAR(50) REFERENCES permissions(id) ON DELETE CASCADE,
			PRIMARY KEY (role_id, permission_id)
		);
	`).Error; err != nil {
		log.Warnf("创建role_permissions表失败: %v", err)
	}

	// 保障组织与用户表新列存在
	if err := db.Exec(`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS usage_summary JSONB DEFAULT '{}'::jsonb`).Error; err != nil {
		log.Warnf("organizations.usage_summary 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN DEFAULT FALSE`).Error; err != nil {
		log.Warnf("users.is_platform_admin 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS current_org_id UUID`).Error; err != nil {
		log.Warnf("users.current_org_id 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_active_org_id UUID`).Error; err != nil {
		log.Warnf("users.last_active_org_id 列创建失败: %v", err)
	}
	// 头像字段长度放宽以兼容外部URL
	if err := db.Exec(`DO $$ BEGIN IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='avatar') THEN ALTER TABLE users ALTER COLUMN avatar TYPE TEXT; END IF; END $$;`).Error; err != nil {
		log.Warnf("users.avatar 列类型调整失败: %v", err)
	}

	// 确保用量日志表存在
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_logs (
			id UUID PRIMARY KEY,
			org_id UUID NOT NULL,
			api_key_id VARCHAR(255) NOT NULL,
			user_id UUID NOT NULL,
			endpoint VARCHAR(255) NOT NULL,
			status_code INTEGER NOT NULL,
			request_id VARCHAR(255),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`).Error; err != nil {
		log.Warnf("创建usage_logs表失败: %v", err)
	}

	// 确保用量聚合表存在（供管理端查询）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_metrics (
			org_id UUID PRIMARY KEY,
			request_count INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`).Error; err != nil {
		log.Warnf("创建usage_metrics表失败: %v", err)
	}

	// 每日聚合表（组织维度）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily (
			org_id UUID NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily表失败: %v", err)
	}

	// 每日聚合表（用户维度）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily_user (
			org_id UUID NOT NULL,
			user_id UUID NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, user_id, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily_user表失败: %v", err)
	}

	// usage_logs 扩展以支持 OAuth 客户端统计
	if err := db.Exec(`
	DO $$
	BEGIN
	    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='usage_logs') THEN
	        ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS oauth_client_id TEXT;
	        CREATE INDEX IF NOT EXISTS idx_usage_logs_oauth_client_id ON usage_logs(oauth_client_id);
	    END IF;
	END $$;`).Error; err != nil {
		log.Warnf("usage_logs 扩展 oauth_client_id 失败: %v", err)
	}

	// 每日聚合表（OAuth客户端维度）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily_client (
			org_id UUID NOT NULL,
			oauth_client_id TEXT NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, oauth_client_id, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily_client表失败: %v", err)
	}

	// 每日聚合表（服务维度）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily_service (
			org_id UUID NOT NULL,
			service_id TEXT NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, service_id, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily_service表失败: %v", err)
	}

	// 每日聚合表（接口维度）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily_endpoint (
			org_id UUID NOT NULL,
			endpoint TEXT NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, endpoint, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily_endpoint表失败: %v", err)
	}

	// 每日聚合表（密钥维度）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily_key (
			org_id UUID NOT NULL,
			api_key_id TEXT NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, api_key_id, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily_key表失败: %v", err)
	}

	// 每日聚合表（密钥+负责人维度，供个人视角）
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_daily_key_user (
			org_id UUID NOT NULL,
			user_id UUID NOT NULL,
			api_key_id TEXT NOT NULL,
			date DATE NOT NULL,
			success INT DEFAULT 0,
			failed INT DEFAULT 0,
			total INT DEFAULT 0,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, user_id, api_key_id, date)
		);
	`).Error; err != nil {
		log.Warnf("创建usage_daily_key_user表失败: %v", err)
	}

	// usage_logs 唯一约束以提升计量准确性（request_id+endpoint）
	if err := db.Exec(`
	DO $$
	BEGIN
	    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uni_usage_logs_req_endpoint') THEN
	        ALTER TABLE usage_logs ADD CONSTRAINT uni_usage_logs_req_endpoint UNIQUE (request_id, endpoint);
	    END IF;
	END $$;`).Error; err != nil {
		log.Warnf("usage_logs 唯一约束创建失败: %v", err)
	}

	// 修复 permissions 表缺少时间戳列
	if err := db.Exec(`
	DO $$
	BEGIN
	    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='permissions') THEN
	        ALTER TABLE permissions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW();
	        ALTER TABLE permissions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();
	    END IF;
	END $$;`).Error; err != nil {
		log.Warnf("permissions 列扩展失败: %v", err)
	}

	// 统一 users.org_id 列类型为UUID（安全迁移）
	if err := db.Exec(`
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='org_id' AND data_type IN ('text','character varying')
			) THEN
				ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id_uuid UUID;
				UPDATE users SET org_id_uuid = NULLIF(org_id, '')::uuid WHERE org_id IS NOT NULL;
				ALTER TABLE users DROP COLUMN org_id;
				ALTER TABLE users RENAME COLUMN org_id_uuid TO org_id;
				CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);
			END IF;
		END $$;
	`).Error; err != nil {
		log.Warnf("users.org_id 类型迁移失败: %v", err)
	}

	// users 软删除与状态保障
	if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP WITH TIME ZONE`).Error; err != nil {
		log.Warnf("users.deleted_at 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'active'`).Error; err != nil {
		log.Warnf("users.status 列创建失败: %v", err)
	}

	// 组织成员活跃时间列
	if err := db.Exec(`ALTER TABLE organization_members ADD COLUMN IF NOT EXISTS last_active_at TIMESTAMP WITH TIME ZONE`).Error; err != nil {
		log.Warnf("organization_members.last_active_at 列创建失败: %v", err)
	}

	// 保障 api_keys 新列存在（兼容旧库）
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS prefix VARCHAR(20)`).Error; err != nil {
		log.Warnf("api_keys.prefix 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS total_requests24h INT DEFAULT 0`).Error; err != nil {
		log.Warnf("api_keys.total_requests24h 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS success_rate24h DOUBLE PRECISION DEFAULT 0`).Error; err != nil {
		log.Warnf("api_keys.success_rate24h 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_error_message TEXT`).Error; err != nil {
		log.Warnf("api_keys.last_error_message 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMP WITH TIME ZONE`).Error; err != nil {
		log.Warnf("api_keys.last_error_at 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_used_ip TEXT`).Error; err != nil {
		log.Warnf("api_keys.last_used_ip 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS created_by_user_id UUID`).Error; err != nil {
		log.Warnf("api_keys.created_by_user_id 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS secret_enc TEXT`).Error; err != nil {
		log.Warnf("api_keys.secret_enc 列创建失败: %v", err)
	}

	// 请求日志表补充Key快照字段
	if err := db.Exec(`ALTER TABLE api_request_logs ADD COLUMN IF NOT EXISTS api_key_name TEXT`).Error; err != nil {
		log.Warnf("api_request_logs.api_key_name 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE api_request_logs ADD COLUMN IF NOT EXISTS api_key_owner_id UUID`).Error; err != nil {
		log.Warnf("api_request_logs.api_key_owner_id 列创建失败: %v", err)
	}

	// 扩展 oauth_clients 表以支持IP白名单与速率限制
	if err := db.Exec(`
	DO $$
	BEGIN
	    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='oauth_clients') THEN
	        ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[] DEFAULT '{}'::text[];
	        ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS rate_limit_per_sec INTEGER DEFAULT 0;
	        ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS owner_id TEXT;
	        
	        IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_oauth_clients_ip_whitelist') THEN
	            CREATE INDEX idx_oauth_clients_ip_whitelist ON oauth_clients USING GIN (ip_whitelist);
	        END IF;
	    END IF;
	END $$;
	`).Error; err != nil {
		log.Warnf("oauth_clients 列扩展失败: %v", err)
	}

	// 邀请与通知表
	if err := db.Exec(`
        CREATE TABLE IF NOT EXISTS invitations (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            org_id VARCHAR(255) NOT NULL REFERENCES organizations(id),
            inviter_id VARCHAR(255) NOT NULL REFERENCES users(id),
            email VARCHAR(255) NOT NULL,
            role VARCHAR(50) NOT NULL,
            token TEXT NOT NULL,
            status VARCHAR(20) DEFAULT 'pending',
            expires_at TIMESTAMP NOT NULL,
            created_at TIMESTAMP DEFAULT NOW(),
            accepted_at TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_invites_email ON invitations(email);
        CREATE INDEX IF NOT EXISTS idx_invites_org ON invitations(org_id);
        DO $$
        BEGIN
            IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uni_invitations_token') THEN
                ALTER TABLE invitations ADD CONSTRAINT uni_invitations_token UNIQUE (token);
            END IF;
        END $$;
        CREATE UNIQUE INDEX IF NOT EXISTS idx_invites_pending_unique ON invitations(org_id, email) WHERE status = 'pending';
    `).Error; err != nil {
		log.Fatalf("创建 invitations 表失败: %v", err)
	} else {
		var reg *string
		_ = db.Raw("select to_regclass('public.invitations')").Scan(&reg).Error
		log.Infof("invitations 表存在状态: %v", reg)
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS notifications (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id VARCHAR(255) NOT NULL REFERENCES users(id),
			type VARCHAR(50),
			title VARCHAR(255),
			message TEXT,
			payload JSONB,
			is_read BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id);
		CREATE INDEX IF NOT EXISTS idx_notifications_user_read ON notifications(user_id, is_read);
	`).Error; err != nil {
		log.Fatalf("创建 notifications 表失败: %v", err)
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS organization_quotas (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			organization_id VARCHAR(255) NOT NULL REFERENCES organizations(id),
			service_type VARCHAR(50) NOT NULL,
			allocation INT NOT NULL DEFAULT 0,
			consumed INT NOT NULL DEFAULT 0,
			reset_at TIMESTAMP WITH TIME ZONE,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_org_quotas_unique ON organization_quotas(organization_id, service_type);
		CREATE INDEX IF NOT EXISTS idx_org_quotas_org ON organization_quotas(organization_id);
	`).Error; err != nil {
		log.Fatalf("创建 organization_quotas 表失败: %v", err)
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_actions (
			id VARCHAR(100) PRIMARY KEY,
			name VARCHAR(100),
			category VARCHAR(50) DEFAULT 'General',
			created_at TIMESTAMP DEFAULT NOW()
		);
	`).Error; err != nil {
		log.Fatalf("创建 audit_actions 表失败: %v", err)
	}
	{
		seed := []struct{ id, name, cat string }{
			{"login", "Login", "Auth"},
			{"create_key", "Create API Key", "Keys"},
			{"revoke_key", "Revoke API Key", "Keys"},
			{"member.invite", "Invite Member", "Team"},
			{"member.remove", "Remove Member", "Team"},
			{"member.role_update", "Update Member Role", "Team"},
			{"member.suspend", "Suspend Member", "Team"},
			{"member.activate", "Activate Member", "Team"},
			{"member.join", "Join Organization", "Team"},
			{"update_plan", "Update Plan", "Billing"},
			{"view_organization_members", "View Members", "Team"},
			{"key.update_scopes", "Update Key Scopes", "Keys"},
			{"key.show_secret", "Show Key Secret", "Keys"},
		}
		for _, s := range seed {
			_ = db.Exec("INSERT INTO audit_actions(id,name,category) VALUES(?, ?, ?) ON CONFLICT (id) DO NOTHING", s.id, s.name, s.cat).Error
		}
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS plans (
			id VARCHAR(50) PRIMARY KEY,
			name VARCHAR(100),
			quota_config JSONB,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
	`).Error; err != nil {
		log.Fatalf("创建 plans 表失败: %v", err)
	}
	// 保障 plans 新列存在（管理端所需字段）
	if err := db.Exec(`ALTER TABLE plans ADD COLUMN IF NOT EXISTS price INTEGER DEFAULT 0`).Error; err != nil {
		log.Warnf("plans.price 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE plans ADD COLUMN IF NOT EXISTS currency VARCHAR(10) DEFAULT 'USD'`).Error; err != nil {
		log.Warnf("plans.currency 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE plans ADD COLUMN IF NOT EXISTS requests_limit INTEGER DEFAULT 0`).Error; err != nil {
		log.Warnf("plans.requests_limit 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE plans ADD COLUMN IF NOT EXISTS features JSONB DEFAULT '{}'::jsonb`).Error; err != nil {
		log.Warnf("plans.features 列创建失败: %v", err)
	}
	if err := db.Exec(`ALTER TABLE plans ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE`).Error; err != nil {
		log.Warnf("plans.is_active 列创建失败: %v", err)
	}
	{
		var cnt int64
		_ = db.Table("plans").Count(&cnt).Error
		if cnt == 0 {
			_ = db.Exec("INSERT INTO plans(id,name,quota_config) VALUES(?, ?, ?)", "starter", "Starter", `{"ocr":{"limit":50,"period":"lifetime"},"face":{"limit":30,"period":"lifetime"},"liveness":{"limit":10,"period":"lifetime"}}`).Error
			_ = db.Exec("INSERT INTO plans(id,name,quota_config) VALUES(?, ?, ?)", "growth", "Growth", `{"ocr":{"limit":5000,"period":"monthly"},"face":{"limit":3000,"period":"monthly"},"liveness":{"limit":1000,"period":"monthly"}}`).Error
			_ = db.Exec("INSERT INTO plans(id,name,quota_config) VALUES(?, ?, ?)", "scale", "Scale", `{"ocr":{"limit":50000,"period":"monthly"},"face":{"limit":30000,"period":"monthly"},"liveness":{"limit":10000,"period":"monthly"}}`).Error
		}
	}

	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS global_configs (
			key VARCHAR(100) PRIMARY KEY,
			value TEXT,
			updated_at TIMESTAMP DEFAULT NOW()
		);
	`).Error; err != nil {
		log.Fatalf("创建 global_configs 表失败: %v", err)
	}
	_ = db.Exec("INSERT INTO global_configs(key,value) VALUES('daily_registration_cap','1000') ON CONFLICT (key) DO NOTHING").Error

	// 权限与角色种子
	{
		var permCount int64
		err = db.Table("permissions").Count(&permCount).Error
		if err != nil {
			panic(err)
		}

		if err == nil && permCount == 0 {
			permSeeds := []struct{ id, cat, name, desc string }{
				{"org.read", "Organization", "Read Organization", "Read organization details"},
				{"org.update", "Organization", "Update Organization", "Update organization settings"},
				{"org.delete", "Organization", "Delete Organization", "Delete organization"},
				{"team.read", "Team", "View Members", "View members"},
				{"team.invite", "Team", "Invite Members", "Invite members"},
				{"team.write", "Team", "Modify Members", "Modify/remove members"},
				{"billing.read", "Billing", "View Billing", "View billing"},
				{"org.billing.read", "Billing", "View Organization Billing", "View org billing"},
				{"billing.write", "Billing", "Modify Billing", "Modify payment/subscription"},
				{"keys.read", "API Keys", "View API Keys", "View API keys"},
				{"keys.write", "API Keys", "Manage API Keys", "Create/Revoke API keys"},
				{"logs.read", "Logs", "View Audit Logs", "View audit logs"},
				{"org.usage.read", "Logs", "View Usage", "View organization usage"},
				{"org.audit", "Logs", "Export Audit Logs", "Export organization audit logs"},
			}
			for _, p := range permSeeds {
				_ = db.Exec("INSERT INTO permissions(id, category, name, description) VALUES(?, ?, ?, ?) ON CONFLICT (id) DO NOTHING", p.id, p.cat, p.name, p.desc).Error
			}
		}
		_ = db.Exec("UPDATE permissions SET name = COALESCE(name, description) WHERE name IS NULL OR name = ''").Error
		// 系统角色及关系
		seedRole := func(id, name, desc string, perms []string) {
			var rCnt int64
			_ = db.Table("roles").Where("id = ?", id).Count(&rCnt).Error
			if rCnt == 0 {
				_ = db.Exec("INSERT INTO roles(id, name, description, is_system, created_at) VALUES(?, ?, ?, ?, NOW())", id, name, desc, true).Error
			}
			for _, pid := range perms {
				_ = db.Exec("INSERT INTO role_permissions(role_id, permission_id) VALUES(?, ?) ON CONFLICT DO NOTHING", id, pid).Error
			}
		}
		// owner: 授予所有权限
		var allPerms []struct{ ID string }
		_ = db.Raw("SELECT id FROM permissions").Scan(&allPerms).Error
		var allIDs []string
		for _, ap := range allPerms {
			allIDs = append(allIDs, ap.ID)
		}
		seedRole("owner", "Owner", "系统所有者", allIDs)
		// admin
		seedRole("admin", "Administrator", "组织管理员", []string{"org.read", "team.read", "team.invite", "team.write", "keys.read", "keys.write", "billing.read", "logs.read", "org.audit"})
		// developer
		seedRole("developer", "Developer", "开发者", []string{"keys.read", "keys.write", "logs.read"})
		// viewer
		seedRole("viewer", "Viewer", "只读观察者", []string{"org.read", "team.read", "keys.read", "billing.read", "logs.read"})
	}

	// 平台管理员种子（仅当不存在时创建）
	{
		var adminCount int64
		_ = db.Model(&models.User{}).Where("is_platform_admin = ?", true).Count(&adminCount).Error
		if adminCount == 0 {
			seedEmail := os.Getenv("PLATFORM_ADMIN_EMAIL")
			if seedEmail == "" {
				seedEmail = "admin@verilocale.com"
			}
			seedPassword := os.Getenv("PLATFORM_ADMIN_PASSWORD")
			if seedPassword == "" {
				seedPassword = "123qwe"
			}
			org := models.Organization{ID: utils.GenerateID(), Name: "System", PlanID: "scale", BillingEmail: seedEmail, Status: "active"}
			tx := db.Begin()
			if tx.Error != nil {
				log.Errorf("平台管理员种子初始化失败: %v", tx.Error)
			} else {
				if err := tx.Create(&org).Error; err != nil {
					log.Errorf("平台管理员组织创建失败: %v", err)
					tx.Rollback()
				} else {
					hashed, _ := bcrypt.GenerateFromPassword([]byte(seedPassword), bcrypt.DefaultCost)
					u := models.User{ID: utils.GenerateID(), Email: seedEmail, AvatarURL: fmt.Sprintf("https://api.dicebear.com/7.x/avataaars/svg?seed=%s", seedEmail), Password: string(hashed), Name: "Platform Admin", Role: "admin", OrgID: org.ID, OrgRole: "owner", CurrentOrgID: org.ID, Status: "active", IsPlatformAdmin: true}
					if err := tx.Create(&u).Error; err != nil {
						log.Errorf("平台管理员用户创建失败: %v", err)
						tx.Rollback()
					} else {
						m := models.OrganizationMember{ID: utils.GenerateID(), OrganizationID: org.ID, UserID: u.ID, Role: "owner", Status: "active"}
						if err := tx.Create(&m).Error; err != nil {
							log.Errorf("平台管理员成员创建失败: %v", err)
							tx.Rollback()
						} else if err := tx.Commit().Error; err != nil {
							log.Errorf("平台管理员种子提交失败: %v", err)
						} else {
							log.Infof("✅ 平台管理员已初始化: %s", seedEmail)
						}
					}
				}
			}
		}
	}

	// 已改为基于 permissions/role_permissions 表的关系种子（上方 seedRole 已处理）

	if err := db.Exec(`
		DO $$
		DECLARE
			type_name text;
		BEGIN
			SELECT data_type INTO type_name FROM information_schema.columns WHERE table_name='api_keys' AND column_name='ip_whitelist';
			IF type_name = 'text' THEN
				EXECUTE 'ALTER TABLE api_keys ALTER COLUMN ip_whitelist TYPE TEXT[] USING CASE WHEN ip_whitelist IS NULL THEN ARRAY[]::TEXT[] ELSE string_to_array(ip_whitelist, ",") END';
			END IF;
		END
		$$;
	`).Error; err != nil {
		log.Warnf("ip_whitelist列类型修正失败: %v", err)
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

	// 初始化双向鉴权中间件
	bidirectionalAuth := middleware.NewBidirectionalAuth(
		cfg.Security.KongSharedSecret, // Kong共享密钥
		cfg.Security.ServiceSecretKey, // 服务签名密钥
		"kyc-service",                 // 服务名称
	)

	// 初始化心跳管理器
	heartbeatManager := middleware.NewHeartbeatManager(
		bidirectionalAuth,
		30*time.Second, // 心跳间隔
		5*time.Second,  // 超时时间
		3,              // 最大重试次数
	)

	// 注册健康状态变化回调
	heartbeatManager.RegisterHealthCallback(func(healthy bool, reason string) {
		if healthy {
			log.Info("服务健康状态恢复正常")
		} else {
			log.WithField("reason", reason).Error("服务健康状态异常")
		}
	})

	// 创建Gin引擎
	gin.SetMode(cfg.GinMode)
	r := gin.New()

	// 全局中间件 - 顺序很重要
	r.Use(middleware.Recovery())                         // 自定义恢复中间件
	r.Use(middleware.ErrorHandler())                     // 统一错误处理中间件
	r.Use(middleware.EnterpriseMetricsInstrumentation()) // 企业级OTel指标
	r.Use(middleware.TraceMiddleware())                  // Trace中间件必须在Logger之前
	r.Use(middleware.CORS())
	r.Use(middleware.Security())
	//r.Use(bidirectionalAuth.BypassDetectionMiddleware()) // 绕过检测中间件
	//r.Use(bidirectionalAuth.KongAuthMiddleware())        // Kong网关身份验证
	//r.Use(bidirectionalAuth.ServiceAuthMiddleware())     // 服务到网关认证
	r.Use(middleware.Logger()) // Logger中间件最后，可以访问trace信息

	// 健康检查（支持双向鉴权）
	healthCheck := middleware.NewBidirectionalHealthCheck(bidirectionalAuth)
	r.GET("/health", healthCheck.HealthCheckHandler)

	// 心跳检测接口
	//r.GET("/heartbeat", heartbeatManager.HeartbeatHandler)
	//r.GET("/security-heartbeat", heartbeatManager.SecurityHeartbeatHandler)

	// Prometheus指标
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API路由组
	v1 := r.Group("/api/v1")
	v1.Use(middleware.InjectOrgContext())
	v1.Use(middleware.APIRequestLogMiddleware(kycService))
	{
		meta := api.NewMetaHandler(kycService)
		v1.GET("/meta/permissions", meta.GetPermissions)
		v1.GET("/meta/roles", meta.GetRoles)
		v1.POST("/meta/roles/:id", middleware.JWTAuth(kycService), middleware.RequirePlatformAdmin(), meta.CreateRole)
		v1.PUT("/meta/roles/:id", middleware.JWTAuth(kycService), middleware.RequirePlatformAdmin(), meta.UpdateRole)
		v1.DELETE("/meta/roles/:id", middleware.JWTAuth(kycService), middleware.RequirePlatformAdmin(), meta.DeleteRole)
		// JWT令牌生成接口（标准接口）
		token := v1.Group("/token")
		{
			jwtGenerator := api.NewKongJWTGenerator(cfg.Security.JWTSecret, "kyc-service")
			token.POST("/generate", jwtGenerator.GenerateKongJWTHandler)
		}

		// 控制台认证相关API（新前端使用）
		consoleAuth := v1.Group("/auth")
		{
			consoleAuthHandler := api.NewConsoleAuthHandler(kycService)
			consoleAuth.POST("/login", consoleAuthHandler.Login)
			consoleAuth.POST("/register", consoleAuthHandler.Register)
			consoleAuth.GET("/me", middleware.JWTAuth(kycService), consoleAuthHandler.Me)
		}

		// 控制台API（需要用户认证）
		console := v1.Group("/console")
		console.Use(middleware.JWTAuth(kycService))
		console.Use(middleware.InjectOrgContext())
		{
			consoleHandler := api.NewConsoleHandler(kycService)
			userAuthHandler := api.NewUserAuthHandler(kycService)
			console.GET("/users/me", consoleHandler.GetCurrentUser)
			console.PUT("/users/me", consoleHandler.UpdateUserProfile)
			console.PUT("/users/me/password", userAuthHandler.UpdatePassword)
			console.GET("/keys", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("keys.read"), consoleHandler.ListAPIKeys)
			console.POST("/keys", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("keys.write"), consoleHandler.CreateAPIKey)
			console.DELETE("/keys/:id", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("keys.write"), consoleHandler.RevokeAPIKey)
			console.PATCH("/keys/:id", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("keys.write"), consoleHandler.UpdateAPIKeyScopes)
			console.GET("/keys/:id/secret", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("keys.read"), consoleHandler.GetAPIKeySecret)
			console.GET("/usage", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("logs.read"), consoleHandler.GetUsage)
			console.GET("/usage/stats", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("logs.read"), consoleHandler.GetUsageStats)
			console.GET("/logs", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission("logs.read"), consoleHandler.GetLogs)
			console.DELETE("/users/me", consoleHandler.DeleteMe)
			console.GET("/me/notifications", middleware.JWTAuth(kycService), consoleHandler.GetNotifications)
			console.PUT("/me/notifications/:id/read", middleware.JWTAuth(kycService), consoleHandler.MarkNotificationRead)
			console.GET("/usage/quota", middleware.RequireOrganizationHeader(kycService), consoleHandler.GetQuotaStatus)

			// OAuth 客户端管理（组织维度）
			clientHandler := api.NewClientHandler(kycService)
			clients := console.Group("/oauth/clients")
			clients.Use(middleware.RequireOrganizationHeader(kycService))
			clients.POST("/register", middleware.RequirePermission("keys.write"), clientHandler.RegisterClient)
			clients.GET("", middleware.RequirePermission("keys.read"), clientHandler.ListClients)
			clients.DELETE(":client_id", middleware.RequirePermission("keys.write"), clientHandler.DeleteClient)
			clients.POST(":id/rotate", middleware.RequirePermission("keys.write"), clientHandler.RotateClientSecret)
			clients.PATCH(":id/status", middleware.RequirePermission("keys.write"), clientHandler.UpdateClientStatus)
			clients.GET(":id/secret", middleware.JWTAuth(kycService), middleware.RequireOrganizationHeader(kycService), middleware.InjectOrgContext(), clientHandler.GetClientSecret)

		}

		// 用户通知别名接口（满足规范 /users/me）
		{
			consoleHandler := api.NewConsoleHandler(kycService)
			v1.GET("/users/me/notifications", middleware.JWTAuth(kycService), consoleHandler.GetNotifications)
			v1.PUT("/users/me/notifications/:id/read", middleware.JWTAuth(kycService), consoleHandler.MarkNotificationRead)
		}

		// 个人邀请接口（全局视角）
		{
			uih := api.NewUserInvitationHandler(kycService)
			v1.GET("/users/me/invitations", middleware.JWTAuth(kycService), uih.ListMyInvitations)
			v1.POST("/users/me/invitations/:id/accept", middleware.JWTAuth(kycService), uih.AcceptMyInvitation)
			v1.POST("/users/me/invitations/:id/decline", middleware.JWTAuth(kycService), uih.DeclineMyInvitation)
		}

		// 管理员API（需要超级管理员权限）
		admin := v1.Group("/admin")
		admin.Use(middleware.JWTAuth(kycService))
		admin.Use(middleware.RequirePlatformAdmin())
		{
			adminHandler := api.NewAdminHandler(kycService)
			admin.GET("/users", adminHandler.GetUserList)
			admin.PUT("/users/:id/status", adminHandler.UpdateUserStatus)
			admin.PUT("/users/:id", adminHandler.UpdateUserAdmin)
			admin.GET("/organizations", adminHandler.GetOrganizationList)
			admin.PUT("/organizations/:id/plan", middleware.RequireOrganizationHeader(kycService), adminHandler.UpdateOrganizationPlan)
			admin.GET("/audit-logs", adminHandler.GetAuditLogs)
			admin.GET("/stats/overview", adminHandler.GetOverviewStats)
			admin.PUT("/config/plans/:plan_id/quota", adminHandler.UpdatePlanQuota)
			admin.PUT("/config/global", adminHandler.UpdateGlobalConfig)
			// 权限定义管理
			admin.POST("/permissions", adminHandler.CreatePermission)
			admin.DELETE("/permissions/:id", adminHandler.DeletePermission)
			// 新增：计划管理
			admin.GET("/plans", adminHandler.GetPlans)
			admin.PUT("/plans/:id", adminHandler.UpdatePlan)
			// 新增：配额管理
			admin.GET("/organizations/:id/quotas", middleware.RequireOrganizationHeader(kycService), adminHandler.GetOrganizationQuotas)
			admin.POST("/organizations/:id/quotas/adjust", middleware.RequireOrganizationHeader(kycService), adminHandler.AdjustOrganizationQuota)
		}

		// 密码重置API
		passwordReset := v1.Group("/auth/password-reset")
		{
			passwordResetHandler := api.NewPasswordResetHandler(kycService)
			passwordReset.POST("/request", passwordResetHandler.RequestPasswordReset)
			passwordReset.POST("/confirm", passwordResetHandler.ConfirmPasswordReset)
		}

		// Google OAuth API
		authGroup := v1.Group("/auth")
		{
			googleOAuthHandler := api.NewGoogleOAuthHandler(kycService)
			authGroup.POST("/google", googleOAuthHandler.GoogleLogin)
		}

		// OAuth2.0认证（保留原有功能）
		oauthGroup := v1.Group("/oauth")
		{
			oauthHandler := api.NewAuthHandler(kycService)
			oauthGroup.POST("/token", oauthHandler.GetToken)
			oauthGroup.POST("/refresh", oauthHandler.RefreshToken)
			oauthGroup.POST("/revoke", oauthHandler.Revoke)
			oauthGroup.POST("/introspect", oauthHandler.Introspect)
		}

		// 通知与邮件发送（需权限）
		notifications := v1.Group("/notifications")
		//notifications.Use(middleware.JWTAuth(kycService))
		{
			nh := api.NewNotificationHandler(kycService)
			//notifications.POST("/email", middleware.RequirePermission("notifications.send"), nh.SendEmail)
			notifications.POST("/email", nh.SendEmail)
		}

		discovery := api.NewDiscoveryHandler()
		r.GET("/.well-known/oauth-authorization-server", discovery.WellKnown)
		r.GET("/jwks.json", discovery.JWKS)

		// Docs & Security Guide
		docs := api.NewDocsHandler()
		v1.GET("/docs/security", docs.SecurityDoc)
		v1.GET("/docs/error-codes", docs.ErrorCodesDoc)
		// 提供 swagger.json 静态文件访问（需提前生成 docs/swagger.json）
		//r.StaticFile("/swagger.json", "./docs/swagger.json")
		r.StaticFile("/swagger-public.json", "./docs/public/swagger.json")
		r.GET("/docs", func(c *gin.Context) {
			c.Writer.WriteString(`<!doctype html><html><head><title>Swagger UI</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"></head><body><div id="swagger"></div><script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>window.ui=SwaggerUIBundle({url:'/swagger-public.json',dom_id:'#swagger'});</script></body></html>`)
		})

		// Callback endpoints
		callbacks := v1.Group("/callbacks")
		{
			kycHandler := api.NewKYCHandler(kycService)
			callbacks.POST("/liveness/action", kycHandler.LivenessActionCallback)
		}

		// API密钥管理（需要用户认证）
		//keys := v1.Group("/keys")
		//keys.Use(middleware.JWTAuth(kycService))
		//keys.Use(middleware.RequireOrganizationHeader(kycService))
		//keys.Use(middleware.InjectOrgContext())
		//{
		//	apiKeyHandler := api.NewAPIKeyHandler(kycService)
		//	keys.GET("", middleware.RequirePermission("keys.read"), apiKeyHandler.GetAPIKeys)
		//	keys.POST("", middleware.RequirePermission("keys.write"), apiKeyHandler.CreateAPIKey)
		//	keys.DELETE("/:id", middleware.RequirePermission("keys.write"), apiKeyHandler.DeleteAPIKey)
		//	keys.POST("/:id/approve", middleware.RequirePermission("keys.write"), apiKeyHandler.ApproveAPIKey)
		//}

		// 组织管理（需要用户认证）
		// 组织切换（JWT即可，无需当前组织上下文）
		orgSwitchHandler := api.NewOrganizationHandler(kycService)
		v1.POST("/orgs/switch", middleware.JWTAuth(kycService), orgSwitchHandler.SwitchOrganization)
		v1.POST("/auth/switch-org", middleware.JWTAuth(kycService), orgSwitchHandler.SwitchOrganization)

		orgs := v1.Group("/orgs")
		orgs.Use(middleware.JWTAuth(kycService))
		orgs.Use(middleware.RequireOrganizationHeader(kycService))
		orgs.Use(middleware.InjectOrgContext())
		{
			orgHandler := api.NewOrganizationHandler(kycService)
			// 组织与成员能力控制
			orgs.GET("/current", middleware.RequirePermission("org.read"), orgHandler.GetCurrentOrganization)
			orgs.GET("/members", middleware.RequirePermission("team.read"), orgHandler.GetOrganizationMembers)
			orgs.POST("/members", middleware.RequirePermission("team.invite"), orgHandler.InviteOrganizationMember)
			orgs.PATCH("/members/:id", middleware.RequirePermission("team.write"), orgHandler.UpdateMemberRole)
			orgs.PUT("/members/:id/password", middleware.RequirePermission("team.write"), orgHandler.ResetMemberPassword)
			orgs.PATCH("/members/:id/status", middleware.RequirePermission("team.write"), orgHandler.UpdateMemberStatus)
			orgs.PUT("/plan", middleware.RequirePermission("billing.write"), orgHandler.UpdatePlan)
			orgs.GET("/:org_id/usage/summary", middleware.RequirePermission("logs.read"), orgHandler.GetUsageSummary)
			orgs.DELETE("/members/:id", middleware.RequirePermission("team.write"), orgHandler.DeleteOrganizationMember)
			orgs.GET("/billing", middleware.ScopePermission([]string{"org.billing.read", "billing.read"}), orgHandler.GetBilling)
			orgs.GET("/usage/daily", middleware.ScopePermission([]string{"org.usage.read", "logs.read"}), orgHandler.GetUsageDaily)
			orgs.GET("/usage/detailed", middleware.ScopePermission([]string{"org.usage.read", "logs.read"}), orgHandler.GetUsageDetailedV2)
			orgs.GET("/audit-logs", middleware.RequirePermission("logs.read"), orgHandler.GetOrgAuditLogs)
			orgs.GET("/audit-logs/actions", middleware.RequirePermission("org.audit"), orgHandler.GetAuditActions)
			orgs.GET("/audit-logs/export", middleware.RequirePermission("org.audit"), orgHandler.ExportOrgAuditLogs)
			orgs.POST("/invitations", middleware.RequirePermission("team.invite"), orgHandler.CreateInvitation)
			orgs.GET("/invitations", middleware.RequirePermission("team.read"), orgHandler.ListInvitations)
			orgs.DELETE("/invitations/:id", middleware.RequirePermission("team.write"), orgHandler.RevokeInvitation)
			// 注销组织（仅owner）
			orgs.DELETE("/:id", middleware.RequirePermission("org.delete"), orgHandler.DeleteOrganization)
		}

		// 创建组织（仅JWT，无需组织上下文）
		orgHandler := api.NewOrganizationHandler(kycService)
		v1.POST("/orgs", middleware.JWTAuth(kycService), orgHandler.CreateOrganization)

		// 邀请处理（登录用户）
		inv := v1.Group("/invitations")
		inv.Use(middleware.JWTAuth(kycService))
		{
			orgHandler := api.NewOrganizationHandler(kycService)
			inv.POST("/accept", orgHandler.AcceptInvitation)
			inv.POST("/:id/accept", orgHandler.AcceptInvitationByID)
			inv.POST("/:id/decline", orgHandler.DeclineInvitationByID)
		}

		// KYC相关API（需要API密钥认证）
		kyc := v1.Group("/kyc")
		kyc.Use(middleware.APIOrOAuthAuth(kycService)) // 支持OAuth2客户端凭证或API Key
		kyc.Use(middleware.InjectOrgContext())
		kyc.Use(middleware.RequestBodyLogger())
		kyc.Use(middleware.ResponseCapture())
		kyc.Use(middleware.Idempotency(redisClient))
		kyc.Use(middleware.RateLimitWithKey(redisClient, kycService)) // 启用IP级别限流（每秒100次）并标记Key
		kyc.Use(middleware.Quota(redisClient, kycService))            // 按组织计划配额检查与扣费
		kyc.Use(middleware.KYCUsageMeter(kycService))                 // 业务计量入队（异步消费入库）
		{
			kycHandler := api.NewKYCHandler(kycService)

			// OCR识别
			kyc.POST("/ocr", middleware.RequireKeyScope("ocr:read"), kycHandler.OCR)
			// 人脸识别
			kyc.POST("/face/search", middleware.RequireKeyScope("face:read"), kycHandler.FaceSearch)
			kyc.POST("/face/compare", middleware.RequireKeyScope("face:read"), kycHandler.FaceCompare)
			kyc.POST("/face/detect", middleware.RequireKeyScope("face:read"), kycHandler.FaceDetect)

			// 活体检测（WebSocket）
			kyc.POST("/liveness/silent", middleware.RequireKeyScope("liveness:read"), kycHandler.LivenessSilent)
			kyc.POST("/liveness/video", middleware.RequireKeyScope("liveness:read"), kycHandler.LivenessVideo)
			kyc.GET("/liveness/ws", middleware.RequireKeyScope("liveness:read"), kycHandler.LivenessWebSocket)
			// Action liveness (MVP placeholder)
			kyc.POST("/liveness/action/session", middleware.RequireKeyScope("liveness:read"), kycHandler.LivenessActionSession)
			kyc.POST("/liveness/action/upload", middleware.RequireKeyScope("liveness:read"), kycHandler.LivenessActionUpload)
			kyc.POST("/liveness/action/verify", middleware.RequireKeyScope("liveness:read"), kycHandler.LivenessActionVerify)
			// 完整KYC流程
			kyc.POST("/verify", middleware.RequireKeyScope("kyc:verify"), kycHandler.CompleteKYC)

			// 查询KYC状态
			kyc.GET("/status/:request_id", kycHandler.GetKYCStatus)
		}
	}

	faces := v1.Group("/faces")
	faces.Use(middleware.APIKeyAuth(kycService))
	faces.Use(middleware.InjectOrgContext())
	{
		faceImageHandler := api.NewFaceImageHandler(kycService)
		faces.GET(":id/image", faceImageHandler.GetImage)
	}

	images := v1.Group("/images")
	images.Use(middleware.APIKeyAuth(kycService))
	images.Use(middleware.InjectOrgContext())
	{
		imageHandler := api.NewImageHandler(kycService)
		images.POST("", imageHandler.Upload)
		images.GET(":id/image", imageHandler.GetImage)
	}

	// 启动HTTP服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: r,
	}

	// 优雅关闭
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	log.Infof("KYC服务启动成功，端口: %d", cfg.Port)

	// 启动心跳检测
	//heartbeatManager.Start(ctx)

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("正在关闭服务...")

	// 停止心跳检测
	//heartbeatManager.Stop()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Errorf("服务器关闭失败: %v", err)
	}

	log.Info("服务已关闭")
	// 确保组织成员表存在
	if err := db.Exec(`
        CREATE TABLE IF NOT EXISTS organization_members (
            id VARCHAR(255) PRIMARY KEY,
            organization_id VARCHAR(255) NOT NULL,
            user_id VARCHAR(255) NOT NULL,
            role VARCHAR(64) NOT NULL,
            status VARCHAR(32) NOT NULL DEFAULT 'active',
            created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_org_members_org ON organization_members (organization_id);
        CREATE INDEX IF NOT EXISTS idx_org_members_user ON organization_members (user_id);
    `).Error; err != nil {
		log.Warnf("创建organization_members表失败: %v", err)
	} else {
		log.Info("✅ 组织成员表已存在或创建成功")
	}
}
