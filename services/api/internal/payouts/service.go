package payouts

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

var (
	ErrInvalidAmount                     = errors.New("amount must be a positive integer in minor units (cents)")
	ErrAmountExceedsCeiling              = errors.New("amount exceeds maximum allowed payout ceiling ($500.00 USD)")
	ErrUnsupportedCurrency               = errors.New("unsupported currency: only USD is supported in sandbox milestone")
	ErrInvalidRecipientEmail             = errors.New("invalid recipient email address")
	ErrInvalidRecipientName              = errors.New("recipient name cannot be empty")
	ErrInvalidTitle                      = errors.New("title cannot be empty")
	ErrSelfApprovalForbidden             = errors.New("self-approval forbidden: dual-custody requires a distinct authorized approver")
	ErrOperationNotApproved              = db.ErrOperationNotApproved
	ErrOperationAlreadySubmitted         = db.ErrOperationAlreadySubmitted
	ErrNotEligibleForCancellation        = errors.New("operation is not eligible for cancellation; only unclaimed payouts can be cancelled")
	ErrNoOperationsToSync                = errors.New("no payout operations found for this case to synchronize")
	ErrActiveOperationExists             = db.ErrActivePayoutOperationConflict
	ErrOperationCannotBeSubmitted        = db.ErrOperationCannotBeSubmitted
	ErrSubmissionClaimConflict           = db.ErrSubmissionClaimConflict
	ErrCaseInTerminalState               = errors.New("cannot perform operation on case in terminal status")
	ErrOperationAlreadyCancelled         = errors.New("payout operation is already cancelled")
	ErrFixtureExternalOperationForbidden = errors.New("forbidden on synthetic demo fixtures: cannot perform external PayPal operation on synthetic demo fixture: demo fixtures are strictly isolated from live financial destinations")
	ErrEvidenceNotFound                  = errors.New("evidence reference not found")
	ErrNoPayPalBatchID                   = errors.New("cannot sync payout case: no paypal payout batch id known; recovery action or manual reconciliation required")
	ErrManualReconciliationRequired      = errors.New("manual reconciliation required: cannot safely recover ambiguous payout submission")
	ErrRecoveryAlreadyInProgress         = db.ErrRecoveryAlreadyInProgress
	ErrFingerprintMismatch               = errors.New("security violation: reconstructed payout request does not match persisted submission fingerprint")
	ErrFingerprintMissing                = errors.New("missing submission fingerprint: ambiguous operation cannot be replayed without cryptographic evidence of intent")
	ErrOperationNotEligibleForRecovery   = errors.New("operation is not eligible for recovery: must be in RECONCILIATION_REQUIRED state without a known paypal batch id")
	ErrRecoveryClaimConflict             = db.ErrRecoveryClaimConflict
	ErrStaleRecoveryClaim                = db.ErrStaleRecoveryClaim
	ErrRecoveryLeaseExpired              = db.ErrRecoveryLeaseExpired
	ErrRecoveryWindowExpired             = db.ErrRecoveryWindowExpired
	ErrSubmissionTimestampMissing        = db.ErrSubmissionTimestampMissing
	ErrSubmissionTimestampInvalid        = db.ErrSubmissionTimestampInvalid
)

// PayPalDuplicateSafetyWindowCutoff is ClaimBridge's conservative policy cutoff for recovery replays.
// PayPal documents sender_batch_id duplicate protection within 30 days.
// ClaimBridge enforces a 27-day cutoff to maintain a 3-day safety margin against provider expiration.
// NOTE: This 27-day threshold is ClaimBridge's conservative internal policy, not a PayPal guarantee.
const PayPalDuplicateSafetyWindowCutoff = 27 * 24 * time.Hour

// AllowedSubmissionClockSkew defines the maximum allowable future timestamp on submitted_at (5 minutes).
const AllowedSubmissionClockSkew = 5 * time.Minute

// RecoveryLeaseDuration defines the duration an in-flight recovery attempt holds exclusive ownership (5 minutes).
const RecoveryLeaseDuration = 5 * time.Minute

// EvidenceField represents an allowlisted safe key-value pair for evidence inspection.
type EvidenceField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// EvidenceResolution contains the verified, privacy-minimized source record fields.
type EvidenceResolution struct {
	ReferenceID        string          `json:"reference_id"`
	SourceType         string          `json:"source_type"` // PAYOUT_CASE, PAYOUT_OPERATION, STATUS_SNAPSHOT
	DisplayLabel       string          `json:"display_label"`
	Fields             []EvidenceField `json:"fields"`
	RecordedAt         *string         `json:"recorded_at,omitempty"`
	IsSyntheticFixture bool            `json:"is_synthetic_fixture"`
	ResolutionStatus   string          `json:"resolution_status"` // RESOLVED, UNAVAILABLE
}

// Service encapsulates business logic for Payout Cases, Operations, and PayPal Sandbox integration.
type Service struct {
	cfg          *config.Config
	repo         db.Repository
	policyEngine *policy.Engine
	paypalClient *paypal.Client
}

func NewService(cfg *config.Config, repo db.Repository, policyEngine *policy.Engine, paypalClient *paypal.Client) *Service {
	return &Service{
		cfg:          cfg,
		repo:         repo,
		policyEngine: policyEngine,
		paypalClient: paypalClient,
	}
}

// CheckConnection probes PayPal Sandbox authentication.
func (s *Service) CheckConnection(ctx context.Context) *paypal.ConnectionStatus {
	return s.paypalClient.CheckConnection(ctx)
}

type CreateCaseRequest struct {
	Reference      string  `json:"reference"`
	RecipientEmail string  `json:"recipient_email"`
	RecipientName  string  `json:"recipient_name"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	Currency       string  `json:"currency"`
	AmountCents    int64   `json:"amount_cents"`
	RiskScore      float64 `json:"risk_score"`
}

// CreatePayoutCase validates inputs, evaluates initial policy, and persists a new payout case.
func (s *Service) CreatePayoutCase(ctx context.Context, user *db.User, org *db.Organization, req CreateCaseRequest) (*db.PayoutCase, error) {
	if req.AmountCents <= 0 {
		return nil, ErrInvalidAmount
	}
	if req.AmountCents > 50000 { // $500.00 USD ceiling
		return nil, ErrAmountExceedsCeiling
	}

	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "USD"
	}
	if currency != "USD" {
		return nil, ErrUnsupportedCurrency
	}

	email := strings.ToLower(strings.TrimSpace(req.RecipientEmail))
	if email == "" || !strings.Contains(email, "@") {
		return nil, ErrInvalidRecipientEmail
	}

	name := strings.TrimSpace(req.RecipientName)
	if name == "" {
		return nil, ErrInvalidRecipientName
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, ErrInvalidTitle
	}

	ref := strings.TrimSpace(req.Reference)
	if ref == "" {
		ref = fmt.Sprintf("CASE-%d-%s", time.Now().UTC().Unix(), crypto.GenerateUUID()[:6])
	}

	// Policy check for dual approval: amount > $250.00 or risk > 20.0
	requiresDual := req.AmountCents > 25000 || req.RiskScore > 20.0

	pCase := &db.PayoutCase{
		OrganizationID:       org.ID,
		CreatedByUserID:      user.ID,
		Reference:            ref,
		RecipientEmail:       email,
		RecipientName:        name,
		Title:                title,
		Description:          strings.TrimSpace(req.Description),
		Currency:             currency,
		AmountCents:          req.AmountCents,
		Status:               "DRAFT",
		RiskScore:            req.RiskScore,
		DualApprovalRequired: requiresDual,
	}

	if err := s.repo.CreatePayoutCase(ctx, pCase); err != nil {
		return nil, err
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_CASE_CREATED",
		Metadata: map[string]interface{}{
			"case_id":                pCase.ID,
			"reference":              pCase.Reference,
			"amount_cents":           pCase.AmountCents,
			"dual_approval_required": requiresDual,
		},
	})

	return pCase, nil
}

// GetPayoutCase retrieves a case and its operations scoped strictly to the organization.
func (s *Service) GetPayoutCase(ctx context.Context, orgID, caseID string) (*db.PayoutCase, []db.PayoutOperation, error) {
	pCase, err := s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
	if err != nil {
		return nil, nil, err
	}

	ops, err := s.repo.ListPayoutOperationsByCaseID(ctx, orgID, caseID)
	if err != nil {
		return nil, nil, err
	}

	return pCase, ops, nil
}

// ListPayoutCases retrieves paginated payout cases for an organization.
func (s *Service) ListPayoutCases(ctx context.Context, orgID string, limit, offset int) ([]db.PayoutCase, int, error) {
	return s.repo.ListPayoutCasesByOrg(ctx, orgID, limit, offset)
}

// generateSafeCorrelationID ensures generated identifiers obey provider length limits (e.g. PayPal's 63-character limit for sender_item_id).
func generateSafeCorrelationID(prefix, reference, suffix string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 63
	}
	fixedLen := len(prefix) + len(suffix)
	if fixedLen >= maxLen {
		combined := prefix + reference + suffix
		if len(combined) > maxLen {
			return combined[:maxLen]
		}
		return combined
	}

	allowedRefLen := maxLen - fixedLen
	ref := reference
	if len(ref) > allowedRefLen {
		ref = ref[:allowedRefLen]
	}
	return prefix + ref + suffix
}

// CanonicalSubmissionPayload defines the unambiguous, fixed-schema canonical representation
// of all outbound PayPal payout request fields affecting financial authorization.
type CanonicalSubmissionPayload struct {
	Version       string `json:"_version"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	EmailSubject  string `json:"email_subject"`
	Note          string `json:"note"`
	Recipient     string `json:"recipient"`
	RecipientType string `json:"recipient_type"`
	SenderBatchID string `json:"sender_batch_id"`
	SenderItemID  string `json:"sender_item_id"`
}

// ComputeSubmissionFingerprint generates a deterministic SHA-256 hex digest of a canonical representation
// matching the exact outbound PayPal payout request fields transmitted to the provider.
// Invariants:
// - Matches the exact fields actually sent to PayPal without silent normalization.
// - Preserves exact whitespace in note, email_subject, and all fields to detect any request-body alterations.
// - Does not store or contain credentials, access tokens, or sensitive raw payloads.
func ComputeSubmissionFingerprint(req paypal.CreatePayoutRequest) string {
	payload := CanonicalSubmissionPayload{
		Version:       "v2",
		Amount:        req.Amount,
		Currency:      req.Currency,
		EmailSubject:  req.EmailSubject,
		Note:          req.Note,
		Recipient:     req.Recipient,
		RecipientType: "EMAIL",
		SenderBatchID: req.SenderBatchID,
		SenderItemID:  req.SenderItemID,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		h := sha256.Sum256([]byte(fmt.Sprintf("%v", payload)))
		return "v2:" + hex.EncodeToString(h[:])
	}
	h := sha256.Sum256(b)
	return "v2:" + hex.EncodeToString(h[:])
}

// computeSubmissionFingerprintV1 calculates legacy v1 delimited fingerprint for backward compatibility.
func computeSubmissionFingerprintV1(req paypal.CreatePayoutRequest) string {
	canonical := fmt.Sprintf("v1|sender_batch_id=%s|sender_item_id=%s|recipient=%s|amount=%s|currency=%s|note=%s|email_subject=%s",
		strings.TrimSpace(req.SenderBatchID),
		strings.TrimSpace(req.SenderItemID),
		strings.ToLower(strings.TrimSpace(req.Recipient)),
		strings.TrimSpace(req.Amount),
		strings.ToUpper(strings.TrimSpace(req.Currency)),
		strings.TrimSpace(req.Note),
		strings.TrimSpace(req.EmailSubject),
	)
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

// VerifySubmissionFingerprint checks a stored fingerprint against the reconstructed payout request
// using constant-time comparison to prevent timing attacks. Fails closed on missing or mismatched fingerprints.
func VerifySubmissionFingerprint(storedFP string, req paypal.CreatePayoutRequest) error {
	trimmed := strings.TrimSpace(storedFP)
	if trimmed == "" {
		return ErrFingerprintMissing
	}

	if strings.HasPrefix(trimmed, "v2:") {
		expected := ComputeSubmissionFingerprint(req)
		if subtle.ConstantTimeCompare([]byte(trimmed), []byte(expected)) != 1 {
			return ErrFingerprintMismatch
		}
		return nil
	}

	// Backward compatibility with legacy v1 fingerprint (64-char hex or v1: prefix)
	if strings.HasPrefix(trimmed, "v1:") || len(trimmed) == 64 {
		rawHex := strings.TrimPrefix(trimmed, "v1:")
		expectedV1 := computeSubmissionFingerprintV1(req)
		if subtle.ConstantTimeCompare([]byte(rawHex), []byte(expectedV1)) != 1 {
			return ErrFingerprintMismatch
		}
		return nil
	}

	return fmt.Errorf("%w: unsupported fingerprint version in stored hash", ErrFingerprintMismatch)
}

// InitiatePayoutOperation prepares a new durable operation with unique idempotency keys.
func (s *Service) InitiatePayoutOperation(ctx context.Context, user *db.User, org *db.Organization, caseID string) (*db.PayoutOperation, error) {
	pCase, err := s.repo.GetPayoutCaseByID(ctx, org.ID, caseID)
	if err != nil {
		return nil, err
	}

	if pCase.Status == "COMPLETED" || pCase.Status == "CANCELLED" {
		return nil, fmt.Errorf("%w: cannot initiate payout on case with status %s", ErrCaseInTerminalState, pCase.Status)
	}

	// Invariant: Payout case must not have multiple concurrent active operations
	existingOps, err := s.repo.ListPayoutOperationsByCaseID(ctx, org.ID, caseID)
	if err != nil {
		return nil, fmt.Errorf("failed listing existing payout operations: %w", err)
	}
	for _, existing := range existingOps {
		if existing.Status != "CANCELLED" && existing.Status != "FAILED" {
			return nil, fmt.Errorf("%w: operation %s has status %s", ErrActiveOperationExists, existing.ID, existing.Status)
		}
	}

	// Generate deterministic idempotency keys obeying provider length limits (63 chars max)
	randomSuffix := crypto.GenerateUUID()[:8]
	senderBatchID := generateSafeCorrelationID("CB-BATCH-", pCase.Reference, "-"+randomSuffix, 63)
	senderItemID := generateSafeCorrelationID("CB-ITEM-", pCase.Reference, "-"+randomSuffix, 63)

	status := "PREPARED"
	if pCase.DualApprovalRequired {
		status = "AWAITING_APPROVAL"
	}

	op := &db.PayoutOperation{
		PayoutCaseID:         pCase.ID,
		OrganizationID:       org.ID,
		InitiatedByUserID:    user.ID,
		SenderBatchID:        senderBatchID,
		SenderItemID:         senderItemID,
		AmountCents:          pCase.AmountCents,
		Currency:             pCase.Currency,
		Status:               status,
		RequiresDualApproval: pCase.DualApprovalRequired,
		IsSyntheticFixture:   pCase.IsSyntheticFixture,
	}

	if err := s.repo.CreatePayoutOperation(ctx, op); err != nil {
		if errors.Is(err, db.ErrActivePayoutOperationConflict) {
			return nil, fmt.Errorf("%w: concurrent initiation conflict", ErrActiveOperationExists)
		}
		return nil, fmt.Errorf("failed to persist payout operation: %w", err)
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_OPERATION_INITIATED",
		Metadata: map[string]interface{}{
			"operation_id":           op.ID,
			"case_id":                pCase.ID,
			"sender_batch_id":        senderBatchID,
			"status":                 status,
			"requires_dual_approval": pCase.DualApprovalRequired,
		},
	})

	return op, nil
}

// ApprovePayoutOperation records dual approval ensuring separation of duties (no self-approval).
func (s *Service) ApprovePayoutOperation(ctx context.Context, user *db.User, org *db.Organization, opID string) (*db.PayoutOperation, error) {
	op, err := s.repo.GetPayoutOperationByID(ctx, org.ID, opID)
	if err != nil {
		return nil, err
	}

	pCase, err := s.repo.GetPayoutCaseByID(ctx, org.ID, op.PayoutCaseID)
	if err != nil {
		return nil, err
	}
	if pCase.Status == "COMPLETED" || pCase.Status == "CANCELLED" {
		return nil, fmt.Errorf("%w: cannot approve operation for case with status %s", ErrCaseInTerminalState, pCase.Status)
	}

	if op.RequiresDualApproval && op.InitiatedByUserID == user.ID {
		return nil, ErrSelfApprovalForbidden
	}

	if op.Status != "AWAITING_APPROVAL" {
		return nil, fmt.Errorf("%w: operation status is %s (must be AWAITING_APPROVAL)", db.ErrInvalidOperationTransition, op.Status)
	}

	if err := s.repo.ApprovePayoutOperation(ctx, org.ID, opID, user.ID); err != nil {
		return nil, err
	}

	op, err = s.repo.GetPayoutOperationByID(ctx, org.ID, opID)
	if err != nil {
		return nil, err
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_OPERATION_APPROVED",
		Metadata: map[string]interface{}{
			"operation_id":        op.ID,
			"case_id":             op.PayoutCaseID,
			"approved_by_user_id": user.ID,
		},
	})

	return op, nil
}

// SubmitPayoutOperation issues the actual payout request to PayPal Sandbox.
func (s *Service) SubmitPayoutOperation(ctx context.Context, user *db.User, org *db.Organization, caseID, opID string) (*db.PayoutOperation, error) {
	pCase, err := s.repo.GetPayoutCaseByID(ctx, org.ID, caseID)
	if err != nil {
		return nil, err
	}

	op, err := s.repo.GetPayoutOperationByID(ctx, org.ID, opID)
	if err != nil {
		return nil, err
	}

	// Synthetic fixture boundary: demo fixtures cannot be submitted to live PayPal
	if pCase.IsSyntheticFixture || op.IsSyntheticFixture {
		return nil, ErrFixtureExternalOperationForbidden
	}

	if pCase.Status == "COMPLETED" || pCase.Status == "CANCELLED" {
		return nil, fmt.Errorf("%w: cannot submit operation for case with status %s", ErrCaseInTerminalState, pCase.Status)
	}

	// Invariant check: operation must be in a submittable state
	if op.RequiresDualApproval {
		if op.Status != "APPROVED" {
			if op.Status == "SUBMITTING" || op.Status == "SUBMITTED" || op.Status == "SUCCEEDED" {
				return nil, ErrOperationAlreadySubmitted
			}
			return nil, ErrOperationNotApproved
		}
	} else {
		if op.Status != "PREPARED" {
			if op.Status == "SUBMITTING" || op.Status == "SUBMITTED" || op.Status == "SUCCEEDED" {
				return nil, ErrOperationAlreadySubmitted
			}
			return nil, fmt.Errorf("%w: operation status is %s (must be PREPARED)", ErrOperationCannotBeSubmitted, op.Status)
		}
	}

	// Verify no other operation on the case was already submitted or succeeded
	existingOps, err := s.repo.ListPayoutOperationsByCaseID(ctx, org.ID, caseID)
	if err != nil {
		return nil, fmt.Errorf("failed listing case payout operations: %w", err)
	}
	for _, existing := range existingOps {
		if existing.ID != op.ID {
			if existing.Status == "SUBMITTING" || existing.Status == "SUBMITTED" || existing.Status == "SUCCEEDED" || existing.Status == "RECONCILIATION_REQUIRED" {
				return nil, fmt.Errorf("%w: case already has active submitted operation %s in status %s", ErrOperationAlreadySubmitted, existing.ID, existing.Status)
			}
		}
	}

	// 1. Compute canonical outbound request and submission fingerprint
	amountStr := fmt.Sprintf("%.2f", float64(op.AmountCents)/100.0)
	paypalReq := paypal.CreatePayoutRequest{
		SenderBatchID: op.SenderBatchID,
		SenderItemID:  op.SenderItemID,
		Recipient:     pCase.RecipientEmail,
		Amount:        amountStr,
		Currency:      op.Currency,
		Note:          pCase.Title,
		EmailSubject:  fmt.Sprintf("Payment for Claim %s", pCase.Reference),
	}

	fp := ComputeSubmissionFingerprint(paypalReq)
	now := time.Now().UTC()

	// Atomic conditional repository transition to SUBMITTING before outbound call
	claimedOp, err := s.repo.ClaimPayoutOperationForSubmission(ctx, org.ID, op.ID, fp, now)
	if err != nil {
		if errors.Is(err, db.ErrSubmissionClaimConflict) {
			return nil, ErrOperationAlreadySubmitted
		}
		return nil, err
	}
	op = claimedOp

	// 2. Call PayPal Sandbox - only the single winning caller dispatches outbound POST
	paypalResp, err := s.paypalClient.CreatePayout(ctx, paypalReq)
	if err != nil {
		// Existing duplicate-response HATEOAS recovery behavior
		var dupErr *paypal.DuplicatePayoutError
		if errors.As(err, &dupErr) {
			reconOp := *op
			reconOp.Status = "RECONCILIATION_REQUIRED"
			if dupErr.OriginalBatchID != "" {
				reconOp.PayPalPayoutBatchID = &dupErr.OriginalBatchID
			}
			errMsg := fmt.Sprintf("Duplicate submission detected: %s", dupErr.Message)
			reconOp.ErrorMessage = &errMsg
			if updateErr := s.repo.UpdatePayoutOperation(ctx, &reconOp); updateErr != nil {
				slog.ErrorContext(ctx, "Failed to persist RECONCILIATION_REQUIRED on duplicate response; preserving SUBMITTING",
					slog.String("operation_id", op.ID),
					slog.Any("update_error", updateErr),
					slog.Any("duplicate_error", dupErr))
				return op, fmt.Errorf("duplicate provider response received but failed to persist RECONCILIATION_REQUIRED state: %w", updateErr)
			}
			*op = reconOp
			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				UserID:         &user.ID,
				OrganizationID: &org.ID,
				EventType:      "PAYOUT_AMBIGUOUS_DUPLICATE_DETECTED",
				Metadata: map[string]interface{}{
					"operation_id":           op.ID,
					"sender_batch_id":        op.SenderBatchID,
					"paypal_payout_batch_id": dupErr.OriginalBatchID,
				},
			})
			return op, err
		}

		// Ambiguous outcome: timeouts, connection resets, response read failures, HTTP 500/502/503/504
		if paypal.IsAmbiguousOutcome(err) {
			errMsg := err.Error()
			reconOp := *op
			reconOp.Status = "RECONCILIATION_REQUIRED"
			reconOp.ErrorMessage = &errMsg
			if updateErr := s.repo.UpdatePayoutOperation(ctx, &reconOp); updateErr != nil {
				// If persistence fails, preserve and report the unresolved SUBMITTING state honestly
				slog.ErrorContext(ctx, "Failed to persist RECONCILIATION_REQUIRED status; preserving unresolved SUBMITTING state",
					slog.String("operation_id", op.ID),
					slog.String("sender_batch_id", op.SenderBatchID),
					slog.Any("update_error", updateErr),
					slog.Any("provider_error", err))
				_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
					UserID:         &user.ID,
					OrganizationID: &org.ID,
					EventType:      "PAYOUT_RECONCILIATION_TRANSITION_FAILED",
					Metadata: map[string]interface{}{
						"operation_id":    op.ID,
						"sender_batch_id": op.SenderBatchID,
						"provider_error":  errMsg,
						"persist_error":   updateErr.Error(),
						"current_status":  op.Status, // "SUBMITTING"
					},
				})
				return op, fmt.Errorf("ambiguous provider response occurred (%w) but failed to persist RECONCILIATION_REQUIRED state: %v", err, updateErr)
			}
			*op = reconOp
			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				UserID:         &user.ID,
				OrganizationID: &org.ID,
				EventType:      "PAYOUT_AMBIGUOUS_TIMEOUT",
				Metadata: map[string]interface{}{
					"operation_id":    op.ID,
					"sender_batch_id": op.SenderBatchID,
					"error":           errMsg,
				},
			})
			return op, err
		}

		// Definitive error: HTTP 400, 401, 403, 422, etc.
		failedOp := *op
		failedOp.Status = "FAILED"
		errMsg := err.Error()
		failedOp.ErrorMessage = &errMsg
		if fe, ok := paypal.AsAPIForbiddenError(err); ok {
			failedOp.RawProviderResponse = map[string]interface{}{
				"error_name": fe.Name,
				"issue":      fe.Issue,
				"debug_id":   fe.DebugID,
			}
		}
		if updateErr := s.repo.UpdatePayoutOperation(ctx, &failedOp); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to persist FAILED status on definitive rejection; preserving unresolved SUBMITTING state",
				slog.String("operation_id", op.ID),
				slog.String("sender_batch_id", op.SenderBatchID),
				slog.Any("update_error", updateErr),
				slog.Any("provider_error", err))
			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				UserID:         &user.ID,
				OrganizationID: &org.ID,
				EventType:      "PAYOUT_FAILED_TRANSITION_FAILED",
				Metadata: map[string]interface{}{
					"operation_id":    op.ID,
					"sender_batch_id": op.SenderBatchID,
					"provider_error":  errMsg,
					"persist_error":   updateErr.Error(),
					"current_status":  op.Status, // "SUBMITTING"
				},
			})
			return op, fmt.Errorf("definitive provider error received (%w) but failed to persist FAILED state: %v", err, updateErr)
		}
		*op = failedOp
		return op, err
	}

	// 3. Success: PayPal accepted batch
	op.Status = "SUBMITTED"
	op.PayPalPayoutBatchID = &paypalResp.BatchHeader.PayoutBatchID
	op.PayPalBatchStatus = &paypalResp.BatchHeader.BatchStatus
	op.RawProviderResponse = map[string]interface{}{
		"payout_batch_id": paypalResp.BatchHeader.PayoutBatchID,
		"batch_status":    paypalResp.BatchHeader.BatchStatus,
		"time_created":    paypalResp.BatchHeader.TimeCreated,
	}

	if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to record submitted operation: %w", err)
	}

	// Update Case status to SUBMITTED
	if err := s.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED"); err != nil {
		return nil, fmt.Errorf("failed to update payout case status: %w", err)
	}

	// Record snapshot
	if err := s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    org.ID,
		Provider:          "PAYPAL",
		BatchStatus:       paypalResp.BatchHeader.BatchStatus,
		RawResponse:       op.RawProviderResponse,
	}); err != nil {
		return nil, fmt.Errorf("failed to record status snapshot: %w", err)
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_SUBMITTED_TO_PAYPAL",
		Metadata: map[string]interface{}{
			"operation_id":           op.ID,
			"sender_batch_id":        op.SenderBatchID,
			"paypal_payout_batch_id": paypalResp.BatchHeader.PayoutBatchID,
			"batch_status":           paypalResp.BatchHeader.BatchStatus,
		},
	})

	return op, nil
}

// SyncPayoutCase queries PayPal for the latest status of the case's payout operation.
// Invariants:
// - Never calls PayPal GET /v1/payments/payouts/{id} with internal SenderBatchID.
// - Returns ErrNoPayPalBatchID if the operation has no known PayPalPayoutBatchID.
// - Preserves item-authoritative status evaluation rules.
func (s *Service) SyncPayoutCase(ctx context.Context, orgID, caseID string) (*db.PayoutCase, *db.PayoutOperation, error) {
	pCase, ops, err := s.GetPayoutCase(ctx, orgID, caseID)
	if err != nil {
		return nil, nil, err
	}

	if len(ops) == 0 {
		return pCase, nil, ErrNoOperationsToSync
	}

	// Get latest operation
	op := ops[0]

	// Look up by PayPalPayoutBatchID only; never query GET /v1/payments/payouts/{id} with internal SenderBatchID
	if op.PayPalPayoutBatchID == nil || strings.TrimSpace(*op.PayPalPayoutBatchID) == "" {
		return pCase, &op, ErrNoPayPalBatchID
	}
	batchLookupID := strings.TrimSpace(*op.PayPalPayoutBatchID)

	// Synthetic fixture boundary: demo fixtures cannot be synchronized with live PayPal
	if pCase.IsSyntheticFixture || op.IsSyntheticFixture {
		return pCase, &op, ErrFixtureExternalOperationForbidden
	}

	batchResp, err := s.paypalClient.GetPayoutBatch(ctx, batchLookupID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query paypal payout batch: %w", err)
	}

	return s.reconcileBatchResponse(ctx, nil, orgID, caseID, &op, batchResp, "PAYOUT_STATUS_SYNCHRONIZED", nil, "")
}

// reconcileBatchResponse applies authoritative item-level status reconciliation rules
// from PayPal batch responses to ensure completion is determined from actual recipient evidence.
func (s *Service) reconcileBatchResponse(
	ctx context.Context,
	user *db.User,
	orgID, caseID string,
	op *db.PayoutOperation,
	batchResp *paypal.PayoutBatchResponse,
	auditEventType string,
	extraAuditMetadata map[string]interface{},
	recoveryAttemptID string,
) (*db.PayoutCase, *db.PayoutOperation, error) {
	batchResp.Normalize()
	if batchResp.BatchHeader.PayoutBatchID != "" {
		op.PayPalPayoutBatchID = &batchResp.BatchHeader.PayoutBatchID
	}

	batchStatus := strings.ToUpper(strings.TrimSpace(batchResp.BatchHeader.BatchStatus))
	op.PayPalBatchStatus = &batchResp.BatchHeader.BatchStatus
	now := time.Now().UTC()

	// 1. Authoritative Item Correlation
	var matchedItem *paypal.PayoutItem
	var correlationFailed bool
	var correlationReason string

	if len(batchResp.Items) == 0 {
		// No item details returned in batch yet
	} else if op.SenderItemID != "" {
		var matches []paypal.PayoutItem
		for i := range batchResp.Items {
			it := batchResp.Items[i]
			it.Normalize()
			if it.SenderItemID != "" && it.SenderItemID == op.SenderItemID {
				matches = append(matches, it)
			}
		}

		if len(matches) == 1 {
			matchedItem = &matches[0]
		} else if len(matches) > 1 {
			correlationFailed = true
			correlationReason = "multiple items matched sender item id"
		} else {
			// len(matches) == 0: no items had it.SenderItemID == op.SenderItemID
			if len(batchResp.Items) == 1 && batchResp.Items[0].SenderItemID == "" {
				// Historical single-item operation fallback where returned item lacks sender item id
				matchedItem = &batchResp.Items[0]
				matchedItem.Normalize()
			} else {
				correlationFailed = true
				if len(batchResp.Items) > 1 {
					correlationReason = "multiple items returned with no unique sender item id match"
				} else {
					correlationReason = "returned item sender item id does not match operation sender item id"
				}
			}
		}
	} else {
		// Historical operation lacking op.SenderItemID
		if len(batchResp.Items) == 1 {
			matchedItem = &batchResp.Items[0]
			matchedItem.Normalize()
		} else {
			correlationFailed = true
			correlationReason = "multiple items returned for historical operation without sender item id"
		}
	}

	var itemStatus string
	if matchedItem != nil {
		if matchedItem.PayoutItemID != "" {
			op.PayPalPayoutItemID = &matchedItem.PayoutItemID
		}
		itemStatus = strings.ToUpper(strings.TrimSpace(matchedItem.TransactionStatus))
		op.PayPalItemStatus = &matchedItem.TransactionStatus
	}

	// 2. State Machine Mapping
	// CRITICAL RULE: Never mark a payout SUCCEEDED or its case COMPLETED from batch status SUCCESS alone.
	// Status determination is item-authoritative.
	if batchStatus == "DENIED" {
		op.Status = "FAILED"
		errMsg := "PayPal payout batch was denied"
		op.ErrorMessage = &errMsg
		if err := s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "DENIED"); err != nil {
			return nil, nil, fmt.Errorf("failed to update payout case status: %w", err)
		}
	} else if correlationFailed {
		// Multiple items and no unique match: never choose first item arbitrarily; fail closed into RECONCILIATION_REQUIRED
		op.Status = "RECONCILIATION_REQUIRED"
		errMsg := fmt.Sprintf("Payout item reconciliation required: %s", correlationReason)
		op.ErrorMessage = &errMsg
	} else if matchedItem == nil {
		// No items present in batch response yet
		switch batchStatus {
		case "CANCELED":
			if op.Status != "CANCELLED" {
				op.Status = "RECONCILIATION_REQUIRED"
				errMsg := "PayPal payout batch reported canceled without local cancellation record"
				op.ErrorMessage = &errMsg
			}
		case "PENDING", "PROCESSING", "SUCCESS":
			// Batch is pending, processing, or success, but item details are not yet available; do NOT complete
			op.Status = "SUBMITTED"
		default:
			op.Status = "RECONCILIATION_REQUIRED"
			errMsg := fmt.Sprintf("Unexpected PayPal batch status: %s", batchStatus)
			op.ErrorMessage = &errMsg
		}
	} else {
		// Authoritative item status evaluation
		switch itemStatus {
		case "SUCCESS":
			op.Status = "SUCCEEDED"
			op.CompletedAt = &now
			op.ErrorMessage = nil
			if err := s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "COMPLETED"); err != nil {
				return nil, nil, fmt.Errorf("failed to update payout case status: %w", err)
			}
		case "PENDING", "UNCLAIMED":
			// Item pending or unclaimed: keep operation submitted; do not mark case completed
			op.Status = "SUBMITTED"
		case "FAILED", "BLOCKED":
			op.Status = "FAILED"
			errMsg := fmt.Sprintf("PayPal payout item status: %s", itemStatus)
			op.ErrorMessage = &errMsg
			if err := s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "FAILED"); err != nil {
				return nil, nil, fmt.Errorf("failed to update payout case status: %w", err)
			}
		case "CANCELED", "RETURNED":
			// Preserve already-recorded successful local cancellation when PayPal subsequently reports returned/cancelled.
			// Do not confuse a returned item with proof that ClaimBridge initiated cancellation.
			if op.Status == "CANCELLED" {
				// Keep existing CANCELLED status and timestamp
			} else {
				op.Status = "RECONCILIATION_REQUIRED"
				errMsg := fmt.Sprintf("Payout item reconciliation required: provider item status is %s without local cancellation record", itemStatus)
				op.ErrorMessage = &errMsg
			}
		case "ONHOLD", "REFUNDED", "REVERSED":
			// Ambiguous outcome: preserve provider evidence and require human reconciliation
			op.Status = "RECONCILIATION_REQUIRED"
			errMsg := fmt.Sprintf("Payout item reconciliation required: provider item status is %s", itemStatus)
			op.ErrorMessage = &errMsg
		default:
			// Unknown or unexpected item status
			op.Status = "RECONCILIATION_REQUIRED"
			errMsg := fmt.Sprintf("Payout item reconciliation required: unknown provider item status %s", itemStatus)
			op.ErrorMessage = &errMsg
		}
	}

	rawResp := map[string]interface{}{
		"batch_header": batchResp.BatchHeader,
		"items_count":  len(batchResp.Items),
	}
	if matchedItem != nil {
		rawResp["matched_item"] = matchedItem
	}
	if correlationReason != "" {
		rawResp["correlation_reason"] = correlationReason
	}
	op.RawProviderResponse = rawResp

	if recoveryAttemptID != "" {
		if err := s.repo.FinalizeRecoveryOperation(ctx, op, recoveryAttemptID); err != nil {
			return nil, nil, fmt.Errorf("failed to finalize recovery operation: %w", err)
		}
	} else {
		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return nil, nil, fmt.Errorf("failed to update payout operation: %w", err)
		}
	}

	if err := s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    orgID,
		Provider:          "PAYPAL",
		BatchStatus:       batchResp.BatchHeader.BatchStatus,
		ItemStatus:        op.PayPalItemStatus,
		RawResponse:       op.RawProviderResponse,
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to create payout status snapshot: %w", err)
	}

	metadata := map[string]interface{}{
		"operation_id": op.ID,
		"batch_status": batchResp.BatchHeader.BatchStatus,
		"item_status":  op.PayPalItemStatus,
		"op_status":    op.Status,
	}
	for k, v := range extraAuditMetadata {
		metadata[k] = v
	}

	var userID *string
	if user != nil {
		userID = &user.ID
	}
	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         userID,
		OrganizationID: &orgID,
		EventType:      auditEventType,
		Metadata:       metadata,
	})

	pCase, err := s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
	if err != nil {
		return nil, op, fmt.Errorf("failed to retrieve updated payout case: %w", err)
	}
	return pCase, op, nil
}

// RecoverPayoutOperation safely recovers an ambiguous payout operation in RECONCILIATION_REQUIRED state.
// Invariants:
// - Resolves operation and case within authenticated organization.
// - Bounded strictly to RECONCILIATION_REQUIRED with no known PayPalPayoutBatchID.
// - Rejects synthetic fixtures, terminal cases, and unapproved dual-custody operations.
// - Re-verifies canonical submission fingerprint against original authorized intent using constant-time comparison.
// - Time-bounded by 27-day conservative cutoff from persisted original submitted_at timestamp (never reset during recovery).
// - Fails closed on missing or future submission timestamps without contacting PayPal.
// - Atomically claims operation at the database layer (single concurrent winner; loser receives conflict with zero provider mutations).
// - Stale claims (expired leases) enter auditable manual-review path without triggering automatic replay.
// - Preserves original sender_batch_id and sender_item_id without generating replacement IDs.
// - Validates duplicate-error HATEOAS links and feeds recovered batch into authoritative item reconciliation.
func (s *Service) RecoverPayoutOperation(ctx context.Context, user *db.User, org *db.Organization, opID string) (*db.PayoutCase, *db.PayoutOperation, string, error) {
	op, err := s.repo.GetPayoutOperationByID(ctx, org.ID, opID)
	if err != nil {
		return nil, nil, "", err
	}

	pCase, err := s.repo.GetPayoutCaseByID(ctx, org.ID, op.PayoutCaseID)
	if err != nil {
		return nil, nil, "", err
	}

	// 1. Synthetic fixture boundary: demo fixtures cannot be recovered or sent to live PayPal
	if pCase.IsSyntheticFixture || op.IsSyntheticFixture {
		return nil, nil, "", ErrFixtureExternalOperationForbidden
	}

	// 2. Terminal case or operation
	if pCase.Status == "COMPLETED" || pCase.Status == "CANCELLED" || op.Status == "SUCCEEDED" || op.Status == "CANCELLED" {
		return nil, nil, "", fmt.Errorf("%w: cannot recover operation for case in status %s (op status: %s)", ErrCaseInTerminalState, pCase.Status, op.Status)
	}

	// 3. Known PayPal payout batch ID: sync should be used, not recovery replay
	if op.PayPalPayoutBatchID != nil && strings.TrimSpace(*op.PayPalPayoutBatchID) != "" {
		return nil, nil, "", fmt.Errorf("%w: paypal batch id is already known (%s); run sync instead", ErrOperationNotEligibleForRecovery, *op.PayPalPayoutBatchID)
	}

	// 4. Dual-custody authorization check: must have been approved if required
	if op.RequiresDualApproval && (op.ApprovedByUserID == nil || *op.ApprovedByUserID == "") {
		return nil, nil, "", ErrOperationNotApproved
	}

	// 5. Operation Status and Recovery In-Progress Check
	now := time.Now().UTC()
	if op.Status == "RECOVERING" {
		if op.RecoveryLeaseExpiresAt != nil && now.After(*op.RecoveryLeaseExpiresAt) {
			attemptID := ""
			if op.RecoveryAttemptID != nil {
				attemptID = *op.RecoveryAttemptID
			}
			errMsg := "Stale recovery claim: recovery lease expired; manual reconciliation required"
			_ = s.repo.TransitionRecoveryToManualReview(ctx, org.ID, op.ID, attemptID, errMsg, nil)
			op.Status = "MANUAL_RECONCILIATION_REQUIRED"
			op.ErrorMessage = &errMsg

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				UserID:         &user.ID,
				OrganizationID: &org.ID,
				EventType:      "PAYOUT_RECOVERY_BLOCKED",
				Metadata: map[string]interface{}{
					"operation_id":     op.ID,
					"reason":           "stale_recovery_claim",
					"lease_expires_at": op.RecoveryLeaseExpiresAt.Format(time.RFC3339),
					"outcome":          "MANUAL_RECONCILIATION_REQUIRED",
				},
			})
			return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrStaleRecoveryClaim
		}
		return nil, nil, "", ErrRecoveryAlreadyInProgress
	}

	if op.Status == "MANUAL_RECONCILIATION_REQUIRED" {
		return nil, op, "MANUAL_RECONCILIATION_REQUIRED", fmt.Errorf("%w: operation requires manual review and cannot be replayed", ErrOperationNotEligibleForRecovery)
	}

	if op.Status != "RECONCILIATION_REQUIRED" {
		return nil, nil, "", fmt.Errorf("%w: operation status is %s (must be RECONCILIATION_REQUIRED)", ErrOperationNotEligibleForRecovery, op.Status)
	}

	// 6. Time-Bounded Recovery Replay Checks (PayPal documented 30-day window, ClaimBridge conservative 27-day cutoff)
	if op.SubmittedAt == nil {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_BLOCKED",
			Metadata: map[string]interface{}{
				"operation_id": op.ID,
				"reason":       "missing_submission_timestamp",
				"outcome":      "MANUAL_RECONCILIATION_REQUIRED",
			},
		})
		return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrSubmissionTimestampMissing
	}

	if op.SubmittedAt.After(now.Add(AllowedSubmissionClockSkew)) {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_BLOCKED",
			Metadata: map[string]interface{}{
				"operation_id": op.ID,
				"reason":       "invalid_submission_timestamp_in_future",
				"submitted_at": op.SubmittedAt.Format(time.RFC3339),
				"outcome":      "MANUAL_RECONCILIATION_REQUIRED",
			},
		})
		return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrSubmissionTimestampInvalid
	}

	if now.Sub(*op.SubmittedAt) > PayPalDuplicateSafetyWindowCutoff {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_BLOCKED",
			Metadata: map[string]interface{}{
				"operation_id": op.ID,
				"reason":       "recovery_window_expired",
				"submitted_at": op.SubmittedAt.Format(time.RFC3339),
				"cutoff_days":  27,
				"outcome":      "MANUAL_RECONCILIATION_REQUIRED",
			},
		})
		return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrRecoveryWindowExpired
	}

	// 7. Reconstruct exact previously authorized payout request
	amountStr := fmt.Sprintf("%.2f", float64(op.AmountCents)/100.0)
	paypalReq := paypal.CreatePayoutRequest{
		SenderBatchID: op.SenderBatchID,
		SenderItemID:  op.SenderItemID,
		Recipient:     pCase.RecipientEmail,
		Amount:        amountStr,
		Currency:      op.Currency,
		Note:          pCase.Title,
		EmailSubject:  fmt.Sprintf("Payment for Claim %s", pCase.Reference),
	}

	// 8. Cryptographic fingerprint verification (Fail closed on missing or mismatched)
	if op.SubmissionFingerprint == nil || strings.TrimSpace(*op.SubmissionFingerprint) == "" {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_BLOCKED",
			Metadata: map[string]interface{}{
				"operation_id": op.ID,
				"reason":       "missing_submission_fingerprint",
				"outcome":      "MANUAL_RECONCILIATION_REQUIRED",
			},
		})
		return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrFingerprintMissing
	}

	if fpErr := VerifySubmissionFingerprint(*op.SubmissionFingerprint, paypalReq); fpErr != nil {
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_BLOCKED",
			Metadata: map[string]interface{}{
				"operation_id": op.ID,
				"reason":       "submission_fingerprint_mismatch",
				"outcome":      "MANUAL_RECONCILIATION_REQUIRED",
			},
		})
		return nil, op, "MANUAL_RECONCILIATION_REQUIRED", fpErr
	}

	// 9. ATOMIC CLAIM at database layer: exactly one concurrent caller wins
	attemptID := crypto.GenerateUUID()
	leaseExpiresAt := now.Add(RecoveryLeaseDuration)
	claimedOp, err := s.repo.ClaimPayoutOperationForRecovery(
		ctx,
		org.ID,
		op.ID,
		attemptID,
		now,
		leaseExpiresAt,
		PayPalDuplicateSafetyWindowCutoff,
	)
	if err != nil {
		if errors.Is(err, ErrRecoveryAlreadyInProgress) || errors.Is(err, ErrRecoveryClaimConflict) {
			return nil, nil, "RECOVERY_IN_PROGRESS", ErrRecoveryAlreadyInProgress
		}
		if errors.Is(err, ErrStaleRecoveryClaim) {
			return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrStaleRecoveryClaim
		}
		if errors.Is(err, ErrRecoveryWindowExpired) {
			return nil, op, "MANUAL_RECONCILIATION_REQUIRED", ErrRecoveryWindowExpired
		}
		if errors.Is(err, ErrSubmissionTimestampMissing) || errors.Is(err, ErrSubmissionTimestampInvalid) {
			return nil, op, "MANUAL_RECONCILIATION_REQUIRED", err
		}
		return nil, nil, "", fmt.Errorf("failed to atomically claim recovery operation: %w", err)
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_RECOVERY_CLAIMED",
		Metadata: map[string]interface{}{
			"operation_id":        claimedOp.ID,
			"recovery_attempt_id": attemptID,
			"lease_expires_at":    leaseExpiresAt.Format(time.RFC3339),
		},
	})

	// 10. Outbound Replay to PayPal Sandbox:
	// Only the single winning caller reaches here. Original submitted_at timestamp is preserved!
	paypalResp, err := s.paypalClient.CreatePayout(ctx, paypalReq)
	if err == nil {
		// Outcome 1: Request newly accepted by PayPal (first request never reached PayPal)
		claimedOp.Status = "SUBMITTED"
		claimedOp.PayPalPayoutBatchID = &paypalResp.BatchHeader.PayoutBatchID
		claimedOp.PayPalBatchStatus = &paypalResp.BatchHeader.BatchStatus
		claimedOp.ErrorMessage = nil
		claimedOp.RawProviderResponse = map[string]interface{}{
			"payout_batch_id":     paypalResp.BatchHeader.PayoutBatchID,
			"batch_status":        paypalResp.BatchHeader.BatchStatus,
			"time_created":        paypalResp.BatchHeader.TimeCreated,
			"recovery_mode":       "REQUEST_NEWLY_ACCEPTED",
			"recovery_attempt_id": attemptID,
		}

		if finalizeErr := s.repo.FinalizeRecoveryOperation(ctx, claimedOp, attemptID); finalizeErr != nil {
			if errors.Is(finalizeErr, db.ErrRecoveryLeaseExpired) || errors.Is(finalizeErr, db.ErrStaleRecoveryClaim) || errors.Is(finalizeErr, db.ErrRecoveryClaimConflict) {
				_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
					UserID:         &user.ID,
					OrganizationID: &org.ID,
					EventType:      "PAYOUT_RECOVERY_LATE_RESPONSE_QUARANTINED",
					Metadata: map[string]interface{}{
						"operation_id":           claimedOp.ID,
						"recovery_attempt_id":    attemptID,
						"paypal_payout_batch_id": paypalResp.BatchHeader.PayoutBatchID,
						"batch_status":           paypalResp.BatchHeader.BatchStatus,
						"finalize_error":         finalizeErr.Error(),
						"outcome":                "MANUAL_RECONCILIATION_REQUIRED",
					},
				})
				claimedOp.Status = "MANUAL_RECONCILIATION_REQUIRED"
				errMsg := fmt.Sprintf("Late PayPal response received after recovery lease expired; quarantined for manual reconciliation: %v", finalizeErr)
				claimedOp.ErrorMessage = &errMsg
				return pCase, claimedOp, "MANUAL_RECONCILIATION_REQUIRED", fmt.Errorf("late paypal response received but recovery lease expired; quarantined for manual reconciliation: %w", finalizeErr)
			}
			return nil, nil, "", fmt.Errorf("failed to finalize newly accepted recovered operation: %w", finalizeErr)
		}
		_ = s.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED")
		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: claimedOp.ID,
			OrganizationID:    org.ID,
			Provider:          "PAYPAL",
			BatchStatus:       paypalResp.BatchHeader.BatchStatus,
			RawResponse:       claimedOp.RawProviderResponse,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_ACCEPTED",
			Metadata: map[string]interface{}{
				"operation_id":           claimedOp.ID,
				"case_id":                pCase.ID,
				"sender_batch_id":        claimedOp.SenderBatchID,
				"paypal_payout_batch_id": paypalResp.BatchHeader.PayoutBatchID,
				"batch_status":           paypalResp.BatchHeader.BatchStatus,
				"outcome":                "REQUEST_NEWLY_ACCEPTED",
				"recovery_attempt_id":    attemptID,
			},
		})

		pCase, _ = s.repo.GetPayoutCaseByID(ctx, org.ID, claimedOp.PayoutCaseID)
		return pCase, claimedOp, "REQUEST_NEWLY_ACCEPTED", nil
	}

	// Outcome 2: PayPal returned duplicate request error
	var dupErr *paypal.DuplicatePayoutError
	if errors.As(err, &dupErr) {
		if dupErr.OriginalBatchID == "" {
			// Malformed link or untrusted HATEOAS host: fail closed into manual reconciliation
			claimedOp.Status = "MANUAL_RECONCILIATION_REQUIRED"
			errMsg := fmt.Sprintf("PayPal duplicate error link validation failed: %s", dupErr.Message)
			claimedOp.ErrorMessage = &errMsg
			_ = s.repo.FinalizeRecoveryOperation(ctx, claimedOp, attemptID)

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				UserID:         &user.ID,
				OrganizationID: &org.ID,
				EventType:      "PAYOUT_RECOVERY_UNRESOLVED",
				Metadata: map[string]interface{}{
					"operation_id":        claimedOp.ID,
					"case_id":             pCase.ID,
					"outcome":             "MANUAL_RECONCILIATION_REQUIRED",
					"error":               errMsg,
					"recovery_attempt_id": attemptID,
				},
			})
			return pCase, claimedOp, "MANUAL_RECONCILIATION_REQUIRED", fmt.Errorf("%w: %s", ErrManualReconciliationRequired, errMsg)
		}

		// Valid batch ID recovered from PayPal duplicate response.
		recoveredBatchID := dupErr.OriginalBatchID
		batchResp, queryErr := s.paypalClient.GetPayoutBatch(ctx, recoveredBatchID)
		if queryErr != nil {
			claimedOp.Status = "MANUAL_RECONCILIATION_REQUIRED"
			claimedOp.PayPalPayoutBatchID = &recoveredBatchID
			errMsg := fmt.Sprintf("Recovered original PayPal batch ID %s, but query failed: %v", recoveredBatchID, queryErr)
			claimedOp.ErrorMessage = &errMsg
			_ = s.repo.FinalizeRecoveryOperation(ctx, claimedOp, attemptID)

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				UserID:         &user.ID,
				OrganizationID: &org.ID,
				EventType:      "PAYOUT_RECOVERY_UNRESOLVED",
				Metadata: map[string]interface{}{
					"operation_id":        claimedOp.ID,
					"case_id":             pCase.ID,
					"outcome":             "MANUAL_RECONCILIATION_REQUIRED",
					"recovered_batch_id":  recoveredBatchID,
					"error":               queryErr.Error(),
					"recovery_attempt_id": attemptID,
				},
			})
			return pCase, claimedOp, "MANUAL_RECONCILIATION_REQUIRED", queryErr
		}

		// Feed recovered batch into authoritative item reconciliation
		updatedCase, updatedOp, reconErr := s.reconcileBatchResponse(
			ctx,
			user,
			org.ID,
			pCase.ID,
			claimedOp,
			batchResp,
			"PAYOUT_RECOVERY_RECONCILED",
			map[string]interface{}{
				"outcome":             "ORIGINAL_BATCH_RECOVERED",
				"recovered_batch_id":  recoveredBatchID,
				"recovery_attempt_id": attemptID,
			},
			attemptID,
		)
		if reconErr != nil {
			if errors.Is(reconErr, db.ErrRecoveryLeaseExpired) || errors.Is(reconErr, db.ErrStaleRecoveryClaim) || errors.Is(reconErr, db.ErrRecoveryClaimConflict) {
				claimedOp.Status = "MANUAL_RECONCILIATION_REQUIRED"
				errMsg := fmt.Sprintf("Original batch recovered (%s) but lease expired before finalization: %v", recoveredBatchID, reconErr)
				claimedOp.ErrorMessage = &errMsg
				return pCase, claimedOp, "MANUAL_RECONCILIATION_REQUIRED", fmt.Errorf("original batch recovered but lease expired: %w", reconErr)
			}
			return nil, nil, "", reconErr
		}
		return updatedCase, updatedOp, "ORIGINAL_BATCH_RECOVERED", nil
	}

	// Outcome 3: Ambiguous timeout during recovery replay -> manual review
	if errors.Is(err, paypal.ErrAmbiguousNetworkTimeout) {
		claimedOp.Status = "MANUAL_RECONCILIATION_REQUIRED"
		errMsg := fmt.Sprintf("Recovery attempt timed out ambiguously: %v; manual reconciliation required", err)
		claimedOp.ErrorMessage = &errMsg
		_ = s.repo.FinalizeRecoveryOperation(ctx, claimedOp, attemptID)

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_RECOVERY_UNRESOLVED",
			Metadata: map[string]interface{}{
				"operation_id":        claimedOp.ID,
				"case_id":             pCase.ID,
				"outcome":             "MANUAL_RECONCILIATION_REQUIRED",
				"error":               errMsg,
				"recovery_attempt_id": attemptID,
			},
		})
		return pCase, claimedOp, "MANUAL_RECONCILIATION_REQUIRED", fmt.Errorf("paypal recovery request timed out ambiguously: %w", err)
	}

	// Definitive provider error (e.g. 500 internal server error) -> remain RECONCILIATION_REQUIRED with UNRESOLVED_OUTCOME
	claimedOp.Status = "RECONCILIATION_REQUIRED"
	errMsg := fmt.Sprintf("Recovery attempt failed: %v", err)
	claimedOp.ErrorMessage = &errMsg
	_ = s.repo.FinalizeRecoveryOperation(ctx, claimedOp, attemptID)

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_RECOVERY_UNRESOLVED",
		Metadata: map[string]interface{}{
			"operation_id":        claimedOp.ID,
			"case_id":             pCase.ID,
			"outcome":             "UNRESOLVED_OUTCOME",
			"error":               errMsg,
			"recovery_attempt_id": attemptID,
		},
	})
	return pCase, claimedOp, "UNRESOLVED_OUTCOME", fmt.Errorf("paypal recovery request failed: %w", err)
}

// CancelPayoutOperation cancels an eligible payout operation locally or via PayPal Sandbox.
func (s *Service) CancelPayoutOperation(ctx context.Context, user *db.User, org *db.Organization, opID string) (*db.PayoutOperation, error) {
	op, err := s.repo.GetPayoutOperationByID(ctx, org.ID, opID)
	if err != nil {
		return nil, err
	}

	// Synthetic fixture boundary: demo fixtures cannot be cancelled or sent to live PayPal
	pCase, _ := s.repo.GetPayoutCaseByID(ctx, org.ID, op.PayoutCaseID)
	if op.IsSyntheticFixture || (pCase != nil && pCase.IsSyntheticFixture) {
		return nil, ErrFixtureExternalOperationForbidden
	}

	if op.Status == "CANCELLED" {
		return nil, ErrOperationAlreadyCancelled
	}

	// If local prepared or awaiting approval, cancel immediately
	if op.Status == "PREPARED" || op.Status == "AWAITING_APPROVAL" || op.Status == "APPROVED" {
		op.Status = "CANCELLED"
		now := time.Now().UTC()
		op.CancelledAt = &now
		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return nil, err
		}
		if err := s.repo.UpdatePayoutCaseStatus(ctx, org.ID, op.PayoutCaseID, "CANCELLED"); err != nil {
			return nil, err
		}
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			UserID:         &user.ID,
			OrganizationID: &org.ID,
			EventType:      "PAYOUT_CANCELLED_LOCAL",
			Metadata:       map[string]interface{}{"operation_id": op.ID},
		})
		return op, nil
	}

	// If submitted to PayPal, must have payout_item_id and item status UNCLAIMED
	if op.PayPalPayoutItemID == nil || *op.PayPalPayoutItemID == "" {
		return nil, ErrNotEligibleForCancellation
	}

	// Verify live item status with PayPal
	itemDetails, err := s.paypalClient.GetPayoutItem(ctx, *op.PayPalPayoutItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to verify item status with paypal: %w", err)
	}
	itemDetails.Normalize()

	if !strings.EqualFold(itemDetails.TransactionStatus, "UNCLAIMED") {
		return nil, fmt.Errorf("%w: item status is %s (must be UNCLAIMED)", ErrNotEligibleForCancellation, itemDetails.TransactionStatus)
	}

	// Call PayPal Cancel
	cancelledItem, err := s.paypalClient.CancelPayoutItem(ctx, *op.PayPalPayoutItemID)
	if err != nil {
		return nil, fmt.Errorf("paypal cancellation failed: %w", err)
	}
	cancelledItem.Normalize()

	op.Status = "CANCELLED"
	now := time.Now().UTC()
	op.CancelledAt = &now
	cancelledStatus := cancelledItem.TransactionStatus
	op.PayPalItemStatus = &cancelledStatus

	if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
		return nil, err
	}
	if err := s.repo.UpdatePayoutCaseStatus(ctx, org.ID, op.PayoutCaseID, "CANCELLED"); err != nil {
		return nil, err
	}

	if err := s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    org.ID,
		Provider:          "PAYPAL",
		BatchStatus:       "CANCELED",
		ItemStatus:        &cancelledStatus,
		RawResponse:       map[string]interface{}{"cancelled_item": cancelledItem},
	}); err != nil {
		return nil, err
	}

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &user.ID,
		OrganizationID: &org.ID,
		EventType:      "PAYOUT_CANCELLED_PAYPAL",
		Metadata: map[string]interface{}{
			"operation_id":   op.ID,
			"payout_item_id": *op.PayPalPayoutItemID,
		},
	})

	return op, nil
}

// ResolveEvidence inspects a grounded AI evidence reference against the authenticated tenant's case records.
// This is a strictly read-only explainability method: no PayPal calls, external queries, or mutations are performed.
func (s *Service) ResolveEvidence(ctx context.Context, orgID, caseID, evidenceID string) (*EvidenceResolution, error) {
	trimmedEvidenceID := strings.TrimSpace(evidenceID)
	if trimmedEvidenceID == "" {
		return nil, ErrEvidenceNotFound
	}

	// 1. Fetch case scoped to tenant
	pCase, err := s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
	if err != nil {
		if errors.Is(err, db.ErrPayoutCaseNotFound) {
			return nil, ErrEvidenceNotFound
		}
		return nil, fmt.Errorf("failed to fetch payout case: %w", err)
	}

	// Case Reference Match (case ID)
	if pCase.ID == trimmedEvidenceID {
		recAt := pCase.CreatedAt.UTC().Format(time.RFC3339)
		fields := []EvidenceField{
			{Name: "Case Reference", Value: pCase.Reference},
			{Name: "Title", Value: pCase.Title},
			{Name: "Status", Value: pCase.Status},
			{Name: "Amount", Value: fmt.Sprintf("$%.2f %s", float64(pCase.AmountCents)/100.0, pCase.Currency)},
			{Name: "Recipient (Masked)", Value: ai.MaskEmail(pCase.RecipientEmail)},
			{Name: "Dual Approval Required", Value: fmt.Sprintf("%t", pCase.DualApprovalRequired)},
			{Name: "Risk Score", Value: fmt.Sprintf("%.2f", pCase.RiskScore)},
			{Name: "Created At", Value: recAt},
			{Name: "Updated At", Value: pCase.UpdatedAt.UTC().Format(time.RFC3339)},
		}
		if pCase.Description != "" {
			fields = append([]EvidenceField{{Name: "Description", Value: pCase.Description}}, fields...)
		}
		return &EvidenceResolution{
			ReferenceID:        trimmedEvidenceID,
			SourceType:         "PAYOUT_CASE",
			DisplayLabel:       fmt.Sprintf("Payout Case (%s)", pCase.Reference),
			Fields:             fields,
			RecordedAt:         &recAt,
			IsSyntheticFixture: pCase.IsSyntheticFixture,
			ResolutionStatus:   "RESOLVED",
		}, nil
	}

	// 2. Fetch operations scoped to tenant and case
	ops, err := s.repo.ListPayoutOperationsByCaseID(ctx, orgID, caseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list payout operations: %w", err)
	}

	// Check if evidenceID matches any operation identifier
	for _, op := range ops {
		matched := false
		if op.ID == trimmedEvidenceID || op.SenderBatchID == trimmedEvidenceID || op.SenderItemID == trimmedEvidenceID {
			matched = true
		} else if op.PayPalPayoutBatchID != nil && *op.PayPalPayoutBatchID == trimmedEvidenceID {
			matched = true
		} else if op.PayPalPayoutItemID != nil && *op.PayPalPayoutItemID == trimmedEvidenceID {
			matched = true
		}

		if matched {
			recAt := op.CreatedAt.UTC().Format(time.RFC3339)
			fields := []EvidenceField{
				{Name: "Operation ID", Value: op.ID},
				{Name: "Sender Batch ID", Value: op.SenderBatchID},
				{Name: "Sender Item ID", Value: op.SenderItemID},
				{Name: "Operation Status", Value: op.Status},
				{Name: "Amount", Value: fmt.Sprintf("$%.2f %s", float64(op.AmountCents)/100.0, op.Currency)},
				{Name: "Requires Dual Approval", Value: fmt.Sprintf("%t", op.RequiresDualApproval)},
			}
			if op.PayPalPayoutBatchID != nil && *op.PayPalPayoutBatchID != "" {
				fields = append(fields, EvidenceField{Name: "PayPal Batch ID", Value: *op.PayPalPayoutBatchID})
			}
			if op.PayPalBatchStatus != nil && *op.PayPalBatchStatus != "" {
				fields = append(fields, EvidenceField{Name: "PayPal Batch Status", Value: *op.PayPalBatchStatus})
			}
			if op.PayPalPayoutItemID != nil && *op.PayPalPayoutItemID != "" {
				fields = append(fields, EvidenceField{Name: "PayPal Item ID", Value: *op.PayPalPayoutItemID})
			}
			if op.PayPalItemStatus != nil && *op.PayPalItemStatus != "" {
				fields = append(fields, EvidenceField{Name: "PayPal Item Status", Value: *op.PayPalItemStatus})
			}
			if op.ErrorMessage != nil && *op.ErrorMessage != "" {
				fields = append(fields, EvidenceField{Name: "Error Message", Value: *op.ErrorMessage})
			}
			if op.ApprovedAt != nil {
				fields = append(fields, EvidenceField{Name: "Approved At", Value: op.ApprovedAt.UTC().Format(time.RFC3339)})
			}
			if op.SubmittedAt != nil {
				fields = append(fields, EvidenceField{Name: "Submitted At", Value: op.SubmittedAt.UTC().Format(time.RFC3339)})
			}
			if op.CompletedAt != nil {
				fields = append(fields, EvidenceField{Name: "Completed At", Value: op.CompletedAt.UTC().Format(time.RFC3339)})
			}
			if op.CancelledAt != nil {
				fields = append(fields, EvidenceField{Name: "Cancelled At", Value: op.CancelledAt.UTC().Format(time.RFC3339)})
			}
			fields = append(fields, EvidenceField{Name: "Created At", Value: recAt})

			return &EvidenceResolution{
				ReferenceID:        trimmedEvidenceID,
				SourceType:         "PAYOUT_OPERATION",
				DisplayLabel:       fmt.Sprintf("Payout Operation (%s)", op.SenderBatchID),
				Fields:             fields,
				RecordedAt:         &recAt,
				IsSyntheticFixture: op.IsSyntheticFixture,
				ResolutionStatus:   "RESOLVED",
			}, nil
		}
	}

	// 3. Check status snapshots across operations of this case
	for _, op := range ops {
		snaps, snapErr := s.repo.ListPayoutStatusSnapshots(ctx, orgID, op.ID)
		if snapErr != nil {
			continue
		}
		for _, snap := range snaps {
			if snap.ID == trimmedEvidenceID {
				recAt := snap.RecordedAt.UTC().Format(time.RFC3339)
				fields := []EvidenceField{
					{Name: "Snapshot ID", Value: snap.ID},
					{Name: "Payout Operation ID", Value: snap.PayoutOperationID},
					{Name: "Provider", Value: snap.Provider},
					{Name: "Batch Status", Value: snap.BatchStatus},
				}
				if snap.ItemStatus != nil && *snap.ItemStatus != "" {
					fields = append(fields, EvidenceField{Name: "Item Status", Value: *snap.ItemStatus})
				}
				fields = append(fields, EvidenceField{Name: "Recorded At", Value: recAt})

				return &EvidenceResolution{
					ReferenceID:        trimmedEvidenceID,
					SourceType:         "STATUS_SNAPSHOT",
					DisplayLabel:       fmt.Sprintf("Status Snapshot (%s)", snap.Provider),
					Fields:             fields,
					RecordedAt:         &recAt,
					IsSyntheticFixture: snap.IsSyntheticFixture,
					ResolutionStatus:   "RESOLVED",
				}, nil
			}
		}
	}

	// 4. Unknown, cross-tenant, or cross-case reference
	return nil, ErrEvidenceNotFound
}
