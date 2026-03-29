package api

import (
	"kyc-service/internal/apps/attendance/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册考勤微应用的所有路由
// 这里展示了微应用的路由是如何从核心代码中物理隔离的。
// 我们将 jwtSecret 作为参数传入，保持了底层对配置的解耦。
func RegisterRoutes(r *gin.RouterGroup, jwtSecret string) {
	// 所有考勤 C端 (H5) 相关的路由，挂载在 /api/v1/attendance 下
	// 这些路由不需要登录，但需要一个特殊的 Magic Link Token 中间件来提取 OrgID
	attendanceGroup := r.Group("/attendance")
	attendanceGroup.Use(middleware.MagicLinkAuth(jwtSecret))
	{
		// 1. 注册相关
		enrollGroup := attendanceGroup.Group("/enroll")
		{
			enrollGroup.POST("/ocr", handleOCR)
			enrollGroup.POST("/submit", handleSubmit)
		}

		// 2. 打卡相关
		attendanceGroup.GET("/config", handleGetConfig)
		attendanceGroup.POST("/punch/identity", handleIdentityMatch)
		attendanceGroup.POST("/punch", handlePunch)

		// 3. 员工自助查询
		selfGroup := attendanceGroup.Group("/self")
		{
			selfGroup.POST("/otp", handleRequestOTP)
			selfGroup.GET("/records", handleGetSelfRecords)
		}
	}

	// 所有考勤 B端 (老板/HR Console) 相关的路由，挂载在 /api/v1/console/attendance 下
	// 注意：这些路由应该在外部被现有的 Console JWT Auth 中间件保护
	consoleAttendanceGroup := r.Group("/console/attendance")
	{
		consoleAttendanceGroup.GET("/records", handleConsoleGetRecords)
		consoleAttendanceGroup.PUT("/records/:id/review", handleConsoleReviewRecord)
		consoleAttendanceGroup.GET("/stats", handleConsoleGetStats)
	}
}

// ==============================================================================
// 以下为 Handler 骨架 (待填充具体业务逻辑)
// ==============================================================================

func handleOCR(c *gin.Context)            { c.JSON(200, gin.H{"status": "ok"}) }
func handleSubmit(c *gin.Context)         { c.JSON(200, gin.H{"status": "ok"}) }
func handleGetConfig(c *gin.Context)      { c.JSON(200, gin.H{"status": "ok"}) }
func handleIdentityMatch(c *gin.Context)  { c.JSON(200, gin.H{"status": "ok"}) }
func handlePunch(c *gin.Context)          { c.JSON(200, gin.H{"status": "ok"}) }
func handleRequestOTP(c *gin.Context)     { c.JSON(200, gin.H{"status": "ok"}) }
func handleGetSelfRecords(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) }

func handleConsoleGetRecords(c *gin.Context)   { c.JSON(200, gin.H{"status": "ok"}) }
func handleConsoleReviewRecord(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) }
func handleConsoleGetStats(c *gin.Context)     { c.JSON(200, gin.H{"status": "ok"}) }
