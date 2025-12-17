package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
)

// LoadTester 负载测试器
type LoadTester struct {
	config     *Config
	httpClient *http.Client
	metrics    *Metrics
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// Config 测试配置
type Config struct {
	TargetURL      string        `json:"target_url"`
	QPS            int           `json:"qps"`
	Duration       time.Duration `json:"duration"`
	AuthType       string        `json:"auth_type"` // jwt, oauth, none
	JWTToken       string        `json:"jwt_token"`
	JWTSecret      string        `json:"jwt_secret"`
	OAuthToken     string        `json:"oauth_token"`
	OAuthConfig    OAuthConfig   `json:"oauth_config"`
	ErrorRate      float64       `json:"error_rate"` // 错误率，0-1之间
	MetricsPort    int           `json:"metrics_port"`
	ConcurrentReqs int           `json:"concurrent_reqs"`
}

// OAuthConfig OAuth配置
type OAuthConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GrantType    string `json:"grant_type"`
	Scope        string `json:"scope"`
}

// Metrics 监控指标
type Metrics struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	activeRequests  prometheus.Gauge
	errorRate       prometheus.Gauge
	qps             prometheus.Gauge
	successRate     prometheus.Gauge
}

type BaseResponse struct {
	Code      int    `json:"code"`             // response code
	Message   string `json:"message"`          // response message
	Timestamp int64  `json:"timestamp"`        // timestamp (ms)
	RequestID string `json:"request_id"`       // request id
	Path      string `json:"path,omitempty"`   // request path
	Method    string `json:"method,omitempty"` // request method
}

type SuccessResponse[T any] struct {
	BaseResponse
	Data T `json:"data,omitempty"` // response data
}

// APIEndpoint API端点配置
type APIEndpoint struct {
	Path       string
	Method     string
	Weight     int // 权重，用于请求分布
	ErrorCodes []int
}

var (
	endpoints = []APIEndpoint{
		//{Path: "/api/v1/oauth/token", Method: "POST", Weight: 10, ErrorCodes: []int{400, 401, 403}},
		//{Path: "/api/v1/oauth/refresh", Method: "POST", Weight: 5, ErrorCodes: []int{400, 401}},
		{Path: "/api/v1/kyc/ocr", Method: "POST", Weight: 15, ErrorCodes: []int{400, 401, 413, 422}},
		// {Path: "/api/v1/kyc/face/search", Method: "POST", Weight: 15, ErrorCodes: []int{400, 401, 413, 422}},
		// {Path: "/api/v1/kyc/face/compare", Method: "POST", Weight: 10, ErrorCodes: []int{400, 401, 413, 422}},
		// {Path: "/api/v1/kyc/face/detect", Method: "POST", Weight: 10, ErrorCodes: []int{400, 401, 413, 422}},
		// {Path: "/api/v1/kyc/liveness/silent", Method: "POST", Weight: 10, ErrorCodes: []int{400, 401, 413, 422}},
		//{Path: "/api/v1/kyc/liveness/video", Method: "POST", Weight: 5, ErrorCodes: []int{400, 401, 413, 422}},
		//{Path: "/api/v1/kyc/verify", Method: "POST", Weight: 10, ErrorCodes: []int{400, 401, 413, 422}},
		//{Path: "/api/v1/kyc/status/test-id", Method: "GET", Weight: 5, ErrorCodes: []int{400, 401, 404}},
	}
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "kyc-loadtest",
		Short: "KYC服务8082端口负载测试工具",
		Long:  `模拟高并发用户访问KYC服务8082端口，支持JWT和OAuth2认证，可配置错误率和QPS`,
		Run:   runLoadTest,
	}

	// 命令行参数
	//rootCmd.Flags().StringP("target", "t", "http://127.0.0.1:8000", "目标服务URL")
	rootCmd.Flags().StringP("target", "t", "http://127.0.0.1:8082", "目标服务URL")
	rootCmd.Flags().IntP("qps", "q", 1, "每秒查询数")
	rootCmd.Flags().DurationP("duration", "d", 5*time.Second, "测试持续时间")
	rootCmd.Flags().StringP("auth", "a", "oauth", "认证类型: jwt, oauth, none")
	//rootCmd.Flags().StringP("token", "k", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb25zdW1lcl9pZCI6InlZQkQ0S2RhWThZN1lhV09ZSnZsdmdsMmZOZkk4Z3pHIiwiZXhwIjoxNzYzNjU0Njc3LCJpYXQiOjE3NjM2NTEwNzcsImlzcyI6InlZQkQ0S2RhWThZN1lhV09ZSnZsdmdsMmZOZkk4Z3pHIiwia2V5IjoieVlCRDRLZGFZOFk3WWFXT1lKdmx2Z2wyZk5mSThnekciLCJuYmYiOjE3NjM2NTEwNzd9.ssopG1Xq-8-uKAHVvDbjE0bUSMptjawsv5nKb1D0uzo", "JWT或OAuth令牌")
	//rootCmd.Flags().StringP("jwt-secret", "s", "your-secret-key-here-must-be-32-bytes-long", "JWT密钥")
	rootCmd.Flags().Float64P("error-rate", "e", 0.1, "错误率(0-1)")
	rootCmd.Flags().IntP("metrics-port", "m", 9091, "Prometheus指标端口")
	rootCmd.Flags().IntP("concurrent", "c", 10, "并发请求数")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func runLoadTest(cmd *cobra.Command, args []string) {
	// 解析参数
	targetURL, _ := cmd.Flags().GetString("target")
	qps, _ := cmd.Flags().GetInt("qps")
	duration, _ := cmd.Flags().GetDuration("duration")
	authType, _ := cmd.Flags().GetString("auth")
	token, _ := cmd.Flags().GetString("token")
	jwtSecret, _ := cmd.Flags().GetString("jwt-secret")
	errorRate, _ := cmd.Flags().GetFloat64("error-rate")
	metricsPort, _ := cmd.Flags().GetInt("metrics-port")
	concurrentReqs, _ := cmd.Flags().GetInt("concurrent")

	config := &Config{
		TargetURL:  targetURL,
		QPS:        qps,
		Duration:   duration,
		AuthType:   authType,
		JWTToken:   token,
		JWTSecret:  jwtSecret,
		OAuthToken: token,
		OAuthConfig: OAuthConfig{
			ClientID:     "fb049a77-9d8c-41ff-bf56-202f1a269740",
			ClientSecret: "bfc4de25-fb35-4784-bf7f-b913286fa157",
			GrantType:    "client_credentials",
			//Scope:        "ocr:read liveness:read face:read",
		},
		ErrorRate:      errorRate,
		MetricsPort:    metricsPort,
		ConcurrentReqs: concurrentReqs,
	}

	// 创建负载测试器
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tester := NewLoadTester(config, ctx, cancel)

	// 启动Prometheus指标服务
	go tester.startMetricsServer()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("开始KYC服务8082端口负载测试...\n")
	fmt.Printf("目标: %s\n", targetURL)
	fmt.Printf("QPS: %d\n", qps)
	fmt.Printf("持续时间: %v\n", duration)
	fmt.Printf("认证类型: %s\n", authType)
	fmt.Printf("错误率: %.2f%%\n", errorRate*100)
	fmt.Printf("并发数: %d\n", concurrentReqs)
	fmt.Printf("监控指标: http://localhost:%d/metrics\n", metricsPort)

	// 处理认证
	if err := tester.setupAuth(); err != nil {
		fmt.Printf("认证设置失败: %v\n", err)
		return
	}

	// 开始测试
	tester.Start()

	// 等待测试完成或信号中断
	select {
	case <-time.After(duration):
		fmt.Println("测试时间到，停止测试")
	case <-sigChan:
		fmt.Println("收到中断信号，停止测试")
	}

	tester.Stop()
	fmt.Println("负载测试完成")
}

func NewLoadTester(config *Config, ctx context.Context, cancel context.CancelFunc) *LoadTester {
	return &LoadTester{
		config: config,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		metrics: NewMetrics(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func NewMetrics() *Metrics {
	m := &Metrics{
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kyc_loadtest_requests_total",
				Help: "Total number of requests to KYC service",
			},
			[]string{"endpoint", "method", "status"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kyc_loadtest_request_duration_seconds",
				Help:    "Request duration in seconds",
				Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"endpoint", "method", "status"},
		),
		activeRequests: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "kyc_loadtest_active_requests",
				Help: "Number of active requests",
			},
		),
		errorRate: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "kyc_loadtest_error_rate",
				Help: "Current error rate",
			},
		),
		qps: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "kyc_loadtest_qps",
				Help: "Current QPS",
			},
		),
		successRate: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "kyc_loadtest_success_rate",
				Help: "Current success rate",
			},
		),
	}

	// 注册指标
	prometheus.MustRegister(m.requestsTotal)
	prometheus.MustRegister(m.requestDuration)
	prometheus.MustRegister(m.activeRequests)
	prometheus.MustRegister(m.errorRate)
	prometheus.MustRegister(m.qps)
	prometheus.MustRegister(m.successRate)

	return m
}

func (lt *LoadTester) setupAuth() error {
	switch lt.config.AuthType {
	case "jwt":
		if lt.config.JWTToken == "" {
			// 生成JWT令牌
			token, err := lt.generateJWTToken()
			if err != nil {
				return fmt.Errorf("JWT令牌生成失败: %v", err)
			}
			lt.config.JWTToken = token
			fmt.Printf("生成的JWT令牌: %s\n", token)
		}
	case "oauth":
		if lt.config.OAuthToken == "" {
			// 获取OAuth令牌
			token, err := lt.getOAuthToken()
			if err != nil {
				return fmt.Errorf("OAuth令牌获取失败: %v", err)
			}
			lt.config.OAuthToken = token
			fmt.Printf("获取的OAuth令牌: %s\n", token)
		}
	}
	return nil
}

func (lt *LoadTester) generateJWTToken() (string, error) {
	// 创建JWT声明
	claims := jwt.MapClaims{
		"key":    "yYBD4KdaY8Y7YaWOYJvlvgl2fNfI8gzG", // 发行者
		"secret": "T4mw1HP8Y5KRrxW9F4pkp6aNFvxQh7Ql",
		"sub":    "loadtest-client",                                 // 主题
		"aud":    "kyc-service",                                     // 受众
		"exp":    time.Now().Add(24 * time.Hour).Unix(),             // 过期时间
		"nbf":    time.Now().Unix(),                                 // 生效时间
		"iat":    time.Now().Unix(),                                 // 发行时间
		"jti":    fmt.Sprintf("loadtest-jwt-%d", time.Now().Unix()), // JWT ID
		"scope":  "kyc:read kyc:write",                              // 权限范围
	}

	// 创建JWT令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名令牌
	tokenString, err := token.SignedString([]byte(lt.config.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("JWT签名失败: %v", err)
	}

	return tokenString, nil
}

func (lt *LoadTester) getOAuthToken() (string, error) {
	// 构建请求体
	tokenRequest := map[string]string{
		"client_id":     lt.config.OAuthConfig.ClientID,
		"client_secret": lt.config.OAuthConfig.ClientSecret,
		"grant_type":    lt.config.OAuthConfig.GrantType,
		"scope":         lt.config.OAuthConfig.Scope,
	}

	jsonData, err := json.Marshal(tokenRequest)
	if err != nil {
		return "", fmt.Errorf("JSON编码失败: %v", err)
	}

	// 发送请求
	url := lt.config.TargetURL + "/api/v1/oauth/token"
	resp, err := lt.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("OAuth2请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OAuth2认证失败 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result SuccessResponse[struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token,omitempty"`
		Scope        string `json:"scope"`
	}]

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("OAuth2响应解析失败: %v", err)
	}

	return result.Data.AccessToken, nil
}

func (lt *LoadTester) startMetricsServer() {
	http.Handle("/metrics", promhttp.Handler())
	addr := fmt.Sprintf(":%d", lt.config.MetricsPort)
	fmt.Printf("启动指标服务: http://localhost%s/metrics\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("指标服务启动失败: %v\n", err)
	}
}

func (lt *LoadTester) Start() {
	// 启动QPS监控
	go lt.monitorQPS()

	// 启动多个工作协程
	for i := 0; i < lt.config.ConcurrentReqs; i++ {
		lt.wg.Add(1)
		go lt.worker(i)
	}
}

func (lt *LoadTester) Stop() {
	lt.cancel()
	lt.wg.Wait()
}

func (lt *LoadTester) worker(id int) {
	defer lt.wg.Done()

	// 计算每个worker的QPS配额
	workerQPS := lt.config.QPS / lt.config.ConcurrentReqs
	if workerQPS < 1 {
		workerQPS = 1
	}

	// 创建定时器，精确控制QPS
	interval := time.Second / time.Duration(workerQPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	fmt.Printf("Worker %d 启动: %d QPS, 间隔: %v\n", id, workerQPS, interval)

	for {
		select {
		case <-lt.ctx.Done():
			fmt.Printf("Worker %d 停止\n", id)
			return
		case <-ticker.C:
			lt.makeRequest()
		}
	}
}

func (lt *LoadTester) makeRequest() {
	// 随机选择端点
	endpoint := lt.selectEndpoint()

	// 构建请求
	req, err := lt.buildRequest(endpoint)
	if err != nil {
		fmt.Printf("构建请求失败: %v\n", err)
		return
	}

	// 记录活跃请求
	lt.metrics.activeRequests.Inc()
	defer lt.metrics.activeRequests.Dec()

	start := time.Now()

	// 发送请求

	resp, err := lt.httpClient.Do(req)
	if err != nil {
		lt.recordMetrics(endpoint, "0", time.Since(start))
		return
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("read body err: %v\n", err)
		return
	}
	fmt.Printf("----------------start-------------------- %s %s\n", req.Method, req.URL.String())
	for k, v := range req.Header {
		if strings.Contains(k, "Authorization") {
			continue
		}
		fmt.Printf("header %s: %s\n", k, strings.Join(v, ","))
	}
	if dbg := req.Context().Value("debugBody"); dbg != nil {
		if s, ok := dbg.(string); ok {
			fmt.Printf("req body: %s\n", s)
		}
	}
	fmt.Printf("resp body:%v, %s\n", time.Since(start), respBody)

	// 记录指标
	status := fmt.Sprintf("%d", resp.StatusCode)
	lt.recordMetrics(endpoint, status, time.Since(start))
}

func (lt *LoadTester) selectEndpoint() APIEndpoint {
	totalWeight := 0
	for _, ep := range endpoints {
		totalWeight += ep.Weight
	}

	random := rand.Intn(totalWeight)
	currentWeight := 0

	for _, ep := range endpoints {
		currentWeight += ep.Weight
		if random < currentWeight {
			return ep
		}
	}

	return endpoints[0]
}

func (lt *LoadTester) buildRequest(endpoint APIEndpoint) (*http.Request, error) {
	url := lt.config.TargetURL + endpoint.Path

	imagePath := "/Users/bytedance/Downloads/t1.jpg"
	videoPath := "/Users/bytedance/Downloads/test.mp4"

	var body io.Reader
	var contentType string

	var debug string
	switch endpoint.Path {
	case "/api/v1/oauth/token":
		payload := map[string]string{
			"client_id":     lt.config.OAuthConfig.ClientID,
			"client_secret": lt.config.OAuthConfig.ClientSecret,
			"grant_type":    lt.config.OAuthConfig.GrantType,
			"scope":         lt.config.OAuthConfig.Scope,
		}
		if rand.Float64() < lt.config.ErrorRate {
			payload["client_id"] = "invalid"
			payload["client_secret"] = "wrong"
			payload["grant_type"] = "invalid"
		}
		buf, _ := json.Marshal(payload)
		body = bytes.NewBuffer(buf)
		contentType = "application/json"
		debug = string(buf)

	case "/api/v1/oauth/refresh":
		payload := map[string]string{
			"refresh_token": "valid-refresh-token",
			"client_id":     lt.config.OAuthConfig.ClientID,
		}
		if rand.Float64() < lt.config.ErrorRate {
			payload["refresh_token"] = "invalid-token"
		}
		buf, _ := json.Marshal(payload)
		body = bytes.NewBuffer(buf)
		contentType = "application/json"
		debug = string(buf)

	case "/api/v1/kyc/ocr":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		if f, err := os.Open(imagePath); err == nil {
			defer f.Close()
			part, _ := w.CreateFormFile("picture", filepath.Base(imagePath))
			io.Copy(part, f)
		}
		w.WriteField("language", "zh-CN")
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		if fi, err := os.Stat(imagePath); err == nil {
			m := map[string]interface{}{"picture": map[string]interface{}{"filename": filepath.Base(imagePath), "size": fi.Size()}, "language": "zh-CN"}
			if d, err := json.Marshal(m); err == nil {
				debug = string(d)
			}
		}

	case "/api/v1/kyc/face/search":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		if f, err := os.Open(imagePath); err == nil {
			defer f.Close()
			part, _ := w.CreateFormFile("picture", filepath.Base(imagePath))
			io.Copy(part, f)
		}
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		if fi, err := os.Stat(imagePath); err == nil {
			m := map[string]interface{}{"picture": map[string]interface{}{"filename": filepath.Base(imagePath), "size": fi.Size()}}
			if d, err := json.Marshal(m); err == nil {
				debug = string(d)
			}
		}

	case "/api/v1/kyc/face/compare":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		if f1, err := os.Open(imagePath); err == nil {
			defer f1.Close()
			p1, _ := w.CreateFormFile("source_image", filepath.Base(imagePath))
			io.Copy(p1, f1)
		}
		if f2, err := os.Open(imagePath); err == nil {
			defer f2.Close()
			p2, _ := w.CreateFormFile("target_image", filepath.Base(imagePath))
			io.Copy(p2, f2)
		}
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		var s1, s2 int64
		if fi, err := os.Stat(imagePath); err == nil {
			s1 = fi.Size()
			s2 = s1
		}
		m := map[string]interface{}{"source_image": map[string]interface{}{"filename": filepath.Base(imagePath), "size": s1}, "target_image": map[string]interface{}{"filename": filepath.Base(imagePath), "size": s2}}
		if d, err := json.Marshal(m); err == nil {
			debug = string(d)
		}

	case "/api/v1/kyc/face/detect":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		if f, err := os.Open(imagePath); err == nil {
			defer f.Close()
			part, _ := w.CreateFormFile("picture", filepath.Base(imagePath))
			io.Copy(part, f)
		}
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		if fi, err := os.Stat(imagePath); err == nil {
			m := map[string]interface{}{"picture": map[string]interface{}{"filename": filepath.Base(imagePath), "size": fi.Size()}}
			if d, err := json.Marshal(m); err == nil {
				debug = string(d)
			}
		}

	case "/api/v1/kyc/liveness/silent":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		if f, err := os.Open(imagePath); err == nil {
			defer f.Close()
			part, _ := w.CreateFormFile("picture", filepath.Base(imagePath))
			io.Copy(part, f)
		}
		w.WriteField("language", "zh-CN")
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		if fi, err := os.Stat(imagePath); err == nil {
			m := map[string]interface{}{"picture": map[string]interface{}{"filename": filepath.Base(imagePath), "size": fi.Size()}, "language": "zh-CN"}
			if d, err := json.Marshal(m); err == nil {
				debug = string(d)
			}
		}

	case "/api/v1/kyc/liveness/video":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		fname := "test.mp4"
		fp := videoPath
		if _, err := os.Stat(videoPath); os.IsNotExist(err) {
			fp = imagePath
		}
		if f, err := os.Open(fp); err == nil {
			defer f.Close()
			part, _ := w.CreateFormFile("video", fname)
			io.Copy(part, f)
		}
		w.WriteField("language", "en-US")
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		if fi, err := os.Stat(fp); err == nil {
			m := map[string]interface{}{"video": map[string]interface{}{"filename": filepath.Base(fp), "size": fi.Size()}, "language": "en-US"}
			if d, err := json.Marshal(m); err == nil {
				debug = string(d)
			}
		}

	case "/api/v1/kyc/verify":
		b := &bytes.Buffer{}
		w := multipart.NewWriter(b)
		if f1, err := os.Open(imagePath); err == nil {
			defer f1.Close()
			p1, _ := w.CreateFormFile("idcard_image", filepath.Base(imagePath))
			io.Copy(p1, f1)
		}
		if f2, err := os.Open(imagePath); err == nil {
			defer f2.Close()
			p2, _ := w.CreateFormFile("face_image", filepath.Base(imagePath))
			io.Copy(p2, f2)
		}
		w.WriteField("name", "Test User")
		w.WriteField("idcard", "1234567890")
		w.WriteField("phone", "18800000000")
		w.Close()
		body = b
		contentType = w.FormDataContentType()
		var s1, s2 int64
		if fi, err := os.Stat(imagePath); err == nil {
			s1 = fi.Size()
			s2 = s1
		}
		m := map[string]interface{}{"idcard_image": map[string]interface{}{"filename": filepath.Base(imagePath), "size": s1}, "face_image": map[string]interface{}{"filename": filepath.Base(imagePath), "size": s2}, "name": "Test User", "idcard": "1234567890", "phone": "18800000000"}
		if d, err := json.Marshal(m); err == nil {
			debug = string(d)
		}

	case "/api/v1/kyc/status/test-id":
		body = nil
		debug = ""
	default:
		body = nil
		debug = ""
	}

	req, err := http.NewRequest(endpoint.Method, url, body)
	if err != nil {
		return nil, err
	}

	lt.addAuthHeaders(req)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if strings.Contains(endpoint.Path, "/api/v1/kyc/") {
		req.Header.Set("X-Quota-Bypass", "1")
	}
	req = req.WithContext(context.WithValue(req.Context(), "debugBody", debug))
	return req, nil
}

func getImages(imagePath string) io.Reader {
	// 检查图片文件是否存在
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		fmt.Printf("图片文件 '%s' 未找到", imagePath)
		return nil
	}

	// 获取文件信息
	fileInfo, err := os.Stat(imagePath)
	if err == nil {
		fmt.Printf("文件大小: %d bytes\n", fileInfo.Size())
		// 获取文件扩展名
		ext := ""
		if idx := strings.LastIndex(imagePath, "."); idx != -1 {
			ext = strings.ToLower(imagePath[idx:])
		}
		fmt.Printf("文件类型: %s\n", ext)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	filename := imagePath
	if strings.Contains(imagePath, "/") {
		// 简单的路径处理，获取文件名
		parts := strings.Split(imagePath, "/")
		filename = parts[len(parts)-1]
	}

	ext := ""
	if idx := strings.LastIndex(imagePath, "."); idx != -1 {
		ext = strings.ToLower(imagePath[idx:])
	}

	var contentType string
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".bmp":
		contentType = "image/bmp"
	case ".gif":
		contentType = "image/gif"
	default:
		contentType = "image/jpeg" // 默认使用jpeg
	}

	// 创建带正确MIME类型的表单字段
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="picture"; filename="%s"`, filename)},
		"Content-Type":        {contentType},
	})
	if err != nil {
		fmt.Printf("创建表单文件失败: %v", err)
		return nil
	}

	// 复制文件数据
	file, err := os.Open(imagePath)
	if err != nil {
		fmt.Printf("打开图片文件失败: %v", err)
		return nil
	}
	defer file.Close()

	_, err = io.Copy(part, file)
	if err != nil {
		fmt.Printf("复制文件数据失败: %v", err)
		return nil
	}

	// 添加其他表单字段
	formData := map[string]string{
		"token":   "",
		"user_id": "123",
	}

	for key, value := range formData {
		if err := writer.WriteField(key, value); err != nil {
			fmt.Printf("写入字段 %s 失败: %v", key, err)
			return nil
		}
	}

	if err := writer.Close(); err != nil {
		fmt.Printf("关闭表单写入器失败: %v", err)
		return nil
	}

	//req.Header.Set("Content-Type", writer.FormDataContentType())

	return body
}

func (lt *LoadTester) addAuthHeaders(req *http.Request) {
	if strings.Contains(req.URL.Path, "/api/v1/oauth/") {
		return
	}
	switch lt.config.AuthType {
	case "jwt":
		if lt.config.JWTToken != "" {
			req.Header.Set("Authorization", "Bearer "+lt.config.JWTToken)
		}
	case "oauth":
		if lt.config.OAuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+lt.config.OAuthToken)
		}
	}
}

func (lt *LoadTester) recordMetrics(endpoint APIEndpoint, status string, duration time.Duration) {
	endpointLabel := endpoint.Path
	methodLabel := endpoint.Method

	lt.metrics.requestsTotal.WithLabelValues(endpointLabel, methodLabel, status).Inc()
	lt.metrics.requestDuration.WithLabelValues(endpointLabel, methodLabel, status).Observe(duration.Seconds())
}

func (lt *LoadTester) monitorQPS() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var requestCount int64
	var errorCount int64
	var lastQPSUpdate time.Time

	for {
		select {
		case <-lt.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(lastQPSUpdate).Seconds()
			if elapsed > 0 {
				qps := float64(requestCount) / elapsed
				errorRate := 0.0
				successRate := 1.0
				if requestCount > 0 {
					errorRate = float64(errorCount) / float64(requestCount)
					successRate = 1.0 - errorRate
				}

				lt.metrics.qps.Set(qps)
				lt.metrics.errorRate.Set(errorRate)
				lt.metrics.successRate.Set(successRate)

				requestCount = 0
				errorCount = 0
				lastQPSUpdate = now
			}
		}
	}
}
