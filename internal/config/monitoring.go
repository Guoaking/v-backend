package config

import (
	"time"
)

// LogConfig 日志配置
type LogConfig struct {
	Level         string        `mapstructure:"level"`
	Format        string        `mapstructure:"format"`
	Output        string        `mapstructure:"output"`
	FilePath      string        `mapstructure:"file_path"`
	MaxSize       int           `mapstructure:"max_size"`    // MB
	MaxBackups    int           `mapstructure:"max_backups"` // 保留文件数
	MaxAge        int           `mapstructure:"max_age"`     // days
	Compress      bool          `mapstructure:"compress"`
	LocalTime     bool          `mapstructure:"local_time"`
	FlushInterval time.Duration `mapstructure:"flush_interval"`
}

// MetricsConfig 监控指标配置
type MetricsConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	Port             int           `mapstructure:"port"`
	Path             string        `mapstructure:"path"`
	Namespace        string        `mapstructure:"namespace"`
	Subsystem        string        `mapstructure:"subsystem"`
	PushGateway      string        `mapstructure:"push_gateway"`
	PushInterval     time.Duration `mapstructure:"push_interval"`
	HistogramBuckets []float64     `mapstructure:"histogram_buckets"`
}

// TracingConfig 链路追踪配置
type TracingConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	ServiceName    string  `mapstructure:"service_name"`
	ServiceVersion string  `mapstructure:"service_version"`
	Environment    string  `mapstructure:"environment"`
	JaegerEndpoint string  `mapstructure:"jaeger_endpoint"`
	SampleRate     float64 `mapstructure:"sample_rate"`
	LogSpans       bool    `mapstructure:"log_spans"`
}

// AlertingConfig 告警配置
type AlertingConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	WebhookURL    string   `mapstructure:"webhook_url"`
	SlackWebhook  string   `mapstructure:"slack_webhook"`
	EmailSMTP     string   `mapstructure:"email_smtp"`
	EmailFrom     string   `mapstructure:"email_from"`
	EmailUser     string   `mapstructure:"email_user"`
	EmailPassword string   `mapstructure:"email_password"`
	EmailPort     int      `mapstructure:"email_port"`
	EmailTLS      bool     `mapstructure:"email_tls"`
	EmailTo       []string `mapstructure:"email_to"`

	Rules []AlertRule `mapstructure:"rules"`
}

// AlertRule 告警规则
type AlertRule struct {
	Name        string        `mapstructure:"name"`
	Metric      string        `mapstructure:"metric"`
	Condition   string        `mapstructure:"condition"` // >, <, >=, <=, ==, !=
	Threshold   float64       `mapstructure:"threshold"`
	Duration    time.Duration `mapstructure:"duration"`
	Severity    string        `mapstructure:"severity"` // critical, warning, info
	Description string        `mapstructure:"description"`
	Action      string        `mapstructure:"action"` // webhook, email, slack
}
