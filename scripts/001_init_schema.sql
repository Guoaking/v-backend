-- Create core tables if not exists
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255),
    full_name VARCHAR(100),
    avatar VARCHAR(255),
    role VARCHAR(20) DEFAULT 'user',
    org_id UUID,
    org_role VARCHAR(20) DEFAULT 'viewer',
    current_org_id UUID,
    is_platform_admin BOOLEAN DEFAULT FALSE,
    status VARCHAR(20) DEFAULT 'active',
    last_login_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    plan_id VARCHAR(50) DEFAULT 'starter',
    billing_email VARCHAR(255),
    status VARCHAR(20) DEFAULT 'active',
    usage_summary JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS organization_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    user_id UUID NOT NULL REFERENCES users(id),
    role VARCHAR(20) NOT NULL,
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID REFERENCES organizations(id),
    user_id UUID REFERENCES users(id),
    name VARCHAR(100) NOT NULL,
    prefix VARCHAR(20),
    secret_hash VARCHAR(255) NOT NULL,
    scopes JSONB DEFAULT '[]'::jsonb,
    status VARCHAR(20) DEFAULT 'active',
    ip_whitelist TEXT[] DEFAULT '{}',
    last_used_at TIMESTAMP,
    total_requests_24h INT DEFAULT 0,
    success_rate_24h DOUBLE PRECISION DEFAULT 0,
    last_error_message TEXT,
    last_error_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_org ON api_keys(organization_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);

-- Safe alters for existing deployments
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS current_org_id UUID;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS usage_summary JSONB DEFAULT '{}'::jsonb;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS prefix VARCHAR(20);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scopes JSONB DEFAULT '[]'::jsonb;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'active';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS total_requests_24h INT DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS success_rate_24h DOUBLE PRECISION DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_error_message TEXT;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMP;
CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(100),
    description TEXT,
    is_system BOOLEAN DEFAULT FALSE,
    permissions JSONB DEFAULT '[]',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
    id VARCHAR(50) PRIMARY KEY,
    category VARCHAR(50),
    description TEXT
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id VARCHAR(50) REFERENCES roles(id) ON DELETE CASCADE,
    permission_id VARCHAR(50) REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS usage_metrics (
    org_id UUID PRIMARY KEY,
    request_count INT DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Safe alters
ALTER TABLE roles ADD COLUMN IF NOT EXISTS is_system BOOLEAN DEFAULT FALSE;
ALTER TABLE roles ADD COLUMN IF NOT EXISTS description TEXT;
