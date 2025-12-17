package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"kyc-service/pkg/logger"
	"kyc-service/pkg/metrics"

	"github.com/gin-gonic/gin"
)

// HeartbeatManager 心跳管理器
type HeartbeatManager struct {
	auth           *BidirectionalAuth
	interval       time.Duration
	timeout        time.Duration
	maxRetries     int
	
	// 状态跟踪
	lastHeartbeat  time.Time
	isHealthy      bool
	failureCount   int
	
	// 并发控制
	mu             sync.RWMutex
	
	// 通知机制
	healthCallbacks []func(bool, string)
	
	// 停止信号
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

// NewHeartbeatManager 创建心跳管理器
func NewHeartbeatManager(auth *BidirectionalAuth, interval, timeout time.Duration, maxRetries int) *HeartbeatManager {
	return &HeartbeatManager{
		auth:            auth,
		interval:        interval,
		timeout:         timeout,
		maxRetries:      maxRetries,
		isHealthy:       true,
		healthCallbacks: make([]func(bool, string), 0),
		stopCh:          make(chan struct{}),
	}
}

// Start 启动心跳检测
func (h *HeartbeatManager) Start(ctx context.Context) {
	h.wg.Add(1)
	go h.heartbeatLoop(ctx)
	logger.GetLogger().Info("双向心跳检测已启动")
}

// Stop 停止心跳检测
func (h *HeartbeatManager) Stop() {
	close(h.stopCh)
	h.wg.Wait()
	logger.GetLogger().Info("双向心跳检测已停止")
}

// heartbeatLoop 心跳循环
func (h *HeartbeatManager) heartbeatLoop(ctx context.Context) {
	defer h.wg.Done()
	
	// 立即执行一次心跳
	h.performHeartbeat(ctx)
	
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			h.performHeartbeat(ctx)
		case <-h.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// performHeartbeat 执行心跳检测
func (h *HeartbeatManager) performHeartbeat(ctx context.Context) {
	start := time.Now()
	
	// 生成心跳请求签名
	serviceToken := h.auth.generateServiceToken("/health", "GET")
	
	// 构建请求
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:8082/health", nil)
	if err != nil {
		h.handleHeartbeatFailure("构建心跳请求失败: " + err.Error())
		return
	}
	
	// 添加双向鉴权头
	req.Header.Set("X-Service-Signature", serviceToken.Signature)
	req.Header.Set("X-Service-Timestamp", serviceToken.Timestamp)
	req.Header.Set("X-Service-Name", h.auth.ServiceName)
	req.Header.Set("X-Service-Nonce", serviceToken.Nonce)
	req.Header.Set("X-Kong-Signature", h.auth.generateHMACSignature(
		fmt.Sprintf("%s:%s:%s:%s", h.auth.ServiceName, "/health", "GET", serviceToken.Timestamp),
		h.auth.KongSharedSecret,
	))
	req.Header.Set("X-Kong-Timestamp", serviceToken.Timestamp)
	req.Header.Set("X-Kong-Service", h.auth.ServiceName)
	
	// 执行请求
	client := &http.Client{Timeout: h.timeout}
	resp, err := client.Do(req)
	if err != nil {
		h.handleHeartbeatFailure("心跳请求失败: " + err.Error())
		return
	}
	defer resp.Body.Close()
	
	duration := time.Since(start)
	
	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		h.handleHeartbeatFailure(fmt.Sprintf("心跳响应状态异常: %d", resp.StatusCode))
		return
	}
	
	// 验证响应头（双向鉴权）
	serviceSignature := resp.Header.Get("X-Service-Signature")
	serviceTimestamp := resp.Header.Get("X-Service-Timestamp")
	serviceName := resp.Header.Get("X-Service-Name")
	
	if serviceSignature == "" || serviceTimestamp == "" || serviceName == "" {
		h.handleHeartbeatFailure("缺少服务响应认证头")
		return
	}
	
	// 验证服务签名
	message := fmt.Sprintf("%s:%s:%s:%s", serviceName, "/health", "GET", serviceTimestamp)
	expectedSignature := h.auth.generateHMACSignature(message, h.auth.ServiceSecretKey)
	
	if serviceSignature != expectedSignature {
		h.handleHeartbeatFailure("服务响应签名验证失败")
		return
	}
	
	// 心跳成功
	h.handleHeartbeatSuccess(duration)
}

// handleHeartbeatSuccess 处理心跳成功
func (h *HeartbeatManager) handleHeartbeatSuccess(duration time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.lastHeartbeat = time.Now()
	h.failureCount = 0
	
	if !h.isHealthy {
		h.isHealthy = true
		h.notifyHealthChange(true, "服务恢复正常")
		logger.GetLogger().WithFields(map[string]interface{}{
			"duration_ms": duration.Milliseconds(),
		}).Info("双向心跳检测恢复正常")
	}
	
	// 记录心跳成功指标
	metrics.RecordHeartbeatSuccess(duration)
	
	logger.GetLogger().WithFields(map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
	}).Debug("双向心跳检测成功")
}

// handleHeartbeatFailure 处理心跳失败
func (h *HeartbeatManager) handleHeartbeatFailure(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.failureCount++
	
	logger.GetLogger().WithFields(map[string]interface{}{
		"failure_count": h.failureCount,
		"max_retries":   h.maxRetries,
		"reason":        reason,
	}).Warn("双向心跳检测失败")
	
	// 记录心跳失败指标
	metrics.RecordHeartbeatFailure()
	
	if h.failureCount >= h.maxRetries && h.isHealthy {
		h.isHealthy = false
		h.notifyHealthChange(false, reason)
		logger.GetLogger().Error("双向心跳检测连续失败，服务标记为不健康")
	}
}

// notifyHealthChange 通知健康状态变化
func (h *HeartbeatManager) notifyHealthChange(healthy bool, reason string) {
	for _, callback := range h.healthCallbacks {
		go callback(healthy, reason)
	}
	
	// 更新双向鉴权健康状态指标
	metrics.UpdateBidirectionalAuthHealth(healthy)
}

// RegisterHealthCallback 注册健康状态变化回调
func (h *HeartbeatManager) RegisterHealthCallback(callback func(bool, string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.healthCallbacks = append(h.healthCallbacks, callback)
}

// IsHealthy 获取当前健康状态
func (h *HeartbeatManager) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isHealthy
}

// GetLastHeartbeat 获取最后心跳时间
func (h *HeartbeatManager) GetLastHeartbeat() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastHeartbeat
}

// GetStatus 获取心跳状态信息
func (h *HeartbeatManager) GetStatus() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	return map[string]interface{}{
		"is_healthy":      h.isHealthy,
		"last_heartbeat":  h.lastHeartbeat.Format(time.RFC3339),
		"failure_count":   h.failureCount,
		"interval":        h.interval.String(),
		"timeout":         h.timeout.String(),
		"max_retries":     h.maxRetries,
	}
}

// HeartbeatHandler 心跳检查处理器（用于HTTP接口）
func (h *HeartbeatManager) HeartbeatHandler(c *gin.Context) {
	status := h.GetStatus()
	
	// 生成服务响应签名
	serviceToken := h.auth.generateServiceToken("/heartbeat", "GET")
	
	c.Header("X-Service-Signature", serviceToken.Signature)
	c.Header("X-Service-Timestamp", serviceToken.Timestamp)
	c.Header("X-Service-Name", h.auth.ServiceName)
	c.Header("X-Service-Nonce", serviceToken.Nonce)
	
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "kyc-service",
		"heartbeat": status,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// SecurityHeartbeatHandler 安全心跳处理器（需要双向鉴权）
func (h *HeartbeatManager) SecurityHeartbeatHandler(c *gin.Context) {
	// 验证请求是否来自Kong网关
	kongVerified := c.GetBool("kong_verified")
	if !kongVerified {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "安全心跳检查请求必须通过Kong网关",
			"code":    "SECURITY_HEARTBEAT_UNAUTHORIZED",
		})
		return
	}
	
	status := h.GetStatus()
	
	// 生成服务响应签名
	serviceToken := h.auth.generateServiceToken("/security-heartbeat", "GET")
	
	c.Header("X-Service-Signature", serviceToken.Signature)
	c.Header("X-Service-Timestamp", serviceToken.Timestamp)
	c.Header("X-Service-Name", h.auth.ServiceName)
	c.Header("X-Service-Nonce", serviceToken.Nonce)
	
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "kyc-service",
		"security": true,
		"heartbeat": status,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}