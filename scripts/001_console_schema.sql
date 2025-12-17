-- backend/migrations/001_console_schema.sql

-- 启用 UUID 扩展
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. 组织表 (Organizations)
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    billing_email VARCHAR(255),
    plan_id VARCHAR(50) DEFAULT 'starter', -- 对应前端的 starter, growth, scale
    stripe_customer_id VARCHAR(255),
    status VARCHAR(50) DEFAULT 'active', -- 'active', 'suspended'
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. 用户表 (Users)
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL, -- 存储 bcrypt 哈希
    full_name VARCHAR(255) NOT NULL,
    avatar_url TEXT,
    company VARCHAR(255),
    role VARCHAR(50) DEFAULT 'user', -- 'user' 或 'admin' (平台管理员)
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    org_role VARCHAR(50) DEFAULT 'owner', -- 'owner', 'editor', 'viewer'
    status VARCHAR(50) DEFAULT 'active', -- 'active', 'suspended'
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP WITH TIME ZONE
);

-- 3. API 密钥表 (API Keys)
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    secret_key VARCHAR(255) UNIQUE NOT NULL, -- sk_live_...
    secret_hash VARCHAR(255) NOT NULL, -- 用于验证的哈希
    scopes TEXT[] DEFAULT '{"ocr:read"}', -- 对应前端 ApiScope
    status VARCHAR(50) DEFAULT 'active', -- 'active', 'revoked'
    last_used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 4. 用量统计表 (Usage Metrics - 简化版)
CREATE TABLE IF NOT EXISTS usage_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    request_count INTEGER DEFAULT 0,
    error_count INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(org_id, date)
);

-- 5. 审计日志 (Audit Logs)
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    action VARCHAR(255) NOT NULL, -- e.g., 'create_key', 'update_plan'
    details JSONB,
    ip_address VARCHAR(45),
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 6. 组织成员邀请表 (Organization Invitations)
CREATE TABLE IF NOT EXISTS organization_invitations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    role VARCHAR(50) DEFAULT 'viewer', -- 'owner', 'editor', 'viewer'
    invited_by UUID REFERENCES users(id) ON DELETE CASCADE,
    token VARCHAR(255) UNIQUE NOT NULL,
    status VARCHAR(50) DEFAULT 'pending', -- 'pending', 'accepted', 'expired'
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(org_id, email)
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_org_id ON api_keys(org_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_secret_key ON api_keys(secret_key);
CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys(status);
CREATE INDEX IF NOT EXISTS idx_usage_metrics_org_id ON usage_metrics(org_id);
CREATE INDEX IF NOT EXISTS idx_usage_metrics_date ON usage_metrics(date);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_org_id ON audit_logs(org_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_org_invitations_org_id ON organization_invitations(org_id);
CREATE INDEX IF NOT EXISTS idx_org_invitations_email ON organization_invitations(email);
CREATE INDEX IF NOT EXISTS idx_org_invitations_token ON organization_invitations(token);

-- 预置管理员账户 (密码: admin123 的 bcrypt hash)
-- 注意: 在生产环境中应该删除或修改这些默认账户
INSERT INTO organizations (id, name, plan_id, billing_email) 
VALUES ('00000000-0000-0000-0000-000000000001', 'Verilocale Admin', 'scale', 'admin@verilocale.com') 
ON CONFLICT (id) DO NOTHING;

INSERT INTO users (id, email, password_hash, full_name, role, org_id, org_role) 
VALUES ('00000000-0000-0000-0000-000000000002', 'admin@verilocale.com', '$2a$10$92.IPX5lyaFvqCTGK1s5zO0s1W7I9PVW3qtpjN6vdTsF9gC5bT2Eu', 'System Administrator', 'admin', '00000000-0000-0000-0000-000000000001', 'owner') 
ON CONFLICT (email) DO NOTHING;