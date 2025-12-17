package metrics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"kyc-service/pkg/tracing"

	runtimeotel "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	// HTTP指标
	httpRequestDuration metric.Float64Histogram
	httpRequestsTotal   metric.Int64Counter
	httpRequestErrors   metric.Int64Counter

	// 业务逻辑指标
	businessOperationsTotal   metric.Int64Counter
	businessOperationDuration metric.Float64Histogram
	businessOperationErrors   metric.Int64Counter

	// 认证安全指标
	authFailuresTotal     metric.Int64Counter
	permissionDeniedTotal metric.Int64Counter

	// 敏感数据访问指标
	sensitiveDataAccessTotal metric.Int64Counter

	//dependencyCalls* ：外部依赖指标: 用于内部依赖或基础设施调用
	// - 例如 DB 、 Redis 、内部微服务、队列等
	// - 标签维度： service 、 method 、 status （可选 org_id 、 third_party_code ）
	dependencyCallsTotal   metric.Int64Counter
	dependencyCallDuration metric.Float64Histogram
	dependencyCallErrors   metric.Int64Counter

	//thirdParty* ：第三方服务监控指标: 用于外部厂商/付费供应商的 API 调用
	// - 建议只在第三方集成层（例如 internal/service/third_party_service.go ）统一调用
	// - 标签维度： third_party_name 、 result 、 http_status_code
	thirdPartyRequestDuration metric.Float64Histogram
	thirdPartyRequestsTotal   metric.Int64Counter
	thirdPartyRequestErrors   metric.Int64Counter

	// 系统指标
	systemErrorRate metric.Float64Gauge
	activeRequests  metric.Int64UpDownCounter
	requestRate     metric.Float64Gauge

	orgQuotaLimit  metric.Int64UpDownCounter
	orgQuotaUsed   metric.Int64UpDownCounter
	lastQuotaLimit = map[string]int{}

	// 审计事件计数
	auditEventsTotal metric.Int64Counter

	// 配额持久化失败计数
	quotaPersistErrorsTotal metric.Int64Counter
)

const (
	ResultSuccess                 = "success"
	ResultDialError               = "dial_error"
	ResultRequestTimeout          = "request_timeout"
	ResultContextCanceled         = "context_canceled"
	ResultHTTPClientError         = "http_client_error"
	ResultHTTPServerError         = "http_server_error"
	ResultResponseReadFailed      = "response_read_failed"
	ResultResponseUnmarshalFailed = "response_unmarshal_failed"
	ResultBusinessFailed          = "business_failed"
	ResultRequestPrepareFailed    = "request_prepare_failed"
)

// InitOTelMetrics 初始化OpenTelemetry指标
func InitOTelMetrics() error {
	meter := tracing.GetMeter()

	var err error

	// HTTP指标
	httpRequestDuration, err = meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("创建HTTP请求耗时指标失败: %w", err)
	}

	httpRequestsTotal, err = meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建HTTP请求总数指标失败: %w", err)
	}

	httpRequestErrors, err = meter.Int64Counter(
		"http_request_errors_total",
		metric.WithDescription("Total number of HTTP request errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建HTTP请求错误指标失败: %w", err)
	}

	// 业务逻辑指标
	businessOperationsTotal, err = meter.Int64Counter(
		"business_operations_total",
		metric.WithDescription("Total number of business operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建业务操作总数指标失败: %w", err)
	}

	businessOperationDuration, err = meter.Float64Histogram(
		"business_operation_duration_seconds",
		metric.WithDescription("Business operation duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("创建业务操作耗时指标失败: %w", err)
	}

	businessOperationErrors, err = meter.Int64Counter(
		"business_operation_errors_total",
		metric.WithDescription("Total number of business operation errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建业务操作错误指标失败: %w", err)
	}

	// 认证安全指标
	authFailuresTotal, err = meter.Int64Counter(
		"auth_failures_total",
		metric.WithDescription("Total number of authentication failures"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建认证失败指标失败: %w", err)
	}

	permissionDeniedTotal, err = meter.Int64Counter(
		"permission_denied_total",
		metric.WithDescription("Total number of permission denied errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建权限拒绝指标失败: %w", err)
	}

	// 敏感数据访问指标
	sensitiveDataAccessTotal, err = meter.Int64Counter(
		"sensitive_data_access_total",
		metric.WithDescription("Total number of sensitive data access attempts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建敏感数据访问指标失败: %w", err)
	}

	// 外部依赖指标
	dependencyCallsTotal, err = meter.Int64Counter(
		"dependency_calls_total",
		metric.WithDescription("Total number of dependency calls"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建依赖调用总数指标失败: %w", err)
	}

	dependencyCallDuration, err = meter.Float64Histogram(
		"dependency_call_duration_seconds",
		metric.WithDescription("Dependency call duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("创建依赖调用耗时指标失败: %w", err)
	}

	dependencyCallErrors, err = meter.Int64Counter(
		"dependency_call_errors_total",
		metric.WithDescription("Total number of dependency call errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建依赖调用错误指标失败: %w", err)
	}

	// 新增：第三方服务监控指标
	thirdPartyRequestsTotal, err = meter.Int64Counter(
		"third_party_requests_total",
		metric.WithDescription("Total number of requests to third-party services."),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建第三方服务请求总数指标失败: %w", err)
	}

	thirdPartyRequestDuration, err = meter.Float64Histogram(
		"third_party_request_duration_seconds",
		metric.WithDescription("Duration of requests to third-party services."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("创建第三方服务请求耗时指标失败: %w", err)
	}

	thirdPartyRequestErrors, err = meter.Int64Counter(
		"third_party_request_errors_total",
		metric.WithDescription("Total number of failed requests to third-party services by error type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建第三方服务错误总数指标失败: %w", err)
	}

	thirdPartyRequestErrors, err = meter.Int64Counter(
		"third_party_request_errors_total",
		metric.WithDescription("Total number of failed requests to third-party services by error type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建第三方服务错误总数指标失败: %w", err)
	}

	// 系统指标
	systemErrorRate, err = meter.Float64Gauge(
		"system_error_rate",
		metric.WithDescription("System error rate"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建系统错误率指标失败: %w", err)
	}

	activeRequests, err = meter.Int64UpDownCounter(
		"active_requests",
		metric.WithDescription("Number of active requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建活跃请求指标失败: %w", err)
	}

	requestRate, err = meter.Float64Gauge(
		"request_rate_per_second",
		metric.WithDescription("Request rate per second"),
		metric.WithUnit("1/s"),
	)
	if err != nil {
		return fmt.Errorf("创建请求速率指标失败: %w", err)
	}

	// 审计事件指标
	auditEventsTotal, err = meter.Int64Counter(
		"audit_events_total",
		metric.WithDescription("Total number of audit events"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建审计事件总数指标失败: %w", err)
	}

	// 配额持久化失败指标
	quotaPersistErrorsTotal, err = meter.Int64Counter(
		"quota_persist_errors_total",
		metric.WithDescription("Total number of quota persist failures"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建配额持久化失败指标失败: %w", err)
	}
	// 组织配额指标
	orgQuotaLimit, err = meter.Int64UpDownCounter(
		"org_quota_limit",
		metric.WithDescription("Organization quota limit per service type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建组织配额上限指标失败: %w", err)
	}

	orgQuotaUsed, err = meter.Int64UpDownCounter(
		"org_quota_used",
		metric.WithDescription("Organization quota used per service type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("创建组织配额使用指标失败: %w", err)
	}

	// 启用 Go runtime 指标导出
	_ = runtimeotel.Start(runtimeotel.WithMinimumReadMemStatsInterval(10 * time.Second))
	return nil
}

// RecordHTTPRequest 记录HTTP请求指标
func RecordHTTPRequest(ctx context.Context, method, endpoint, status string, duration time.Duration) {
	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	reqID := ""
	if v := ctx.Value("request_id"); v != nil {
		if s, ok := v.(string); ok {
			reqID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("http_method", method),
		attribute.String("http_endpoint", endpoint),
		attribute.String("http_status", status),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}
	if reqID != "" {
		attrs = append(attrs, attribute.String("request_id", reqID))
	}

	httpRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	httpRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))

	// 记录活跃请求
	activeRequests.Add(ctx, 1, metric.WithAttributes(attribute.String("type", "http")))
	activeRequests.Add(ctx, -1, metric.WithAttributes(attribute.String("type", "http")))

	// 记录错误
	if code, err := strconv.Atoi(status); err == nil && code >= 400 {
		httpRequestErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordBusinessOperation 记录业务操作指标
func RecordBusinessOperation(ctx context.Context, operation string, success bool, duration time.Duration, errorType string) {
	status := "success"
	if !success {
		status = "failed"
	}

	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("status", status),
		attribute.String("error_type", errorType),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}

	businessOperationsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	durAttrs := []attribute.KeyValue{attribute.String("operation", operation)}
	if orgID != "" {
		durAttrs = append(durAttrs, attribute.String("org_id", orgID))
	}
	businessOperationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(durAttrs...))

	if !success && errorType != "" {
		businessOperationErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordAuthFailure 记录认证失败
func RecordAuthFailure(ctx context.Context, authType, reason, clientIP string) {
	attrs := []attribute.KeyValue{
		attribute.String("auth_type", authType),
		attribute.String("reason", reason),
		attribute.String("client_ip", clientIP),
	}

	authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordPermissionDenied 记录权限拒绝
func RecordPermissionDenied(ctx context.Context, resource, action, userID, reason string) {
	attrs := []attribute.KeyValue{
		attribute.String("resource", resource),
		attribute.String("action", action),
		attribute.String("user_id", userID),
		attribute.String("reason", reason),
	}

	permissionDeniedTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordSensitiveDataAccess 记录敏感数据访问
func RecordSensitiveDataAccess(ctx context.Context, dataType, userID string, authorized bool, endpoint string) {
	status := "authorized"
	if !authorized {
		status = "unauthorized"
	}

	attrs := []attribute.KeyValue{
		attribute.String("data_type", dataType),
		attribute.String("user_id", userID),
		attribute.String("status", status),
		attribute.String("endpoint", endpoint),
	}

	sensitiveDataAccessTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordDependencyCall 记录依赖调用
func RecordDependencyCall(ctx context.Context, service, method string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failed"
	}

	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("service", service),
		attribute.String("method", method),
		attribute.String("status", status),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}

	dependencyCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	durAttrs := []attribute.KeyValue{attribute.String("service", service), attribute.String("method", method)}
	if orgID != "" {
		durAttrs = append(durAttrs, attribute.String("org_id", orgID))
	}
	dependencyCallDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(durAttrs...))

	if !success {
		dependencyCallErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func RecordDependencyCallCode(ctx context.Context, service, method string, success bool, duration time.Duration, code string) {
	status := "success"
	if !success {
		status = "failed"
	}
	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("service", service),
		attribute.String("method", method),
		attribute.String("status", status),
		attribute.String("third_party_code", code),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}
	dependencyCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	durAttrs := []attribute.KeyValue{attribute.String("service", service), attribute.String("method", method), attribute.String("third_party_code", code)}
	if orgID != "" {
		durAttrs = append(durAttrs, attribute.String("org_id", orgID))
	}
	dependencyCallDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(durAttrs...))
	if !success {
		dependencyCallErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// 新增：RecordThirdPartyRequest 记录第三方服务请求指标
func RecordThirdPartyRequest(ctx context.Context, thirdPartyName, result, httpStatusCode string, duration time.Duration) {
	RecordThirdPartyRequestWithOp(ctx, thirdPartyName, "", result, httpStatusCode, duration)
}

func RecordThirdPartyRequestWithOp(ctx context.Context, thirdPartyName, operation, result, httpStatusCode string, duration time.Duration) {
	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	reqID := ""
	if v := ctx.Value("request_id"); v != nil {
		if s, ok := v.(string); ok {
			reqID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("third_party_name", thirdPartyName),
		attribute.String("operation", operation),
		attribute.String("result", result),
		attribute.String("http_status_code", httpStatusCode),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}
	if reqID != "" {
		attrs = append(attrs, attribute.String("request_id", reqID))
	}
	thirdPartyRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))

	durationAttrs := []attribute.KeyValue{
		attribute.String("third_party_name", thirdPartyName),
		attribute.String("operation", operation),
		attribute.String("result", result),
	}
	if orgID != "" {
		durationAttrs = append(durationAttrs, attribute.String("org_id", orgID))
	}
	if reqID != "" {
		durationAttrs = append(durationAttrs, attribute.String("request_id", reqID))
	}
	thirdPartyRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(durationAttrs...))
}

func RecordThirdPartyError(ctx context.Context, thirdPartyName, operation, errorType string) {
	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	reqID := ""
	if v := ctx.Value("request_id"); v != nil {
		if s, ok := v.(string); ok {
			reqID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("third_party_name", thirdPartyName),
		attribute.String("operation", operation),
		attribute.String("error_type", errorType),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}
	if reqID != "" {
		attrs = append(attrs, attribute.String("request_id", reqID))
	}
	thirdPartyRequestErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordAuditEvent 记录审计事件计数
func RecordAuditEvent(ctx context.Context, action, resource, status string) {
	orgID := ""
	if v := ctx.Value("org_id"); v != nil {
		if s, ok := v.(string); ok {
			orgID = s
		}
	}
	reqID := ""
	if v := ctx.Value("request_id"); v != nil {
		if s, ok := v.(string); ok {
			reqID = s
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String("action", action),
		attribute.String("resource", resource),
		attribute.String("status", status),
	}
	if orgID != "" {
		attrs = append(attrs, attribute.String("org_id", orgID))
	}
	if reqID != "" {
		attrs = append(attrs, attribute.String("request_id", reqID))
	}
	auditEventsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordQuotaPersistFailure 记录配额持久化失败
func RecordQuotaPersistFailure(ctx context.Context, orgID, serviceType, reason string) {
	attrs := []attribute.KeyValue{
		attribute.String("org_id", orgID),
		attribute.String("service_type", serviceType),
		attribute.String("reason", reason),
	}
	quotaPersistErrorsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// UpdateSystemMetrics 更新系统指标
func UpdateSystemMetrics(ctx context.Context, errorRate float64, requestRate float64) {
	if systemErrorRate != nil {
		systemErrorRate.Record(ctx, errorRate)
	}
	// requestRate 参数暂时不使用，避免编译错误
}

// RecordActiveRequest 记录活跃请求
func RecordActiveRequest(ctx context.Context, requestType string, delta int64) {
	attrs := []attribute.KeyValue{
		attribute.String("type", requestType),
	}

	activeRequests.Add(ctx, delta, metric.WithAttributes(attrs...))
}

func SetOrgQuotaLimit(ctx context.Context, orgID, serviceType string, newLimit int) {
	key := orgID + ":" + serviceType
	old := lastQuotaLimit[key]
	delta := newLimit - old
	if delta != 0 {
		attrs := []attribute.KeyValue{
			attribute.String("org_id", orgID),
			attribute.String("service_type", serviceType),
		}
		if orgQuotaLimit != nil {
			orgQuotaLimit.Add(ctx, int64(delta), metric.WithAttributes(attrs...))
		}
		lastQuotaLimit[key] = newLimit
	}
}

func IncOrgQuotaUsed(ctx context.Context, orgID, serviceType string, delta int) {
	attrs := []attribute.KeyValue{
		attribute.String("org_id", orgID),
		attribute.String("service_type", serviceType),
	}
	if orgQuotaUsed != nil {
		orgQuotaUsed.Add(ctx, int64(delta), metric.WithAttributes(attrs...))
	}
}
