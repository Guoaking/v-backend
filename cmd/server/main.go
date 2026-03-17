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

	"kyc-service/internal/bootstrap"
	"kyc-service/internal/migration"
	"kyc-service/internal/router"
	"kyc-service/pkg/logger"
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

func main() {
	ctx := context.Background()

	// 解析命令行参数
	var configFile string
	flag.StringVar(&configFile, "config", "config.local", "配置文件路径 (不包含 .yaml 扩展名)")
	flag.Parse()

	// 1. Bootstrap: 初始化配置、日志、存储、服务等
	app, tracerCleanup, err := bootstrap.Init(ctx, configFile)
	if err != nil {
		// bootstrap.Init 内部已经记录了日志，但为了保险起见，这里再panic
		panic(fmt.Sprintf("Bootstrap failed: %v", err))
	}
	defer tracerCleanup()
	log := logger.GetLogger()

	// 2. Migration: 执行数据库迁移
	if err := migration.Run(app.DB); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	// 3. Router: 初始化路由和中间件
	r, heartbeatManager := router.New(app.Config, app.KYCService, app.RedisClient)

	// 启动HTTP服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", app.Config.Port),
		Handler: r,
	}

	// 优雅关闭
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	log.Infof("KYC服务启动成功，端口: %d", app.Config.Port)

	// 启动心跳检测
	//heartbeatManager.Start(ctx)

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("正在关闭服务...")

	// 停止心跳检测
	heartbeatManager.Stop()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Errorf("服务器关闭失败: %v", err)
	}

	log.Info("服务已关闭")
}
