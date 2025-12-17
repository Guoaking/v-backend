package tracing

import (
	"context"
	"fmt"
	"log"

	"kyc-service/internal/config"
	"kyc-service/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	otelmetric "go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	tracer oteltrace.Tracer
	meter  otelmetric.Meter
)

// Init 初始化OpenTelemetry
func Init(cfg *config.Config) (func(), error) {
	var cleanupFuncs []func()

	// 创建资源
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.Monitoring.Tracing.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Monitoring.Tracing.ServiceVersion),
			attribute.String("environment", cfg.Monitoring.Tracing.Environment),
		),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("创建资源失败: %w", err)
	}

	// 初始化追踪
	if cfg.Monitoring.Tracing.Enabled {
		// 创建Jaeger导出器
		traceExporter, err := jaeger.New(jaeger.WithCollectorEndpoint(
			jaeger.WithEndpoint(cfg.Monitoring.Tracing.JaegerEndpoint),
		))
		if err != nil {
			return nil, fmt.Errorf("创建Jaeger导出器失败: %w", err)
		}

		// 创建TracerProvider
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.Monitoring.Tracing.SampleRate)),
		)

		// 设置全局TracerProvider
		otel.SetTracerProvider(tp)

		// 创建tracer
		tracer = tp.Tracer(cfg.Monitoring.Tracing.ServiceName)

		cleanupFuncs = append(cleanupFuncs, func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Printf("TracerProvider关闭失败: %v", err)
			}
		})
	}

	// 初始化指标
	if cfg.Monitoring.Metrics.Enabled {
		// 创建Prometheus导出器
		promExporter, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("创建Prometheus导出器失败: %w", err)
		}

		// 创建MeterProvider
		mp := metric.NewMeterProvider(
			metric.WithReader(promExporter),
			metric.WithResource(res),
		)

		// 设置全局MeterProvider
		otel.SetMeterProvider(mp)

		// 创建meter
		meter = mp.Meter(cfg.Monitoring.Tracing.ServiceName)

		// 初始化自定义指标将在main函数中完成，避免循环导入

		cleanupFuncs = append(cleanupFuncs, func() {
			if err := mp.Shutdown(context.Background()); err != nil {
				log.Printf("MeterProvider关闭失败: %v", err)
			}
		})

		logger.GetLogger().Infof("OpenTelemetry指标已启用，Prometheus端点: %s:%d%s", 
			"localhost", cfg.Monitoring.Metrics.Port, cfg.Monitoring.Metrics.Path)
	}

	// 设置全局传播器
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.GetLogger().WithFields(map[string]interface{}{
		"service_name":    cfg.Monitoring.Tracing.ServiceName,
		"service_version": cfg.Monitoring.Tracing.ServiceVersion,
		"environment":     cfg.Monitoring.Tracing.Environment,
		"sample_rate":     cfg.Monitoring.Tracing.SampleRate,
		"jaeger_endpoint": cfg.Monitoring.Tracing.JaegerEndpoint,
		"metrics_enabled": cfg.Monitoring.Metrics.Enabled,
		"metrics_port":    cfg.Monitoring.Metrics.Port,
		"metrics_path":    cfg.Monitoring.Metrics.Path,
	}).Info("OpenTelemetry初始化成功")

	// 系统指标收集器将在main函数中启动，避免循环导入

	// 返回清理函数
	cleanup := func() {
		for _, f := range cleanupFuncs {
			f()
		}
	}

	return cleanup, nil
}


// GetTracer 获取tracer
func GetTracer() oteltrace.Tracer {
	if tracer == nil {
		return otel.Tracer("kyc-service")
	}
	return tracer
}

// GetMeter 获取meter
func GetMeter() otelmetric.Meter {
	if meter == nil {
		return otel.Meter("kyc-service")
	}
	return meter
}

// StartSpan 开始一个span
func StartSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	return GetTracer().Start(ctx, name, opts...)
}

// SpanFromContext 从context获取span
func SpanFromContext(ctx context.Context) oteltrace.Span {
	return oteltrace.SpanFromContext(ctx)
}