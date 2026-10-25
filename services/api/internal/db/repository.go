package db

import (
	"context"
	"errors"
	"time"
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
	ErrRecoveryClaimConflict      = errors.New("recovery claim conflict: operation already claimed by another process")
	ErrRecoveryAlreadyInProgress  = errors.New("recovery is already in progress for this operation")
	ErrStaleRecoveryClaim         = errors.New("stale recovery claim: prior recovery lease expired; manual reconciliation required")
	ErrRecoveryLeaseExpired       = errors.New("recovery lease expired: lease time elapsed before operation could be finalized")
	ErrRecoveryWindowExpired      = errors.New("recovery window expired: original submission is older than 27-day cutoff; manual reconciliation required")
	ErrSubmissionTimestampMissing = errors.New("submission timestamp is missing; manual reconciliation required")
	ErrSubmissionTimestampInvalid = errors.New("submission timestamp is in the future; manual reconciliation required")
)

// Repository encapsulates data access operations for ClaimBridge.
type Repository interface {
	// User & Organization Lifecycle
	CreateUser(ctx context.Context, user *User) (*User, error)
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
	AddOrganizationMember(ctx context.Context, orgID, userID, role string) error

	// Payout Cases
	CreatePayoutCase(ctx context.Context, c *PayoutCase) error
	GetPayoutCaseByID(ctx context.Context, orgID, caseID string) (*PayoutCase, error)
	GetPayoutCaseByReference(ctx context.Context, orgID, reference string) (*PayoutCase, error)
	ListPayoutCasesByOrg(ctx context.Context, orgID string, limit, offset int) ([]PayoutCase, int, error)
	UpdatePayoutCaseStatus(ctx context.Context, orgID, caseID, status string) error

	// Payout Operations
	CreatePayoutOperation(ctx context.Context, op *PayoutOperation) error
	GetPayoutOperationByID(ctx context.Context, orgID, opID string) (*PayoutOperation, error)
	GetPayoutOperationBySenderBatchID(ctx context.Context, senderBatchID string) (*PayoutOperation, error)
	ListPayoutOperationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutOperation, error)
	UpdatePayoutOperation(ctx context.Context, op *PayoutOperation) error
	ApprovePayoutOperation(ctx context.Context, orgID, opID, approvedByUserID string) error
	ClaimPayoutOperationForRecovery(ctx context.Context, orgID, opID, attemptID string, claimedAt, leaseExpiresAt time.Time, maxAge time.Duration) (*PayoutOperation, error)
	RenewRecoveryLease(ctx context.Context, orgID, opID, attemptID string, newLeaseExpiresAt time.Time) error
	FinalizeRecoveryOperation(ctx context.Context, op *PayoutOperation, attemptID string) error
	TransitionRecoveryToManualReview(ctx context.Context, orgID, opID, attemptID, reason string, rawProviderResponse map[string]interface{}) error

	// Status Snapshots
	CreatePayoutStatusSnapshot(ctx context.Context, snapshot *PayoutStatusSnapshot) error
	ListPayoutStatusSnapshots(ctx context.Context, orgID, opID string) ([]PayoutStatusSnapshot, error)

	// Investigations
	CreateInvestigation(ctx context.Context, inv *PayoutInvestigation) error
	GetInvestigationByID(ctx context.Context, orgID, investigationID string) (*PayoutInvestigation, error)
	ListInvestigationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutInvestigation, error)

	// Fixture Lifecycle & Clean Reset
	DeleteSyntheticFixtures(ctx context.Context, orgID string) (int64, error)

	// Audit Logging
	RecordAuditEvent(ctx context.Context, event *AuditEvent) error

	// Health
	Ping(ctx context.Context) error
}
