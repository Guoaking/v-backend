package mail

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      bool
}

func buildMessage(from, to, subject, bodyHTML, bodyText string) []byte {
	ct := "text/html; charset=\"UTF-8\""
	if bodyText != "" {
		// 简化：仍用HTML；如需multipart/alternative可扩展
	}
	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		fmt.Sprintf("Content-Type: %s", ct),
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + bodyHTML)
}

// loginAuth implements SMTP AUTH LOGIN challenge-response
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte{}, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	s := strings.ToLower(string(fromServer))
	if strings.Contains(s, "username") {
		return []byte(base64.StdEncoding.EncodeToString([]byte(a.username))), nil
	}
	if strings.Contains(s, "password") {
		return []byte(base64.StdEncoding.EncodeToString([]byte(a.password))), nil
	}
	// Some servers send base64 prompts; try to decode
	if dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fromServer))); err == nil {
		ds := strings.ToLower(string(dec))
		if strings.Contains(ds, "username") {
			return []byte(base64.StdEncoding.EncodeToString([]byte(a.username))), nil
		}
		if strings.Contains(ds, "password") {
			return []byte(base64.StdEncoding.EncodeToString([]byte(a.password))), nil
		}
	}
	// Fallback: send username first
	return []byte(base64.StdEncoding.EncodeToString([]byte(a.username))), nil
}

func SendSMTP(cfg SMTPConfig, to string, subject string, bodyHTML string, bodyText string) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	msg := buildMessage(cfg.From, to, subject, bodyHTML, bodyText)
	// Implicit TLS (SMTPS 465)
	if cfg.TLS && cfg.Port == 465 {
		tlsCfg := &tls.Config{ServerName: cfg.Host}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return err
		}
		defer c.Quit()
		_ = c.Hello("localhost")
		if err = c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			// Fallback to AUTH LOGIN
			if err2 := c.Auth(&loginAuth{username: cfg.Username, password: cfg.Password}); err2 != nil {
				return err
			}
		}
		if err = c.Mail(cfg.From); err != nil {
			return err
		}
		if err = c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err = w.Write(msg); err != nil {
			_ = w.Close()
			return err
		}
		if err = w.Close(); err != nil {
			return err
		}
		return nil
	}
	// Plain or STARTTLS (587)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	_ = c.Hello("localhost")
	if cfg.TLS { // STARTTLS upgrade
		tlsCfg := &tls.Config{ServerName: cfg.Host}
		if err = c.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if err = c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
		// Fallback to AUTH LOGIN
		if err2 := c.Auth(&loginAuth{username: cfg.Username, password: cfg.Password}); err2 != nil {
			return err
		}
	}
	if err = c.Mail(cfg.From); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	//defer w.Close()
	if _, err = w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	//if err = w.Close(); err != nil {
	//	return err
	//}

	if err = c.Quit(); err != nil {
		return err
	}

	return nil
}
