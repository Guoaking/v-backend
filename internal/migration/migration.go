package migration

import (
	"fmt"
	"os"

	"kyc-service/internal/models"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/utils"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

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

// Run executes all database migrations
func Run(db *gorm.DB) error {
	log := logger.GetLogger()

	// 执行安全审计数据库迁移
	log.Info("执行安全审计数据库迁移...")
	// 确保pgcrypto扩展可用以支持gen_random_uuid()
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto").Error; err != nil {
		log.Warnf("创建pgcrypto扩展失败: %v", err)
	}
	if err := executeSecurityAuditMigration(db); err != nil {
		log.Fatalf("安全审计数据库迁移失败: %v", err)
		return err
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

	// 安全且幂等的权限名称变更 (绕过外键约束问题)
	// 我们不直接 UPDATE 主键，而是:
	// 1. 插入新权限
	// 2. 将关联表数据复制到新权限
	// 3. 删除旧权限
	migratePermission := func(oldID, newID, category, name, desc string) {
		// 1. 插入新权限（如果不存在）
		_ = db.Exec("INSERT INTO permissions(id, category, name, description) VALUES(?, ?, ?, ?) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name", newID, category, name, desc).Error
		
		// 2. 迁移角色关联：把拥有旧权限的角色，也赋予新权限
		_ = db.Exec("INSERT INTO role_permissions(role_id, permission_id) SELECT role_id, ? FROM role_permissions WHERE permission_id = ? ON CONFLICT DO NOTHING", newID, oldID).Error
		
		// 3. 删除旧权限的关联
		_ = db.Exec("DELETE FROM role_permissions WHERE permission_id = ?", oldID).Error
		
		// 4. 删除旧权限定义
		_ = db.Exec("DELETE FROM permissions WHERE id = ?", oldID).Error
	}

	migratePermission("keys.read", "oauth.read", "OAuth Apps", "View OAuth Apps", "View OAuth applications")
	migratePermission("keys.write", "oauth.write", "OAuth Apps", "Manage OAuth Apps", "Create/Revoke OAuth applications")

	// 权限与角色种子
	{
		var permCount int64
		err := db.Table("permissions").Count(&permCount).Error
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
				{"oauth.read", "OAuth Apps", "View OAuth Apps", "View OAuth applications"},
				{"oauth.write", "OAuth Apps", "Manage OAuth Apps", "Create/Revoke OAuth applications"},
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
		seedRole("admin", "Administrator", "组织管理员", []string{"org.read", "team.read", "team.invite", "team.write", "oauth.read", "oauth.write", "billing.read", "logs.read", "org.audit"})
		// developer
		seedRole("developer", "Developer", "开发者", []string{"oauth.read", "oauth.write", "logs.read"})
		// viewer
		seedRole("viewer", "Viewer", "只读观察者", []string{"org.read", "team.read", "oauth.read", "billing.read", "logs.read"})
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

	// 注入系统内置的 OAuth Client (Playground STS 使用)
	{
		var sysClientCount int64
		_ = db.Model(&models.OAuthClient{}).Where("client_id = ?", "sys_web_console_playground").Count(&sysClientCount).Error
		if sysClientCount == 0 {
			// 如果没有环境变量，生成一个随机的超长字符串作为兜底，反正前端也不需要知道
			sysSecret := os.Getenv("SYS_PLAYGROUND_CLIENT_SECRET")
			if sysSecret == "" {
				sysSecret = utils.GenerateID() + utils.GenerateID() 
			}
			sysClient := models.OAuthClient{
				ID:              "sys_web_console_playground", // 强行写入指定 ID
				Secret:          sysSecret,
				Name:            "System Web Console Playground",
				Description:     "Built-in client for web console playground STS. DO NOT DELETE.",
				Status:          "active",
				Scopes:          "ocr:read face:read liveness:read kyc:verify",
				TokenTTLSeconds: 900, // 15分钟
				IsSystem:        true,
			}
			if err := db.Create(&sysClient).Error; err != nil {
				log.Errorf("系统内置 OAuth Client 初始化失败: %v", err)
			} else {
				log.Info("系统内置 OAuth Client 初始化成功")
			}
		}
	}

	return nil
}
