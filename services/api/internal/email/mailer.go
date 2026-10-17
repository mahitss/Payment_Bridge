package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
)

var (
	ErrSMTPUnavailable = errors.New("smtp delivery failed: mail server is unreachable or refused connection")
	ErrInvalidEmail     = errors.New("invalid recipient email address")
)

// Mailer defines the application email delivery contract.
type Mailer interface {
	SendVerificationEmail(ctx context.Context, toEmail, recipientName, verifyURL string) error
	SendPasswordResetEmail(ctx context.Context, toEmail, recipientName, resetURL string) error
}

// SMTPMailer delivers emails over SMTP (e.g. Mailpit locally or custom SMTP relay).
type SMTPMailer struct {
	host string
	port int
	from string
	auth smtp.Auth
}

// NewSMTPMailer creates a new SMTP mailer instance.
func NewSMTPMailer(host string, port int, from, user, password string) *SMTPMailer {
	var auth smtp.Auth
	if user != "" && password != "" {
		auth = smtp.PlainAuth("", user, password, host)
	}

	return &SMTPMailer{
		host: host,
		port: port,
		from: from,
		auth: auth,
	}
}

func (m *SMTPMailer) SendVerificationEmail(ctx context.Context, toEmail, recipientName, verifyURL string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" || !strings.Contains(toEmail, "@") {
		return ErrInvalidEmail
	}

	subject := "Verify your ClaimBridge Account"
	bodyText := fmt.Sprintf(
		"Hello %s,\n\nWelcome to ClaimBridge. Please verify your email by opening the link below:\n\n%s\n\nThis verification link expires in 24 hours.\n\nIf you did not create this account, please ignore this email.\n\n— The ClaimBridge Team",
		recipientName,
		verifyURL,
	)

	bodyHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><style>body{font-family:sans-serif;color:#1e293b;line-height:1.5;margin:0;padding:24px;background:#f8fafc}.container{max-width:540px;margin:0 auto;background:#ffffff;border:1px solid #e2e8f0;border-radius:8px;padding:32px}.btn{display:inline-block;background:#2563eb;color:#ffffff;text-decoration:none;font-weight:600;padding:12px 24px;border-radius:6px;margin:20px 0}</style></head>
<body>
<div class="container">
  <h2>Verify your ClaimBridge Account</h2>
  <p>Hello %s,</p>
  <p>Thank you for registering with ClaimBridge. Please confirm your email address to activate your account and access claims processing:</p>
  <p><a href="%s" class="btn">Verify Email Address</a></p>
  <p style="font-size:12px;color:#64748b">If the button above does not work, copy and paste this link into your browser:<br>%s</p>
  <p style="font-size:12px;color:#64748b">This link will expire in 24 hours.</p>
</div>
</body>
</html>`, recipientName, verifyURL, verifyURL)

	err := m.sendMail(toEmail, subject, bodyText, bodyHTML)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to deliver verification email",
			slog.String("recipient", toEmail),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: %v", ErrSMTPUnavailable, err)
	}

	slog.InfoContext(ctx, "Verification email dispatched via SMTP",
		slog.String("recipient", toEmail),
	)
	return nil
}

func (m *SMTPMailer) SendPasswordResetEmail(ctx context.Context, toEmail, recipientName, resetURL string) error {
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" || !strings.Contains(toEmail, "@") {
		return ErrInvalidEmail
	}

	subject := "Reset your ClaimBridge Password"
	bodyText := fmt.Sprintf(
		"Hello %s,\n\nA password reset was requested for your ClaimBridge account. Open the link below to set a new password:\n\n%s\n\nThis single-use reset link expires in 30 minutes.\n\nIf you did not request this, please disregard this email.\n\n— The ClaimBridge Team",
		recipientName,
		resetURL,
	)

	bodyHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><style>body{font-family:sans-serif;color:#1e293b;line-height:1.5;margin:0;padding:24px;background:#f8fafc}.container{max-width:540px;margin:0 auto;background:#ffffff;border:1px solid #e2e8f0;border-radius:8px;padding:32px}.btn{display:inline-block;background:#dc2626;color:#ffffff;text-decoration:none;font-weight:600;padding:12px 24px;border-radius:6px;margin:20px 0}</style></head>
<body>
<div class="container">
  <h2>Password Reset Request</h2>
  <p>Hello %s,</p>
  <p>We received a request to reset your password. Click the button below to choose a new password:</p>
  <p><a href="%s" class="btn">Reset Password</a></p>
  <p style="font-size:12px;color:#64748b">If the button does not work, copy and paste this link into your browser:<br>%s</p>
  <p style="font-size:12px;color:#64748b">This link expires in 30 minutes and can only be used once. All active sessions will be terminated upon reset.</p>
</div>
</body>
</html>`, recipientName, resetURL, resetURL)

	err := m.sendMail(toEmail, subject, bodyText, bodyHTML)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to deliver password reset email",
			slog.String("recipient", toEmail),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%w: %v", ErrSMTPUnavailable, err)
	}

	slog.InfoContext(ctx, "Password reset email dispatched via SMTP",
		slog.String("recipient", toEmail),
	)
	return nil
}

func (m *SMTPMailer) sendMail(to, subject, textBody, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", m.host, m.port)
	boundary := "boundary-claimbridge-mime-multipart"

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n"+
		"--%s\r\n"+
		"Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n"+
		"%s\r\n\r\n"+
		"--%s\r\n"+
		"Content-Type: text/html; charset=\"utf-8\"\r\n\r\n"+
		"%s\r\n\r\n"+
		"--%s--\r\n",
		m.from, to, subject, boundary, boundary, textBody, boundary, htmlBody, boundary)

	return smtp.SendMail(addr, m.auth, m.from, []string{to}, []byte(msg))
}

// MockMailer records dispatched emails for automated unit and integration tests.
type MockMailer struct {
	mu           sync.Mutex
	SentMails    []SentMessage
	FailNextCall bool
}

type SentMessage struct {
	Type          string // "verification" or "password_reset"
	To            string
	RecipientName string
	URL           string
}

func NewMockMailer() *MockMailer {
	return &MockMailer{SentMails: make([]SentMessage, 0)}
}

func (m *MockMailer) SendVerificationEmail(ctx context.Context, toEmail, recipientName, verifyURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailNextCall {
		m.FailNextCall = false
		return ErrSMTPUnavailable
	}

	m.SentMails = append(m.SentMails, SentMessage{
		Type:          "verification",
		To:            toEmail,
		RecipientName: recipientName,
		URL:           verifyURL,
	})
	return nil
}

func (m *MockMailer) SendPasswordResetEmail(ctx context.Context, toEmail, recipientName, resetURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailNextCall {
		m.FailNextCall = false
		return ErrSMTPUnavailable
	}

	m.SentMails = append(m.SentMails, SentMessage{
		Type:          "password_reset",
		To:            toEmail,
		RecipientName: recipientName,
		URL:           resetURL,
	})
	return nil
}

func (m *MockMailer) GetLastMail() *SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.SentMails) == 0 {
		return nil
	}
	last := m.SentMails[len(m.SentMails)-1]
	return &last
}
