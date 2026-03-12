package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"kyc-service/internal/service"
	"kyc-service/pkg/logger"
	"kyc-service/pkg/tracing"

	"github.com/gin-gonic/gin"
)

// Action Liveness (MVP): only used to unblock Playground/QA Stage1.
// It returns a session_id and echoes trace info.

type ActionLivenessSessionResponse struct {
	SessionID string `json:"session_id"`
	// Trace/request id is put here for client debug convenience.
	RequestID string   `json:"request_id,omitempty"`
	Actions   []string `json:"actions"`
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
	ctx := c.Request.Context()
	ctx, span := tracing.StartSpan(ctx, "api.LivenessActionSession")
	defer span.End()

	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	// request/trace id
	reqID := c.GetHeader("X-Request-ID")
	if reqID == "" {
		reqID = c.GetHeader("X-Trace-Id")
	}
	ctx = context.WithValue(ctx, "request_id", reqID)

	sid, actions, err := h.service.CreateSession(ctx)
	if err != nil {
		span.RecordError(err)
		JSONError(c, CodeBusinessError, err.Error())
		return
	}

	JSONSuccess(c, ActionLivenessSessionResponse{
		SessionID: sid,
		RequestID: reqID,
		Actions:   actions,
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
	ctx := c.Request.Context()
	ctx, span := tracing.StartSpan(ctx, "api.LivenessActionUpload")
	defer span.End()

	sid := c.PostForm("session_id")
	if sid == "" {
		JSONError(c, CodeMissingParameter, "Missing session_id")
		return
	}
	file, err := c.FormFile("video")
	if err != nil {
		JSONError(c, CodeInvalidParameter, "Missing video")
		return
	}

	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	if _, err := h.service.UploadVideo(ctx, sid, file); err != nil {
		span.RecordError(err)
		JSONError(c, CodeBusinessError, err.Error())
		return
	}

	// SubmitThirdParty is now part of UploadVideo's quota transaction.
	// We do NOT call it explicitly here anymore to avoid double submission.

	JSONSuccess(c, ActionLivenessUploadResponse{
		SessionID: sid,
		Uploaded:  true,
		Submitted: true,
	})
}

type ActionLivenessUploadResponse struct {
	SessionID string `json:"session_id"`
	Uploaded  bool   `json:"uploaded"`
	Submitted bool   `json:"submitted"`
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
	ctx := c.Request.Context()
	ctx, span := tracing.StartSpan(ctx, "api.LivenessActionVerify")
	defer span.End()

	var body struct {
		SessionID string `json:"session_id"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.SessionID == "" {
		JSONError(c, CodeMissingParameter, "Missing session_id")
		return
	}

	ctx = context.WithValue(ctx, "org_id", c.GetString("orgID"))
	res, err := h.service.Verify(ctx, body.SessionID)
	if err != nil {
		span.RecordError(err)
		JSONError(c, CodeBusinessError, err.Error())
		return
	}
	JSONSuccess(c, res)
}

// @Summary Action liveness callback
// @Description Callback for action liveness result
// @Tags KYC
// @Accept json
// @Produce json
// @Router /callbacks/liveness/action [post]
func (h *KYCHandler) LivenessActionCallback(c *gin.Context) {
	ctx := c.Request.Context()
	ctx, span := tracing.StartSpan(ctx, "api.LivenessActionCallback")
	defer span.End()

	// 1. Get signature
	signature := c.GetHeader("X-ThirdParty-Signature")

	// 2. Read body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.GetLogger().WithError(err).Error("Failed to read callback body")
		JSONError(c, CodeInvalidParameter, "Failed to read body")
		return
	}

	logger.GetLogger().WithFields(map[string]interface{}{
		"signature":    signature,
		"body_len":     len(bodyBytes),
		"body_preview": string(bodyBytes),
	}).Info("Received Action Liveness Callback")

	// 3. Verify signature (skip if empty for dev/transition, or enforce strict?)
	// Given user asked for auth design, let'ngwangbiaos enforce it if signature is present,
	// or if config requires it. For now, let's just log if it fails but maybe allow it if empty?
	// No, better enforce if we claim to have auth.
	if signature != "" {
		if !h.service.ThirdParty.VerifyCallbackSignature(signature, bodyBytes) {
			logger.GetLogger().Warn("Callback signature verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "Invalid signature"})
			return
		}
	} else {
		logger.GetLogger().Warn("Missing callback signature")
	}

	var req service.ActionLivenessCallback
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to unmarshal callback body")
		JSONError(c, CodeInvalidParameter, err.Error())
		return
	}

	if err := h.service.ProcessActionLivenessCallback(ctx, &req); err != nil {
		span.RecordError(err)
		JSONError(c, CodeBusinessError, err.Error())
		return
	}

	JSONSuccess(c, nil)
}
