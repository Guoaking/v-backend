package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Action Liveness (MVP): only used to unblock Playground/QA Stage1.
// It returns a session_id and echoes trace info.

type ActionLivenessSessionResponse struct {
	SessionID string `json:"session_id"`
	// Trace/request id is put here for client debug convenience.
	RequestID string `json:"request_id,omitempty"`
}

// @Summary Action liveness create session (MVP)
// @Description Create an action-liveness session. (Placeholder implementation)
// @Tags KYC
// @Accept json
// @Produce json
// @Success 200 {object} ActionLivenessSessionResponse
// @Router /kyc/liveness/action/session [post]
// @Security ApiKeyAuth
func (h *KYCHandler) LivenessActionSession(c *gin.Context) {
	// NOTE: keep it simple: generate a UUID session id.
	// request/trace id: middleware.TraceMiddleware should set header "X-Request-ID".
	reqID := c.GetHeader("X-Request-ID")
	if reqID == "" {
		reqID = c.GetHeader("X-Trace-Id")
	}

	c.JSON(http.StatusOK, ActionLivenessSessionResponse{
		SessionID: uuid.New().String(),
		RequestID: reqID,
	})
}

// @Summary Action liveness upload (MVP)
// @Description Upload media for action-liveness session. (Placeholder implementation)
// @Tags KYC
// @Accept multipart/form-data
// @Produce json
// @Param session_id formData string true "Session ID"
// @Param video formData file false "Video"
// @Success 200 {object} map[string]any
// @Router /kyc/liveness/action/upload [post]
// @Security ApiKeyAuth
func (h *KYCHandler) LivenessActionUpload(c *gin.Context) {
	sid := c.PostForm("session_id")
	if sid == "" {
		JSONError(c, CodeMissingParameter, "Missing session_id")
		return
	}
	JSONSuccess(c, gin.H{"session_id": sid, "uploaded": true})
}

// @Summary Action liveness verify (MVP)
// @Description Verify action-liveness session. (Placeholder implementation)
// @Tags KYC
// @Accept json
// @Produce json
// @Success 200 {object} map[string]any
// @Router /kyc/liveness/action/verify [post]
// @Security ApiKeyAuth
func (h *KYCHandler) LivenessActionVerify(c *gin.Context) {
	var body struct {
		SessionID string `json:"session_id"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.SessionID == "" {
		JSONError(c, CodeMissingParameter, "Missing session_id")
		return
	}
	JSONSuccess(c, gin.H{"session_id": body.SessionID, "is_live": true, "score": 1.0})
}
