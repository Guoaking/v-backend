package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ResponseCode 响应代码定义
type ResponseCode int

const (
	CodeSuccess ResponseCode = 200

	// 客户端错误 1xxx
	CodeBadRequest       ResponseCode = 1000 // invalid request parameters
	CodeUnauthorized     ResponseCode = 1001 // unauthorized
	CodeForbidden        ResponseCode = 1002 // forbidden
	CodeNotFound         ResponseCode = 1003 // resource not found
	CodeMethodNotAllowed ResponseCode = 1004 // method not allowed
	CodeTooManyRequests  ResponseCode = 1005 // too many requests
	CodeInvalidParameter ResponseCode = 1006 // invalid parameter
	CodeMissingParameter ResponseCode = 1007 // missing parameter

	// 业务错误 2xxx
	CodeBusinessError    ResponseCode = 2000 // generic business error
	CodeOCRFailed        ResponseCode = 2001 // ocr recognition failed
	CodeFaceVerifyFailed ResponseCode = 2002 // face verification failed
	CodeLivenessFailed   ResponseCode = 2003 // liveness detection failed
	CodeKYCFailed        ResponseCode = 2004 // kyc verification failed
	CodeIDCardNotMatch   ResponseCode = 2005 // id card information not matched
	CodeFaceNotMatch     ResponseCode = 2006 // face not matched
	CodeConflict         ResponseCode = 2007 // resource conflict

	// 支付相关 4xxx
	CodePaymentRequired ResponseCode = 40201 // quota exceeded, please upgrade your plan

	// 服务器错误 5xxx
	CodeInternalError      ResponseCode = 5000 // internal server error
	CodeDatabaseError      ResponseCode = 5001 // database error
	CodeThirdPartyError    ResponseCode = 5002 // third party service error
	CodeServiceUnavailable ResponseCode = 5003 // service unavailable
	CodeEncryptionError    ResponseCode = 5004 // encryption error
)

// ResponseMessage 响应消息映射
var ResponseMessage = map[ResponseCode]string{
	CodeSuccess:            "success",
	CodeBadRequest:         "Invalid request parameters",
	CodeUnauthorized:       "Unauthorized",
	CodeForbidden:          "Forbidden",
	CodeNotFound:           "Resource not found",
	CodeMethodNotAllowed:   "Method not allowed",
	CodeTooManyRequests:    "Too many requests",
	CodeInvalidParameter:   "Invalid parameter",
	CodeMissingParameter:   "Missing required parameter",
	CodeBusinessError:      "Business operation failed",
	CodeOCRFailed:          "OCR recognition failed",
	CodeFaceVerifyFailed:   "Face verification failed",
	CodeLivenessFailed:     "Liveness detection failed",
	CodeKYCFailed:          "KYC verification failed",
	CodeIDCardNotMatch:     "ID card information not matched",
	CodeFaceNotMatch:       "Face not matched",
	CodeConflict:           "Resource conflict",
	CodePaymentRequired:    "Quota exceeded, please upgrade your plan",
	CodeInternalError:      "Internal server error",
	CodeDatabaseError:      "Database operation failed",
	CodeThirdPartyError:    "Third party service error",
	CodeServiceUnavailable: "Service temporarily unavailable",
	CodeEncryptionError:    "Data encryption failed",
}

// BaseResponse base response envelope
type BaseResponse struct {
	Code      ResponseCode `json:"code"`       // response code
	Message   string       `json:"message"`    // response message
	Timestamp int64        `json:"timestamp"`  // timestamp (ms)
	RequestID string       `json:"request_id"` // request id
	//Path      string       `json:"path,omitempty"`   // request path
	//Method    string       `json:"method,omitempty"` // request method
}

// SuccessResponse success response with data payload
type SuccessResponse struct {
	BaseResponse
	Data interface{} `json:"data,omitempty"` // response data
}

// ErrorResponse error response with details
type ErrorResponse struct {
	BaseResponse
	Error  string       `json:"error,omitempty"`  // error detail
	Errors []FieldError `json:"errors,omitempty"` // field errors
}

// FieldError field error information
type FieldError struct {
	Field   string `json:"field"`           // field name
	Message string `json:"message"`         // error message
	Value   string `json:"value,omitempty"` // field value
}

// Pagination pagination info
type Pagination struct {
	Page      int `json:"page"`       // current page number
	PageSize  int `json:"page_size"`  // page size
	Total     int `json:"total"`      // total records
	TotalPage int `json:"total_page"` // total pages
}

// PaginatedResponse paginated response with list and pagination info
type PaginatedResponse struct {
	BaseResponse
	Data       interface{} `json:"data"`       // data list
	Pagination Pagination  `json:"pagination"` // pagination info
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(data interface{}) *SuccessResponse {
	return &SuccessResponse{
		BaseResponse: BaseResponse{
			Code:    CodeSuccess,
			Message: ResponseMessage[CodeSuccess],
		},
		Data: data,
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(code ResponseCode, err string) *ErrorResponse {
	return &ErrorResponse{
		BaseResponse: BaseResponse{
			Code:    code,
			Message: ResponseMessage[code],
		},
		Error: err,
	}
}

// NewErrorResponseWithFields 创建带字段错误的错误响应
func NewErrorResponseWithFields(code ResponseCode, err string, fieldErrors []FieldError) *ErrorResponse {
	return &ErrorResponse{
		BaseResponse: BaseResponse{
			Code:    code,
			Message: ResponseMessage[code],
		},
		Error:  err,
		Errors: fieldErrors,
	}
}

// NewPaginatedResponse 创建分页响应
func NewPaginatedResponse(data interface{}, page, pageSize, total int) *PaginatedResponse {
	totalPage := (total + pageSize - 1) / pageSize
	return &PaginatedResponse{
		BaseResponse: BaseResponse{
			Code:    CodeSuccess,
			Message: ResponseMessage[CodeSuccess],
		},
		Data: data,
		Pagination: Pagination{
			Page:      page,
			PageSize:  pageSize,
			Total:     total,
			TotalPage: totalPage,
		},
	}
}

// SetRequestInfo 设置请求信息
func (r *BaseResponse) SetRequestInfo(c *gin.Context) {
	r.Timestamp = time.Now().UnixMilli()
	r.RequestID = c.GetString("request_id")
	//r.Path = c.Request.URL.Path
	//r.Method = c.Request.Method
}

// JSONSuccess 返回成功JSON响应
func JSONSuccess(c *gin.Context, data interface{}) {
	resp := NewSuccessResponse(data)
	resp.SetRequestInfo(c)
	c.JSON(http.StatusOK, resp)
}

// JSONError 返回错误JSON响应
func JSONError(c *gin.Context, code ResponseCode, err string) {
	resp := NewErrorResponse(code, err)
	resp.SetRequestInfo(c)

	// 根据错误代码设置HTTP状态码
	statusCode := getHTTPStatusCode(code)
	c.JSON(statusCode, resp)
}

// JSONErrorWithFields 返回带字段错误的JSON响应
func JSONErrorWithFields(c *gin.Context, code ResponseCode, err string, fieldErrors []FieldError) {
	resp := NewErrorResponseWithFields(code, err, fieldErrors)
	resp.SetRequestInfo(c)

	statusCode := getHTTPStatusCode(code)
	c.JSON(statusCode, resp)
}

func JSONErrorWithStatus(c *gin.Context, code ResponseCode, err string, status int) {
	resp := NewErrorResponse(code, err)
	resp.SetRequestInfo(c)
	c.JSON(status, resp)
}

// JSONPaginated 返回分页JSON响应
func JSONPaginated(c *gin.Context, data interface{}, page, pageSize, total int) {
	resp := NewPaginatedResponse(data, page, pageSize, total)
	resp.SetRequestInfo(c)
	c.JSON(http.StatusOK, resp)
}

// getHTTPStatusCode 根据响应代码获取HTTP状态码
func getHTTPStatusCode(code ResponseCode) int {
	switch {
	case code == CodePaymentRequired:
		return http.StatusPaymentRequired
	case code == CodeSuccess:
		return http.StatusOK
	case code >= 1000 && code < 2000:
		return http.StatusBadRequest
	case code >= 2000 && code < 3000:
		return http.StatusBadRequest
	case code >= 5000:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
