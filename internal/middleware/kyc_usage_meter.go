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
	APIKeyID      string    `json:"api_key_id"`
	UserID        string    `json:"user_id"`
	APIKeyOwnerID string    `json:"api_key_owner_id"`
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
			apiKeyID := c.GetString("apiKeyID")
			userID := c.GetString("userID")
			apiKeyOwnerID := c.GetString("apiKeyOwnerID")
			oauthClientID := c.GetString("oauthClientID")
			// actor: 若 userID 非当前组织活跃成员，则回退到 apiKeyOwnerID
			actorUserID := userID
			if actorUserID != "" && orgID != "" {
				var cnt int64
				_ = svc.DB.Model(&models.OrganizationMember{}).Where("organization_id = ? AND user_id = ? AND status = ?", orgID, actorUserID, "active").Count(&cnt).Error
				if cnt == 0 {
					actorUserID = ""
				}
			}
			if actorUserID == "" {
				actorUserID = apiKeyOwnerID
			}
			if actorUserID == "" {
				actorUserID = c.GetString("clientOwnerID")
			}

			// 如果发生 panic，Gin 的 Recovery 中间件会写入 500，但可能在 defer 之后执行
			// 因此这里只能获取当前状态。如果 c.Writer.Status() 还是 200 但发生了 panic，
			// 通常意味着 Recovery 还没跑。但在 Gin 中，Recovery 是最外层，
			// 所以当这个 defer 执行时，panic 已经向上冒泡了，这里拿到的状态码可能不准。
			// 但我们依靠 "billable" 逻辑兜底。
			statusCode := c.Writer.Status()

			ev := usageEvent{
				ID:            utils.GenerateID(),
				OrgID:         orgID,
				APIKeyID:      apiKeyID,
				UserID:        userID,
				APIKeyOwnerID: apiKeyOwnerID,
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
			// 某些业务可能一次调用算多次（例如批量处理），下游 Handler 可设置此值
			usageUnits := c.GetInt("usage_units")
			if usageUnits == 0 {
				usageUnits = 1
			}

			// 默认非 500 的都算作有效计费 (业务可自定义覆盖)
			billable := true
			// 5xx: 服务端错误 -> 免单
			// 429: 限流/配额超限 -> 免单 (虽然占用了网关资源，但通常不计费)
			// 402: 欠费/需要付费 -> 免单
			if statusCode >= 500 || statusCode == http.StatusTooManyRequests || statusCode == http.StatusPaymentRequired {
				billable = false
			}
			if b, exists := c.Get("billable"); exists {
				billable = b.(bool)
			}

			// 获取 SessionID (TraceID 或 自定义 SessionID)
			sessionID := c.GetString("session_id")
			if sessionID == "" {
				sessionID = ev.RequestID // 默认使用 RequestID 作为 SessionID
			}

			// 获取 Metadata (支持业务层传递结构化的附加信息)
			var metadata map[string]interface{}
			if md, exists := c.Get("metadata"); exists {
				if m, ok := md.(map[string]interface{}); ok {
					metadata = m
				}
			}

			// 构建强类型 Payload
			payload := models.BillingPayload{
				ID:            ev.ID,
				APIKeyID:      ev.APIKeyID,
				APIKeyOwnerID: ev.APIKeyOwnerID,
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
