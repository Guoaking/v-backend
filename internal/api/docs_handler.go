package api

import (
	"github.com/gin-gonic/gin"
)

type SecurityGuide struct {
	AuthMethods []string `json:"auth_methods"`
	ApiKey      struct {
		Header string   `json:"header"`
		Format string   `json:"format"`
		Scopes []string `json:"scopes"`
		Curl   []string `json:"curl"`
	} `json:"api_key"`
	OAuth2 struct {
		GrantType string   `json:"grant_type"`
		TokenURL  string   `json:"token_url"`
		Scopes    []string `json:"scopes"`
		Curl      []string `json:"curl"`
	} `json:"oauth2"`
}

func NewDocsHandler() *DocsHandler { return &DocsHandler{} }

type DocsHandler struct{}

// SecurityDoc
// @Summary Security Access Guide
// @Description How to use ApiKey and OAuth2 Client Credentials, scopes and cURL examples
// @Tags Docs
// @Produce json
// @Success 200 {object} SecurityGuide
// @Router /docs/security [get]
func (h *DocsHandler) SecurityDoc(c *gin.Context) {
	guide := SecurityGuide{
		AuthMethods: []string{"ApiKey", "OAuth2 Client Credentials"},
	}
	guide.ApiKey.Header = "Authorization"
	guide.ApiKey.Format = "Bearer <api_key>"
	guide.ApiKey.Scopes = []string{"ocr:read", "face:read", "liveness:read", "kyc:verify"}
	guide.ApiKey.Curl = []string{
		`curl -X POST "http://localhost:8082/api/v1/kyc/ocr" -H "Authorization: Bearer <api_key>" -F "picture=@/path/id.jpg"`,
		`curl -X POST "http://localhost:8082/api/v1/kyc/face/compare" -H "Authorization: Bearer <api_key>" -F "source_image=@/path/a.jpg" -F "target_image=@/path/b.jpg"`,
	}
	guide.OAuth2.GrantType = "client_credentials"
	guide.OAuth2.TokenURL = "/api/v1/oauth/token"
	guide.OAuth2.Scopes = []string{"ocr:read", "face:read", "liveness:read", "kyc:verify"}
	guide.OAuth2.Curl = []string{
		`curl -X POST "http://localhost:8082/api/v1/oauth/token" -H "Content-Type: application/json" -d '{"client_id":"<id>","client_secret":"<secret>","grant_type":"client_credentials","scope":"ocr:read face:read"}'`,
		`curl -X POST "http://localhost:8082/api/v1/kyc/ocr" -H "Authorization: Bearer <access_token>" -F "picture=@/path/id.jpg"`,
	}
	JSONSuccess(c, guide)
}

type ErrorCodeEntry struct {
	Code       ResponseCode `json:"code"`
	Name       string       `json:"name"`
	Message    string       `json:"message"`
	HTTPStatus int          `json:"http_status"`
}

type ErrorCodesDoc struct {
    Client []ErrorCodeEntry `json:"client_errors"`
    Business []ErrorCodeEntry `json:"business_errors"`
    Payment []ErrorCodeEntry `json:"payment_errors"`
    Server []ErrorCodeEntry `json:"server_errors"`
}

// ErrorCodesDoc
// @Summary Error Codes Reference
// @Description Client, business, payment and server error codes with messages and HTTP status mapping
// @Produce json
// @Success 200 {object} ErrorCodesDoc
// @Router /docs/error-codes [get]
func (h *DocsHandler) ErrorCodesDoc(c *gin.Context) {
	var out ErrorCodesDoc
	add := func(code ResponseCode) {
		name := ""
		switch code {
		case CodeBadRequest:
			name = "CodeBadRequest"
		case CodeUnauthorized:
			name = "CodeUnauthorized"
		case CodeForbidden:
			name = "CodeForbidden"
		case CodeNotFound:
			name = "CodeNotFound"
		case CodeMethodNotAllowed:
			name = "CodeMethodNotAllowed"
		case CodeTooManyRequests:
			name = "CodeTooManyRequests"
		case CodeInvalidParameter:
			name = "CodeInvalidParameter"
		case CodeMissingParameter:
			name = "CodeMissingParameter"
		case CodeBusinessError:
			name = "CodeBusinessError"
		case CodeOCRFailed:
			name = "CodeOCRFailed"
		case CodeFaceVerifyFailed:
			name = "CodeFaceVerifyFailed"
		case CodeLivenessFailed:
			name = "CodeLivenessFailed"
		case CodeKYCFailed:
			name = "CodeKYCFailed"
		case CodeIDCardNotMatch:
			name = "CodeIDCardNotMatch"
		case CodeFaceNotMatch:
			name = "CodeFaceNotMatch"
		case CodeConflict:
			name = "CodeConflict"
		case CodePaymentRequired:
			name = "CodePaymentRequired"
		case CodeInternalError:
			name = "CodeInternalError"
		case CodeDatabaseError:
			name = "CodeDatabaseError"
		case CodeThirdPartyError:
			name = "CodeThirdPartyError"
		case CodeServiceUnavailable:
			name = "CodeServiceUnavailable"
		case CodeEncryptionError:
			name = "CodeEncryptionError"
		}
		entry := ErrorCodeEntry{Code: code, Name: name, Message: ResponseMessage[code], HTTPStatus: getHTTPStatusCode(code)}
		switch {
		case code >= 1000 && code < 2000:
			out.Client = append(out.Client, entry)
		case code >= 2000 && code < 3000:
			out.Business = append(out.Business, entry)
		case code == CodePaymentRequired:
			out.Payment = append(out.Payment, entry)
		case code >= 5000:
			out.Server = append(out.Server, entry)
		}
	}
	// enumerate
	codes := []ResponseCode{
		CodeBadRequest, CodeUnauthorized, CodeForbidden, CodeNotFound, CodeMethodNotAllowed, CodeTooManyRequests, CodeInvalidParameter, CodeMissingParameter,
		CodeBusinessError, CodeOCRFailed, CodeFaceVerifyFailed, CodeLivenessFailed, CodeKYCFailed, CodeIDCardNotMatch, CodeFaceNotMatch, CodeConflict,
		CodePaymentRequired,
		CodeInternalError, CodeDatabaseError, CodeThirdPartyError, CodeServiceUnavailable, CodeEncryptionError,
	}
	for _, ccode := range codes {
		add(ccode)
	}
	JSONSuccess(c, out)
}
