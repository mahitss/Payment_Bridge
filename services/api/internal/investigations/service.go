package investigations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

var (
	ErrCaseNotFound             = errors.New("payout case not found")
	ErrInvestigationNotFound    = errors.New("payout investigation not found")
	ErrAIUnavailable            = errors.New("ai investigation service is temporarily unavailable")
	ErrInvestigationFailed      = errors.New("investigation failed to complete")
	ErrInvalidEvidenceReference = errors.New("ai model cited non-existent evidence identifiers")
)

// Service coordinates payout investigation assembly, model inference, and policy cross-checks.
type Service struct {
	cfg          *config.Config
	repo         db.Repository
	aiProvider   ai.AIProvider
	policyEngine policy.PolicyEngine
	paypalClient *paypal.Client
}

// NewService constructs a new investigations service.
func NewService(
	cfg *config.Config,
	repo db.Repository,
	aiProvider ai.AIProvider,
	policyEngine policy.PolicyEngine,
	paypalClient *paypal.Client,
) *Service {
	return &Service{
		cfg:          cfg,
		repo:         repo,
		aiProvider:   aiProvider,
		policyEngine: policyEngine,
		paypalClient: paypalClient,
	}
}

// InvestigateRequest represents the parameters for running an investigation.
type InvestigateRequest struct {
	ForceSync bool `json:"force_sync"`
}

// InvestigatePayoutCase runs an evidence-grounded AI investigation on a specific payout case.
func (s *Service) InvestigatePayoutCase(ctx context.Context, orgID, caseID, requestingUserID string, req InvestigateRequest) (*db.PayoutInvestigation, error) {
	// 1. Verify case existence and tenant isolation
	pCase, err := s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
	if err != nil {
		if errors.Is(err, db.ErrPayoutCaseNotFound) {
			return nil, ErrCaseNotFound
		}
		return nil, fmt.Errorf("failed to fetch payout case: %w", err)
	}

	// 2. Fetch all durable operations for this case
	ops, err := s.repo.ListPayoutOperationsByCaseID(ctx, orgID, caseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list payout operations: %w", err)
	}

	// 3. Optional PayPal status sync if requested and in-flight operation exists
	if req.ForceSync && s.paypalClient != nil && len(ops) > 0 {
		latestOp := ops[0]
		if latestOp.PayPalPayoutBatchID != nil && *latestOp.PayPalPayoutBatchID != "" {
			slog.InfoContext(ctx, "Syncing PayPal status before investigation",
				slog.String("case_id", caseID),
				slog.String("batch_id", *latestOp.PayPalPayoutBatchID),
			)
			batchResp, syncErr := s.paypalClient.GetPayoutBatch(ctx, *latestOp.PayPalPayoutBatchID)
			if syncErr == nil && batchResp != nil {
				batchStatus := batchResp.BatchHeader.BatchStatus
				var itemStatus *string
				if len(batchResp.Items) > 0 {
					itemStatus = &batchResp.Items[0].TransactionStatus
				}
				rawResp := map[string]interface{}{
					"batch_header": batchResp.BatchHeader,
					"items":        batchResp.Items,
				}
				// Record snapshot
				snapshot := &db.PayoutStatusSnapshot{
					PayoutOperationID: latestOp.ID,
					OrganizationID:    orgID,
					Provider:          "PAYPAL",
					BatchStatus:       batchStatus,
					ItemStatus:        itemStatus,
					RawResponse:       rawResp,
				}
				_ = s.repo.CreatePayoutStatusSnapshot(ctx, snapshot)

				// Update operation record
				latestOp.PayPalBatchStatus = &batchStatus
				latestOp.PayPalItemStatus = itemStatus
				_ = s.repo.UpdatePayoutOperation(ctx, &latestOp)
			}
		}
	}

	// 4. Fetch all status snapshots across all operations
	var allSnapshots []db.PayoutStatusSnapshot
	for _, op := range ops {
		opSnaps, snapErr := s.repo.ListPayoutStatusSnapshots(ctx, orgID, op.ID)
		if snapErr == nil {
			allSnapshots = append(allSnapshots, opSnaps...)
		}
	}

	// 5. Assemble privacy-minimized investigation snapshot
	snapshotOps := make([]ai.SnapshotOperation, 0, len(ops))
	for _, op := range ops {
		snapshotOps = append(snapshotOps, ai.SnapshotOperation{
			ID:                   op.ID,
			SenderBatchID:        op.SenderBatchID,
			SenderItemID:         op.SenderItemID,
			Status:               op.Status,
			PayPalPayoutBatchID:  op.PayPalPayoutBatchID,
			PayPalBatchStatus:    op.PayPalBatchStatus,
			PayPalPayoutItemID:   op.PayPalPayoutItemID,
			PayPalItemStatus:     op.PayPalItemStatus,
			ErrorMessage:         op.ErrorMessage,
			RequiresDualApproval: op.RequiresDualApproval,
			ApprovedAt:           op.ApprovedAt,
			SubmittedAt:          op.SubmittedAt,
			CompletedAt:          op.CompletedAt,
			CancelledAt:          op.CancelledAt,
		})
	}

	snapshotRecords := make([]ai.SnapshotRecord, 0, len(allSnapshots))
	for _, snap := range allSnapshots {
		snapshotRecords = append(snapshotRecords, ai.SnapshotRecord{
			ID:          snap.ID,
			OperationID: snap.PayoutOperationID,
			Provider:    snap.Provider,
			BatchStatus: snap.BatchStatus,
			ItemStatus:  snap.ItemStatus,
			RecordedAt:  snap.RecordedAt,
		})
	}

	validEvidenceIDs := ai.BuildValidEvidenceIDs(pCase.ID, snapshotOps, snapshotRecords)

	// Sanitize and length-limit user-supplied text
	sanitizedDesc := sanitizeUserText(pCase.Description, 500)
	sanitizedName := sanitizeUserText(pCase.RecipientName, 100)

	aiSnapshot := &ai.InvestigationSnapshot{
		CaseID:               pCase.ID,
		Reference:            pCase.Reference,
		Currency:             pCase.Currency,
		AmountCents:          pCase.AmountCents,
		AmountFormatted:      fmt.Sprintf("$%.2f %s", float64(pCase.AmountCents)/100.0, pCase.Currency),
		CaseStatus:           pCase.Status,
		RiskScore:            pCase.RiskScore,
		DualApprovalRequired: pCase.DualApprovalRequired,
		DescriptionSanitized: sanitizedDesc,
		RecipientEmailMasked: ai.MaskEmail(pCase.RecipientEmail),
		RecipientName:        sanitizedName,
		CreatedAt:            pCase.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            pCase.UpdatedAt.UTC().Format(time.RFC3339),
		Operations:           snapshotOps,
		Snapshots:            snapshotRecords,
		ValidEvidenceIDs:     validEvidenceIDs,
	}

	// 6. Check provider availability
	if !s.aiProvider.IsAvailable(ctx) {
		slog.WarnContext(ctx, "AI provider unavailable for payout investigation",
			slog.String("provider", s.aiProvider.Name()),
			slog.String("case_id", caseID),
		)
		return nil, ErrAIUnavailable
	}

	// 7. Invoke Model with request-scoped timeout
	investigationCtx, cancel := context.WithTimeout(ctx, s.cfg.AIRequestTimeout)
	defer cancel()

	aiResult, err := s.aiProvider.Investigate(investigationCtx, aiSnapshot)
	if err != nil {
		slog.WarnContext(ctx, "AI model inference failed",
			slog.String("error", err.Error()),
			slog.String("case_id", caseID),
		)

		status := "FAILED"
		if errors.Is(err, ai.ErrInvalidEvidenceRef) {
			status = "INVALID_EVIDENCE_REFERENCE"
		}

		errMsg := err.Error()
		failedRecord := &db.PayoutInvestigation{
			PayoutCaseID:        caseID,
			OrganizationID:      orgID,
			RequestedByUserID:   requestingUserID,
			Provider:            s.aiProvider.Name(),
			Model:               s.aiProvider.Model(),
			PromptVersion:       ai.PromptVersion,
			Status:              status,
			Summary:             "Investigation could not be completed.",
			Confidence:          ai.ConfidenceInsufficientEvidence,
			RequiresHumanReview: true,
			ErrorMessage:        &errMsg,
		}
		_ = s.repo.CreateInvestigation(ctx, failedRecord)

		if errors.Is(err, ai.ErrInvalidEvidenceRef) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidEvidenceReference, err.Error())
		}
		if errors.Is(err, ai.ErrAIUnavailable) {
			return nil, ErrAIUnavailable
		}
		return nil, fmt.Errorf("%w: %v", ErrInvestigationFailed, err)
	}

	// 8. Policy Cross-Check for Recommendations
	// Deterministically check each recommended action against the current case state and policy rules
	for i := range aiResult.RecommendedNextSteps {
		rec := &aiResult.RecommendedNextSteps[i]
		s.evaluateRecommendationSafety(rec, pCase, ops)
	}

	// 9. Map findings and recommendations into database model
	dbFindings := make([]db.InvestigationFinding, 0, len(aiResult.Findings))
	for _, f := range aiResult.Findings {
		dbFindings = append(dbFindings, db.InvestigationFinding{
			Explanation:        f.Explanation,
			EvidenceReferences: f.EvidenceReferences,
		})
	}

	dbRecs := make([]db.InvestigationRecommendation, 0, len(aiResult.RecommendedNextSteps))
	for _, r := range aiResult.RecommendedNextSteps {
		dbRecs = append(dbRecs, db.InvestigationRecommendation{
			Action:                       r.Action,
			Reason:                       r.Reason,
			RequiresHumanReview:          r.RequiresHumanReview,
			DeterministicPolicyPermitted: r.DeterministicPolicyPermitted,
			PolicyRuleID:                 r.PolicyRuleID,
		})
	}

	investigation := &db.PayoutInvestigation{
		PayoutCaseID:         caseID,
		OrganizationID:       orgID,
		RequestedByUserID:    requestingUserID,
		Provider:             s.aiProvider.Name(),
		Model:                s.aiProvider.Model(),
		PromptVersion:        ai.PromptVersion,
		Status:               "COMPLETED",
		Summary:              aiResult.Summary,
		Confidence:           aiResult.Confidence,
		RequiresHumanReview:  aiResult.RequiresHumanReview,
		ObservedFacts:        aiResult.ObservedFacts,
		Findings:             dbFindings,
		RecommendedNextSteps: dbRecs,
		MissingInformation:   aiResult.MissingInformation,
		RiskFlags:            aiResult.RiskFlags,
		Limitations:          aiResult.Limitations,
		RawResponse:          aiResult.RawResponse,
	}

	if err := s.repo.CreateInvestigation(ctx, investigation); err != nil {
		return nil, fmt.Errorf("failed to persist investigation record: %w", err)
	}

	// 10. Audit Log
	_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
		UserID:         &requestingUserID,
		OrganizationID: &orgID,
		EventType:      "PAYOUT_INVESTIGATION_COMPLETED",
		Metadata: map[string]interface{}{
			"investigation_id": investigation.ID,
			"case_id":          caseID,
			"confidence":       investigation.Confidence,
			"provider":         s.aiProvider.Name(),
			"model":            s.aiProvider.Model(),
		},
	})

	return investigation, nil
}

// GetInvestigation retrieves an investigation by ID ensuring organization isolation.
func (s *Service) GetInvestigation(ctx context.Context, orgID, investigationID string) (*db.PayoutInvestigation, error) {
	inv, err := s.repo.GetInvestigationByID(ctx, orgID, investigationID)
	if err != nil {
		if errors.Is(err, db.ErrInvestigationNotFound) {
			return nil, ErrInvestigationNotFound
		}
		return nil, err
	}
	return inv, nil
}

// ListInvestigations lists all investigations for a case ensuring organization isolation.
func (s *Service) ListInvestigations(ctx context.Context, orgID, caseID string) ([]db.PayoutInvestigation, error) {
	// Verify case exists in this org
	_, err := s.repo.GetPayoutCaseByID(ctx, orgID, caseID)
	if err != nil {
		if errors.Is(err, db.ErrPayoutCaseNotFound) {
			return nil, ErrCaseNotFound
		}
		return nil, err
	}
	return s.repo.ListInvestigationsByCaseID(ctx, orgID, caseID)
}

// evaluateRecommendationSafety validates recommended actions against deterministic facts and policy rules.
func (s *Service) evaluateRecommendationSafety(
	rec *ai.InvestigationRecommendation,
	pCase *db.PayoutCase,
	ops []db.PayoutOperation,
) {
	rec.DeterministicPolicyPermitted = true

	switch rec.Action {
	case ai.ActionReviewCancellationEligibility:
		// PayPal item cancellation is only legally permitted if an operation exists, has an item ID, and status is UNCLAIMED
		if len(ops) == 0 {
			rec.DeterministicPolicyPermitted = false
			rec.PolicyRuleID = "RULE-PAYPAL-NO-OP"
			rec.Reason = rec.Reason + " [POLICY REJECTED: No payout operation exists for this case]"
			return
		}
		latestOp := ops[0]
		itemStatus := ""
		if latestOp.PayPalItemStatus != nil {
			itemStatus = *latestOp.PayPalItemStatus
		}
		if latestOp.PayPalPayoutItemID == nil || *latestOp.PayPalPayoutItemID == "" {
			rec.DeterministicPolicyPermitted = false
			rec.PolicyRuleID = "RULE-PAYPAL-NO-ITEM-ID"
			rec.Reason = rec.Reason + " [POLICY REJECTED: No PayPal payout item identifier exists]"
			return
		}
		if itemStatus != "UNCLAIMED" {
			rec.DeterministicPolicyPermitted = false
			rec.PolicyRuleID = "RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY"
			rec.Reason = rec.Reason + fmt.Sprintf(" [POLICY REJECTED: PayPal item status is %q, only UNCLAIMED items may be cancelled]", itemStatus)
			return
		}
		rec.PolicyRuleID = "RULE-PAYPAL-CANCEL-ELIGIBLE"

	case ai.ActionCheckProviderStatus:
		if len(ops) == 0 {
			rec.DeterministicPolicyPermitted = false
			rec.PolicyRuleID = "RULE-NO-SUBMITTED-OPERATION"
			rec.Reason = rec.Reason + " [POLICY REJECTED: No operation has been submitted to PayPal yet]"
			return
		}
		rec.PolicyRuleID = "RULE-SYNC-PERMITTED"

	case ai.ActionRequestRecipientInfo, ai.ActionReviewRecipientDetails:
		rec.PolicyRuleID = "RULE-OFFICER-DATA-REVIEW"

	case ai.ActionEscalateToOperator:
		rec.PolicyRuleID = "RULE-OPERATOR-ESCALATION"

	case ai.ActionNoActionUntilReconciled:
		rec.PolicyRuleID = "RULE-RECONCILIATION-HOLD"

	default:
		rec.DeterministicPolicyPermitted = false
		rec.PolicyRuleID = "RULE-UNSUPPORTED-ACTION"
	}
}

// sanitizeUserText limits length and strips control characters from user strings.
func sanitizeUserText(text string, maxLen int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, text)

	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen] + "..."
	}
	return cleaned
}
