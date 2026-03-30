package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// rateLimiter 简单的内存令牌桶限流器 (用于演示，生产环境可替换为 Redis + Lua)
type rateLimiter struct {
	tokens     int
	capacity   int
	lastRefill time.Time
	mu         sync.Mutex
	rate       int // tokens per second
}

var (
	limiters = make(map[string]*rateLimiter)
	globalMu sync.Mutex
)

func getLimiter(ip string, rate int) *rateLimiter {
	globalMu.Lock()
	defer globalMu.Unlock()

	limiter, exists := limiters[ip]
	if !exists {
		limiter = &rateLimiter{
			tokens:     rate,
			capacity:   rate,
			lastRefill: time.Now(),
			rate:       rate,
		}
		limiters[ip] = limiter
	}
	return limiter
}

func (l *rateLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	// Refill tokens based on elapsed time
	elapsed := now.Sub(l.lastRefill).Seconds()
	if elapsed > 0 {
		newTokens := int(elapsed * float64(l.rate))
		if newTokens > 0 {
			l.tokens += newTokens
			if l.tokens > l.capacity {
				l.tokens = l.capacity
			}
			l.lastRefill = now
		}
	}

	if l.tokens > 0 {
		l.tokens--
		return true
	}
	return false
}

// RateLimitMiddleware 根据架构规范，单 IP 限流，触发时直接返回 429 以便前端降级
func RateLimitMiddleware(qps int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := getLimiter(ip, qps)

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "Too Many Requests. Please use fallback mode.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
