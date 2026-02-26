package service

import (
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"time"

	"kyc-service/internal/models"
	"kyc-service/pkg/metrics"
	"kyc-service/pkg/tracing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

type FaceVerifyRequest struct {
	Image1 *multipart.FileHeader `form:"image1" binding:"required"`
	Image2 *multipart.FileHeader `form:"image2" binding:"required"`
	ID     string                `form:"id"`
}

type FaceVerifyResponse struct {
	Success   bool    `json:"success"`
	Score     float64 `json:"score,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	Error     string  `json:"error,omitempty"`
}

type StandardFaceVerifyResponse struct {
	Success        bool    `json:"success"`
	Score          float64 `json:"score"`
	Threshold      float64 `json:"threshold"`
	IsMatch        bool    `json:"is_match"`
	Confidence     float64 `json:"confidence"`
	ProcessingTime int64   `json:"processing_time_ms"`
}

type FaceSearchResponse struct {
	Code             int    `json:"code"`
	Msg              string `json:"msg"`
	SearchingResults struct {
		SearchedSimilarPictures []struct {
			ID         string  `json:"id"`
			Confidence float64 `json:"confidence"`
			Picture    string  `json:"picture,omitempty"`
		} `json:"searched_similar_pictures"`
		HasSimilarPicture int `json:"has_similar_picture"`
	} `json:"searching_results"`
	Filename string `json:"filename"`
}

func (s *KYCService) FaceSearch(ctx context.Context, file *multipart.FileHeader) (*FaceSearchResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.FaceSearch")
	defer span.End()
	start := time.Now()

	kycRequest := &models.KYCRequest{
		ID:          uuid.New().String(),
		UserID:      getUserID(ctx),
		RequestType: "face_search",
		Status:      "processing",
		IPAddress:   getClientIP(ctx),
		UserAgent:   getUserAgent(ctx),
	}
	if err := s.DB.Create(kycRequest).Error; err != nil {
		return nil, fmt.Errorf("create request failed")
	}

	span.SetAttributes(attribute.String("service.name", "face-service"), attribute.String("face.op", "search"), attribute.String("org.id", getOrgID(ctx)))

	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("missing organization context")
	}

	if err := s.checkAndConsumeQuota(ctx, orgID, "face", func() error { return nil }); err != nil {
		if strings.Contains(err.Error(), "QUOTA_EXCEEDED") {
			if s.faceVerifySuccessRate != nil {
				s.faceVerifySuccessRate.Record(ctx, 0.0)
			}
			metrics.RecordBusinessOperation(ctx, "face_search", false, time.Since(start), "quota_exceeded")
			return nil, fmt.Errorf("Quota exceeded. Please upgrade your plan.")
		}
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_search", false, time.Since(start), "quota_error")
		return nil, err
	}

	f, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open image failed: %w", err)
	}
	defer f.Close()

	tp := NewThirdPartyService(s.Config)
	out, err := tp.CallFaceSearch(ctx, f, file.Filename)
	if err != nil {
		kycRequest.Status = "failed"
		kycRequest.ErrorMessage = err.Error()
		s.DB.Save(kycRequest)
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_search", false, time.Since(start), "third_party_error")
		return nil, err
	}

	if out.Code != 0 {
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_search", false, time.Since(start), "third_party_code")
		return nil, fmt.Errorf("FACE_VERIFY_FAILED")
	}

	// Persist image refs and replace IDs with internal IDs for nginx forwarding
	for i := range out.SearchingResults.SearchedSimilarPictures {
		pic := out.SearchingResults.SearchedSimilarPictures[i].Picture
		if pic == "" {
			continue
		}
		id := uuid.New().String()
		_ = s.DB.Create(&models.FaceImageRef{ID: id, OrganizationID: orgID, FilePath: pic, SafeFilename: pic, CreatedAt: time.Now()}).Error
		out.SearchingResults.SearchedSimilarPictures[i].ID = id
	}

	if s.faceVerifySuccessRate != nil {
		s.faceVerifySuccessRate.Record(ctx, 1.0)
	}
	metrics.RecordBusinessOperation(ctx, "face_search", true, time.Since(start), "")
	if s.kycProcessingTime != nil {
		s.kycProcessingTime.Record(ctx, time.Since(start).Seconds())
	}

	s.RecordAuditLog(ctx, "face.search", "face", "", "success", "")
	return out, nil
}

type FaceCompareResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	ComparisonResults struct {
		IsFaceExist     int       `json:"is_face_exist"`
		ConfidenceExist []float64 `json:"confidence_exist"`
		IsSameFace      int       `json:"is_same_face"`
		Confidence      float64   `json:"confidence"`
		DetectionResult string    `json:"detection_result"`
	} `json:"comparison_results"`
	Filename []string `json:"filename"`
}

func (s *KYCService) FaceCompare(ctx context.Context, src, dst *multipart.FileHeader) (*FaceCompareResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.FaceCompare")
	defer span.End()
	span.SetAttributes(attribute.String("service.name", "face-service"), attribute.String("face.op", "compare"), attribute.String("org.id", getOrgID(ctx)))
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("missing organization context")
	}
	start := time.Now()
	if err := s.checkAndConsumeQuota(ctx, orgID, "face", func() error { return nil }); err != nil {
		if strings.Contains(err.Error(), "QUOTA_EXCEEDED") {
			if s.faceVerifySuccessRate != nil {
				s.faceVerifySuccessRate.Record(ctx, 0.0)
			}
			metrics.RecordBusinessOperation(ctx, "face_compare", false, time.Since(start), "quota_exceeded")
			return nil, fmt.Errorf("Quota exceeded. Please upgrade your plan.")
		}
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_compare", false, time.Since(start), "quota_error")
		return nil, err
	}
	f1, err := src.Open()
	if err != nil {
		return nil, fmt.Errorf("open image1 failed: %w", err)
	}
	defer f1.Close()
	f2, err := dst.Open()
	if err != nil {
		return nil, fmt.Errorf("open image2 failed: %w", err)
	}
	defer f2.Close()

	tp := NewThirdPartyService(s.Config)
	out, err := tp.CallFaceCompare(ctx, f1, src.Filename, f2, dst.Filename)
	if err != nil {
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_compare", false, time.Since(start), "third_party_error")
		return nil, err
	}
	s.RecordAuditLog(ctx, "face.compare", "face", "", "success", "")
	if s.faceVerifySuccessRate != nil {
		s.faceVerifySuccessRate.Record(ctx, 1.0)
	}
	metrics.RecordBusinessOperation(ctx, "face_compare", true, time.Since(start), "")
	return out, nil
}

type FaceDetectResponse struct {
	Code             int    `json:"code"`
	Msg              string `json:"msg"`
	DetectionResults struct {
		IsFaceExist   int `json:"is_face_exist"`
		FaceNum       int `json:"face_num"`
		FacesDetected []struct {
			FacialArea struct {
				X        int   `json:"x"`
				Y        int   `json:"y"`
				W        int   `json:"w"`
				H        int   `json:"h"`
				LeftEye  []int `json:"left_eye"`
				RightEye []int `json:"right_eye"`
			} `json:"facial_area"`
			Confidence float64 `json:"confidence"`
		} `json:"faces_detected"`
	} `json:"detection_results"`
	Filename string `json:"filename"`
}

func (s *KYCService) FaceDetect(ctx context.Context, file *multipart.FileHeader) (*FaceDetectResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "KYCService.FaceDetect")
	defer span.End()
	span.SetAttributes(attribute.String("service.name", "face-service"), attribute.String("face.op", "detect"), attribute.String("org.id", getOrgID(ctx)))
	orgID := getOrgID(ctx)
	if orgID == "" {
		return nil, fmt.Errorf("missing organization context")
	}
	start := time.Now()
	if err := s.checkAndConsumeQuota(ctx, orgID, "face", func() error { return nil }); err != nil {
		if strings.Contains(err.Error(), "QUOTA_EXCEEDED") {
			if s.faceVerifySuccessRate != nil {
				s.faceVerifySuccessRate.Record(ctx, 0.0)
			}
			metrics.RecordBusinessOperation(ctx, "face_detect", false, time.Since(start), "quota_exceeded")
			return nil, fmt.Errorf("Quota exceeded. Please upgrade your plan.")
		}
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_detect", false, time.Since(start), "quota_error")
		return nil, err
	}
	f, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open image failed: %w", err)
	}
	defer f.Close()

	tp := NewThirdPartyService(s.Config)
	out, err := tp.CallFaceDetect(ctx, f, file.Filename)
	if err != nil {
		if s.faceVerifySuccessRate != nil {
			s.faceVerifySuccessRate.Record(ctx, 0.0)
		}
		metrics.RecordBusinessOperation(ctx, "face_detect", false, time.Since(start), "third_party_error")
		return nil, err
	}
	s.RecordAuditLog(ctx, "face.detect", "face", "", "success", "")
	if s.faceVerifySuccessRate != nil {
		s.faceVerifySuccessRate.Record(ctx, 1.0)
	}
	metrics.RecordBusinessOperation(ctx, "face_detect", true, time.Since(start), "")
	return out, nil
}
