package db

import (
	"time"
)

type User struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	PasswordHash    string     `json:"-"`
	Name            string     `json:"name"`
	EmailVerified   bool       `json:"email_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	IsActive        bool       `json:"is_active"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Organization struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	IsPersonal bool      `json:"is_personal"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type OrganizationMembership struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Role           string    `json:"role"` // OWNER, ADMIN, MEMBER
	CreatedAt      time.Time `json:"created_at"`
}

type OrganizationMembershipWithOrg struct {
	Membership   OrganizationMembership `json:"membership"`
	Organization Organization           `json:"organization"`
}

type Session struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"user_id"`
	TokenHash            string    `json:"-"`
	ActiveOrganizationID *string   `json:"active_organization_id,omitempty"`
	IPAddress            string    `json:"ip_address,omitempty"`
	UserAgent            string    `json:"user_agent,omitempty"`
	ExpiresAt            time.Time `json:"expires_at"`
	CreatedAt            time.Time `json:"created_at"`
	LastActiveAt         time.Time `json:"last_active_at"`
}

type EmailVerificationToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type PasswordResetToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type AuditEvent struct {
	ID             string                 `json:"id"`
	UserID         *string                `json:"user_id,omitempty"`
	OrganizationID *string                `json:"organization_id,omitempty"`
	EventType      string                 `json:"event_type"`
	IPAddress      string                 `json:"ip_address,omitempty"`
	UserAgent      string                 `json:"user_agent,omitempty"`
	Metadata       map[string]interface{} `json:"metadata"`
	CreatedAt      time.Time              `json:"created_at"`
}

type PayoutCase struct {
	ID                   string    `json:"id"`
	OrganizationID       string    `json:"organization_id"`
	CreatedByUserID      string    `json:"created_by_user_id"`
	Reference            string    `json:"reference"`
	RecipientEmail       string    `json:"recipient_email"`
	RecipientName        string    `json:"recipient_name"`
	Title                string    `json:"title"`
	Description          string    `json:"description"`
	Currency             string    `json:"currency"`
	AmountCents          int64     `json:"amount_cents"`
	Status               string    `json:"status"` // DRAFT, SUBMITTED, COMPLETED, DENIED, CANCELLED
	RiskScore            float64   `json:"risk_score"`
	DualApprovalRequired bool      `json:"dual_approval_required"`
	IsSyntheticFixture   bool      `json:"is_synthetic_fixture"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type PayoutOperation struct {
	ID                     string                 `json:"id"`
	PayoutCaseID           string                 `json:"payout_case_id"`
	OrganizationID         string                 `json:"organization_id"`
	InitiatedByUserID      string                 `json:"initiated_by_user_id"`
	SenderBatchID          string                 `json:"sender_batch_id"`
	SenderItemID           string                 `json:"sender_item_id"`
	AmountCents            int64                  `json:"amount_cents"`
	Currency               string                 `json:"currency"`
	Status                 string                 `json:"status"` // PREPARED, AWAITING_APPROVAL, APPROVED, SUBMITTING, SUBMITTED, SUCCEEDED, FAILED, RECONCILIATION_REQUIRED, RECOVERING, CANCELLED
	SubmissionFingerprint  *string                `json:"submission_fingerprint,omitempty"`
	PayPalPayoutBatchID    *string                `json:"paypal_payout_batch_id,omitempty"`
	PayPalBatchStatus      *string                `json:"paypal_batch_status,omitempty"`
	PayPalPayoutItemID     *string                `json:"paypal_payout_item_id,omitempty"`
	PayPalItemStatus       *string                `json:"paypal_item_status,omitempty"`
	ErrorMessage           *string                `json:"error_message,omitempty"`
	RawProviderResponse    map[string]interface{} `json:"raw_provider_response,omitempty"`
	RequiresDualApproval   bool                   `json:"requires_dual_approval"`
	ApprovedByUserID       *string                `json:"approved_by_user_id,omitempty"`
	ApprovedAt             *time.Time             `json:"approved_at,omitempty"`
	SubmittedAt            *time.Time             `json:"submitted_at,omitempty"`
	CompletedAt            *time.Time             `json:"completed_at,omitempty"`
	CancelledAt            *time.Time             `json:"cancelled_at,omitempty"`
	IsSyntheticFixture     bool                   `json:"is_synthetic_fixture"`
	RecoveryAttemptID      *string                `json:"recovery_attempt_id,omitempty"`
	RecoveryClaimedAt      *time.Time             `json:"recovery_claimed_at,omitempty"`
	RecoveryLeaseExpiresAt *time.Time             `json:"recovery_lease_expires_at,omitempty"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

type PayoutStatusSnapshot struct {
	ID                 string                 `json:"id"`
	PayoutOperationID  string                 `json:"payout_operation_id"`
	OrganizationID     string                 `json:"organization_id"`
	Provider           string                 `json:"provider"`
	BatchStatus        string                 `json:"batch_status"`
	ItemStatus         *string                `json:"item_status,omitempty"`
	RawResponse        map[string]interface{} `json:"raw_response"`
	IsSyntheticFixture bool                   `json:"is_synthetic_fixture"`
	RecordedAt         time.Time              `json:"recorded_at"`
}

type InvestigationFinding struct {
	Explanation        string   `json:"explanation"`
	EvidenceReferences []string `json:"evidence_references"`
}

type InvestigationRecommendation struct {
	Action                       string `json:"action"` // REQUEST_RECIPIENT_INFORMATION, CHECK_PROVIDER_STATUS, REVIEW_RECIPIENT_DETAILS, REVIEW_CANCELLATION_ELIGIBILITY, ESCALATE_TO_OPERATOR, NO_ACTION_UNTIL_RECONCILED
	Reason                       string `json:"reason"`
	RequiresHumanReview          bool   `json:"requires_human_review"`
	DeterministicPolicyPermitted bool   `json:"deterministic_policy_permitted"`
	PolicyRuleID                 string `json:"policy_rule_id,omitempty"`
}

type PayoutInvestigation struct {
	ID                   string                        `json:"id"`
	PayoutCaseID         string                        `json:"payout_case_id"`
	OrganizationID       string                        `json:"organization_id"`
	RequestedByUserID    string                        `json:"requested_by_user_id"`
	Provider             string                        `json:"provider"`
	Model                string                        `json:"model"`
	PromptVersion        string                        `json:"prompt_version"`
	Status               string                        `json:"status"` // COMPLETED, FAILED, INVALID_EVIDENCE_REFERENCE
	Summary              string                        `json:"summary"`
	Confidence           string                        `json:"confidence"` // HIGH, MEDIUM, LOW, INSUFFICIENT_EVIDENCE
	RequiresHumanReview  bool                          `json:"requires_human_review"`
	ObservedFacts        []string                      `json:"observed_facts"`
	Findings             []InvestigationFinding        `json:"findings"`
	RecommendedNextSteps []InvestigationRecommendation `json:"recommended_next_steps"`
	MissingInformation   []string                      `json:"missing_information"`
	RiskFlags            []string                      `json:"risk_flags"`
	Limitations          []string                      `json:"limitations"`
	RawResponse          map[string]interface{}        `json:"raw_response,omitempty"`
	ErrorMessage         *string                       `json:"error_message,omitempty"`
	CreatedAt            time.Time                     `json:"created_at"`
}
