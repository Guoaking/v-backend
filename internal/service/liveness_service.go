package service

import (
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"time"

	"kyc-service/pkg/metrics"
	"kyc-service/pkg/tracing"

	"go.opentelemetry.io/otel/attribute"
)

type LivenessSilentResponse struct {
	Code            int    `json:"code"`
	Msg             string `json:"msg"`
	LivenessResults struct {
		IsLiveness          int     `json:"is_liveness"`
		Confidence          float64 `json:"confidence"`
		IsFaceExist         int     `json:"is_face_exist"`
		FaceExistConfidence float64 `json:"face_exist_confidence"`
	} `json:"liveness_results"`
	Filename string `json:"filename"`
}

type LivenessVideoResponse struct {
	Code            int    `json:"code"`
	Msg             string `json:"msg"`
	LivenessResults struct {
		IsLiveness          int     `json:"is_liveness"`
		Confidence          float64 `json:"confidence"`
		IsFaceExist         float64 `json:"is_face_exist"`
		FaceExistConfidence float64 `json:"face_exist_confidence"`
	} `json:"liveness_results"`
	Filename string `json:"filename"`
}

func (s *KYCService) LivenessSilent(ctx context.Context, file *multipart.FileHeader, language string) (*LivenessSilentResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.LivenessSilent")
	defer span.End()
	span.SetAttributes(attribute.String("service.name", "liveness-service"), attribute.String("liveness.op", "silent"), attribute.String("org.id", getOrgID(ctx)))
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("缺少组织信息")
	}
	if err := s.checkAndConsumeQuota(ctx, orgID, "liveness", func() error { return nil }); err != nil {
		if strings.Contains(err.Error(), "QUOTA_EXCEEDED") {
			return nil, fmt.Errorf("Quota exceeded. Please upgrade your plan.")
		}
		return nil, err
	}
	asset, err := s.IngestImage(ctx, orgID, file)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	tp := NewThirdPartyService(s.Config)
	out, err := tp.CallLivenessSilent(ctx, asset.FilePath, language)
	if err != nil {
		metrics.RecordBusinessOperation(ctx, "liveness_silent", false, time.Since(start), "third_party_error")
		return nil, err
	}
	if out.Code != 0 {
		metrics.RecordBusinessOperation(ctx, "liveness_silent", false, time.Since(start), "third_party_code")
		return nil, fmt.Errorf("静态活体失败: code=%d msg=%s", out.Code, out.Msg)
	}
	metrics.RecordBusinessOperation(ctx, "liveness_silent", true, time.Since(start), "")
	s.RecordAuditLog(ctx, "liveness.silent", "liveness", asset.ID, "success", "")
	return out, nil
}

func (s *KYCService) LivenessVideo(ctx context.Context, file *multipart.FileHeader, language string) (*LivenessVideoResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.LivenessVideo")
	defer span.End()
	span.SetAttributes(attribute.String("service.name", "liveness-service"), attribute.String("liveness.op", "video"), attribute.String("org.id", getOrgID(ctx)))
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("缺少组织信息")
	}
	if err := s.checkAndConsumeQuota(ctx, orgID, "liveness", func() error { return nil }); err != nil {
		if strings.Contains(err.Error(), "QUOTA_EXCEEDED") {
			return nil, fmt.Errorf("Quota exceeded. Please upgrade your plan.")
		}
		return nil, err
	}
	asset, err := s.IngestVideo(ctx, orgID, file)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	tp := NewThirdPartyService(s.Config)
	out, err := tp.CallLivenessVideo(ctx, asset.FilePath, language)
	if err != nil {
		metrics.RecordBusinessOperation(ctx, "liveness_video", false, time.Since(start), "third_party_error")
		return nil, err
	}
	if out.Code != 0 {
		metrics.RecordBusinessOperation(ctx, "liveness_video", false, time.Since(start), "third_party_code")
		return nil, fmt.Errorf("动态活体失败: code=%d msg=%s", out.Code, out.Msg)
	}
	metrics.RecordBusinessOperation(ctx, "liveness_video", true, time.Since(start), "")
	s.RecordAuditLog(ctx, "liveness.video", "liveness", asset.ID, "success", "")
	return out, nil
}
