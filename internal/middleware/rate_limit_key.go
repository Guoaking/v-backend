package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"kyc-service/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func RateLimitWithKey(redisClient *redis.Client, svc *service.KYCService) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		keyName := fmt.Sprintf("rate_limit:%s", clientIP)
		ctx := context.Background()

		current, err := redisClient.Get(ctx, keyName).Int()
		if err != nil && err != redis.Nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 5000, "message": "服务器内部错误", "error": "限流服务异常"})
			c.Abort()
			return
		}
		if current >= 300 {
			// IMPORTANT: rate limiting should NOT permanently change API key status.
			// Otherwise, clients will see 401 "revoked" on subsequent requests, which breaks retry semantics.
			c.Header("Retry-After", "1")
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 1005, "message": "请求过于频繁"})
			c.Abort()
			return
		}
		pipe := redisClient.Pipeline()
		pipe.Incr(ctx, keyName)
		pipe.Expire(ctx, keyName, time.Second)
		_, _ = pipe.Exec(ctx)
		c.Next()
	}
}
