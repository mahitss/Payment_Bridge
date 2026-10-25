package payouts

import (
	"context"
	"errors"
	"fmt"
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
	ErrOperationNotApproved              = errors.New("operation requires approval before submission")
	ErrOperationAlreadySubmitted         = errors.New("operation is already in-flight or submitted")
	ErrNotEligibleForCancellation        = errors.New("operation is not eligible for cancellation; only unclaimed payouts can be cancelled")
	ErrNoOperationsToSync                = errors.New("no payout operations found for this case to synchronize")
	ErrActiveOperationExists             = errors.New("payout case already has an active operation in progress")
	ErrOperationCannotBeSubmitted        = errors.New("operation cannot be submitted from current status")
	ErrCaseInTerminalState               = errors.New("cannot perform operation on case in terminal status")
	ErrOperationAlreadyCancelled         = errors.New("payout operation is already cancelled")
	ErrFixtureExternalOperationForbidden = errors.New("forbidden on synthetic demo fixtures: cannot perform external PayPal operation on synthetic demo fixture: demo fixtures are strictly isolated from live financial destinations")
	ErrEvidenceNotFound                  = errors.New("evidence reference not found")
)

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
	if err == nil {
		for _, existing := range existingOps {
			if existing.Status != "CANCELLED" && existing.Status != "FAILED" {
				return nil, fmt.Errorf("%w: operation %s has status %s", ErrActiveOperationExists, existing.ID, existing.Status)
			}
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
		return nil, fmt.Errorf("operation cannot be approved from status: %s", op.Status)
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
	if err == nil {
		for _, existing := range existingOps {
			if existing.ID != op.ID {
				if existing.Status == "SUBMITTING" || existing.Status == "SUBMITTED" || existing.Status == "SUCCEEDED" || existing.Status == "RECONCILIATION_REQUIRED" {
					return nil, fmt.Errorf("%w: case already has active submitted operation %s in status %s", ErrOperationAlreadySubmitted, existing.ID, existing.Status)
				}
			}
		}
	}

	// 1. Transactionally transition to SUBMITTING before outbound call
	op.Status = "SUBMITTING"
	now := time.Now().UTC()
	op.SubmittedAt = &now
	if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to update operation to SUBMITTING: %w", err)
	}

	// 2. Call PayPal Sandbox
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

	paypalResp, err := s.paypalClient.CreatePayout(ctx, paypalReq)
	if err != nil {
		// SAFETY: If ambiguous timeout, transition to RECONCILIATION_REQUIRED
		if errors.Is(err, paypal.ErrAmbiguousNetworkTimeout) {
			op.Status = "RECONCILIATION_REQUIRED"
			errMsg := err.Error()
			op.ErrorMessage = &errMsg
			_ = s.repo.UpdatePayoutOperation(ctx, op)

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

		// Definitive error
		op.Status = "FAILED"
		errMsg := err.Error()
		op.ErrorMessage = &errMsg
		_ = s.repo.UpdatePayoutOperation(ctx, op)
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

	// Determine lookup identifier: prefer PayPalPayoutBatchID; fallback to SenderBatchID if operation is awaiting reconciliation
	batchLookupID := ""
	if op.PayPalPayoutBatchID != nil && *op.PayPalPayoutBatchID != "" {
		batchLookupID = *op.PayPalPayoutBatchID
	} else if op.SenderBatchID != "" && op.Status == "RECONCILIATION_REQUIRED" {
		batchLookupID = op.SenderBatchID
	}

	if batchLookupID == "" {
		return pCase, &op, nil
	}

	// Synthetic fixture boundary: demo fixtures cannot be synchronized with live PayPal
	if pCase.IsSyntheticFixture || op.IsSyntheticFixture {
		return pCase, &op, ErrFixtureExternalOperationForbidden
	}

	batchResp, err := s.paypalClient.GetPayoutBatch(ctx, batchLookupID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query paypal payout batch: %w", err)
	}

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

	if err := s.repo.UpdatePayoutOperation(ctx, &op); err != nil {
		return nil, nil, fmt.Errorf("failed to update payout operation: %w", err)
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

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		OrganizationID: &orgID,
		EventType:      "PAYOUT_STATUS_SYNCHRONIZED",
		Metadata: map[string]interface{}{
			"operation_id": op.ID,
			"batch_status": batchResp.BatchHeader.BatchStatus,
			"item_status":  op.PayPalItemStatus,
			"op_status":    op.Status,
		},
	})

	pCase, err = s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
	if err != nil {
		return nil, &op, fmt.Errorf("failed to retrieve updated payout case: %w", err)
	}
	return pCase, &op, nil
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
