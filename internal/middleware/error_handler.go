package middleware

import (
	"runtime/debug"
	"time"

	"kyc-service/pkg/logger"

	"github.com/gin-gonic/gin"
)

// ErrorHandler 统一错误处理中间件
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 继续处理请求
		c.Next()

		// 检查是否有错误需要处理
		if len(c.Errors) > 0 {
			// 获取最后一个错误
			err := c.Errors.Last()

			logger.GetLogger().WithFields(map[string]interface{}{
				"error":      err.Error(),
				"status":     c.Writer.Status(),
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
				"request_id": c.GetString("request_id"),
			}).Error("请求处理失败")

			// 如果还没有响应，返回统一的错误响应
			if !c.Writer.Written() {
				// 使用通用的JSON响应，避免循环导入
				c.JSON(-1, gin.H{
					"code":       getErrorCode(c.Writer.Status()),
					"message":    getErrorMessage(c.Writer.Status()),
					"error":      err.Error(),
					"timestamp":  time.Now().UnixMilli(),
					"request_id": c.GetString("request_id"),
					"path":       c.Request.URL.Path,
					"method":     c.Request.Method,
				})
			}
		}
	}
}

// Recovery 恢复中间件，处理panic
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				stack := string(debug.Stack())
				logger.GetLogger().WithFields(map[string]interface{}{
					"error":      err,
					"path":       c.Request.URL.Path,
					"method":     c.Request.Method,
					"request_id": c.GetString("request_id"),
					"stack":      stack,
				}).Error("系统发生panic")

				// 返回统一的错误响应
				c.JSON(500, gin.H{
					"code":       5000,
					"message":    "系统内部错误",
					"error":      "系统内部错误，请联系管理员",
					"timestamp":  time.Now().UnixMilli(),
					"request_id": c.GetString("request_id"),
					"path":       c.Request.URL.Path,
					"method":     c.Request.Method,
				})
				c.Abort()
			}
		}()

		c.Next()
	}
}

// getErrorCode 根据HTTP状态码获取错误代码
func getErrorCode(status int) int {
	switch status {
	case 400:
		return 1000
	case 401:
		return 1001
	case 403:
		return 1002
	case 404:
		return 1003
	case 429:
		return 1005
	case 500:
		return 5000
	case 503:
		return 5003
	default:
		return 5000
	}
}

// getErrorMessage 根据HTTP状态码获取错误消息
func getErrorMessage(status int) string {
	switch status {
	case 400:
		return "请求参数错误"
	case 401:
		return "未授权访问"
	case 403:
		return "权限不足"
	case 404:
		return "资源不存在"
	case 429:
		return "请求过于频繁"
	case 500:
		return "服务器内部错误"
	case 503:
		return "服务暂时不可用"
	default:
		return "未知错误"
	}
}
