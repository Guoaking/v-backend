package main

import (
	"fmt"
	"log"

	"kyc-service/internal/config"
	"kyc-service/internal/storage"
)

func main() {
	// 加载配置
	cfg := config.Load()
	
	// 初始化数据库连接
	db, err := storage.InitDB(cfg.Database)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	
	fmt.Println("开始执行数据库安全审计增强脚本...")
	
	// 1. 增强API Key表，添加IP白名单字段
	if err := db.Exec(`
		ALTER TABLE api_keys 
		ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[] DEFAULT '{}'
	`).Error; err != nil {
		log.Printf("添加IP白名单字段失败: %v", err)
	} else {
		fmt.Println("✅ API Key表已添加IP白名单字段")
	}
	
	// 2. 创建API请求日志表
	if err := db.Exec(`
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
		)
	`).Error; err != nil {
		log.Printf("创建API请求日志表失败: %v", err)
	} else {
		fmt.Println("✅ API请求日志表创建成功")
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
			log.Printf("创建索引 %s 失败: %v", idx.name, err)
		} else {
			fmt.Printf("✅ 索引 %s 创建成功\n", idx.name)
		}
	}
	
	// 4. 为现有API密钥设置默认空IP白名单
	if err := db.Exec(`
		UPDATE api_keys SET ip_whitelist = '{}' WHERE ip_whitelist IS NULL
	`).Error; err != nil {
		log.Printf("更新现有API密钥IP白名单失败: %v", err)
	} else {
		fmt.Println("✅ 现有API密钥IP白名单已更新")
	}
	
	// 5. 创建性能监控视图
	if err := db.Exec(`
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
		ORDER BY hour DESC
	`).Error; err != nil {
		log.Printf("创建性能监控视图失败: %v", err)
	} else {
		fmt.Println("✅ 性能监控视图创建成功")
	}
	
	fmt.Println("\n🎉 数据库安全审计增强脚本执行完成！")
	fmt.Println("\n主要变更：")
	fmt.Println("1. ✅ API Key表新增ip_whitelist字段")
	fmt.Println("2. ✅ 创建api_request_logs请求日志表")
	fmt.Println("3. ✅ 创建相关性能索引")
	fmt.Println("4. ✅ 更新现有API密钥默认设置")
	fmt.Println("5. ✅ 创建API性能监控视图")
}