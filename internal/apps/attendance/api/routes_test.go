package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"kyc-service/internal/apps/attendance/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleRequestOTPDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/attendance/self/otp", handleRequestOTP(nil))

	req := httptest.NewRequest(http.MethodPost, "/attendance/self/otp", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "employee self-service is disabled")
}

func TestHandleGetSelfRecordsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/attendance/self/records", handleGetSelfRecords(nil))

	req := httptest.NewRequest(http.MethodGet, "/attendance/self/records?employee_no=00001234", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "employee self-service records are disabled")
}

func TestHandleConsoleReviewRecordRequiresValidAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/console/attendance/records/:id/review", func(c *gin.Context) {
		c.Set("orgID", "org-1")
		c.Set("userID", "user-1")
		handleConsoleReviewRecord(&service.AttendanceService{})(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/console/attendance/records/review-1/review", bytes.NewBufferString(`{"action":"invalid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), service.ErrInvalidReviewAction.Error())
}
