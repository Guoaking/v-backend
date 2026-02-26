package middleware

import (
    "strings"
    "time"

    "kyc-service/internal/models"
    "kyc-service/internal/service"

    "github.com/gin-gonic/gin"
)

// APIRequestLogMiddleware 记录API请求到数据库
func APIRequestLogMiddleware(kyc *service.KYCService) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()
        latency := time.Since(start).Milliseconds()

        p := c.FullPath()
        if strings.Contains(p, "/kyc/") {
            return
        }

        orgID := c.GetString("orgID")
        ip := c.ClientIP()

        log := &models.APIRequestLog{
            OrgID:      orgID,
            Method:     c.Request.Method,
            Path:       p,
            StatusCode: c.Writer.Status(),
            LatencyMs:  int(latency),
            ClientIP:   ip,
            CreatedAt:  time.Now(),
        }
        // 非关键路径失败可忽略
        _ = kyc.DB.Create(log).Error
    }
}
