-- 数据库安全审计增强脚本 002_security_audit.sql
-- 适用于KYC企业认证服务的安全增强

-- 1. 增强 API Key 表，支持 IP 白名单
ALTER TABLE api_keys 
ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[] DEFAULT '{}'; -- 存储 CIDR 格式，如 ['10.0.0.1', '192.168.0.0/24']

-- 添加注释说明
COMMENT ON COLUMN api_keys.ip_whitelist IS 'IP白名单，存储CIDR格式，如["10.0.0.1", "192.168.0.0/24"]'; 

-- 2. 新增请求日志表 (用于 Console 展示)
-- 注意：这不同于审计日志(audit_logs)。audit_logs 是记录"谁修改了密码"，request_logs 是记录"API调用详情"。
CREATE TABLE IF NOT EXISTS api_request_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    api_key_id UUID REFERENCES api_keys(id),
    method VARCHAR(10) NOT NULL,
    path VARCHAR(255) NOT NULL,
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    client_ip VARCHAR(45),
    request_body JSONB,  -- 生产环境注意脱敏敏感字段(如图片base64)
    response_body JSONB, -- 生产环境注意脱敏
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引以加速查询
CREATE INDEX IF NOT EXISTS idx_req_logs_user_date ON api_request_logs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_api_key ON api_request_logs (api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_created_at ON api_request_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_req_logs_client_ip ON api_request_logs (client_ip);
CREATE INDEX IF NOT EXISTS idx_req_logs_status ON api_request_logs (status_code);

-- 添加表注释
COMMENT ON TABLE api_request_logs IS 'API请求日志表，记录详细的API调用信息，用于Console展示和性能分析';
COMMENT ON COLUMN api_request_logs.user_id IS '关联的用户ID';
COMMENT ON COLUMN api_request_logs.api_key_id IS '关联的API密钥ID';
COMMENT ON COLUMN api_request_logs.method IS 'HTTP方法';
COMMENT ON COLUMN api_request_logs.path IS '请求路径';
COMMENT ON COLUMN api_request_logs.status_code IS '响应状态码';
COMMENT ON COLUMN api_request_logs.latency_ms IS '响应延迟(毫秒)';
COMMENT ON COLUMN api_request_logs.client_ip IS '客户端IP地址';
COMMENT ON COLUMN api_request_logs.request_body IS '请求体(生产环境需脱敏)';
COMMENT ON COLUMN api_request_logs.response_body IS '响应体(生产环境需脱敏)';

-- 3. 为现有API密钥添加默认空IP白名单
UPDATE api_keys SET ip_whitelist = '{}' WHERE ip_whitelist IS NULL;

-- 4. 创建清理旧日志的函数（可选）
CREATE OR REPLACE FUNCTION cleanup_old_request_logs() RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    -- 删除30天前的请求日志
    DELETE FROM api_request_logs 
    WHERE created_at < NOW() - INTERVAL '30 days';
    
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- 5. 创建视图用于快速查询（可选）
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

COMMENT ON VIEW v_api_request_summary IS 'API请求汇总视图，按小时统计请求量、延迟和错误率';

-- 6. 创建性能监控函数（可选）
CREATE OR REPLACE FUNCTION get_api_performance_stats(
    start_time TIMESTAMP WITH TIME ZONE DEFAULT NOW() - INTERVAL '1 hour',
    end_time TIMESTAMP WITH TIME ZONE DEFAULT NOW()
) RETURNS TABLE (
    api_key_id UUID,
    total_requests BIGINT,
    avg_latency_ms NUMERIC,
    success_rate NUMERIC,
    error_rate NUMERIC,
    top_client_ip TEXT
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        l.api_key_id,
        COUNT(*) as total_requests,
        AVG(l.latency_ms) as avg_latency_ms,
        (COUNT(CASE WHEN l.status_code >= 200 AND l.status_code < 300 THEN 1 END) * 100.0 / COUNT(*)) as success_rate,
        (COUNT(CASE WHEN l.status_code >= 400 THEN 1 END) * 100.0 / COUNT(*)) as error_rate,
        mode() WITHIN GROUP (ORDER BY l.client_ip) as top_client_ip
    FROM api_request_logs l
    WHERE l.created_at BETWEEN start_time AND end_time
    GROUP BY l.api_key_id
    ORDER BY total_requests DESC;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION get_api_performance_stats IS '获取指定时间范围内的API性能统计';

-- 7. 添加数据保留策略（可选）
-- 注意：PostgreSQL原生不支持自动分区，需要额外配置或使用pg_partman扩展
-- 这里提供基本的数据生命周期管理思路

-- 创建日志清理任务（需要配合外部调度器如cron或pg_cron）
-- SELECT cron.schedule('cleanup-request-logs', '0 2 * * *', 'SELECT cleanup_old_request_logs();');

-- 8. 安全审计相关索引
CREATE INDEX IF NOT EXISTS idx_api_keys_ip_whitelist ON api_keys USING GIN (ip_whitelist);

-- 9. 添加外键约束（如果还没有）
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE constraint_name = 'api_request_logs_user_id_fkey' 
        AND table_name = 'api_request_logs'
    ) THEN
        ALTER TABLE api_request_logs 
        ADD CONSTRAINT api_request_logs_user_id_fkey 
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;
    END IF;
    
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE constraint_name = 'api_request_logs_api_key_id_fkey' 
        AND table_name = 'api_request_logs'
    ) THEN
        ALTER TABLE api_request_logs 
        ADD CONSTRAINT api_request_logs_api_key_id_fkey 
        FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL;
    END IF;
END $$;

-- 10. 记录变更日志
INSERT INTO schema_migrations (version, description, executed_at) 
VALUES ('002_security_audit', 'Add IP whitelist to api_keys and create api_request_logs table', CURRENT_TIMESTAMP);

-- 输出成功信息
RAISE NOTICE '数据库安全审计增强脚本执行成功！';
RAISE NOTICE '主要变更：';
RAISE NOTICE '1. api_keys表新增ip_whitelist字段';
RAISE NOTICE '2. 创建api_request_logs请求日志表';
RAISE NOTICE '3. 创建相关索引和视图';
RAISE NOTICE '4. 添加性能监控和安全函数';