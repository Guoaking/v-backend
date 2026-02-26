package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

var log *logrus.Logger

func Init(level string) {
	log = logrus.New()

	// 设置日志格式
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05.000",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			// 简化文件名
			filename := strings.Split(f.File, "/")
			return "", fmt.Sprintf("%s:%d", filename[len(filename)-1], f.Line)
		},
	})

	// 设置日志级别
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logLevel = logrus.InfoLevel
	}
	log.SetLevel(logLevel)

	// 设置输出：控制台 + 文件（带滚动切分）
	out := io.Writer(os.Stdout)
	filePath := os.Getenv("KYC_LOG_FILE")
	if filePath == "" {
		filePath = "./logs/kyc-service.log"
	}
	_ = os.MkdirAll(filepath.Dir(filePath), 0755)

	maxSize := 100
	if v := os.Getenv("KYC_LOG_MAX_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			maxSize = i
		}
	}
	maxBackups := 7
	if v := os.Getenv("KYC_LOG_MAX_BACKUPS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			maxBackups = i
		}
	}
	maxAge := 14
	if v := os.Getenv("KYC_LOG_MAX_AGE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			maxAge = i
		}
	}
	compress := true
	if v := os.Getenv("KYC_LOG_COMPRESS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			compress = b
		}
	}

	rotator := &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    maxSize, // MB
		MaxBackups: maxBackups,
		MaxAge:     maxAge, // days
		Compress:   compress,
	}
	log.SetOutput(io.MultiWriter(out, rotator))

	// 添加自定义钩子
	log.AddHook(&ContextHook{})
}

func GetLogger() *logrus.Logger {
	if log == nil {
		Init("info")
	}
	return log
}

// ContextHook 添加上下文信息
type ContextHook struct{}

func (hook *ContextHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (hook *ContextHook) Fire(entry *logrus.Entry) error {
	// 添加goroutine ID等信息
	entry.Data["goroutine"] = getGID()
	return nil
}

func getGID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	var gid uint64
	fmt.Sscanf(string(b), "goroutine %d", &gid)
	return gid
}

// 脱敏函数
func DesensitizeIDCard(id string) string {
	if len(id) < 8 {
		return "***"
	}
	return id[:4] + "****" + id[len(id)-4:]
}

func DesensitizeName(name string) string {
	if len(name) <= 1 {
		return "*"
	}
	return name[:1] + "*"
}

func DesensitizePhone(phone string) string {
	if len(phone) < 7 {
		return "***"
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}
