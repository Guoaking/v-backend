package api

import (
	"fmt"

	"kyc-service/internal/service"
	"kyc-service/pkg/mail"

	"github.com/gin-gonic/gin"
)

type NotificationHandler struct{ service *service.KYCService }

func NewNotificationHandler(svc *service.KYCService) *NotificationHandler {
	return &NotificationHandler{service: svc}
}

type SendEmailRequest struct {
	To       string   `json:"to"`
	ToList   []string `json:"to_list"`
	Subject  string   `json:"subject" binding:"required"`
	BodyHTML string   `json:"body_html" binding:"required"`
}

// 发送邮件（需权限 notifications.send）
func (h *NotificationHandler) SendEmail(c *gin.Context) {
	var req SendEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	cfg := h.service.Config.Monitoring.Alerting
	smtpCfg := mail.SMTPConfig{Host: cfg.EmailSMTP, Port: cfg.EmailPort, Username: cfg.EmailUser, Password: cfg.EmailPassword, From: cfg.EmailFrom, TLS: cfg.EmailTLS}
	recipients := []string{}
	if req.To != "" {
		recipients = append(recipients, req.To)
	}
	if len(req.ToList) > 0 {
		recipients = append(recipients, req.ToList...)
	}
	if len(recipients) == 0 {
		JSONError(c, CodeInvalidParameter, "至少提供一个收件人")
		return
	}
	sent := 0
	failed := []string{}
	for _, r := range recipients {
		if err := mail.SendSMTP(smtpCfg, r, req.Subject, req.BodyHTML, ""); err == nil {
			sent++
		} else {
			failed = append(failed, fmt.Sprintf("%v: %v", r, err))
		}
	}
	if len(failed) == len(recipients) {
		JSONError(c, CodeInternalError, fmt.Sprintf("邮件发送失败，请检查SMTP端口与TLS模式、凭据是否正确, %v", failed[0]))
		return
	}
	JSONSuccess(c, gin.H{"sent_count": sent, "requested": len(recipients), "failed": failed})
}
