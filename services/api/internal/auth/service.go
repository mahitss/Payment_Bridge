package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/email"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email address has not been verified")
	ErrAccountDisabled    = errors.New("account has been disabled")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong    = errors.New("password cannot exceed 128 characters")
	ErrInvalidToken       = errors.New("token is invalid, expired, or has already been used")
	ErrNameRequired       = errors.New("full name is required")
	ErrInvalidEmail       = errors.New("valid email address is required")
)

// Service encapsulates all authentication, session, and credential recovery logic.
type Service struct {
	cfg    *config.Config
	repo   db.Repository
	mailer email.Mailer
}

// NewService constructs an authentication service instance.
func NewService(cfg *config.Config, repo db.Repository, mailer email.Mailer) *Service {
	return &Service{
		cfg:    cfg,
		repo:   repo,
		mailer: mailer,
	}
}

// RegisterInput parameters for account registration.
type RegisterInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// RegisterOutput result of registration.
type RegisterOutput struct {
	Message                   string `json:"message"`
	EmailVerificationRequired bool   `json:"email_verification_required"`
}

// Register creates a new user, personal organization, and sends verification link.
func (s *Service) Register(ctx context.Context, input RegisterInput, ip, userAgent string) (*RegisterOutput, error) {
	normEmail := crypto.NormalizeEmail(input.Email)
	if normEmail == "" || !strings.Contains(normEmail, "@") {
		return nil, ErrInvalidEmail
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrNameRequired
	}

	if len(input.Password) < 8 {
		return nil, ErrPasswordTooShort
	}
	if len(input.Password) > 128 {
		return nil, ErrPasswordTooLong
	}

	// Hash password with Argon2id
	pwdHash, err := crypto.HashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	orgName := fmt.Sprintf("%s's Workspace", name)
	user := &db.User{
		Email:         normEmail,
		PasswordHash:  pwdHash,
		Name:          name,
		EmailVerified: !s.cfg.RequireEmailVerify, // Auto-verified if policy allows
		IsActive:      true,
	}

	createdUser, createdOrg, err := s.repo.CreateUserWithPersonalOrg(ctx, user, orgName)
	if err != nil {
		if errors.Is(err, db.ErrUserAlreadyExists) {
			// Do not leak account existence; return generic message
			slog.WarnContext(ctx, "Registration attempted with existing email (silent ignore)",
				slog.String("email", normEmail),
			)
			return &RegisterOutput{
				Message:                   "If this email is not already registered, a verification link has been sent.",
				EmailVerificationRequired: s.cfg.RequireEmailVerify,
			}, nil
		}
		return nil, fmt.Errorf("failed to register user: %w", err)
	}

	// Generate and dispatch verification token if required
	if s.cfg.RequireEmailVerify {
		rawToken, hashHex, err := crypto.GenerateOpaqueToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate verification token: %w", err)
		}

		vToken := &db.EmailVerificationToken{
			UserID:    createdUser.ID,
			TokenHash: hashHex,
			ExpiresAt: time.Now().UTC().Add(s.cfg.EmailVerificationTTL),
		}

		if err := s.repo.CreateEmailVerificationToken(ctx, vToken); err != nil {
			return nil, fmt.Errorf("failed to persist verification token: %w", err)
		}

		verifyURL := fmt.Sprintf("%s/verify-email?token=%s", s.cfg.AppBaseURL, rawToken)
		if err := s.mailer.SendVerificationEmail(ctx, createdUser.Email, createdUser.Name, verifyURL); err != nil {
			slog.WarnContext(ctx, "Failed to send verification email (will allow resend)",
				slog.String("error", err.Error()),
			)
		}
	}

	// Audit Log
	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &createdUser.ID,
		OrganizationID: &createdOrg.ID,
		EventType:      "USER_REGISTERED",
		IPAddress:      ip,
		UserAgent:      userAgent,
		Metadata: map[string]interface{}{
			"email": createdUser.Email,
		},
	})

	return &RegisterOutput{
		Message:                   "Account created successfully. Please check your email to verify your account.",
		EmailVerificationRequired: s.cfg.RequireEmailVerify,
	}, nil
}

// VerifyEmailInput parameters for email confirmation.
type VerifyEmailInput struct {
	Token string `json:"token"`
}

// VerifyEmail confirms user's email address using a single-use token.
func (s *Service) VerifyEmail(ctx context.Context, input VerifyEmailInput, ip, userAgent string) error {
	rawToken := strings.TrimSpace(input.Token)
	if rawToken == "" {
		return ErrInvalidToken
	}

	tokenHash := crypto.HashToken(rawToken)
	tok, err := s.repo.ConsumeEmailVerificationToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, db.ErrTokenNotFoundOrUsed) {
			return ErrInvalidToken
		}
		return fmt.Errorf("failed to verify email: %w", err)
	}

	if err := s.repo.MarkEmailVerified(ctx, tok.UserID); err != nil {
		return fmt.Errorf("failed to mark email verified: %w", err)
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:    &tok.UserID,
		EventType: "EMAIL_VERIFIED",
		IPAddress: ip,
		UserAgent: userAgent,
	})

	return nil
}

// LoginInput credentials for logging in.
type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginOutput authenticated session and profile details.
type LoginOutput struct {
	SessionToken string          `json:"-"` // Sent via HttpOnly cookie only
	User         SafeUser        `json:"user"`
	Organization db.Organization `json:"organization"`
}

type SafeUser struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

// Login authenticates credentials and establishes an opaque server-side session.
func (s *Service) Login(ctx context.Context, input LoginInput, ip, userAgent string) (*LoginOutput, error) {
	normEmail := crypto.NormalizeEmail(input.Email)
	if normEmail == "" || input.Password == "" {
		crypto.DummyVerifyPassword("dummy")
		return nil, ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByEmail(ctx, normEmail)
	if err != nil {
		// Run constant-time comparison to defeat timing enumeration
		crypto.DummyVerifyPassword(input.Password)
		return nil, ErrInvalidCredentials
	}

	valid, err := crypto.VerifyPassword(input.Password, user.PasswordHash)
	if err != nil || !valid {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:    &user.ID,
			EventType: "LOGIN_FAILED",
			IPAddress: ip,
			UserAgent: userAgent,
		})
		return nil, ErrInvalidCredentials
	}

	if !user.IsActive {
		return nil, ErrAccountDisabled
	}

	if s.cfg.RequireEmailVerify && !user.EmailVerified {
		return nil, ErrEmailNotVerified
	}

	// Fetch primary organization
	memberships, err := s.repo.GetOrganizationMemberships(ctx, user.ID)
	if err != nil || len(memberships) == 0 {
		return nil, errors.New("no organization memberships found for user")
	}

	primaryOrg := memberships[0].Organization

	// Generate opaque session token
	rawSessionToken, tokenHash, err := crypto.GenerateOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session: %w", err)
	}

	session := &db.Session{
		UserID:               user.ID,
		TokenHash:            tokenHash,
		ActiveOrganizationID: &primaryOrg.ID,
		IPAddress:            ip,
		UserAgent:            userAgent,
		ExpiresAt:            time.Now().UTC().Add(s.cfg.SessionTTL),
	}

	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &primaryOrg.ID,
		EventType:      "LOGIN_SUCCESS",
		IPAddress:      ip,
		UserAgent:      userAgent,
	})

	return &LoginOutput{
		SessionToken: rawSessionToken,
		User: SafeUser{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			EmailVerified: user.EmailVerified,
			CreatedAt:     user.CreatedAt,
		},
		Organization: primaryOrg,
	}, nil
}

// Logout invalidates the active server-side session.
func (s *Service) Logout(ctx context.Context, rawSessionToken, ip, userAgent string) error {
	tokenHash := crypto.HashToken(rawSessionToken)
	sess, user, _, _ := s.repo.GetSessionByTokenHash(ctx, tokenHash)

	if err := s.repo.DeleteSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	if sess != nil && user != nil {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: sess.ActiveOrganizationID,
			EventType:      "LOGOUT",
			IPAddress:      ip,
			UserAgent:      userAgent,
		})
	}

	return nil
}

// ForgotPasswordInput parameters for requesting credential recovery.
type ForgotPasswordInput struct {
	Email string `json:"email"`
}

// ForgotPassword generates single-use reset token and emails the link without account enumeration.
func (s *Service) ForgotPassword(ctx context.Context, input ForgotPasswordInput, ip, userAgent string) error {
	normEmail := crypto.NormalizeEmail(input.Email)
	if normEmail == "" || !strings.Contains(normEmail, "@") {
		// Return generic success to avoid enumeration
		return nil
	}

	user, err := s.repo.GetUserByEmail(ctx, normEmail)
	if err != nil {
		// User does not exist; balance timing and return success
		crypto.DummyVerifyPassword("dummy")
		return nil
	}

	rawToken, tokenHash, err := crypto.GenerateOpaqueToken()
	if err != nil {
		return fmt.Errorf("failed to generate reset token: %w", err)
	}

	resetToken := &db.PasswordResetToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().UTC().Add(s.cfg.PasswordResetTTL),
	}

	if err := s.repo.CreatePasswordResetToken(ctx, resetToken); err != nil {
		return fmt.Errorf("failed to save password reset token: %w", err)
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.cfg.AppBaseURL, rawToken)
	if err := s.mailer.SendPasswordResetEmail(ctx, user.Email, user.Name, resetURL); err != nil {
		slog.WarnContext(ctx, "Failed to deliver password reset email", slog.String("error", err.Error()))
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:    &user.ID,
		EventType: "PASSWORD_RESET_REQUESTED",
		IPAddress: ip,
		UserAgent: userAgent,
	})

	return nil
}

// ResetPasswordInput parameters for completing credential recovery.
type ResetPasswordInput struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ResetPassword updates password, consumes token, and invalidates all existing sessions.
func (s *Service) ResetPassword(ctx context.Context, input ResetPasswordInput, ip, userAgent string) error {
	rawToken := strings.TrimSpace(input.Token)
	if rawToken == "" {
		return ErrInvalidToken
	}

	if len(input.NewPassword) < 8 {
		return ErrPasswordTooShort
	}
	if len(input.NewPassword) > 128 {
		return ErrPasswordTooLong
	}

	tokenHash := crypto.HashToken(rawToken)
	tok, err := s.repo.ConsumePasswordResetToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, db.ErrTokenNotFoundOrUsed) {
			return ErrInvalidToken
		}
		return fmt.Errorf("failed to consume reset token: %w", err)
	}

	// Hash new password
	newHash, err := crypto.HashPassword(input.NewPassword)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := s.repo.UpdateUserPassword(ctx, tok.UserID, newHash); err != nil {
		return fmt.Errorf("failed to update user password: %w", err)
	}

	// CRITICAL SECURITY INVARIANT: Revoke all existing sessions for this user!
	if err := s.repo.DeleteAllUserSessions(ctx, tok.UserID); err != nil {
		slog.ErrorContext(ctx, "Failed to invalidate user sessions after password reset",
			slog.String("user_id", tok.UserID),
			slog.String("error", err.Error()),
		)
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:    &tok.UserID,
		EventType: "PASSWORD_RESET_COMPLETED",
		IPAddress: ip,
		UserAgent: userAgent,
	})

	return nil
}

// ValidateSession verifies a raw session token and returns the authenticated user and organization.
func (s *Service) ValidateSession(ctx context.Context, rawSessionToken string) (*db.Session, *db.User, *db.Organization, error) {
	if strings.TrimSpace(rawSessionToken) == "" {
		return nil, nil, nil, db.ErrSessionNotFound
	}

	tokenHash := crypto.HashToken(rawSessionToken)
	sess, user, org, err := s.repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, nil, nil, err
	}

	// Touch session asynchronously to update last_active_at
	_ = s.repo.TouchSession(ctx, sess.ID)

	return sess, user, org, nil
}
