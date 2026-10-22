package investigations

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/policy"
)

func setupTestService(t *testing.T) (*Service, *db.MockRepository, *ai.MockProvider) {
	cfg := &config.Config{
		AIEnabled:        true,
		AIProvider:       "mock",
		AIModel:          "mock-llama3.2",
		AIRequestTimeout: 5 * time.Second,
	}
	repo := db.NewMockRepository()
	aiMock := ai.NewMockProvider()
	policyEngine := policy.NewEngine("")
	svc := NewService(cfg, repo, aiMock, policyEngine, nil)
	return svc, repo, aiMock
}

func TestInvestigatePayoutCase_Success(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := setupTestService(t)

	orgID := "org-1"
	userID := "user-1"
	pCase := &db.PayoutCase{
		ID:              "case-1",
		OrganizationID:  orgID,
		CreatedByUserID: userID,
		Reference:       "CLAIM-100",
		RecipientEmail:  "dest@example.com",
		AmountCents:     15000,
		Status:          "SUBMITTED",
	}
	repo.Cases[pCase.ID] = pCase

	op := &db.PayoutOperation{
		ID:                  "op-1",
		PayoutCaseID:        pCase.ID,
		OrganizationID:      orgID,
		InitiatedByUserID:   userID,
		SenderBatchID:       "BATCH-1",
		SenderItemID:        "ITEM-1",
		PayPalPayoutBatchID: strPtr("PP-BATCH-1"),
		PayPalPayoutItemID:  strPtr("PP-ITEM-1"),
		PayPalItemStatus:    strPtr("UNCLAIMED"),
		Status:              "SUBMITTED",
	}
	repo.Operations[op.ID] = op

	inv, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
	if err != nil {
		t.Fatalf("expected successful investigation, got %v", err)
	}

	if inv.Status != "COMPLETED" {
		t.Errorf("expected status COMPLETED, got %s", inv.Status)
	}
	if inv.PayoutCaseID != pCase.ID {
		t.Errorf("expected case ID %s, got %s", pCase.ID, inv.PayoutCaseID)
	}
	if len(inv.Findings) == 0 {
		t.Errorf("expected at least 1 finding")
	}

	// Verify persistence
	persisted, err := repo.GetInvestigationByID(ctx, orgID, inv.ID)
	if err != nil {
		t.Fatalf("failed to retrieve persisted investigation: %v", err)
	}
	if persisted.ID != inv.ID {
		t.Errorf("expected ID %s, got %s", inv.ID, persisted.ID)
	}
}

func TestInvestigatePayoutCase_FabricatedEvidenceRejected(t *testing.T) {
	ctx := context.Background()
	svc, repo, aiMock := setupTestService(t)

	orgID := "org-1"
	userID := "user-1"
	pCase := &db.PayoutCase{
		ID:              "case-2",
		OrganizationID:  orgID,
		CreatedByUserID: userID,
		Reference:       "CLAIM-200",
		AmountCents:     10000,
		Status:          "SUBMITTED",
	}
	repo.Cases[pCase.ID] = pCase

	// Configure mock to return fabricated evidence ID
	aiMock.CustomResult = &ai.InvestigationResult{
		Summary:    "Hallucinated finding",
		Confidence: ai.ConfidenceHigh,
		Findings: []ai.InvestigationFinding{
			{
				Explanation:        "Made up batch reference",
				EvidenceReferences: []string{"fabricated-id-never-existed"},
			},
		},
		RecommendedNextSteps: []ai.InvestigationRecommendation{
			{Action: ai.ActionCheckProviderStatus, Reason: "Check"},
		},
	}

	_, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
	if !errors.Is(err, ErrInvalidEvidenceReference) {
		t.Fatalf("expected ErrInvalidEvidenceReference, got %v", err)
	}

	// Verify failure record was persisted with INVALID_EVIDENCE_REFERENCE status
	list, _ := repo.ListInvestigationsByCaseID(ctx, orgID, pCase.ID)
	if len(list) != 1 {
		t.Fatalf("expected 1 failed investigation persisted, got %d", len(list))
	}
	if list[0].Status != "INVALID_EVIDENCE_REFERENCE" {
		t.Errorf("expected status INVALID_EVIDENCE_REFERENCE, got %s", list[0].Status)
	}
}

func TestInvestigatePayoutCase_CancellationPolicyFeasibility(t *testing.T) {
	ctx := context.Background()
	svc, repo, aiMock := setupTestService(t)

	orgID := "org-1"
	userID := "user-1"

	t.Run("Item is UNCLAIMED - Cancellation Permitted", func(t *testing.T) {
		pCase := &db.PayoutCase{ID: "case-3", OrganizationID: orgID, Status: "SUBMITTED"}
		repo.Cases[pCase.ID] = pCase
		op := &db.PayoutOperation{
			ID:                 "op-3",
			PayoutCaseID:       pCase.ID,
			OrganizationID:     orgID,
			PayPalPayoutItemID: strPtr("PP-ITEM-3"),
			PayPalItemStatus:   strPtr("UNCLAIMED"),
			Status:             "SUBMITTED",
		}
		repo.Operations[op.ID] = op

		aiMock.CustomResult = &ai.InvestigationResult{
			Summary:    "Item unclaimed",
			Confidence: ai.ConfidenceHigh,
			Findings: []ai.InvestigationFinding{
				{Explanation: "Observed", EvidenceReferences: []string{pCase.ID}},
			},
			RecommendedNextSteps: []ai.InvestigationRecommendation{
				{Action: ai.ActionReviewCancellationEligibility, Reason: "Recipient did not claim"},
			},
		}

		inv, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(inv.RecommendedNextSteps) != 1 {
			t.Fatalf("expected 1 recommendation")
		}
		rec := inv.RecommendedNextSteps[0]
		if !rec.DeterministicPolicyPermitted {
			t.Errorf("expected cancellation to be permitted for UNCLAIMED item, got disallowed")
		}
		if rec.PolicyRuleID != "RULE-PAYPAL-CANCEL-ELIGIBLE" {
			t.Errorf("expected RULE-PAYPAL-CANCEL-ELIGIBLE, got %s", rec.PolicyRuleID)
		}
	})

	t.Run("Item is SUCCESS - Cancellation Rejected by Policy", func(t *testing.T) {
		pCase := &db.PayoutCase{ID: "case-4", OrganizationID: orgID, Status: "COMPLETED"}
		repo.Cases[pCase.ID] = pCase
		op := &db.PayoutOperation{
			ID:                 "op-4",
			PayoutCaseID:       pCase.ID,
			OrganizationID:     orgID,
			PayPalPayoutItemID: strPtr("PP-ITEM-4"),
			PayPalItemStatus:   strPtr("SUCCESS"),
			Status:             "SUCCEEDED",
		}
		repo.Operations[op.ID] = op

		aiMock.CustomResult = &ai.InvestigationResult{
			Summary:    "Item settled",
			Confidence: ai.ConfidenceHigh,
			Findings: []ai.InvestigationFinding{
				{Explanation: "Observed", EvidenceReferences: []string{pCase.ID}},
			},
			RecommendedNextSteps: []ai.InvestigationRecommendation{
				{Action: ai.ActionReviewCancellationEligibility, Reason: "Operator asked for cancellation"},
			},
		}

		inv, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rec := inv.RecommendedNextSteps[0]
		if rec.DeterministicPolicyPermitted {
			t.Errorf("expected cancellation to be REJECTED for SUCCESS item, but was permitted")
		}
		if rec.PolicyRuleID != "RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY" {
			t.Errorf("expected RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY, got %s", rec.PolicyRuleID)
		}
	})
}

func TestInvestigatePayoutCase_ProviderUnavailable(t *testing.T) {
	ctx := context.Background()
	svc, repo, aiMock := setupTestService(t)

	orgID := "org-1"
	userID := "user-1"
	pCase := &db.PayoutCase{ID: "case-5", OrganizationID: orgID, Status: "SUBMITTED"}
	repo.Cases[pCase.ID] = pCase

	// Set AI provider offline
	aiMock.Available = false

	_, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("expected ErrAIUnavailable, got %v", err)
	}

	// Case status MUST remain unchanged
	afterCase, _ := repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if afterCase.Status != "SUBMITTED" {
		t.Errorf("expected case status to remain SUBMITTED, got %s", afterCase.Status)
	}
}

func TestInvestigatePayoutCase_PreflightDiagnosticLogging(t *testing.T) {
	var logBuf bytes.Buffer
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	oldLogger := slog.Default()
	slog.SetDefault(testLogger)
	defer slog.SetDefault(oldLogger)

	ctx := context.Background()
	svc, repo, aiMock := setupTestService(t)

	orgID := "org-1"
	userID := "user-1"
	pCase := &db.PayoutCase{ID: "case-diag-1", OrganizationID: orgID, Status: "SUBMITTED"}
	repo.Cases[pCase.ID] = pCase

	// Set AI provider offline
	aiMock.Available = false

	_, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("expected ErrAIUnavailable, got %v", err)
	}

	logOutput := logBuf.String()

	// Verify safe structured fields are present in the log
	expectedFields := []string{
		"AI provider unavailable for payout investigation",
		"provider=mock",
		"case_id=case-diag-1",
		"reachable=false",
		"model_available=false",
		"configured_model=mock-llama3.2",
		"error_category=MOCK_UNAVAILABLE",
		"available_models=",
	}
	for _, field := range expectedFields {
		if !strings.Contains(logOutput, field) {
			t.Errorf("expected log output to contain %q, but got:\n%s", field, logOutput)
		}
	}

	// Verify no sensitive credentials or URLs are leaked
	disallowed := []string{
		"api_key",
		"Authorization",
		"Bearer",
		"password",
		"secret",
	}
	for _, dis := range disallowed {
		if strings.Contains(strings.ToLower(logOutput), dis) {
			t.Errorf("security violation: log contains sensitive term %q", dis)
		}
	}

	// Verify single probe: HealthCheckCount must be exactly 1
	if aiMock.HealthCheckCount != 1 {
		t.Errorf("expected exactly 1 health check probe, got %d", aiMock.HealthCheckCount)
	}

	// Verify Investigate was never called
	if aiMock.CallCount != 0 {
		t.Errorf("expected Investigate not to be called, got %d", aiMock.CallCount)
	}

	// Case status MUST remain unchanged (fail-closed)
	afterCase, _ := repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if afterCase.Status != "SUBMITTED" {
		t.Errorf("expected case status to remain SUBMITTED, got %s", afterCase.Status)
	}
}

func TestInvestigatePayoutCase_NilProvider(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{AIEnabled: false}
	repo := db.NewMockRepository()
	policyEngine := policy.NewEngine("")
	svc := NewService(cfg, repo, nil, policyEngine, nil)

	orgID := "org-1"
	userID := "user-1"
	pCase := &db.PayoutCase{ID: "case-nil-1", OrganizationID: orgID, Status: "SUBMITTED"}
	repo.Cases[pCase.ID] = pCase

	_, err := svc.InvestigatePayoutCase(ctx, orgID, pCase.ID, userID, InvestigateRequest{})
	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("expected ErrAIUnavailable for nil provider, got %v", err)
	}
}

func strPtr(s string) *string {
	return &s
}
