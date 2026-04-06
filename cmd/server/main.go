package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kyc-service/internal/bootstrap"
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
// @scope face:write "Face write access"
// @scope liveness:read "Liveness read access"
// @scope kyc:verify "KYC verify access"

func main() {
	ctx := context.Background()

	// 解析命令行参数与环境变量
	var configFile string
	// 优先读取环境变量 KYC_CONFIG_FILE，如果没有则使用命令行参数，再没有则默认 "config.local"
	envConfig := os.Getenv("KYC_CONFIG_FILE")
	defaultConfig := "config.local"
	if envConfig != "" {
		defaultConfig = envConfig
	}
	flag.StringVar(&configFile, "config", defaultConfig, "配置文件路径 (不包含 .yaml 扩展名)，也可通过环境变量 KYC_CONFIG_FILE 设置")
	flag.Parse()

	// 0. 读取基础配置（仅为了获取端口号）
	// 注意：此处仅为了前置端口检查，完整配置会在 SetupApp 中再次加载
	// 如果配置很重，可以考虑将端口号作为独立环境变量或参数，但这里为了兼容现有逻辑
	// 我们在 SetupApp 之前先做一次端口探活检查
	tempCfg, err := bootstrap.LoadConfigOnly(configFile)
	if err == nil {
		addr := fmt.Sprintf("%s:%d", tempCfg.Host, tempCfg.Port)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Printf("FATAL: Port %d is already in use or unavailable. Error: %v\n", tempCfg.Port, err)
			os.Exit(1)
		}
		// 端口可用，释放监听器，让后续的 http.Server 正常绑定
		listener.Close()
	}

	// 1. SetupApp: 初始化配置、日志、存储、服务、数据库迁移、路由等
	app, cleanup, err := bootstrap.SetupApp(ctx, configFile)
	if err != nil {
		panic(fmt.Sprintf("SetupApp failed: %v", err))
	}
	defer cleanup()
	defer app.LogWorker.Stop()
	log := logger.GetLogger()

	// 启动HTTP服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", app.Config.Host, app.Config.Port),
		Handler: app.Engine,
	}

	// 优雅关闭
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	log.Infof("KYC服务启动成功，端口: %d", app.Config.Port)

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("正在关闭服务...")

	// 停止心跳检测
	if app.HeartbeatManager != nil {
		app.HeartbeatManager.Stop()
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Errorf("服务器关闭失败: %v", err)
	}

	log.Info("服务已关闭")
}
