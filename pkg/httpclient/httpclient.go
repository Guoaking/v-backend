package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

// HTTPClient defines the interface for a generic HTTP client.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	//SetCount (count int)
}

// Config holds the configuration for the custom HTTP client.
type Config struct {
	Timeout       time.Duration
	RetryCount    int
	RetryInterval time.Duration
	Logger        *logrus.Logger
}

// Client is a custom HTTP client with added functionalities.
type Client struct {
	client HTTPClient
	config Config
	cache  *cache.Cache
	logger *logrus.Logger
}

// HTTPStatusError is returned when the response status code indicates an upstream error
type HTTPStatusError struct {
	StatusCode int
	Body       []byte
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("http status %d, msg: %s", e.StatusCode, e.Body)
}

// New creates a new instance of the custom HTTP client.
func New(config Config) *Client {
	return &Client{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		config: config,
		cache:  cache.New(5*time.Minute, 10*time.Minute),
		logger: config.Logger,
	}
}

func (c *Client) SetConfig(count, timeout int) {
	if count != 0 {
		c.config.RetryCount = count
	}

	if timeout != 0 {
		duration := time.Duration(timeout) * time.Second
		c.client = &http.Client{
			Timeout: duration,
		}
		c.config.Timeout = duration
	}
}

// Post sends a POST request to the specified URL.
func (c *Client) Post(ctx context.Context, url string, body interface{}, headers http.Header) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		c.logger.WithField("error", err).Error("Failed to marshal request body")
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		c.logger.WithField("error", err).Error("Failed to create request")
		return nil, err
	}

	req.Header = headers
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	for i := 0; i < c.config.RetryCount+1; i++ {
		resp, err = c.client.Do(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			break
		}
		c.logger.WithFields(logrus.Fields{
			"url":     url,
			"attempt": i + 1,
			"error":   err,
		}).Warn("Request failed, retrying...")
		if i < c.config.RetryCount {
			time.Sleep(c.config.RetryInterval)
		}
	}

	if err != nil {
		c.logger.WithField("error", err).Error("Request failed after retries")
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.logger.WithField("error", err).Error("Failed to read response body")
		return nil, err
	}

	return respBody, nil
}

// PostMultipart sends a multipart/form-data request to the specified URL.
func (c *Client) PostMultipart(ctx context.Context, url string, data map[string]string, file io.Reader, fileName string) ([]byte, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加文件
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "picture", fileName))

	header.Set("Content-Type", "image/jpeg")
	//h := map[string][]string{
	//	"Content-Disposition": {fmt.Sprintf(`form-data; name="picture"; filename="%s"`, fileName)},
	//	"Content-Type":        {"image/jpeg"},
	//}

	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}

	//part.Header.Set("Content-Type", "image/jpeg")
	_, err = io.Copy(part, file)
	if err != nil {
		return nil, err
	}

	// 添加其他表单字段
	for key, val := range data {
		if err := writer.WriteField(key, val); err != nil {
			return nil, fmt.Errorf("写入字段 %s 失败: %v", key, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	var resp *http.Response

	now4 := time.Now()
	for i := 0; i < c.config.RetryCount+1; i++ {
		now5 := time.Now()
		resp, err = c.client.Do(req)

		fmt.Printf("output: %v, %v, %v\n", "end5", time.Since(now5), c.config.RetryInterval)

		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			break
		}
		c.logger.WithFields(logrus.Fields{"url": url, "attempt": i + 1, "error": err}).Warn("Request failed, retrying...")
		if i < c.config.RetryCount {
			time.Sleep(c.config.RetryInterval)
		}
	}
	fmt.Printf("output: %v, %v\n", "end4", time.Since(now4))

	if err != nil {
		c.logger.WithField("error", err).Error("Request failed after retries")
		return nil, err
	}

	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.logger.WithField("error", err).Error("Failed to read response body")
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: respBody}
	}
	return respBody, nil
}

// PostMultipartNamed sends multipart/form-data with a custom file field name and extra fields.
func (c *Client) PostMultipartNamed(ctx context.Context, url string, fileField string, file io.Reader, fileName string, fields map[string]string) ([]byte, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fileField, fileName))
	hdr.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(hdr)
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(part, file); err != nil {
		return nil, err
	}

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("写入字段 %s 失败: %v", k, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	var resp *http.Response
	for i := 0; i < c.config.RetryCount+1; i++ {
		resp, err = c.client.Do(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			break
		}
		c.logger.WithFields(logrus.Fields{"url": url, "attempt": i + 1, "error": err}).Warn("Request failed, retrying...")
		if i < c.config.RetryCount {
			time.Sleep(c.config.RetryInterval)
		}
	}
	if err != nil {
		c.logger.WithField("error", err).Error("Request failed after retries")
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.logger.WithField("error", err).Error("Failed to read response body")
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: respBody}
	}
	return respBody, nil
}

// PostMultipartMulti sends multipart/form-data with two file parts and extra fields.
func (c *Client) PostMultipartMulti(ctx context.Context, url string, files map[string]struct {
	Reader io.Reader
	Name   string
}, fields map[string]string) ([]byte, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for field, f := range files {
		hdr := make(textproto.MIMEHeader)
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, f.Name))
		hdr.Set("Content-Type", "image/jpeg")
		part, err := writer.CreatePart(hdr)
		if err != nil {
			return nil, err
		}
		if _, err = io.Copy(part, f.Reader); err != nil {
			return nil, err
		}
	}
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("写入字段 %s 失败: %v", k, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭表单写入器失败: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	var resp *http.Response
	for i := 0; i < c.config.RetryCount+1; i++ {
		resp, err = c.client.Do(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			break
		}
		c.logger.WithFields(logrus.Fields{"url": url, "attempt": i + 1, "error": err}).Warn("Request failed, retrying...")
		if i < c.config.RetryCount {
			time.Sleep(c.config.RetryInterval)
		}
	}
	if err != nil {
		c.logger.WithField("error", err).Error("Request failed after retries")
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.logger.WithField("error", err).Error("Failed to read response body")
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: respBody}
	}
	return respBody, nil
}

// ExtractTopLevelCode parses a JSON response and extracts the top-level "code" field.
// Returns the code as string and a boolean indicating success (code==0).
func ExtractTopLevelCode(body []byte) (string, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", false
	}
	v, ok := m["code"]
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case float64:
		return fmt.Sprintf("%d", int(t)), int(t) == 0
	case int:
		return fmt.Sprintf("%d", t), t == 0
	case string:
		return t, t == "0"
	default:
		return "", false
	}
}
