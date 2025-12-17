package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Port     int    `mapstructure:"port"`
	GinMode  string `mapstructure:"gin_mode"`
	LogLevel string `mapstructure:"log_level"`
	UseMock  bool   `mapstructure:"use_mock"`

	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`

	Security SecurityConfig `mapstructure:"security"`

	ThirdParty ThirdPartyConfig `mapstructure:"third_party"`

	Monitoring MonitoringConfig `mapstructure:"monitoring"`

	Storage StorageConfig `mapstructure:"storage"`
}

type MonitoringConfig struct {
	Metrics  MetricsConfig  `mapstructure:"metrics"`
	Tracing  TracingConfig  `mapstructure:"tracing"`
	Alerting AlertingConfig `mapstructure:"alerting"`
}

type DatabaseConfig struct {
	Host               string `mapstructure:"host"`
	Port               int    `mapstructure:"port"`
	User               string `mapstructure:"user"`
	Password           string `mapstructure:"password"`
	DBName             string `mapstructure:"dbname"`
	SSLMode            string `mapstructure:"sslmode"`
	MaxOpenConns       int    `mapstructure:"max_open_conns"`
	MaxIdleConns       int    `mapstructure:"max_idle_conns"`
	AutoMigrateEnabled bool   `mapstructure:"auto_migrate_enabled"`
}

type RedisConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	MaxRetries   int           `mapstructure:"max_retries"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type SecurityConfig struct {
	JWTSecret          string        `mapstructure:"jwt_secret"`
	JWTExpiration      time.Duration `mapstructure:"jwt_expiration"`
	EncryptionKey      string        `mapstructure:"encryption_key"`
	RateLimitPerSecond int           `mapstructure:"rate_limit_per_second"`
	RateLimitBurst     int           `mapstructure:"rate_limit_burst"`
	KongSharedSecret   string        `mapstructure:"kong_shared_secret"`
	ServiceSecretKey   string        `mapstructure:"service_secret_key"`
}

type StorageConfig struct {
	IngestDir string `mapstructure:"ingest_dir"`
}

type ThirdPartyConfig struct {
	OCRService struct {
		URL        string `mapstructure:"url"`
		APIKey     string `mapstructure:"api_key"`
		Timeout    int    `mapstructure:"timeout"`
		RetryCount int    `mapstructure:"retry_count"`
	} `mapstructure:"ocr_service"`

	FaceService struct {
		URL        string `mapstructure:"url"`
		APIKey     string `mapstructure:"api_key"`
		Timeout    int    `mapstructure:"timeout"`
		RetryCount int    `mapstructure:"retry_count"`
	} `mapstructure:"face_service"`

	LivenessSlient struct {
		URL        string `mapstructure:"url"`
		APIKey     string `mapstructure:"api_key"`
		Timeout    int    `mapstructure:"timeout"`
		RetryCount int    `mapstructure:"retry_count"`
	} `mapstructure:"liveness_silent"`

	LivenessVideo struct {
		URL        string `mapstructure:"url"`
		APIKey     string `mapstructure:"api_key"`
		Timeout    int    `mapstructure:"timeout"`
		RetryCount int    `mapstructure:"retry_count"`
	} `mapstructure:"liveness_video"`
}

func Load() *Config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/kyc-service/")

	// 设置默认值
	setDefaults()

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			panic(fmt.Errorf("配置文件读取错误: %w", err))
		}
	}

	// 读取环境变量
	viper.AutomaticEnv()
	viper.SetEnvPrefix("KYC")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		panic(fmt.Errorf("配置解析错误: %w", err))
	}

	return &config
}

func setDefaults() {
	viper.SetDefault("port", 8080)
	viper.SetDefault("gin_mode", "release")
	viper.SetDefault("log_level", "info")

	// 数据库默认值
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.user", "kong")
	viper.SetDefault("database.password", "kongpassword")
	viper.SetDefault("database.dbname", "kong")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("database.max_open_conns", 25)
	viper.SetDefault("database.max_idle_conns", 5)
	viper.SetDefault("database.auto_migrate_enabled", false)

	// Redis默认值
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.pool_size", 10)
	viper.SetDefault("redis.min_idle_conns", 5)
	viper.SetDefault("redis.max_retries", 3)
	viper.SetDefault("redis.dial_timeout", "5s")
	viper.SetDefault("redis.read_timeout", "3s")
	viper.SetDefault("redis.write_timeout", "3s")

	// 安全默认值
	viper.SetDefault("security.jwt_secret", "your-secret-key-here-must-be-32-by")
	viper.SetDefault("security.jwt_expiration", "24h")
	viper.SetDefault("security.encryption_key", "your-encryption-key-here-32-by")
	viper.SetDefault("security.rate_limit_per_second", 100)
	viper.SetDefault("security.rate_limit_burst", 200)
	viper.SetDefault("security.kong_shared_secret", "kong-shared-secret-key-2024")
	viper.SetDefault("security.service_secret_key", "kyc-service-secret-key-2024")

	viper.SetDefault("storage.ingest_dir", "/data/ingest")

	// 第三方服务默认值
	viper.SetDefault("third_party.ocr_service.timeout", 30)
	viper.SetDefault("third_party.ocr_service.retry_count", 3)
	viper.SetDefault("third_party.face_service.timeout", 30)
	viper.SetDefault("third_party.face_service.retry_count", 3)
	viper.SetDefault("third_party.liveness_service.timeout", 60)
	viper.SetDefault("third_party.liveness_service.retry_count", 3)

	// 监控默认值
	viper.SetDefault("monitoring.metrics.enabled", true)
	viper.SetDefault("monitoring.metrics.port", 9090)
	viper.SetDefault("monitoring.metrics.path", "/metrics")
	viper.SetDefault("monitoring.metrics.namespace", "kyc")
	viper.SetDefault("monitoring.metrics.subsystem", "service")
	viper.SetDefault("monitoring.metrics.push_interval", "30s")

	viper.SetDefault("monitoring.tracing.enabled", true)
	viper.SetDefault("monitoring.tracing.service_name", "kyc-service")
	viper.SetDefault("monitoring.tracing.service_version", "1.0.0")
	viper.SetDefault("monitoring.tracing.environment", "production")
	viper.SetDefault("monitoring.tracing.jaeger_endpoint", "http://localhost:14268/api/traces")
	viper.SetDefault("monitoring.tracing.sample_rate", 1.0)
	viper.SetDefault("monitoring.tracing.log_spans", false)

	// 告警默认值
	viper.SetDefault("monitoring.alerting.enabled", false)
	viper.SetDefault("monitoring.alerting.webhook_url", "")
	viper.SetDefault("monitoring.alerting.slack_webhook", "")
	viper.SetDefault("monitoring.alerting.email_smtp", "")
	viper.SetDefault("monitoring.alerting.email_from", "")
}
