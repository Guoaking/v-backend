package middleware

import (
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
		c.Next()
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

		ev := usageEvent{
			ID:            utils.GenerateID(),
			OrgID:         orgID,
			APIKeyID:      apiKeyID,
			UserID:        userID,
			APIKeyOwnerID: apiKeyOwnerID,
			ActorUserID:   actorUserID,
			OAuthClientID: oauthClientID,
			Endpoint:      c.FullPath(),
			StatusCode:    c.Writer.Status(),
			RequestID:     c.GetString("request_id"),
			CreatedAt:     time.Now(),
		}
		ctx := c.Request.Context()
		if svc.Redis != nil {
			b := []byte(utils.ToJSONString(ev))
			if err := svc.Redis.LPush(ctx, "usage:events", string(b)).Err(); err == nil {
				_ = svc.Redis.Expire(ctx, "usage:events", 24*time.Hour).Err()
			} else {
				// 作为回退，直接异步落库，避免丢失
				go func(e usageEvent) {
					_ = svc.DB.Create(&models.UsageLog{ID: utils.GenerateID(), OrgID: e.OrgID, APIKeyID: e.APIKeyID, UserID: e.UserID, Endpoint: e.Endpoint, StatusCode: e.StatusCode, RequestID: e.RequestID, CreatedAt: e.CreatedAt}).Error
				}(ev)
			}
		} else {
			// 无Redis时直接异步落库
			go func(e usageEvent) {
				_ = svc.DB.Create(&models.UsageLog{ID: utils.GenerateID(), OrgID: e.OrgID, APIKeyID: e.APIKeyID, UserID: e.UserID, Endpoint: e.Endpoint, StatusCode: e.StatusCode, RequestID: e.RequestID, CreatedAt: e.CreatedAt}).Error
			}(ev)
		}
		_ = latency
	}
}
