package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

func setupTestService(t *testing.T, paypalHandler http.HandlerFunc) (*Service, *db.MockRepository, *db.User, *db.User, *db.Organization) {
	t.Helper()

	repo := db.NewMockRepository()
	ctx := context.Background()

	// Org & Users
	userA, orgA, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "initiator@acme.com",
		Name:          "Alice Initiator",
		EmailVerified: true,
	}, "Acme Claims Corp")
	if err != nil {
		t.Fatalf("failed to create user A and org: %v", err)
	}

	userB, _, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "approver@acme.com",
		Name:          "Bob Approver",
		EmailVerified: true,
	}, "Bob Claims Corp")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test-client-id",
		PayPalClientSecret:   "test-client-secret",
		PayPalBaseURL:        "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout: 2 * time.Second,
	}

	// PayPal client with test server
	var ppClient *paypal.Client
	if paypalHandler != nil {
		ts := httptest.NewServer(paypalHandler)
		t.Cleanup(ts.Close)

		cfg.PayPalBaseURL = ts.URL
		client, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("failed to create paypal test client: %v", err)
		}
		ppClient = client
	} else {
		client, _ := paypal.NewClient(cfg, http.DefaultClient)
		ppClient = client
	}

	policyEngine := policy.NewEngine("")
	svc := NewService(cfg, repo, policyEngine, ppClient)

	return svc, repo, userA, userB, orgA
}

func TestPayoutService_CreatePayoutCase_ValidationAndDualApproval(t *testing.T) {
	svc, _, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	// 1. Invalid amount: negative or zero
	_, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@example.com",
		RecipientName:  "Jane Recipient",
		Title:          "Claim 101",
		AmountCents:    0,
	})
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got: %v", err)
	}

	// 2. Ceiling exceeded: $500.01 (50001 cents)
	_, err = svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@example.com",
		RecipientName:  "Jane Recipient",
		Title:          "Claim 102",
		AmountCents:    50001,
	})
	if !errors.Is(err, ErrAmountExceedsCeiling) {
		t.Fatalf("expected ErrAmountExceedsCeiling, got: %v", err)
	}

	// 3. Invalid email
	_, err = svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "invalid-email",
		RecipientName:  "Jane Recipient",
		Title:          "Claim 103",
		AmountCents:    10000,
	})
	if !errors.Is(err, ErrInvalidRecipientEmail) {
		t.Fatalf("expected ErrInvalidRecipientEmail, got: %v", err)
	}

	// 4. Valid single-approval case (<= $250.00 and risk <= 20.0)
	c1, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@example.com",
		RecipientName:  "Jane Recipient",
		Title:          "Claim 104",
		AmountCents:    20000, // $200.00
		RiskScore:      15.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c1.DualApprovalRequired {
		t.Errorf("expected DualApprovalRequired=false for $200.00 with risk 15.0")
	}

	// 5. Valid dual-approval case (> $250.00)
	c2, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient2@example.com",
		RecipientName:  "John Recipient",
		Title:          "Claim 105",
		AmountCents:    30000, // $300.00
		RiskScore:      5.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c2.DualApprovalRequired {
		t.Errorf("expected DualApprovalRequired=true for $300.00")
	}

	// 6. Valid dual-approval case due to high risk (> 20.0)
	c3, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient3@example.com",
		RecipientName:  "Alice Risk",
		Title:          "Claim 106",
		AmountCents:    10000, // $100.00
		RiskScore:      25.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c3.DualApprovalRequired {
		t.Errorf("expected DualApprovalRequired=true for risk > 20.0")
	}
}

func TestPayoutService_DualApproval_SelfApprovalForbidden(t *testing.T) {
	svc, _, userA, userB, org := setupTestService(t, nil)
	ctx := context.Background()

	// Create dual-approval case ($300.00)
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@example.com",
		RecipientName:  "Jane Doe",
		Title:          "Settlement #441",
		AmountCents:    30000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	// Initiate operation by userA
	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate operation: %v", err)
	}
	if op.Status != "AWAITING_APPROVAL" {
		t.Errorf("expected status AWAITING_APPROVAL, got: %s", op.Status)
	}

	// 1. UserA (initiator) attempts self-approval -> MUST BE FORBIDDEN
	_, err = svc.ApprovePayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err, ErrSelfApprovalForbidden) {
		t.Fatalf("expected ErrSelfApprovalForbidden, got: %v", err)
	}

	// 2. UserB (distinct user) approves -> MUST SUCCEED
	approvedOp, err := svc.ApprovePayoutOperation(ctx, userB, org, op.ID)
	if err != nil {
		t.Fatalf("expected successful approval by userB, got: %v", err)
	}
	if approvedOp.Status != "APPROVED" {
		t.Errorf("expected status APPROVED, got: %s", approvedOp.Status)
	}
	if approvedOp.ApprovedByUserID == nil || *approvedOp.ApprovedByUserID != userB.ID {
		t.Errorf("expected ApprovedByUserID to be %s", userB.ID)
	}
}

func TestPayoutService_SubmitOperation_SuccessAndAudit(t *testing.T) {
	// Mock PayPal server returning OAuth token and batch response
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
			})
		case "/v1/payments/payouts":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-12345",
					"batch_status":    "PENDING",
					"time_created":    time.Now().UTC().Format(time.RFC3339),
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	// Case with no dual approval ($150.00)
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@example.com",
		RecipientName:  "Jane Recipient",
		Title:          "Claim Small",
		AmountCents:    15000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}
	if op.Status != "PREPARED" {
		t.Fatalf("expected status PREPARED, got %s", op.Status)
	}

	// Submit operation
	submittedOp, err := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if err != nil {
		t.Fatalf("unexpected error submitting payout: %v", err)
	}

	if submittedOp.Status != "SUBMITTED" {
		t.Errorf("expected status SUBMITTED, got: %s", submittedOp.Status)
	}
	if submittedOp.PayPalPayoutBatchID == nil || *submittedOp.PayPalPayoutBatchID != "PAYPAL-BATCH-12345" {
		t.Errorf("expected PayPalPayoutBatchID PAYPAL-BATCH-12345, got %v", submittedOp.PayPalPayoutBatchID)
	}

	// Verify Case status transitioned to SUBMITTED
	updatedCase, _, err := svc.GetPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to get case: %v", err)
	}
	if updatedCase.Status != "SUBMITTED" {
		t.Errorf("expected case status SUBMITTED, got %s", updatedCase.Status)
	}

	// Verify audit event was logged
	found := false
	for _, a := range repo.AuditEvents {
		if a.EventType == "PAYOUT_SUBMITTED_TO_PAYPAL" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected PAYOUT_SUBMITTED_TO_PAYPAL audit event to be recorded")
	}
}

func TestPayoutService_SubmitOperation_AmbiguousTimeoutSafety(t *testing.T) {
	// Mock PayPal server that sleeps to trigger timeout on payouts POST
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			// Sleep longer than request timeout (configured to 50ms in this subtest)
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	svc.cfg.PayPalRequestTimeout = 20 * time.Millisecond // force fast timeout

	ctx := context.Background()
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "timeout@example.com",
		RecipientName:  "Jane Timeout",
		Title:          "Claim Timeout",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	// Submit with 20ms timeout should encounter context deadline exceeded and transition to RECONCILIATION_REQUIRED
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	failedOp, submitErr := svc.SubmitPayoutOperation(timeoutCtx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(submitErr, paypal.ErrAmbiguousNetworkTimeout) {
		t.Fatalf("expected ErrAmbiguousNetworkTimeout, got: %v", submitErr)
	}

	if failedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected op status RECONCILIATION_REQUIRED, got: %s", failedOp.Status)
	}
}

func TestPayoutService_SyncAndCancel(t *testing.T) {
	// Mock PayPal server that handles GetPayoutBatch and CancelPayoutItem
	batchStatus := "PENDING"
	itemStatus := "UNCLAIMED"

	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/v1/payments/payouts/PAYPAL-BATCH-777":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-777",
					"batch_status":    batchStatus,
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-999",
						"transaction_status": itemStatus,
					},
				},
			})
		case "/v1/payments/payouts-item/ITEM-999":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"payout_item_id":     "ITEM-999",
				"transaction_status": itemStatus,
			})
		case "/v1/payments/payouts-item/ITEM-999/cancel":
			itemStatus = "RETURNED"
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"payout_item_id":     "ITEM-999",
				"transaction_status": "RETURNED",
			})
		default:
			http.NotFound(w, r)
		}
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@example.com",
		RecipientName:  "Jane Recipient",
		Title:          "Claim Sync",
		AmountCents:    10000,
	})

	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	batchID := "PAYPAL-BATCH-777"
	op.PayPalPayoutBatchID = &batchID
	op.Status = "SUBMITTED"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)

	// 1. Sync case: retrieves item ID and updates to UNCLAIMED
	_, syncedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}
	if syncedOp.PayPalPayoutItemID == nil || *syncedOp.PayPalPayoutItemID != "ITEM-999" {
		t.Fatalf("expected item ID ITEM-999, got %v", syncedOp.PayPalPayoutItemID)
	}
	if syncedOp.PayPalItemStatus == nil || *syncedOp.PayPalItemStatus != "UNCLAIMED" {
		t.Fatalf("expected item status UNCLAIMED, got %v", syncedOp.PayPalItemStatus)
	}

	// 2. Cancel payout item: item is UNCLAIMED, so cancellation must succeed
	cancelledOp, err := svc.CancelPayoutOperation(ctx, userA, org, syncedOp.ID)
	if err != nil {
		t.Fatalf("failed to cancel unclaimed payout: %v", err)
	}
	if cancelledOp.Status != "CANCELLED" {
		t.Errorf("expected op status CANCELLED, got %s", cancelledOp.Status)
	}

	// 3. Attempting cancellation again must fail
	_, err = svc.CancelPayoutOperation(ctx, userA, org, syncedOp.ID)
	if err == nil {
		t.Errorf("expected error cancelling already cancelled payout")
	}
}

func TestPayoutService_MultiTenantIsolation(t *testing.T) {
	svc, repo, userA, _, orgA := setupTestService(t, nil)
	ctx := context.Background()

	// Create User B in Org B
	_, orgB, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "userb@orgb.com",
		Name:          "User B",
		EmailVerified: true,
	}, "Evil Corp B")

	// Create case in Org A
	caseA, err := svc.CreatePayoutCase(ctx, userA, orgA, CreateCaseRequest{
		RecipientEmail: "user@example.com",
		RecipientName:  "Jane",
		Title:          "Org A Claim",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	// Org B attempts to retrieve Org A's case -> Must be ErrPayoutCaseNotFound
	_, _, err = svc.GetPayoutCase(ctx, orgB.ID, caseA.ID)
	if !errors.Is(err, db.ErrPayoutCaseNotFound) {
		t.Fatalf("expected ErrPayoutCaseNotFound across organizations, got: %v", err)
	}

	// Org B attempts to initiate operation on Org A's case -> Must fail
	_, err = svc.InitiatePayoutOperation(ctx, userA, orgB, caseA.ID)
	if !errors.Is(err, db.ErrPayoutCaseNotFound) {
		t.Fatalf("expected ErrPayoutCaseNotFound when initiating on foreign case, got: %v", err)
	}
}

func TestPayoutService_RejectSubmitFromInvalidStates(t *testing.T) {
	svc, _, userA, userB, org := setupTestService(t, nil)
	ctx := context.Background()

	// 1. Single-officer case ($100.00 USD)
	case1, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Single Officer Claim",
		AmountCents:    10000,
	})

	op1, err := svc.InitiatePayoutOperation(ctx, userA, org, case1.ID)
	if err != nil {
		t.Fatalf("failed to initiate op1: %v", err)
	}

	// Cancel op1 -> status becomes CANCELLED, case status becomes CANCELLED
	_, err = svc.CancelPayoutOperation(ctx, userA, org, op1.ID)
	if err != nil {
		t.Fatalf("failed to cancel op1: %v", err)
	}

	// Attempting to submit cancelled op1 must fail fail-closed
	_, err = svc.SubmitPayoutOperation(ctx, userA, org, case1.ID, op1.ID)
	if err == nil {
		t.Fatalf("expected error submitting cancelled payout operation, got nil")
	}
	if !errors.Is(err, ErrCaseInTerminalState) && !errors.Is(err, ErrOperationCannotBeSubmitted) {
		t.Errorf("expected ErrCaseInTerminalState or ErrOperationCannotBeSubmitted, got: %v", err)
	}

	// 2. Dual-approval case ($350.00 USD)
	case2, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Dual Approval Claim",
		AmountCents:    35000,
	})

	op2, err := svc.InitiatePayoutOperation(ctx, userA, org, case2.ID)
	if err != nil {
		t.Fatalf("failed to initiate op2: %v", err)
	}

	// Attempting to submit op2 while AWAITING_APPROVAL must fail
	_, err = svc.SubmitPayoutOperation(ctx, userA, org, case2.ID, op2.ID)
	if !errors.Is(err, ErrOperationNotApproved) {
		t.Fatalf("expected ErrOperationNotApproved for unapproved dual-custody op, got: %v", err)
	}

	// Approve op2 by userB -> APPROVED
	_, err = svc.ApprovePayoutOperation(ctx, userB, org, op2.ID)
	if err != nil {
		t.Fatalf("failed to approve op2: %v", err)
	}

	// Manually simulate RECONCILIATION_REQUIRED
	op2.Status = "RECONCILIATION_REQUIRED"
	_ = svc.repo.UpdatePayoutOperation(ctx, op2)

	// Attempting to submit op2 while in RECONCILIATION_REQUIRED must fail
	_, err = svc.SubmitPayoutOperation(ctx, userA, org, case2.ID, op2.ID)
	if err == nil {
		t.Fatalf("expected error submitting operation in RECONCILIATION_REQUIRED, got nil")
	}
}

func TestPayoutService_PreventDuplicateActiveOperations(t *testing.T) {
	svc, _, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Duplicate Test Claim",
		AmountCents:    15000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	// First initiation succeeds
	op1, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op1: %v", err)
	}
	if op1.Status != "PREPARED" {
		t.Fatalf("expected status PREPARED, got %s", op1.Status)
	}

	// Second initiation on the same case with active op1 MUST fail
	_, err = svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if !errors.Is(err, ErrActiveOperationExists) {
		t.Fatalf("expected ErrActiveOperationExists when active operation in progress, got: %v", err)
	}
}

func TestPayoutService_InitiateOperation_SafeIDLength(t *testing.T) {
	svc, _, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	// Case with a very long reference
	longRef := "REF-VERY-LONG-IDENTIFIER-THAT-EXCEEDS-USUAL-LIMITS-CLAIM-BRIDGE-PAYOUTS-99999999999999999999"
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		Reference:      longRef,
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Long Ref Claim",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	if len(op.SenderItemID) > 63 {
		t.Errorf("SenderItemID length %d exceeds 63 characters: %q", len(op.SenderItemID), op.SenderItemID)
	}
	if len(op.SenderBatchID) > 63 {
		t.Errorf("SenderBatchID length %d exceeds 63 characters: %q", len(op.SenderBatchID), op.SenderBatchID)
	}
}

func TestPayoutService_SubmitOperation_SendsPersistedSenderItemID(t *testing.T) {
	var capturedPayload map[string]interface{}

	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			_ = json.NewDecoder(r.Body).Decode(&capturedPayload)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-SUBMIT-1",
					"batch_status":    "PENDING",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Item ID Transmission Test",
		AmountCents:    15000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	submittedOp, err := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if err != nil {
		t.Fatalf("failed to submit op: %v", err)
	}

	// Verify outgoing request body captured by mock server has matching sender_item_id
	items, ok := capturedPayload["items"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected items in payload, got %v", capturedPayload)
	}
	firstItem := items[0].(map[string]interface{})
	if firstItem["sender_item_id"] != op.SenderItemID {
		t.Errorf("expected sent sender_item_id %q, got %v", op.SenderItemID, firstItem["sender_item_id"])
	}
	if submittedOp.Status != "SUBMITTED" {
		t.Errorf("expected status SUBMITTED, got %s", submittedOp.Status)
	}
}

func TestPayoutService_SyncPayoutCase_BatchSuccessItemUnclaimed_DoesNotComplete(t *testing.T) {
	// Proves requirement: Batch SUCCESS + item UNCLAIMED does not mark the operation successful or case completed
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/PAYPAL-BATCH-UNCLAIMED" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-UNCLAIMED",
					"batch_status":    "SUCCESS", // Batch is SUCCESS
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-UNC-01",
						"transaction_status": "UNCLAIMED", // Item is UNCLAIMED
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-TARGET",
							"receiver":       "claimant@example.com",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Unclaimed Payout Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	op.SenderItemID = "CB-ITEM-TARGET"
	batchID := "PAYPAL-BATCH-UNCLAIMED"
	op.PayPalPayoutBatchID = &batchID
	op.Status = "SUBMITTED"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)
	_ = svc.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED")

	updatedCase, updatedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}

	// Operation MUST remain SUBMITTED, not SUCCEEDED
	if updatedOp.Status != "SUBMITTED" {
		t.Errorf("expected op status SUBMITTED, got %s", updatedOp.Status)
	}
	// Case MUST remain SUBMITTED, not COMPLETED
	if updatedCase.Status != "SUBMITTED" {
		t.Errorf("expected case status SUBMITTED, got %s", updatedCase.Status)
	}
}

func TestPayoutService_SyncPayoutCase_BatchSuccessItemSuccess_MarksCompleted(t *testing.T) {
	// Proves requirement: Batch SUCCESS + item SUCCESS does mark the operation successful and case completed
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/PAYPAL-BATCH-SUCCESS" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-SUCCESS",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-SUCC-01",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-TARGET-SUCCESS",
							"receiver":       "claimant@example.com",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Success Payout Test",
		AmountCents:    12000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	op.SenderItemID = "CB-ITEM-TARGET-SUCCESS"
	batchID := "PAYPAL-BATCH-SUCCESS"
	op.PayPalPayoutBatchID = &batchID
	op.Status = "SUBMITTED"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)
	_ = svc.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED")

	updatedCase, updatedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}

	if updatedOp.Status != "SUCCEEDED" {
		t.Errorf("expected op status SUCCEEDED, got %s", updatedOp.Status)
	}
	if updatedCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED, got %s", updatedCase.Status)
	}
	if updatedOp.CompletedAt == nil {
		t.Errorf("expected op CompletedAt to be set")
	}
}

func TestPayoutService_SyncPayoutCase_MultiItemNoUniqueMatch_FailsClosed(t *testing.T) {
	// Proves requirement: A multi-item response with no unique sender-item match cannot silently select the first item
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/PAYPAL-BATCH-MULTI-MISMATCH" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-MULTI-MISMATCH",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-FOREIGN-1",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-FOREIGN-1",
						},
					},
					{
						"payout_item_id":     "ITEM-FOREIGN-2",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-FOREIGN-2",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Multi Mismatch Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	// Local op expects CB-ITEM-MY-OP, but batch contains ITEM-FOREIGN-1 and ITEM-FOREIGN-2
	op.SenderItemID = "CB-ITEM-MY-OP"
	batchID := "PAYPAL-BATCH-MULTI-MISMATCH"
	op.PayPalPayoutBatchID = &batchID
	op.Status = "SUBMITTED"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)
	_ = svc.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED")

	updatedCase, updatedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}

	// MUST fail closed into RECONCILIATION_REQUIRED, never select item 0 (ITEM-FOREIGN-1)
	if updatedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected op status RECONCILIATION_REQUIRED, got %s", updatedOp.Status)
	}
	if updatedCase.Status == "COMPLETED" {
		t.Errorf("case must not be marked COMPLETED on correlation failure")
	}
}

func TestPayoutService_SyncPayoutCase_MultiItemUniqueMatch_Succeeds(t *testing.T) {
	// Proves that when multiple items are present, the UNIQUE matching item is correlated, even if it's the 2nd item
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/PAYPAL-BATCH-MULTI-MATCH" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-MULTI-MATCH",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-FIRST-OTHER",
						"transaction_status": "UNCLAIMED",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-OTHER",
						},
					},
					{
						"payout_item_id":     "ITEM-SECOND-TARGET",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-TARGET-MATCH",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Multi Match Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	op.SenderItemID = "CB-ITEM-TARGET-MATCH"
	batchID := "PAYPAL-BATCH-MULTI-MATCH"
	op.PayPalPayoutBatchID = &batchID
	op.Status = "SUBMITTED"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)
	_ = svc.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "SUBMITTED")

	updatedCase, updatedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}

	// Matched second item (SUCCESS)
	if updatedOp.Status != "SUCCEEDED" {
		t.Errorf("expected op status SUCCEEDED, got %s", updatedOp.Status)
	}
	if updatedOp.PayPalPayoutItemID == nil || *updatedOp.PayPalPayoutItemID != "ITEM-SECOND-TARGET" {
		t.Errorf("expected correlated item ITEM-SECOND-TARGET, got %v", updatedOp.PayPalPayoutItemID)
	}
	if updatedCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED, got %s", updatedCase.Status)
	}
}

func TestPayoutService_SyncPayoutCase_AmbiguousTimeoutReconciledWithoutRepaying(t *testing.T) {
	// Proves requirement 6: Ambiguous submission timeout followed by a valid provider status lookup can be reconciled without issuing another payout
	var payoutSubmissions int32
	batchID := "PAYPAL-BATCH-TIMEOUT-RESOLVED"

	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			// Count submission attempts
			atomic.AddInt32(&payoutSubmissions, 1)
			// Simulate timeout on POST
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": batchID,
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-RESOLVED-1",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-TIMEOUT-OP",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	svc.cfg.PayPalRequestTimeout = 20 * time.Millisecond

	ctx := context.Background()
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Timeout Reconciliation Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}
	op.SenderItemID = "CB-ITEM-TIMEOUT-OP"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)

	// 1. Submit encounters timeout -> operation quarantined in RECONCILIATION_REQUIRED
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	timedOutOp, submitErr := svc.SubmitPayoutOperation(timeoutCtx, userA, org, pCase.ID, op.ID)
	if submitErr == nil || !errors.Is(submitErr, paypal.ErrAmbiguousNetworkTimeout) {
		t.Fatalf("expected ErrAmbiguousNetworkTimeout, got: %v", submitErr)
	}
	if timedOutOp.Status != "RECONCILIATION_REQUIRED" {
		t.Fatalf("expected op status RECONCILIATION_REQUIRED, got %s", timedOutOp.Status)
	}
	if atomic.LoadInt32(&payoutSubmissions) != 1 {
		t.Fatalf("expected exactly 1 payout submission attempt, got %d", atomic.LoadInt32(&payoutSubmissions))
	}

	// 2. Perform provider status lookup and sync WITHOUT issuing another payout
	// Set the batch ID discovered via provider reconciliation
	timedOutOp.PayPalPayoutBatchID = &batchID
	_ = svc.repo.UpdatePayoutOperation(ctx, timedOutOp)

	reconciledCase, reconciledOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}

	// 3. Verify operation reconciled to SUCCEEDED and case to COMPLETED without issuing new payouts
	if reconciledOp.Status != "SUCCEEDED" {
		t.Errorf("expected reconciled op status SUCCEEDED, got %s", reconciledOp.Status)
	}
	if reconciledCase.Status != "COMPLETED" {
		t.Errorf("expected reconciled case status COMPLETED, got %s", reconciledCase.Status)
	}
	// Submissions must still be exactly 1: NO second payout was dispatched!
	if atomic.LoadInt32(&payoutSubmissions) != 1 {
		t.Errorf("expected exactly 1 payout submission attempt (no retries), got %d", atomic.LoadInt32(&payoutSubmissions))
	}
}

func TestPayoutService_SyncPayoutCase_PreserveLocalCancellation(t *testing.T) {
	// Proves requirement: Preserve an already-recorded successful local cancellation when PayPal subsequently reports returned/cancelled item
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/PAYPAL-BATCH-CANCELLED" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-CANCELLED",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-RETURNED-01",
						"transaction_status": "RETURNED",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-LOCAL-CANCEL",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Preserve Cancel Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	op.SenderItemID = "CB-ITEM-LOCAL-CANCEL"
	batchID := "PAYPAL-BATCH-CANCELLED"
	op.PayPalPayoutBatchID = &batchID
	// Operation was already cancelled locally
	op.Status = "CANCELLED"
	now := time.Now().UTC()
	op.CancelledAt = &now
	_ = svc.repo.UpdatePayoutOperation(ctx, op)
	_ = svc.repo.UpdatePayoutCaseStatus(ctx, org.ID, pCase.ID, "CANCELLED")

	// When PayPal returns RETURNED item, op status CANCELLED must be preserved
	_, updatedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to sync payout case: %v", err)
	}

	if updatedOp.Status != "CANCELLED" {
		t.Errorf("expected op status CANCELLED to be preserved, got %s", updatedOp.Status)
	}
}

func TestPayoutService_SyncPayoutCase_WithoutBatchID_ReturnsError(t *testing.T) {
	// Mission 030 Requirement 2:
	// In SyncPayoutCase, do not call GET /v1/payments/payouts/{id} with internal SenderBatchID.
	// When no PayPal batch ID is known, return clear actionable ErrNoPayPalBatchID.
	var paypalCalls int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&paypalCalls, 1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "No Batch ID Sync Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	op.Status = "RECONCILIATION_REQUIRED"
	op.PayPalPayoutBatchID = nil
	_ = svc.repo.UpdatePayoutOperation(ctx, op)

	_, _, err = svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if !errors.Is(err, ErrNoPayPalBatchID) {
		t.Fatalf("expected ErrNoPayPalBatchID, got: %v", err)
	}

	// Must never make an outbound call with SenderBatchID
	if calls := atomic.LoadInt32(&paypalCalls); calls != 0 {
		t.Errorf("expected 0 calls to PayPal, got %d", calls)
	}
}

func TestPayoutService_Recovery_TimeoutFollowedByDuplicateRecovery(t *testing.T) {
	// Mission 030 Scenario:
	// A first request times out after PayPal has accepted it; recovery finds the original batch
	// through PayPal's duplicate response and retrieves its real status.
	var postCount int32
	var getBatchCount int32
	var capturedSenderBatchID string
	var capturedSenderItemID string

	var tsURL string
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			n := atomic.AddInt32(&postCount, 1)
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)

			if header, ok := payload["sender_batch_header"].(map[string]interface{}); ok {
				capturedSenderBatchID = header["sender_batch_id"].(string)
			}
			if items, ok := payload["items"].([]interface{}); ok && len(items) > 0 {
				if item, ok := items[0].(map[string]interface{}); ok {
					capturedSenderItemID = item["sender_item_id"].(string)
				}
			}

			if n == 1 {
				// Simulate server timeout on first request (hang or close)
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					_ = conn.Close()
					return
				}
				http.Error(w, "timeout", http.StatusGatewayTimeout)
				return
			}

			// Call 2 (Recovery replay): Returns duplicate error with link to original batch
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "DUPLICATE_REQUEST",
				"message": "A payout request with the same sender_batch_id was already submitted.",
				"links": []map[string]string{
					{
						"href":   tsURL + "/v1/payments/payouts/RECOVERED-PAYPAL-BATCH-123",
						"rel":    "payouts",
						"method": "GET",
					},
				},
			})
			return
		}

		if r.Method == http.MethodGet && r.URL.Path == "/v1/payments/payouts/RECOVERED-PAYPAL-BATCH-123" {
			atomic.AddInt32(&getBatchCount, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "RECOVERED-PAYPAL-BATCH-123",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "RECOVERED-ITEM-001",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": capturedSenderItemID,
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	})

	ts := httptest.NewServer(paypalHandler)
	defer ts.Close()
	tsURL = ts.URL

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "test-client-id",
		PayPalClientSecret:   "test-client-secret",
		PayPalRequestTimeout: 2 * time.Second,
	}

	client, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	repo := db.NewMockRepository()
	ctx := context.Background()
	userA, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email: "user@example.com", Name: "Alice", EmailVerified: true,
	}, "Alice Corp")

	svc := NewService(cfg, repo, policy.NewEngine(""), client)

	// Create case and operation
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Recovery Test Claim",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	origSenderBatchID := op.SenderBatchID
	origSenderItemID := op.SenderItemID

	// 1. Submit payout operation -> times out
	timedOutOp, err := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if err == nil {
		t.Fatalf("expected timeout error on first submission, got nil")
	}
	if timedOutOp.Status != "RECONCILIATION_REQUIRED" {
		t.Fatalf("expected status RECONCILIATION_REQUIRED, got %s", timedOutOp.Status)
	}
	if timedOutOp.SubmissionFingerprint == nil || *timedOutOp.SubmissionFingerprint == "" {
		t.Fatalf("expected submission fingerprint to be persisted before outbound call")
	}

	// 2. Perform explicit recovery
	recoveredCase, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, timedOutOp.ID)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if outcome != "ORIGINAL_BATCH_RECOVERED" {
		t.Errorf("expected outcome ORIGINAL_BATCH_RECOVERED, got %s", outcome)
	}
	if recoveredOp.Status != "SUCCEEDED" {
		t.Errorf("expected op status SUCCEEDED, got %s", recoveredOp.Status)
	}
	if recoveredCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED, got %s", recoveredCase.Status)
	}
	if recoveredOp.PayPalPayoutBatchID == nil || *recoveredOp.PayPalPayoutBatchID != "RECOVERED-PAYPAL-BATCH-123" {
		t.Errorf("expected recovered batch ID RECOVERED-PAYPAL-BATCH-123, got %v", recoveredOp.PayPalPayoutBatchID)
	}
	if recoveredOp.PayPalPayoutItemID == nil || *recoveredOp.PayPalPayoutItemID != "RECOVERED-ITEM-001" {
		t.Errorf("expected recovered item ID RECOVERED-ITEM-001, got %v", recoveredOp.PayPalPayoutItemID)
	}

	// Invariant: The exact same durable identifiers were reused; no replacement generated!
	if capturedSenderBatchID != origSenderBatchID {
		t.Errorf("expected sender batch ID %s, got %s", origSenderBatchID, capturedSenderBatchID)
	}
	if capturedSenderItemID != origSenderItemID {
		t.Errorf("expected sender item ID %s, got %s", origSenderItemID, capturedSenderItemID)
	}
	if atomic.LoadInt32(&getBatchCount) != 1 {
		t.Errorf("expected 1 GET batch lookup, got %d", atomic.LoadInt32(&getBatchCount))
	}
}

func TestPayoutService_Recovery_FirstRequestNeverReachedPayPal(t *testing.T) {
	// Scenario: First request timed out without reaching PayPal.
	// Explicit recovery submits the original request with the same identifiers and is newly accepted.
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			n := atomic.AddInt32(&postCount, 1)
			if n == 1 {
				// Timeout before PayPal processes
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					_ = conn.Close()
					return
				}
				http.Error(w, "network error", http.StatusGatewayTimeout)
				return
			}

			// Call 2 (Recovery): Accepted as new creation
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-NEW-BATCH-555",
					"batch_status":    "PENDING",
					"time_created":    "2026-10-25T12:00:00Z",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	ts := httptest.NewServer(paypalHandler)
	defer ts.Close()

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 2 * time.Second,
	}
	client, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	repo := db.NewMockRepository()
	ctx := context.Background()
	userA, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "user@example.com", Name: "Alice"}, "Alice Corp")
	svc := NewService(cfg, repo, policy.NewEngine(""), client)

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Fresh Accept Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)

	_, err = svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if err == nil {
		t.Fatal("expected first submission to time out")
	}

	recoveredCase, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err != nil {
		t.Fatalf("expected recovery to succeed, got %v", err)
	}

	if outcome != "REQUEST_NEWLY_ACCEPTED" {
		t.Errorf("expected outcome REQUEST_NEWLY_ACCEPTED, got %s", outcome)
	}
	if recoveredOp.Status != "SUBMITTED" {
		t.Errorf("expected op status SUBMITTED, got %s", recoveredOp.Status)
	}
	if recoveredCase.Status != "SUBMITTED" {
		t.Errorf("expected case status SUBMITTED, got %s", recoveredCase.Status)
	}
	if recoveredOp.PayPalPayoutBatchID == nil || *recoveredOp.PayPalPayoutBatchID != "PAYPAL-NEW-BATCH-555" {
		t.Errorf("expected batch ID PAYPAL-NEW-BATCH-555, got %v", recoveredOp.PayPalPayoutBatchID)
	}
}

func TestPayoutService_Recovery_UnclaimedItemDoesNotCompleteCase(t *testing.T) {
	// Scenario: Recovered batch contains an UNCLAIMED item; case is NOT marked completed.
	var tsURL string
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600,
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "DUPLICATE_REQUEST",
				"message": "duplicate sender batch",
				"links": []map[string]string{
					{"href": tsURL + "/v1/payments/payouts/BATCH-UNCLAIMED", "rel": "payouts", "method": "GET"},
				},
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/payments/payouts/BATCH-UNCLAIMED" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "BATCH-UNCLAIMED",
					"batch_status":    "SUCCESS", // Batch header says SUCCESS but item is UNCLAIMED!
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "ITEM-UNCLAIMED-01",
						"transaction_status": "UNCLAIMED",
						"payout_item": map[string]interface{}{
							"sender_item_id": "CB-ITEM-UNCLAIMED",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	ts := httptest.NewServer(paypalHandler)
	defer ts.Close()
	tsURL = ts.URL

	cfg := &config.Config{
		AppEnv: "development", PayPalEnv: "sandbox", PayPalBaseURL: ts.URL, PayPalClientID: "id", PayPalClientSecret: "sec",
	}
	client, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := db.NewMockRepository()
	ctx := context.Background()
	userA, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "user@example.com", Name: "Alice"}, "Alice Corp")
	svc := NewService(cfg, repo, policy.NewEngine(""), client)

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Unclaimed Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.SenderItemID = "CB-ITEM-UNCLAIMED"
	op.Status = "RECONCILIATION_REQUIRED"

	// Set valid submission fingerprint
	amountStr := "100.00"
	req := paypal.CreatePayoutRequest{
		SenderBatchID: op.SenderBatchID,
		SenderItemID:  op.SenderItemID,
		Recipient:     pCase.RecipientEmail,
		Amount:        amountStr,
		Currency:      op.Currency,
		Note:          pCase.Title,
		EmailSubject:  "Payment for Claim " + pCase.Reference,
	}
	fp := ComputeSubmissionFingerprint(req)
	op.SubmissionFingerprint = &fp
	_ = repo.UpdatePayoutOperation(ctx, op)

	recCase, recOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if outcome != "ORIGINAL_BATCH_RECOVERED" {
		t.Errorf("expected ORIGINAL_BATCH_RECOVERED, got %s", outcome)
	}
	// Item UNCLAIMED must NOT mark case COMPLETED!
	if recCase.Status == "COMPLETED" {
		t.Errorf("security violation: case was marked COMPLETED for UNCLAIMED item")
	}
	if recOp.Status != "SUBMITTED" {
		t.Errorf("expected op status SUBMITTED for UNCLAIMED item, got %s", recOp.Status)
	}
}

func TestPayoutService_Recovery_UntrustedHATEOASLinkFailsClosed(t *testing.T) {
	// Scenario: Duplicate errors with untrusted HATEOAS links fail closed without sending OAuth credentials to another host.
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "DUPLICATE_REQUEST",
				"message": "duplicate batch",
				"links": []map[string]string{
					// Untrusted external host!
					{"href": "https://attacker-steals-tokens.com/v1/payments/payouts/STOLEN", "rel": "payouts", "method": "GET"},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	ts := httptest.NewServer(paypalHandler)
	defer ts.Close()

	cfg := &config.Config{
		AppEnv: "development", PayPalEnv: "sandbox", PayPalBaseURL: ts.URL, PayPalClientID: "id", PayPalClientSecret: "sec",
	}
	client, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := db.NewMockRepository()
	ctx := context.Background()
	userA, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "user@example.com", Name: "Alice"}, "Alice Corp")
	svc := NewService(cfg, repo, policy.NewEngine(""), client)

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Untrusted Link Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"

	req := paypal.CreatePayoutRequest{
		SenderBatchID: op.SenderBatchID,
		SenderItemID:  op.SenderItemID,
		Recipient:     pCase.RecipientEmail,
		Amount:        "100.00",
		Currency:      op.Currency,
		Note:          pCase.Title,
		EmailSubject:  "Payment for Claim " + pCase.Reference,
	}
	fp := ComputeSubmissionFingerprint(req)
	op.SubmissionFingerprint = &fp
	_ = repo.UpdatePayoutOperation(ctx, op)

	_, recOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err == nil {
		t.Fatal("expected error on untrusted HATEOAS link, got nil")
	}
	if !errors.Is(err, ErrManualReconciliationRequired) {
		t.Errorf("expected ErrManualReconciliationRequired, got %v", err)
	}
	if outcome != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got %s", outcome)
	}
	if recOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected status RECONCILIATION_REQUIRED, got %s", recOp.Status)
	}
}

func TestPayoutService_Recovery_ChangedFingerprintBlocksRecovery(t *testing.T) {
	// Scenario: A changed payload fingerprint blocks recovery.
	svc, _, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Tamper Test Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"

	// Persist an arbitrary fingerprint that does NOT match current case parameters
	bogusFP := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	op.SubmissionFingerprint = &bogusFP
	_ = svc.repo.UpdatePayoutOperation(ctx, op)

	_, _, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("expected ErrFingerprintMismatch, got: %v", err)
	}
	if outcome != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got %s", outcome)
	}
}

func TestPayoutService_Recovery_MissingFingerprintBlocksRecovery(t *testing.T) {
	// Scenario: Missing fingerprint fails closed into manual reconciliation.
	svc, _, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Missing Fingerprint Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	op.SubmissionFingerprint = nil // No fingerprint recorded
	_ = svc.repo.UpdatePayoutOperation(ctx, op)

	_, _, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err, ErrFingerprintMissing) {
		t.Fatalf("expected ErrFingerprintMissing, got: %v", err)
	}
	if outcome != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got %s", outcome)
	}
}

func TestPayoutService_Recovery_ConcurrentRecoveryBlocked(t *testing.T) {
	// Scenario: Concurrent recovery attempts cannot submit a second conflicting operation.
	svc, _, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Concurrent Test Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)

	// Simulate op currently in RECOVERING state from another thread
	op.Status = "RECOVERING"
	_ = svc.repo.UpdatePayoutOperation(ctx, op)

	_, _, _, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err, ErrRecoveryAlreadyInProgress) {
		t.Fatalf("expected ErrRecoveryAlreadyInProgress, got: %v", err)
	}
}

func TestPayoutService_Recovery_SyntheticFixtureAndCrossTenantRejected(t *testing.T) {
	svc, _, userA, _, orgA := setupTestService(t, nil)
	ctx := context.Background()

	// 1. Synthetic Fixture Rejection
	pCaseFixture, _ := svc.CreatePayoutCase(ctx, userA, orgA, CreateCaseRequest{
		RecipientEmail: "demo@example.com", RecipientName: "Demo Claimant", Title: "Demo Fixture", AmountCents: 10000,
	})
	opFixture, _ := svc.InitiatePayoutOperation(ctx, userA, orgA, pCaseFixture.ID)
	pCaseFixture.IsSyntheticFixture = true
	opFixture.IsSyntheticFixture = true
	opFixture.Status = "RECONCILIATION_REQUIRED"
	fp := "some-fingerprint"
	opFixture.SubmissionFingerprint = &fp
	_ = svc.repo.UpdatePayoutOperation(ctx, opFixture)

	_, _, _, err := svc.RecoverPayoutOperation(ctx, userA, orgA, opFixture.ID)
	if !errors.Is(err, ErrFixtureExternalOperationForbidden) {
		t.Errorf("expected ErrFixtureExternalOperationForbidden for synthetic fixture, got: %v", err)
	}

	// 2. Cross-Tenant Rejection
	userB, orgB, _ := svc.repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "hacker@evil.com", Name: "Eve"}, "Evil Corp")
	_, _, _, err = svc.RecoverPayoutOperation(ctx, userB, orgB, opFixture.ID)
	if err == nil {
		t.Fatal("expected cross-tenant access to fail, got nil")
	}
}
