package router

import (
	"time"

	"kyc-service/internal/api"
	"kyc-service/internal/config"
	"kyc-service/internal/middleware"
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// New initializes the Gin engine and routes
func New(cfg *config.Config, kycService *service.KYCService, redisClient *redis.Client) (*gin.Engine, *middleware.HeartbeatManager) {
	log := logger.GetLogger()

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
	r.Use(middleware.Recovery()) // 自定义恢复中间件
	// 引入 OTel 中间件，作为最外层 Tracing 入口 (服务名从配置取，或默认 kyc-service)
	serviceName := cfg.Monitoring.Tracing.ServiceName
	if serviceName == "" {
		serviceName = "kyc-service"
	}
	r.Use(otelgin.Middleware(serviceName))

	r.Use(middleware.UnifiedContextMiddleware())         // 1. 统一上下文（TraceID/RequestID）
	r.Use(middleware.SystemLoggerMiddleware())           // 2. 系统日志（Stdout）
	r.Use(middleware.ErrorHandler())                     // 统一错误处理中间件
	r.Use(middleware.EnterpriseMetricsInstrumentation()) // 企业级OTel指标
	r.Use(middleware.CORS())
	r.Use(middleware.Security())

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
	// v1.Use(middleware.APIRequestLogMiddleware(kycService)) // 已废弃，由 SystemLoggerMiddleware 替代
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

			// Sandbox / STS endpoint for playground
			consoleAuthHandler := api.NewConsoleAuthHandler(kycService)
			console.POST("/sandbox/token", consoleAuthHandler.GenerateSandboxToken)

			console.GET("/users/me", consoleHandler.GetCurrentUser)
			console.PUT("/users/me", consoleHandler.UpdateUserProfile)
			console.PUT("/users/me/password", userAuthHandler.UpdatePassword)

			console.GET("/usage", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission(models.PermLogsRead), consoleHandler.GetUsage)
			console.GET("/usage/stats", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission(models.PermLogsRead), consoleHandler.GetUsageStats)
			console.GET("/logs", middleware.RequireOrganizationHeader(kycService), middleware.RequirePermission(models.PermLogsRead), consoleHandler.GetLogs)
			console.DELETE("/users/me", consoleHandler.DeleteMe)
			console.GET("/me/notifications", middleware.JWTAuth(kycService), consoleHandler.GetNotifications)
			console.PUT("/me/notifications/:id/read", middleware.JWTAuth(kycService), consoleHandler.MarkNotificationRead)
			console.GET("/usage/quota", middleware.RequireOrganizationHeader(kycService), consoleHandler.GetQuotaStatus)

			// OAuth 客户端管理（组织维度）
			clientHandler := api.NewClientHandler(kycService)
			clients := console.Group("/oauth/clients")
			clients.Use(middleware.RequireOrganizationHeader(kycService))
			clients.POST("/register", middleware.RequirePermission(models.PermOAuthWrite), clientHandler.RegisterClient)
			clients.GET("", middleware.RequirePermission(models.PermOAuthRead), clientHandler.ListClients)
			clients.PUT(":id", middleware.RequirePermission(models.PermOAuthWrite), clientHandler.UpdateClient)
			clients.DELETE(":client_id", middleware.RequirePermission(models.PermOAuthWrite), clientHandler.DeleteClient)
			clients.POST(":id/rotate", middleware.RequirePermission(models.PermOAuthWrite), clientHandler.RotateClientSecret)
			clients.POST(":id/transfer", middleware.RequirePermission(models.PermOAuthWrite), clientHandler.TransferClientOwnership)
			clients.PATCH(":id/status", middleware.RequirePermission(models.PermOAuthWrite), clientHandler.UpdateClientStatus)
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
			notifications.POST("/email", middleware.RequirePermission(models.PermNotificationsSend), nh.SendEmail)
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

		// 组织管理（需要用户认证）
		// 组织切换（JWT即可，无需当前组织上下文）
		orgCoreHandler := api.NewOrganizationHandler(kycService)
		orgMemberHandler := api.NewOrgMemberHandler(kycService)
		orgUsageHandler := api.NewOrgUsageHandler(kycService)
		orgAuditHandler := api.NewOrgAuditHandler(kycService)

		v1.POST("/orgs/switch", middleware.JWTAuth(kycService), orgCoreHandler.SwitchOrganization)
		v1.POST("/auth/switch-org", middleware.JWTAuth(kycService), orgCoreHandler.SwitchOrganization)

		// 暴露一个全局的测试后门用于聚合数据，绕过普通鉴权 (dev 阶段使用)
		v1.POST("/aggregate-usage/:org_id", orgUsageHandler.TriggerUsageAggregation)

		orgs := v1.Group("/orgs")
		orgs.Use(middleware.JWTAuth(kycService))
		orgs.Use(middleware.RequireOrganizationHeader(kycService))
		orgs.Use(middleware.InjectOrgContext())
		{
			// 组织核心能力
			orgs.GET("/current", middleware.RequirePermission(models.PermOrgRead), orgCoreHandler.GetCurrentOrganization)
			orgs.PUT("/:id", middleware.RequirePermission(models.PermOrgUpdate), orgCoreHandler.UpdateOrganization)
			orgs.PUT("/plan", middleware.RequirePermission(models.PermBillingWrite), orgCoreHandler.UpdatePlan)
			orgs.DELETE("/:id", middleware.RequirePermission(models.PermOrgDelete), orgCoreHandler.DeleteOrganization)

			// 成员管理
			orgs.GET("/members", middleware.RequirePermission(models.PermTeamRead), orgMemberHandler.GetOrganizationMembers)
			orgs.POST("/members", middleware.RequirePermission(models.PermTeamInvite), orgMemberHandler.InviteOrganizationMember)
			orgs.PATCH("/members/:id", middleware.RequirePermission(models.PermTeamWrite), orgMemberHandler.UpdateMemberRole)
			orgs.PUT("/members/:id/password", middleware.RequirePermission(models.PermTeamWrite), orgMemberHandler.ResetMemberPassword)
			orgs.PATCH("/members/:id/status", middleware.RequirePermission(models.PermTeamWrite), orgMemberHandler.UpdateMemberStatus)
			orgs.DELETE("/members/:id", middleware.RequirePermission(models.PermTeamWrite), orgMemberHandler.DeleteOrganizationMember)

			// 邀请管理
			orgs.POST("/invitations", middleware.RequirePermission(models.PermTeamInvite), orgMemberHandler.CreateInvitation)
			orgs.GET("/invitations", middleware.RequirePermission(models.PermTeamRead), orgMemberHandler.ListInvitations)
			orgs.DELETE("/invitations/:id", middleware.RequirePermission(models.PermTeamWrite), orgMemberHandler.RevokeInvitation)

			// 用量与账单
			orgs.GET("/:org_id/usage/summary", middleware.RequirePermission(models.PermLogsRead), orgUsageHandler.GetUsageSummary)
			orgs.GET("/billing", middleware.ScopePermission([]string{models.PermOrgBillingRead, models.PermBillingRead}), orgUsageHandler.GetBilling)
			orgs.GET("/usage/daily", middleware.ScopePermission([]string{models.PermOrgUsageRead, models.PermLogsRead}), orgUsageHandler.GetUsageDaily)
			orgs.GET("/usage/detailed", middleware.ScopePermission([]string{models.PermOrgUsageRead, models.PermLogsRead}), orgUsageHandler.GetUsageDetailedV2)

			// 审计日志
			orgs.GET("/audit-logs", middleware.RequirePermission(models.PermLogsRead), orgAuditHandler.GetOrgAuditLogs)
			orgs.GET("/audit-logs/actions", middleware.RequirePermission(models.PermOrgAudit), orgAuditHandler.GetAuditActions)
			orgs.GET("/audit-logs/export", middleware.RequirePermission(models.PermOrgAudit), orgAuditHandler.ExportOrgAuditLogs)
		}

		// 创建组织（仅JWT，无需组织上下文）
		v1.POST("/orgs", middleware.JWTAuth(kycService), orgCoreHandler.CreateOrganization)

		// 邀请处理（登录用户）
		inv := v1.Group("/invitations")
		inv.Use(middleware.JWTAuth(kycService))
		{
			inv.POST("/accept", orgMemberHandler.AcceptInvitation)
			inv.POST("/:id/accept", orgMemberHandler.AcceptInvitationByID)
			inv.POST("/:id/decline", orgMemberHandler.DeclineInvitationByID)
		}

		// KYC相关API（需要API密钥认证）
		kyc := v1.Group("/kyc")
		kyc.Use(middleware.APIOrOAuthAuth(kycService)) // 支持OAuth2客户端凭证或API Key
		kyc.Use(middleware.InjectOrgContext())
		// kyc.Use(middleware.RequestBodyLogger()) // 暂时移除，待统一重构
		// kyc.Use(middleware.ResponseCapture())   // 暂时移除，待统一重构
		kyc.Use(middleware.Idempotency(redisClient))
		kyc.Use(middleware.RateLimitWithKey(redisClient, kycService)) // 启用IP级别限流（每秒100次）并标记Key
		kyc.Use(middleware.Quota(redisClient, kycService))            // 按组织计划配额检查与扣费
		kyc.Use(middleware.KYCUsageMeter(kycService))                 // 业务计量入队（异步消费入库）
		{
			kycHandler := api.NewKYCHandler(kycService)

			// OCR识别
			kyc.POST("/ocr", middleware.RequireKeyScope(models.ScopeOCRRead), kycHandler.OCR)
			// 人脸识别
			kyc.POST("/face/search", middleware.RequireKeyScope(models.ScopeFaceRead), kycHandler.FaceSearch)
			kyc.POST("/face/compare", middleware.RequireKeyScope(models.ScopeFaceRead), kycHandler.FaceCompare)
			kyc.POST("/face/id-match", middleware.RequireKeyScope(models.ScopeFaceRead), kycHandler.FaceIdMatch)
			kyc.POST("/face/detect", middleware.RequireKeyScope(models.ScopeFaceRead), kycHandler.FaceDetect)

			// 活体检测（WebSocket）
			kyc.POST("/liveness/silent", middleware.RequireKeyScope(models.ScopeLivenessRead), kycHandler.LivenessSilent)
			kyc.POST("/liveness/video", middleware.RequireKeyScope(models.ScopeLivenessRead), kycHandler.LivenessVideo)
			kyc.GET("/liveness/ws", middleware.RequireKeyScope(models.ScopeLivenessRead), kycHandler.LivenessWebSocket)
			// Action liveness (MVP placeholder)
			kyc.POST("/liveness/action/session", middleware.RequireKeyScope(models.ScopeLivenessRead), kycHandler.LivenessActionSession)
			kyc.POST("/liveness/action/upload", middleware.RequireKeyScope(models.ScopeLivenessRead), kycHandler.LivenessActionUpload)
			kyc.POST("/liveness/action/verify", middleware.RequireKeyScope(models.ScopeLivenessRead), kycHandler.LivenessActionVerify)
			// 完整KYC流程
			kyc.POST("/verify", middleware.RequireKeyScope(models.ScopeKYCVerify), kycHandler.CompleteKYC)

			// 查询KYC状态
			kyc.GET("/status/:request_id", kycHandler.GetKYCStatus)
		}
	}

	faces := v1.Group("/faces")
	faces.Use(middleware.APIOrOAuthAuth(kycService))
	faces.Use(middleware.InjectOrgContext())
	{
		faceImageHandler := api.NewFaceImageHandler(kycService)
		faces.GET(":id/image", faceImageHandler.GetImage)
	}

	images := v1.Group("/images")
	images.Use(middleware.APIOrOAuthAuth(kycService))
	images.Use(middleware.InjectOrgContext())
	{
		imageHandler := api.NewImageHandler(kycService)
		images.POST("", imageHandler.Upload)
		images.GET(":id/image", imageHandler.GetImage)
	}

	return r, heartbeatManager
}
