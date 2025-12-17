-- 创建密码重置表
CREATE TABLE IF NOT EXISTS password_resets (
    id TEXT PRIMARY KEY DEFAULT uuid_generate_v4()::text,
    user_id TEXT NOT NULL,
    token TEXT UNIQUE NOT NULL,
    status VARCHAR(50) DEFAULT 'pending', -- pending, used, expired
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_password_resets_user_id ON password_resets(user_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token);
CREATE INDEX IF NOT EXISTS idx_password_resets_status ON password_resets(status);
CREATE INDEX IF NOT EXISTS idx_password_resets_expires_at ON password_resets(expires_at);