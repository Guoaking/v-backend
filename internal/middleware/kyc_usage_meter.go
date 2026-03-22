package middleware

import (
	"net/http"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"kyc-service/pkg/utils"

	"github.com/gin-gonic/gin"
)

type usageEvent struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"org_id"`
	UserID        string    `json:"user_id"`
	ActorUserID   string    `json:"actor_user_id"`
	OAuthClientID string    `json:"oauth_client_id"`
	Endpoint      string    `json:"endpoint"`
	StatusCode    int       `json:"status_code"`
	RequestID     string    `json:"request_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func KYCUsageMeter(svc *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 使用 defer 确保即使后续 Handler panic 也能记录日志
		defer func() {
			latency := time.Since(start)

			orgID := c.GetString("orgID")
			userID := c.GetString("userID")
			oauthClientID := c.GetString("oauthClientID")
			// actor: 若 userID 非当前组织活跃成员
			actorUserID := userID
			if actorUserID != "" && orgID != "" {
				var cnt int64
				_ = svc.DB.Model(&models.OrganizationMember{}).Where("organization_id = ? AND user_id = ? AND status = ?", orgID, actorUserID, "active").Count(&cnt).Error
				if cnt == 0 {
					actorUserID = ""
				}
			}
			if actorUserID == "" {
				actorUserID = c.GetString("clientOwnerID")
			}

			// 如果发生 panic，Gin 的 Recovery 中间件会写入 500，但可能在 defer 之后执行
			// 因此这里只能获取当前状态。
			statusCode := c.Writer.Status()

			ev := usageEvent{
				ID:            utils.GenerateID(),
				OrgID:         orgID,
				UserID:        userID,
				ActorUserID:   actorUserID,
				OAuthClientID: oauthClientID,
				Endpoint:      c.FullPath(),
				StatusCode:    statusCode,
				RequestID:     c.GetString(models.ContextKeyRequestID),
				CreatedAt:     time.Now(),
			}

			// 解析服务类型 (可以从 URL 推断，也可以由下游 Handler 写入 Context)
			serviceType := c.GetString("service_type")
			if serviceType == "" {
				// 简单的后备推断逻辑
				if strings.Contains(ev.Endpoint, "/face") {
					serviceType = "face"
				} else if strings.Contains(ev.Endpoint, "/ocr") {
					serviceType = "ocr"
				} else if strings.Contains(ev.Endpoint, "/liveness") {
					serviceType = "liveness"
				} else {
					serviceType = "unknown"
				}
			}

			// 获取计费单位，默认为 1 (次)。
			usageUnits := c.GetInt("usage_units")
			if usageUnits == 0 {
				usageUnits = 1
			}

			// 默认非 500 的都算作有效计费
			billable := true
			if statusCode >= 500 || statusCode == http.StatusTooManyRequests || statusCode == http.StatusPaymentRequired {
				billable = false
			}
			if b, exists := c.Get("billable"); exists {
				billable = b.(bool)
			}

			// 获取 SessionID
			sessionID := c.GetString("session_id")
			if sessionID == "" {
				sessionID = ev.RequestID
			}

			// 获取 Metadata
			var metadata map[string]interface{}
			if md, exists := c.Get("metadata"); exists {
				if m, ok := md.(map[string]interface{}); ok {
					metadata = m
				}
			}

			// 构建强类型 Payload
			payload := models.BillingPayload{
				ID:            ev.ID,
				ActorUserID:   ev.ActorUserID,
				OAuthClientID: ev.OAuthClientID,
				Endpoint:      ev.Endpoint,
				StatusCode:    ev.StatusCode,
				ServiceType:   serviceType,
				UsageUnits:    usageUnits,
				Billable:      billable,
				SessionID:     sessionID,
				Metadata:      metadata,
			}

			// 构建日志信封并发送到 AsyncLogWorker
			envelope := models.LogEnvelope{
				Timestamp: ev.CreatedAt,
				LogType:   models.LogTypeBilling,
				Level:     "INFO",
				Trace: models.TraceInfo{
					TraceID:   c.GetString(models.ContextKeyTraceID),
					RequestID: ev.RequestID,
				},
				Identity: models.IdentityInfo{
					TenantID: ev.OrgID,
					UserID:   ev.UserID,
					ClientIP: c.ClientIP(),
				},
				EventAction: "api_usage",
				Payload:     payload,
			}

			svc.LogWorker.Enqueue(envelope)

			_ = latency
		}()

		c.Next()
	}
}
