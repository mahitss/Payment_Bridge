package readiness

import (
	"context"

	"claimbridge/services/api/internal/paypal"
)

// CheckStatus represents the discrete result of an individual diagnostic check.
type CheckStatus string

const (
	StatusPass    CheckStatus = "PASS"
	StatusFail    CheckStatus = "FAIL"
	StatusBlocked CheckStatus = "BLOCKED"
	StatusWarn    CheckStatus = "WARN"
	StatusSkipped CheckStatus = "SKIPPED"
)

// Profile represents the evaluation scope.
type Profile string

const (
	ProfileCore       Profile = "core"
	ProfileSubmission Profile = "submission"
	ProfileDemo       Profile = "demo" // alias for submission
)

// CheckResult represents the outcome of an individual diagnostic check.
type CheckResult struct {
	ID             int         `json:"id"`
	Name           string      `json:"name"`
	Category       string      `json:"category"`
	Required       bool        `json:"required"`
	Status         CheckStatus `json:"status"`
	Summary        string      `json:"summary"`
	Detail         string      `json:"detail,omitempty"`
	OperatorAction string      `json:"operator_action,omitempty"`
}

// Summary aggregates all check outcomes for the active profile.
type Summary struct {
	Profile       Profile       `json:"profile"`
	TotalChecks   int           `json:"total_checks"`
	PassedCount   int           `json:"passed_count"`
	FailedCount   int           `json:"failed_count"`
	BlockedCount  int           `json:"blocked_count"`
	WarnCount     int           `json:"warn_count"`
	SkippedCount  int           `json:"skipped_count"`
	RequiredTotal int           `json:"required_total"`
	RequiredPass  int           `json:"required_pass"`
	IsReady       bool          `json:"is_ready"`
	ExitCode      int           `json:"exit_code"`
	Results       []CheckResult `json:"results"`
}

// PayPalReadOnlyClient defines the minimal read-only operations required for readiness verification.
// It strictly excludes any payout initiation, submission, mutation, or cancellation.
type PayPalReadOnlyClient interface {
	GetAccessToken(ctx context.Context) (string, error)
	GetPayoutBatch(ctx context.Context, batchID string) (*paypal.PayoutBatchResponse, error)
	GetPayoutItem(ctx context.Context, itemID string) (*paypal.PayoutItemResponse, error)
}
