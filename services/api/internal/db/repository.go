package db

import (
	"context"
	"errors"
)

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrUserAlreadyExists     = errors.New("user with this email already exists")
	ErrSessionNotFound       = errors.New("session not found or expired")
	ErrTokenNotFoundOrUsed   = errors.New("token is invalid, expired, or already consumed")
	ErrOrganizationNotFound  = errors.New("organization not found")
	ErrAccessDenied          = errors.New("access denied: user does not have membership in organization")
	ErrMembershipConflict    = errors.New("user already has membership in this organization")
)

// Repository encapsulates data access operations for ClaimBridge.
type Repository interface {
	// User & Organization Lifecycle
	CreateUserWithPersonalOrg(ctx context.Context, user *User, orgName string) (*User, *Organization, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	UpdateUserPassword(ctx context.Context, userID, newPasswordHash string) error
	MarkEmailVerified(ctx context.Context, userID string) error

	// Sessions
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, *User, *Organization, error)
	TouchSession(ctx context.Context, sessionID string) error
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteAllUserSessions(ctx context.Context, userID string) error

	// Email Verification
	CreateEmailVerificationToken(ctx context.Context, token *EmailVerificationToken) error
	ConsumeEmailVerificationToken(ctx context.Context, tokenHash string) (*EmailVerificationToken, error)

	// Password Reset
	CreatePasswordResetToken(ctx context.Context, token *PasswordResetToken) error
	ConsumePasswordResetToken(ctx context.Context, tokenHash string) (*PasswordResetToken, error)

	// Organizations & Multi-Tenancy
	GetOrganizationMemberships(ctx context.Context, userID string) ([]OrganizationMembershipWithOrg, error)
	GetOrganizationByID(ctx context.Context, orgID string) (*Organization, error)
	VerifyUserOrgAccess(ctx context.Context, userID, orgID string) (bool, string, error)

	// Audit Logging
	RecordAuditEvent(ctx context.Context, event *AuditEvent) error

	// Health
	Ping(ctx context.Context) error
}
