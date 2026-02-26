package api

type OCRTextItem struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

type OCRPublicResult struct {
	Code           int                    `json:"code"`
	Msg            string                 `json:"msg"`
	ParsingResults map[string]OCRTextItem `json:"parsing_results"`
	FullText       string                 `json:"full_text,omitempty"`
	Filename       string                 `json:"filename,omitempty"`
}

type OCRSuccessResponse struct {
	BaseResponse
	Data OCRPublicResult `json:"data"`
}

type FaceSearchItem struct {
	ID         string  `json:"id"`
	Confidence float64 `json:"confidence"`
}

type FaceSearchDTO struct {
	Code             int    `json:"code"`
	Msg              string `json:"msg"`
	SearchingResults struct {
		SearchedSimilarPictures []FaceSearchItem `json:"searched_similar_pictures"`
		HasSimilarPicture       int              `json:"has_similar_picture"`
	} `json:"searching_results"`
	Filename string `json:"filename"`
}

type FaceSearchSuccessResponse struct {
	BaseResponse
	Data FaceSearchDTO `json:"data"`
}

type FaceCompareDTO struct {
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

type FaceCompareSuccessResponse struct {
	BaseResponse
	Data FaceCompareDTO `json:"data"`
}

type FaceDetectDTO struct {
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

type FaceDetectSuccessResponse struct {
	BaseResponse
	Data FaceDetectDTO `json:"data"`
}

type LivenessSilentDTO struct {
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

type LivenessSilentSuccessResponse struct {
	BaseResponse
	Data LivenessSilentDTO `json:"data"`
}

type LivenessVideoDTO struct {
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

type LivenessVideoSuccessResponse struct {
	BaseResponse
	Data LivenessVideoDTO `json:"data"`
}
