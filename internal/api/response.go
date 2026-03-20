package api

import (
	"kyc-service/pkg/response"

	"github.com/gin-gonic/gin"
)

// Alias types from pkg/response for backward compatibility
type ResponseCode = response.ResponseCode

const (
	CodeSuccess ResponseCode = response.CodeSuccess

	// 客户端错误 1xxx
	CodeBadRequest       ResponseCode = response.CodeBadRequest
	CodeUnauthorized     ResponseCode = response.CodeUnauthorized
	CodeForbidden        ResponseCode = response.CodeForbidden
	CodeNotFound         ResponseCode = response.CodeNotFound
	CodeMethodNotAllowed ResponseCode = response.CodeMethodNotAllowed
	CodeTooManyRequests  ResponseCode = response.CodeTooManyRequests
	CodeInvalidParameter ResponseCode = response.CodeInvalidParameter
	CodeMissingParameter ResponseCode = response.CodeMissingParameter

	// 业务错误 2xxx
	CodeBusinessError    ResponseCode = response.CodeBusinessError
	CodeOCRFailed        ResponseCode = response.CodeOCRFailed
	CodeFaceVerifyFailed ResponseCode = response.CodeFaceVerifyFailed
	CodeLivenessFailed   ResponseCode = response.CodeLivenessFailed
	CodeKYCFailed        ResponseCode = response.CodeKYCFailed
	CodeIDCardNotMatch   ResponseCode = response.CodeIDCardNotMatch
	CodeFaceNotMatch     ResponseCode = response.CodeFaceNotMatch
	CodeConflict         ResponseCode = response.CodeConflict

	// 支付相关 4xxx
	CodePaymentRequired ResponseCode = response.CodePaymentRequired

	// 服务器错误 5xxx
	CodeInternalError      ResponseCode = response.CodeInternalError
	CodeDatabaseError      ResponseCode = response.CodeDatabaseError
	CodeThirdPartyError    ResponseCode = response.CodeThirdPartyError
	CodeServiceUnavailable ResponseCode = response.CodeServiceUnavailable
	CodeEncryptionError    ResponseCode = response.CodeEncryptionError
)

// BaseResponse base response envelope
type BaseResponse = response.BaseResponse

// SuccessResponse success response with data payload
type SuccessResponse = response.SuccessResponse

// ErrorResponse error response with details
type ErrorResponse = response.ErrorResponse

// FieldError field error information
type FieldError = response.FieldError

// Pagination pagination info
type Pagination = response.Pagination

// PaginatedResponse paginated response with list and pagination info
type PaginatedResponse = response.PaginatedResponse

// Helper functions that wrap pkg/response helpers
// We re-implement these to use the aliased constants and types if needed,
// or simply delegate to pkg/response if signatures match perfectly.
// Since we want to maintain the 'api' package interface, we'll delegate.

func JSONSuccess(c *gin.Context, data interface{}) {
	response.JSONSuccess(c, data)
}

func JSONError(c *gin.Context, code ResponseCode, err string) {
	response.JSONError(c, code, err)
}

func JSONErrorWithFields(c *gin.Context, code ResponseCode, err string, fieldErrors []FieldError) {
	response.JSONErrorWithFields(c, code, err, fieldErrors)
}

func JSONErrorWithStatus(c *gin.Context, code ResponseCode, err string, status int) {
	response.JSONErrorWithStatus(c, code, err, status)
}

func JSONSuccessWithStatus(c *gin.Context, status int, data interface{}) {
	response.JSONSuccessWithStatus(c, status, data)
}

func JSONPaginated(c *gin.Context, data interface{}, page, pageSize, total int) {
	response.JSONPaginated(c, data, page, pageSize, total)
}
