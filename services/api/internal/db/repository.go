package db

import (
	"context"
	"errors"
)

var (
	ErrUserNotFound               = errors.New("user not found")
	ErrUserAlreadyExists          = errors.New("user with this email already exists")
	ErrSessionNotFound            = errors.New("session not found or expired")
	ErrTokenNotFoundOrUsed        = errors.New("token is invalid, expired, or already consumed")
	ErrOrganizationNotFound       = errors.New("organization not found")
	ErrAccessDenied               = errors.New("access denied: user does not have membership in organization")
	ErrMembershipConflict         = errors.New("user already has membership in this organization")
	ErrPayoutCaseNotFound         = errors.New("payout case not found")
	ErrPayoutOperationNotFound    = errors.New("payout operation not found")
	ErrInvestigationNotFound      = errors.New("payout investigation not found")
	ErrSelfApprovalForbidden      = errors.New("self-approval forbidden: dual-custody requires a distinct authorized approver")
	ErrInvalidOperationTransition = errors.New("invalid payout operation state transition")
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

	// Payout Cases
	CreatePayoutCase(ctx context.Context, c *PayoutCase) error
	GetPayoutCaseByID(ctx context.Context, orgID, caseID string) (*PayoutCase, error)
	ListPayoutCasesByOrg(ctx context.Context, orgID string, limit, offset int) ([]PayoutCase, int, error)
	UpdatePayoutCaseStatus(ctx context.Context, orgID, caseID, status string) error

	// Payout Operations
	CreatePayoutOperation(ctx context.Context, op *PayoutOperation) error
	GetPayoutOperationByID(ctx context.Context, orgID, opID string) (*PayoutOperation, error)
	GetPayoutOperationBySenderBatchID(ctx context.Context, senderBatchID string) (*PayoutOperation, error)
	ListPayoutOperationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutOperation, error)
	UpdatePayoutOperation(ctx context.Context, op *PayoutOperation) error
	ApprovePayoutOperation(ctx context.Context, orgID, opID, approvedByUserID string) error

	// Status Snapshots
	CreatePayoutStatusSnapshot(ctx context.Context, snapshot *PayoutStatusSnapshot) error
	ListPayoutStatusSnapshots(ctx context.Context, orgID, opID string) ([]PayoutStatusSnapshot, error)

	// Investigations
	CreateInvestigation(ctx context.Context, inv *PayoutInvestigation) error
	GetInvestigationByID(ctx context.Context, orgID, investigationID string) (*PayoutInvestigation, error)
	ListInvestigationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutInvestigation, error)

	// Audit Logging
	RecordAuditEvent(ctx context.Context, event *AuditEvent) error

	// Health
	Ping(ctx context.Context) error
}
