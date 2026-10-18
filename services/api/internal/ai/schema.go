package ai

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	PromptVersion = "claimbridge-investigation-v1.0"

	// Confidence Levels
	ConfidenceHigh                 = "HIGH"
	ConfidenceMedium               = "MEDIUM"
	ConfidenceLow                  = "LOW"
	ConfidenceInsufficientEvidence = "INSUFFICIENT_EVIDENCE"

	// Whitelisted Recommendation Actions
	ActionRequestRecipientInfo          = "REQUEST_RECIPIENT_INFORMATION"
	ActionCheckProviderStatus           = "CHECK_PROVIDER_STATUS"
	ActionReviewRecipientDetails        = "REVIEW_RECIPIENT_DETAILS"
	ActionReviewCancellationEligibility = "REVIEW_CANCELLATION_ELIGIBILITY"
	ActionEscalateToOperator            = "ESCALATE_TO_OPERATOR"
	ActionNoActionUntilReconciled       = "NO_ACTION_UNTIL_RECONCILED"
)

var (
	ErrAIUnavailable           = errors.New("ai investigation engine is unavailable")
	ErrAITimeout               = errors.New("ai provider request timed out")
	ErrAIRateLimited           = errors.New("ai provider rate limit exceeded")
	ErrInvalidSchema           = errors.New("ai response failed schema validation")
	ErrInvalidEvidenceRef      = errors.New("ai finding references non-existent or fabricated evidence")
	ErrInvalidRecommendation   = errors.New("ai proposed an unsupported or disallowed recommendation action")
	ErrResponseTooLarge        = errors.New("ai provider response exceeded maximum allowed size")
	ErrPromptInjectionDetected = errors.New("prompt injection detected in evidence payload")
)

var AllowedActions = map[string]bool{
	ActionRequestRecipientInfo:          true,
	ActionCheckProviderStatus:           true,
	ActionReviewRecipientDetails:        true,
	ActionReviewCancellationEligibility: true,
	ActionEscalateToOperator:            true,
	ActionNoActionUntilReconciled:       true,
}

var AllowedConfidence = map[string]bool{
	ConfidenceHigh:                 true,
	ConfidenceMedium:               true,
	ConfidenceLow:                  true,
	ConfidenceInsufficientEvidence: true,
}

// SnapshotOperation represents a durable operation attached to the case.
type SnapshotOperation struct {
	ID                   string     `json:"id"`
	SenderBatchID        string     `json:"sender_batch_id"`
	SenderItemID         string     `json:"sender_item_id"`
	Status               string     `json:"status"`
	PayPalPayoutBatchID  *string    `json:"paypal_payout_batch_id,omitempty"`
	PayPalBatchStatus    *string    `json:"paypal_batch_status,omitempty"`
	PayPalPayoutItemID   *string    `json:"paypal_payout_item_id,omitempty"`
	PayPalItemStatus     *string    `json:"paypal_item_status,omitempty"`
	ErrorMessage         *string    `json:"error_message,omitempty"`
	RequiresDualApproval bool       `json:"requires_dual_approval"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	SubmittedAt          *time.Time `json:"submitted_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	CancelledAt          *time.Time `json:"cancelled_at,omitempty"`
}

// SnapshotRecord represents a recorded PayPal provider response snapshot.
type SnapshotRecord struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id"`
	Provider    string    `json:"provider"`
	BatchStatus string    `json:"batch_status"`
	ItemStatus  *string   `json:"item_status,omitempty"`
	RecordedAt  time.Time `json:"recorded_at"`
}

// InvestigationSnapshot is the normalized, privacy-minimized snapshot sent to the model.
type InvestigationSnapshot struct {
	CaseID               string              `json:"case_id"`
	Reference            string              `json:"reference"`
	Currency             string              `json:"currency"`
	AmountCents          int64               `json:"amount_cents"`
	AmountFormatted      string              `json:"amount_formatted"`
	CaseStatus           string              `json:"case_status"`
	RiskScore            float64             `json:"risk_score"`
	DualApprovalRequired bool                `json:"dual_approval_required"`
	DescriptionSanitized string              `json:"description"`
	RecipientEmailMasked string              `json:"recipient_email_masked"`
	RecipientName        string              `json:"recipient_name"`
	CreatedAt            string              `json:"created_at"`
	UpdatedAt            string              `json:"updated_at"`
	Operations           []SnapshotOperation `json:"operations"`
	Snapshots            []SnapshotRecord    `json:"snapshots"`
	ValidEvidenceIDs     []string            `json:"valid_evidence_ids"`
}

// InvestigationFinding represents a single finding with evidence links.
type InvestigationFinding struct {
	Explanation        string   `json:"explanation"`
	EvidenceReferences []string `json:"evidence_references"`
}

// InvestigationRecommendation represents an advisory next step.
type InvestigationRecommendation struct {
	Action                       string `json:"action"`
	Reason                       string `json:"reason"`
	RequiresHumanReview          bool   `json:"requires_human_review"`
	DeterministicPolicyPermitted bool   `json:"deterministic_policy_permitted"`
	PolicyRuleID                 string `json:"policy_rule_id,omitempty"`
}

// InvestigationResult is the structured, validated output from inference.
type InvestigationResult struct {
	Summary              string                        `json:"summary"`
	ObservedFacts        []string                      `json:"observed_facts"`
	Findings             []InvestigationFinding        `json:"findings"`
	RecommendedNextSteps []InvestigationRecommendation `json:"recommended_next_steps"`
	MissingInformation   []string                      `json:"missing_information"`
	RiskFlags            []string                      `json:"risk_flags"`
	Confidence           string                        `json:"confidence"`
	Limitations          []string                      `json:"limitations"`
	RequiresHumanReview  bool                          `json:"requires_human_review"`
	RawResponse          map[string]interface{}        `json:"raw_response,omitempty"`
}

// IsValidAction checks whether an action is in the whitelisted set.
func IsValidAction(action string) bool {
	return AllowedActions[strings.TrimSpace(action)]
}

// IsValidConfidence checks whether confidence is in the whitelisted set.
func IsValidConfidence(confidence string) bool {
	return AllowedConfidence[strings.TrimSpace(confidence)]
}

// MaskEmail masks sensitive portions of an email for inference minimization.
// e.g. "claimant@example.com" -> "c***t@example.com"
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "[REDACTED_EMAIL]"
	}
	user := parts[0]
	domain := parts[1]
	if len(user) <= 2 {
		return "*@" + domain
	}
	return string(user[0]) + "***" + string(user[len(user)-1]) + "@" + domain
}

// BuildValidEvidenceIDs aggregates all valid verifiable identifiers from the snapshot.
func BuildValidEvidenceIDs(caseID string, ops []SnapshotOperation, snaps []SnapshotRecord) []string {
	ids := make([]string, 0, 1+len(ops)*4+len(snaps))
	if caseID != "" {
		ids = append(ids, caseID)
	}
	for _, op := range ops {
		if op.ID != "" {
			ids = append(ids, op.ID)
		}
		if op.SenderBatchID != "" {
			ids = append(ids, op.SenderBatchID)
		}
		if op.SenderItemID != "" {
			ids = append(ids, op.SenderItemID)
		}
		if op.PayPalPayoutBatchID != nil && *op.PayPalPayoutBatchID != "" {
			ids = append(ids, *op.PayPalPayoutBatchID)
		}
		if op.PayPalPayoutItemID != nil && *op.PayPalPayoutItemID != "" {
			ids = append(ids, *op.PayPalPayoutItemID)
		}
	}
	for _, s := range snaps {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// ValidateGrounding verifies that:
// 1. All findings cite valid evidence references that exist in the snapshot.
// 2. All recommendation actions are within the allowed whitelist.
// 3. Confidence level is valid.
// 4. Required fields are non-empty.
func ValidateGrounding(res *InvestigationResult, snapshot *InvestigationSnapshot) error {
	if strings.TrimSpace(res.Summary) == "" {
		return fmt.Errorf("%w: summary cannot be empty", ErrInvalidSchema)
	}
	if !IsValidConfidence(res.Confidence) {
		return fmt.Errorf("%w: invalid confidence level %q", ErrInvalidSchema, res.Confidence)
	}

	validIDSet := make(map[string]bool, len(snapshot.ValidEvidenceIDs))
	for _, id := range snapshot.ValidEvidenceIDs {
		validIDSet[id] = true
	}

	// Validate Findings and Evidence References
	for i, f := range res.Findings {
		if strings.TrimSpace(f.Explanation) == "" {
			return fmt.Errorf("%w: finding[%d] explanation cannot be empty", ErrInvalidSchema, i)
		}
		if len(f.EvidenceReferences) == 0 {
			return fmt.Errorf("%w: finding[%d] has no evidence references", ErrInvalidEvidenceRef, i)
		}
		for _, ref := range f.EvidenceReferences {
			trimmed := strings.TrimSpace(ref)
			if trimmed == "" || !validIDSet[trimmed] {
				return fmt.Errorf("%w: finding[%d] references unknown or fabricated identifier %q", ErrInvalidEvidenceRef, i, ref)
			}
		}
	}

	// Validate Recommendations
	for i, r := range res.RecommendedNextSteps {
		if !IsValidAction(r.Action) {
			return fmt.Errorf("%w: recommendation[%d] action %q is not in the allowed whitelist", ErrInvalidRecommendation, i, r.Action)
		}
		if strings.TrimSpace(r.Reason) == "" {
			return fmt.Errorf("%w: recommendation[%d] reason cannot be empty", ErrInvalidSchema, i)
		}
	}

	return nil
}
