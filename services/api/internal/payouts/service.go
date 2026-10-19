package payouts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
)

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

	// Generate deterministic idempotency keys
	randomSuffix := crypto.GenerateUUID()[:8]
	senderBatchID := fmt.Sprintf("CB-BATCH-%s-%s", pCase.Reference, randomSuffix)
	senderItemID := fmt.Sprintf("CB-ITEM-%s-%s", pCase.Reference, randomSuffix)

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

	if pCase.Status == "COMPLETED" || pCase.Status == "CANCELLED" {
		return nil, fmt.Errorf("%w: cannot submit operation for case with status %s", ErrCaseInTerminalState, pCase.Status)
	}

	op, err := s.repo.GetPayoutOperationByID(ctx, org.ID, opID)
	if err != nil {
		return nil, err
	}

	// Synthetic fixture boundary: demo fixtures cannot be submitted to live PayPal
	if pCase.IsSyntheticFixture || op.IsSyntheticFixture {
		return nil, ErrFixtureExternalOperationForbidden
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
	_ = s.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED")

	// Record snapshot
	_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    org.ID,
		Provider:          "PAYPAL",
		BatchStatus:       paypalResp.BatchHeader.BatchStatus,
		RawResponse:       op.RawProviderResponse,
	})

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
	if op.PayPalPayoutBatchID == nil || *op.PayPalPayoutBatchID == "" {
		return pCase, &op, nil
	}

	// Synthetic fixture boundary: demo fixtures cannot be synchronized with live PayPal
	if pCase.IsSyntheticFixture || op.IsSyntheticFixture {
		return pCase, &op, ErrFixtureExternalOperationForbidden
	}

	batchResp, err := s.paypalClient.GetPayoutBatch(ctx, *op.PayPalPayoutBatchID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query paypal payout batch: %w", err)
	}

	batchStatus := batchResp.BatchHeader.BatchStatus
	op.PayPalBatchStatus = &batchStatus
	now := time.Now().UTC()

	var itemStatus string
	if len(batchResp.Items) > 0 {
		item := batchResp.Items[0]
		itemStatus = item.TransactionStatus
		op.PayPalPayoutItemID = &item.PayoutItemID
		op.PayPalItemStatus = &itemStatus
	}

	// State machine synchronization
	switch strings.ToUpper(batchStatus) {
	case "SUCCESS":
		op.Status = "SUCCEEDED"
		op.CompletedAt = &now
		_ = s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "COMPLETED")
	case "DENIED":
		op.Status = "FAILED"
		_ = s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "DENIED")
	case "CANCELED":
		op.Status = "CANCELLED"
		op.CancelledAt = &now
		_ = s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "CANCELLED")
	case "PENDING", "PROCESSING":
		op.Status = "SUBMITTED"
	}

	// Check item status specifically
	if strings.EqualFold(itemStatus, "SUCCESS") {
		op.Status = "SUCCEEDED"
		op.CompletedAt = &now
		_ = s.repo.UpdatePayoutCaseStatus(ctx, orgID, caseID, "COMPLETED")
	} else if strings.EqualFold(itemStatus, "FAILED") || strings.EqualFold(itemStatus, "BLOCKED") {
		op.Status = "FAILED"
	}

	op.RawProviderResponse = map[string]interface{}{
		"batch_header": batchResp.BatchHeader,
		"items_count":  len(batchResp.Items),
	}

	if err := s.repo.UpdatePayoutOperation(ctx, &op); err != nil {
		return nil, nil, err
	}

	_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    orgID,
		Provider:          "PAYPAL",
		BatchStatus:       batchStatus,
		ItemStatus:        &itemStatus,
		RawResponse:       op.RawProviderResponse,
	})

	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		OrganizationID: &orgID,
		EventType:      "PAYOUT_STATUS_SYNCHRONIZED",
		Metadata: map[string]interface{}{
			"operation_id": op.ID,
			"batch_status": batchStatus,
			"item_status":  itemStatus,
			"op_status":    op.Status,
		},
	})

	pCase, _ = s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
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
		_ = s.repo.UpdatePayoutCaseStatus(ctx, org.ID, op.PayoutCaseID, "CANCELLED")
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

	if !strings.EqualFold(itemDetails.TransactionStatus, "UNCLAIMED") {
		return nil, fmt.Errorf("%w: item status is %s (must be UNCLAIMED)", ErrNotEligibleForCancellation, itemDetails.TransactionStatus)
	}

	// Call PayPal Cancel
	cancelledItem, err := s.paypalClient.CancelPayoutItem(ctx, *op.PayPalPayoutItemID)
	if err != nil {
		return nil, fmt.Errorf("paypal cancellation failed: %w", err)
	}

	op.Status = "CANCELLED"
	now := time.Now().UTC()
	op.CancelledAt = &now
	cancelledStatus := cancelledItem.TransactionStatus
	op.PayPalItemStatus = &cancelledStatus

	if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
		return nil, err
	}
	_ = s.repo.UpdatePayoutCaseStatus(ctx, org.ID, op.PayoutCaseID, "CANCELLED")

	_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    org.ID,
		Provider:          "PAYPAL",
		BatchStatus:       "CANCELED",
		ItemStatus:        &cancelledStatus,
		RawResponse:       map[string]interface{}{"cancelled_item": cancelledItem},
	})

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
