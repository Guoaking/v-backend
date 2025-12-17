#!/bin/bash

# 数据库安全审计增强脚本 - 简化版本
# 直接执行SQL命令

echo "=== 数据库安全审计增强脚本 ==="
echo

# 检查数据库连接
echo "检查PostgreSQL连接..."
if ! command -v psql &> /dev/null; then
    echo "❌ 未找到psql命令，请确保PostgreSQL客户端已安装"
    echo "您可以手动执行以下SQL语句："
    echo
    cat << 'EOF'
-- 1. 增强 API Key 表，支持 IP 白名单
ALTER TABLE api_keys 
ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[] DEFAULT '{}';

-- 2. 新增请求日志表
CREATE TABLE IF NOT EXISTS api_request_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    api_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    method VARCHAR(10) NOT NULL,
    path VARCHAR(255) NOT NULL,
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    client_ip VARCHAR(45),
    request_body JSONB,
    response_body JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. 创建索引
CREATE INDEX IF NOT EXISTS idx_req_logs_user_date ON api_request_logs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_api_key ON api_request_logs (api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_created_at ON api_request_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_client_ip ON api_request_logs (client_ip);
CREATE INDEX IF NOT EXISTS idx_req_logs_status ON api_request_logs (status_code);
CREATE INDEX IF NOT EXISTS idx_api_keys_ip_whitelist ON api_keys USING GIN (ip_whitelist);

-- 4. 更新现有API密钥
UPDATE api_keys SET ip_whitelist = '{}' WHERE ip_whitelist IS NULL;

-- 5. 创建性能监控视图
CREATE OR REPLACE VIEW v_api_request_summary AS
SELECT 
    DATE_TRUNC('hour', created_at) as hour,
    api_key_id,
    method,
    path,
    status_code,
    COUNT(*) as request_count,
    AVG(latency_ms) as avg_latency_ms,
    MIN(latency_ms) as min_latency_ms,
    MAX(latency_ms) as max_latency_ms,
    COUNT(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 END) as success_count,
    COUNT(CASE WHEN status_code >= 400 THEN 1 END) as error_count
FROM api_request_logs 
WHERE created_at >= NOW() - INTERVAL '7 days'
GROUP BY DATE_TRUNC('hour', created_at), api_key_id, method, path, status_code
ORDER BY hour DESC;
EOF
    exit 1
fi

# 尝试连接数据库并执行SQL
if PGPASSWORD=password psql -h localhost -U postgres -d kyc_db -c "SELECT 1;" > /dev/null 2>&1; then
    echo "✅ 数据库连接成功"
    echo
    
    # 执行SQL脚本
    PGPASSWORD=password psql -h localhost -U postgres -d kyc_db << 'EOF'
-- 1. 增强 API Key 表，支持 IP 白名单
ALTER TABLE api_keys 
ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[] DEFAULT '{}';

-- 添加注释
COMMENT ON COLUMN api_keys.ip_whitelist IS 'IP白名单，存储CIDR格式，如["10.0.0.1", "192.168.0.0/24"]';

-- 2. 新增请求日志表
CREATE TABLE IF NOT EXISTS api_request_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    api_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    method VARCHAR(10) NOT NULL,
    path VARCHAR(255) NOT NULL,
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    client_ip VARCHAR(45),
    request_body JSONB,
    response_body JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. 创建索引
CREATE INDEX IF NOT EXISTS idx_req_logs_user_date ON api_request_logs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_api_key ON api_request_logs (api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_created_at ON api_request_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_client_ip ON api_request_logs (client_ip);
CREATE INDEX IF NOT EXISTS idx_req_logs_status ON api_request_logs (status_code);
CREATE INDEX IF NOT EXISTS idx_api_keys_ip_whitelist ON api_keys USING GIN (ip_whitelist);

-- 4. 更新现有API密钥
UPDATE api_keys SET ip_whitelist = '{}' WHERE ip_whitelist IS NULL;

-- 5. 创建性能监控视图
CREATE OR REPLACE VIEW v_api_request_summary AS
SELECT 
    DATE_TRUNC('hour', created_at) as hour,
    api_key_id,
    method,
    path,
    status_code,
    COUNT(*) as request_count,
    AVG(latency_ms) as avg_latency_ms,
    MIN(latency_ms) as min_latency_ms,
    MAX(latency_ms) as max_latency_ms,
    COUNT(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 END) as success_count,
    COUNT(CASE WHEN status_code >= 400 THEN 1 END) as error_count
FROM api_request_logs 
WHERE created_at >= NOW() - INTERVAL '7 days'
GROUP BY DATE_TRUNC('hour', created_at), api_key_id, method, path, status_code
ORDER BY hour DESC;

-- 输出成功信息
\echo '🎉 数据库安全审计增强脚本执行成功！'
\echo ''
\echo '主要变更：'
\echo '1. ✅ API Key表新增ip_whitelist字段'
\echo '2. ✅ 创建api_request_logs请求日志表'
\echo '3. ✅ 创建相关性能索引'
\echo '4. ✅ 更新现有API密钥默认设置'
\echo '5. ✅ 创建API性能监控视图'
EOF

    if [ $? -eq 0 ]; then
        echo
        echo "🎉 数据库安全审计增强脚本执行成功！"
        echo
        echo "主要变更："
        echo "1. ✅ API Key表新增ip_whitelist字段"
        echo "2. ✅ 创建api_request_logs请求日志表"
        echo "3. ✅ 创建相关性能索引"
        echo "4. ✅ 更新现有API密钥默认设置"
        echo "5. ✅ 创建API性能监控视图"
    else
        echo "❌ SQL执行失败，请检查数据库连接和权限"
        exit 1
    fi
else
    echo "❌ 无法连接到数据库，请确保："
    echo "  - PostgreSQL服务正在运行"
    echo "  - 数据库连接参数正确"
    echo "  - 用户权限足够"
    echo
    echo "您可以手动执行SQL脚本："
    echo "  psql -h localhost -U postgres -d kyc_db -f scripts/002_security_audit.sql"
fi