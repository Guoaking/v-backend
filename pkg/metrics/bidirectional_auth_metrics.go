package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 双向鉴权相关指标
var (
	// 安全事件计数器
	SecurityEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kyc_security_events_total",
			Help: "安全事件总数",
		},
		[]string{"event_type", "client_ip", "path"},
	)

	// 服务认证失败计数器
	ServiceAuthFailuresTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kyc_service_auth_failures_total",
			Help: "服务认证失败总数",
		},
	)

	// 无效签名计数器
	InvalidSignaturesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kyc_invalid_signatures_total",
			Help: "无效签名总数",
		},
	)

	// 时间戳过期计数器
	TimestampExpiredTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kyc_timestamp_expired_total",
			Help: "时间戳过期总数",
		},
	)

	// 心跳健康状态
	HeartbeatHealthy = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kyc_heartbeat_healthy",
			Help: "心跳健康状态 (1=健康, 0=异常)",
		},
	)

	// 心跳延迟
	HeartbeatDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kyc_heartbeat_duration_seconds",
			Help:    "心跳检测延迟",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
	)

	// 认证请求计数器
	AuthRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kyc_auth_requests_total",
			Help: "认证请求总数",
		},
		[]string{"auth_type", "status"},
	)

	// 限流触发计数器
	RateLimitExceededTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kyc_rate_limit_exceeded_total",
			Help: "限流触发总数",
		},
	)

	// 可疑IP访问计数器
	SuspiciousIPAccessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kyc_suspicious_ip_access_total",
			Help: "可疑IP访问总数",
		},
		[]string{"client_ip", "path", "reason"},
	)

	// 证书过期天数
	CertificateExpiryDays = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kyc_certificate_expiry_days",
			Help: "证书剩余过期天数",
		},
	)

	// 服务间通信错误计数器
	InterServiceCommunicationErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kyc_inter_service_communication_errors_total",
			Help: "服务间通信错误总数",
		},
		[]string{"source_service", "target_service", "error_type"},
	)

	// 双向鉴权整体健康状态
	BidirectionalAuthHealthy = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kyc_bidirectional_auth_healthy",
			Help: "双向鉴权整体健康状态 (1=健康, 0=异常)",
		},
	)
)

// RecordSecurityEvent 记录安全事件
func RecordSecurityEvent(eventType, clientIP, path string) {
	SecurityEventsTotal.WithLabelValues(eventType, clientIP, path).Inc()
}

// RecordServiceAuthFailure 记录服务认证失败
func RecordServiceAuthFailure() {
	ServiceAuthFailuresTotal.Inc()
}

// RecordInvalidSignature 记录无效签名
func RecordInvalidSignature() {
	InvalidSignaturesTotal.Inc()
}

// RecordTimestampExpired 记录时间戳过期
func RecordTimestampExpired() {
	TimestampExpiredTotal.Inc()
}

// RecordHeartbeatSuccess 记录心跳成功
func RecordHeartbeatSuccess(duration time.Duration) {
	HeartbeatHealthy.Set(1)
	HeartbeatDuration.Observe(duration.Seconds())
}

// RecordHeartbeatFailure 记录心跳失败
func RecordHeartbeatFailure() {
	HeartbeatHealthy.Set(0)
}

// RecordAuthRequest 记录认证请求
func RecordAuthRequest(authType string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	AuthRequestsTotal.WithLabelValues(authType, status).Inc()
}

// RecordRateLimitExceeded 记录限流触发
func RecordRateLimitExceeded() {
	RateLimitExceededTotal.Inc()
}

// RecordSuspiciousIPAccess 记录可疑IP访问
func RecordSuspiciousIPAccess(clientIP, path, reason string) {
	SuspiciousIPAccessTotal.WithLabelValues(clientIP, path, reason).Inc()
}

// UpdateCertificateExpiry 更新证书过期天数
func UpdateCertificateExpiry(days float64) {
	CertificateExpiryDays.Set(days)
}

// RecordInterServiceCommunicationError 记录服务间通信错误
func RecordInterServiceCommunicationError(sourceService, targetService, errorType string) {
	InterServiceCommunicationErrorsTotal.WithLabelValues(sourceService, targetService, errorType).Inc()
}

// UpdateBidirectionalAuthHealth 更新双向鉴权健康状态
func UpdateBidirectionalAuthHealth(healthy bool) {
	if healthy {
		BidirectionalAuthHealthy.Set(1)
	} else {
		BidirectionalAuthHealthy.Set(0)
	}
}

// JWT相关指标
var (
	// JWT生成计数器
	JWTGenerationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kyc_jwt_generation_total",
			Help: "JWT令牌生成总数",
		},
		[]string{"issuer", "algorithm", "status"},
	)

	// JWT验证计数器
	JWTValidationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kyc_jwt_validation_total",
			Help: "JWT令牌验证总数",
		},
		[]string{"status", "reason"},
	)

	// JWT生成耗时
	JWTGenerationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kyc_jwt_generation_duration_seconds",
			Help:    "JWT令牌生成耗时",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"issuer", "algorithm"},
	)

	// JWT验证耗时
	JWTValidationDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kyc_jwt_validation_duration_seconds",
			Help:    "JWT令牌验证耗时",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
	)
)

// RecordJWTGeneration 记录JWT生成
func RecordJWTGeneration(issuer, algorithm string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	JWTGenerationTotal.WithLabelValues(issuer, algorithm, status).Inc()
}

// RecordJWTValidation 记录JWT验证
func RecordJWTValidation(success bool) {
	status := "success"
	reason := "valid"
	if !success {
		status = "failure"
		reason = "invalid"
	}
	JWTValidationTotal.WithLabelValues(status, reason).Inc()
}

// RecordJWTGenerationDuration 记录JWT生成耗时
func RecordJWTGenerationDuration(issuer, algorithm string, duration time.Duration) {
	JWTGenerationDuration.WithLabelValues(issuer, algorithm).Observe(duration.Seconds())
}

// RecordJWTValidationDuration 记录JWT验证耗时
func RecordJWTValidationDuration(duration time.Duration) {
	JWTValidationDuration.Observe(duration.Seconds())
}

// BidirectionalAuthMetricsCollector 双向鉴权指标收集器
type BidirectionalAuthMetricsCollector struct {
	ctx context.Context
}

// NewBidirectionalAuthMetricsCollector 创建指标收集器
func NewBidirectionalAuthMetricsCollector(ctx context.Context) *BidirectionalAuthMetricsCollector {
	return &BidirectionalAuthMetricsCollector{
		ctx: ctx,
	}
}

// Start 启动指标收集
func (c *BidirectionalAuthMetricsCollector) Start() {
	go c.collectMetrics()
}

// collectMetrics 收集指标
func (c *BidirectionalAuthMetricsCollector) collectMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			c.updateMetrics()
		case <-c.ctx.Done():
			return
		}
	}
}

// updateMetrics 更新指标
func (c *BidirectionalAuthMetricsCollector) updateMetrics() {
	// 这里可以添加具体的指标更新逻辑
	// 例如检查证书过期时间、服务健康状态等
	
	// 模拟证书过期时间（实际应该从证书文件中读取）
	UpdateCertificateExpiry(30.0) // 30天后过期
	
	// 模拟双向鉴权健康状态（实际应该根据实际状态设置）
	UpdateBidirectionalAuthHealth(true)
}