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
	now := time.Now().UTC()
	op.SubmittedAt = &now

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
	now := time.Now().UTC()
	op.SubmittedAt = &now

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
	if recOp.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected status MANUAL_RECONCILIATION_REQUIRED, got %s", recOp.Status)
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
	now := time.Now().UTC()
	op.SubmittedAt = &now

	// Persist an arbitrary fingerprint that does NOT match current case parameters
	bogusFP := "v2:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
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
	now := time.Now().UTC()
	op.SubmittedAt = &now
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

	now := time.Now().UTC()
	op.SubmittedAt = &now
	lease := now.Add(5 * time.Minute)
	op.RecoveryLeaseExpiresAt = &lease
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
	now := time.Now().UTC()
	opFixture.SubmittedAt = &now
	fp := "v2:some-fingerprint"
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

// -----------------------------------------------------------------------------
// Mission 031: 14 Required Regression Tests
// -----------------------------------------------------------------------------

// Req 1: A fresh eligible operation can enter recovery.
func TestMission031_Req01_FreshEligibleOperationCanEnterRecovery(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postCount, 1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-NEW-001",
					"batch_status":    "PENDING",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@fresh.com", RecipientName: "Fresh Claimant", Title: "Fresh Recovery", AmountCents: 15000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-2 * time.Hour) // 2 hours ago (well within 27 days)
	op.SubmittedAt = &submitted

	req := paypal.CreatePayoutRequest{
		SenderBatchID: op.SenderBatchID,
		SenderItemID:  op.SenderItemID,
		Recipient:     pCase.RecipientEmail,
		Amount:        "150.00",
		Currency:      op.Currency,
		Note:          pCase.Title,
		EmailSubject:  "Payment for Claim " + pCase.Reference,
	}
	fp := ComputeSubmissionFingerprint(req)
	op.SubmissionFingerprint = &fp
	_ = repo.UpdatePayoutOperation(ctx, op)

	recoveredCase, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err != nil {
		t.Fatalf("expected recovery to succeed, got: %v", err)
	}
	if outcome != "REQUEST_NEWLY_ACCEPTED" {
		t.Errorf("expected REQUEST_NEWLY_ACCEPTED, got: %s", outcome)
	}
	if recoveredOp.Status != "SUBMITTED" {
		t.Errorf("expected op status SUBMITTED, got: %s", recoveredOp.Status)
	}
	if recoveredCase.Status != "SUBMITTED" {
		t.Errorf("expected case status SUBMITTED, got: %s", recoveredCase.Status)
	}
	if atomic.LoadInt32(&postCount) != 1 {
		t.Errorf("expected 1 POST to paypal, got: %d", atomic.LoadInt32(&postCount))
	}
	// SubmittedAt MUST NOT have been overwritten
	if !recoveredOp.SubmittedAt.Equal(submitted) {
		t.Errorf("expected submitted_at to be preserved (%v), got (%v)", submitted, recoveredOp.SubmittedAt)
	}
}

// Req 2: An operation older than the allowed recovery window (27 days) is rejected before any provider POST.
func TestMission031_Req02_OperationOlderThan27DaysRejectedBeforeProviderCall(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postCount, 1)
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "old@example.com", RecipientName: "Old Claimant", Title: "Old Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	oldTimestamp := time.Now().UTC().Add(-28 * 24 * time.Hour) // 28 days ago (> 27 days cutoff)
	op.SubmittedAt = &oldTimestamp

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

	_, _, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err, ErrRecoveryWindowExpired) {
		t.Fatalf("expected ErrRecoveryWindowExpired, got: %v", err)
	}
	if outcome != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome)
	}
	// Zero mutation requests to PayPal!
	if atomic.LoadInt32(&postCount) != 0 {
		t.Errorf("security violation: expected 0 POST requests to provider, got: %d", atomic.LoadInt32(&postCount))
	}
}

// Req 3: Missing or invalid original submission timestamps require manual reconciliation.
func TestMission031_Req03_MissingOrInvalidSubmissionTimestampRequiresManualRecon(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postCount, 1)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	// 3a. Missing submitted_at
	pCase1, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "missing@example.com", RecipientName: "Missing", Title: "Missing Stamp", AmountCents: 10000,
	})
	op1, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase1.ID)
	op1.Status = "RECONCILIATION_REQUIRED"
	op1.SubmittedAt = nil // Missing!
	req1 := paypal.CreatePayoutRequest{
		SenderBatchID: op1.SenderBatchID, SenderItemID: op1.SenderItemID,
		Recipient: pCase1.RecipientEmail, Amount: "100.00", Currency: op1.Currency,
		Note: pCase1.Title, EmailSubject: "Payment for Claim " + pCase1.Reference,
	}
	fp1 := ComputeSubmissionFingerprint(req1)
	op1.SubmissionFingerprint = &fp1
	_ = repo.UpdatePayoutOperation(ctx, op1)

	_, _, outcome1, err1 := svc.RecoverPayoutOperation(ctx, userA, org, op1.ID)
	if !errors.Is(err1, ErrSubmissionTimestampMissing) {
		t.Errorf("expected ErrSubmissionTimestampMissing, got: %v", err1)
	}
	if outcome1 != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome1)
	}

	// 3b. Invalid future submitted_at (beyond 5 min clock skew)
	pCase2, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "future@example.com", RecipientName: "Future", Title: "Future Stamp", AmountCents: 10000,
	})
	op2, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase2.ID)
	op2.Status = "RECONCILIATION_REQUIRED"
	futureStamp := time.Now().UTC().Add(1 * time.Hour) // 1 hour in the future!
	op2.SubmittedAt = &futureStamp
	req2 := paypal.CreatePayoutRequest{
		SenderBatchID: op2.SenderBatchID, SenderItemID: op2.SenderItemID,
		Recipient: pCase2.RecipientEmail, Amount: "100.00", Currency: op2.Currency,
		Note: pCase2.Title, EmailSubject: "Payment for Claim " + pCase2.Reference,
	}
	fp2 := ComputeSubmissionFingerprint(req2)
	op2.SubmissionFingerprint = &fp2
	_ = repo.UpdatePayoutOperation(ctx, op2)

	_, _, outcome2, err2 := svc.RecoverPayoutOperation(ctx, userA, org, op2.ID)
	if !errors.Is(err2, ErrSubmissionTimestampInvalid) {
		t.Errorf("expected ErrSubmissionTimestampInvalid, got: %v", err2)
	}
	if outcome2 != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome2)
	}

	// Both cases resulted in ZERO provider POST requests
	if atomic.LoadInt32(&postCount) != 0 {
		t.Errorf("security violation: expected 0 provider POST requests, got: %d", atomic.LoadInt32(&postCount))
	}
}

// Req 4 & 5: Exactly one of two concurrent recovery attempts can claim an operation,
// and losing callers produce zero provider mutation requests.
func TestMission031_Req04_Req05_ConcurrentRecoveryExactlyOneWinsZeroDuplicatePOST(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postCount, 1)
			// Simulate network latency so a concurrent caller has time to attempt
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "CONCURRENT-WINNER-BATCH",
					"batch_status":    "PENDING",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@race.com", RecipientName: "Jane", Title: "Race Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted

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

	// Launch two concurrent recovery attempts
	startBarrier := make(chan struct{})
	type res struct {
		outcome string
		err     error
	}
	resultsChan := make(chan res, 2)

	for i := 0; i < 2; i++ {
		go func() {
			<-startBarrier
			_, _, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
			resultsChan <- res{outcome: outcome, err: err}
		}()
	}

	// Release both goroutines simultaneously
	close(startBarrier)

	r1 := <-resultsChan
	r2 := <-resultsChan

	var successes, conflicts int
	for _, r := range []res{r1, r2} {
		if r.err == nil && r.outcome == "REQUEST_NEWLY_ACCEPTED" {
			successes++
		} else if errors.Is(r.err, ErrRecoveryAlreadyInProgress) || errors.Is(r.err, ErrRecoveryClaimConflict) {
			conflicts++
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 winner, got %d (r1: %v, r2: %v)", successes, r1.err, r2.err)
	}
	if conflicts != 1 {
		t.Errorf("expected exactly 1 safe conflict, got %d (r1: %v, r2: %v)", conflicts, r1.err, r2.err)
	}
	// Zero duplicate provider mutations: exactly 1 POST to PayPal
	if atomic.LoadInt32(&postCount) != 1 {
		t.Errorf("expected exactly 1 provider POST, got: %d", atomic.LoadInt32(&postCount))
	}
}

// Req 6: A stale claim cannot be silently finalized by another attempt.
func TestMission031_Req06_StaleClaimCannotBeSilentlyFinalizedOrReplayed(t *testing.T) {
	svc, repo, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@stale.com", RecipientName: "Jane", Title: "Stale Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)

	// Simulate op in RECOVERING owned by attempt A
	op.Status = "RECOVERING"
	attemptA := "attempt-uuid-AAA"
	op.RecoveryAttemptID = &attemptA
	submitted := time.Now().UTC().Add(-2 * time.Hour)
	op.SubmittedAt = &submitted
	// Lease expired 10 minutes ago
	expiredLease := time.Now().UTC().Add(-10 * time.Minute)
	op.RecoveryLeaseExpiresAt = &expiredLease
	_ = repo.UpdatePayoutOperation(ctx, op)

	// 6a. Attempt B cannot finalize an operation owned by Attempt A
	attemptB := "attempt-uuid-BBB"
	op.Status = "SUBMITTED"
	err := repo.FinalizeRecoveryOperation(ctx, op, attemptB)
	if !errors.Is(err, ErrRecoveryClaimConflict) {
		t.Errorf("expected ErrRecoveryClaimConflict on mismatched attempt ID, got: %v", err)
	}

	// 6b. An expired lease fails closed into manual reconciliation instead of automatic replay
	op.Status = "RECOVERING"
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

	_, _, outcome, recoverErr := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(recoverErr, ErrStaleRecoveryClaim) {
		t.Errorf("expected ErrStaleRecoveryClaim on expired lease, got: %v", recoverErr)
	}
	if outcome != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome)
	}
}

// Req 7: Repeated requests cannot bypass the recovery claim while lease is active.
func TestMission031_Req07_RepeatedRequestsCannotBypassActiveRecoveryClaim(t *testing.T) {
	svc, repo, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@repeat.com", RecipientName: "Jane", Title: "Repeat Test", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECOVERING"
	attempt := "attempt-active-123"
	op.RecoveryAttemptID = &attempt
	activeLease := time.Now().UTC().Add(4 * time.Minute) // active lease!
	op.RecoveryLeaseExpiresAt = &activeLease
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted
	_ = repo.UpdatePayoutOperation(ctx, op)

	for i := 0; i < 3; i++ {
		_, _, _, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
		if !errors.Is(err, ErrRecoveryAlreadyInProgress) {
			t.Fatalf("call %d: expected ErrRecoveryAlreadyInProgress, got: %v", i, err)
		}
	}
}

// Req 8: Process-failure/timeout path preserves an honest unresolved outcome.
func TestMission031_Req08_ProcessFailurePreservesHonestUnresolvedOutcome(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			// Simulate provider 500 internal server error during recovery replay
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "INTERNAL_SERVER_ERROR",
				"message": "PayPal system temporarily unavailable",
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "fail@example.com", RecipientName: "Jane", Title: "Failure Test", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted

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

	_, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err == nil {
		t.Fatal("expected error on provider failure, got nil")
	}
	if outcome != "UNRESOLVED_OUTCOME" {
		t.Errorf("expected UNRESOLVED_OUTCOME, got: %s", outcome)
	}
	if recoveredOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected op status RECONCILIATION_REQUIRED, got: %s", recoveredOp.Status)
	}
	if recoveredOp.ErrorMessage == nil || !strings.Contains(*recoveredOp.ErrorMessage, "Recovery attempt failed") {
		t.Errorf("expected diagnostic error message, got: %v", recoveredOp.ErrorMessage)
	}
}

// Req 9: Synthetic payout fixtures can never reach the external PayPal client.
func TestMission031_Req09_SyntheticFixtureNeverReachesPayPal(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&postCount, 1)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "demo@claimbridge.internal", RecipientName: "Demo", Title: "Demo Fixture", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.IsSyntheticFixture = true
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted
	fp := "v2:demo-fingerprint"
	op.SubmissionFingerprint = &fp
	_ = repo.UpdatePayoutOperation(ctx, op)

	_, _, _, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err, ErrFixtureExternalOperationForbidden) {
		t.Fatalf("expected ErrFixtureExternalOperationForbidden, got: %v", err)
	}
	if atomic.LoadInt32(&postCount) != 0 {
		t.Errorf("security violation: synthetic fixture issued %d POST requests", atomic.LoadInt32(&postCount))
	}
}

// Req 10: Cross-tenant recovery remains denied.
func TestMission031_Req10_CrossTenantRecoveryDenied(t *testing.T) {
	svc, repo, userA, _, orgA := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, orgA, CreateCaseRequest{
		RecipientEmail: "orgA@claimbridge.internal", RecipientName: "Jane", Title: "OrgA Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, orgA, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted
	_ = repo.UpdatePayoutOperation(ctx, op)

	// User B from Org B
	userB, orgB, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "userB@orgB.com", Name: "Bob"}, "Org B")

	_, _, _, err := svc.RecoverPayoutOperation(ctx, userB, orgB, op.ID)
	if err == nil {
		t.Fatal("expected cross-tenant recovery to fail, got nil")
	}
	if !errors.Is(err, db.ErrPayoutOperationNotFound) {
		t.Errorf("expected ErrPayoutOperationNotFound, got: %v", err)
	}
}

// Req 11: Missing or mismatched fingerprints remain fail-closed.
func TestMission031_Req11_MissingOrMismatchedFingerprintFailClosed(t *testing.T) {
	svc, repo, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@test.com", RecipientName: "Jane", Title: "FP Test", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted

	// 11a. Missing fingerprint
	op.SubmissionFingerprint = nil
	_ = repo.UpdatePayoutOperation(ctx, op)
	_, _, outcome1, err1 := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err1, ErrFingerprintMissing) {
		t.Errorf("expected ErrFingerprintMissing, got: %v", err1)
	}
	if outcome1 != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome1)
	}

	// 11b. Mismatched fingerprint
	tamperedFP := "v2:ba5d0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
	op.SubmissionFingerprint = &tamperedFP
	_ = repo.UpdatePayoutOperation(ctx, op)
	_, _, outcome2, err2 := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err2, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch, got: %v", err2)
	}
	if outcome2 != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome2)
	}
}

// Req 12: Canonical fingerprints distinguish adversarial field combinations and changed payout intent.
func TestMission031_Req12_CanonicalFingerprintsAdversarialCases(t *testing.T) {
	baseReq := paypal.CreatePayoutRequest{
		SenderBatchID: "CB-BATCH-100",
		SenderItemID:  "CB-ITEM-100",
		Recipient:     "alice@example.com",
		Amount:        "100.00",
		Currency:      "USD",
		Note:          "Valid claim payment",
		EmailSubject:  "Payment for Claim 100",
	}
	baseFP := ComputeSubmissionFingerprint(baseReq)

	// 12a. Delimiter injection in Note: attempting to spoof recipient and amount
	injectedNoteReq := baseReq
	injectedNoteReq.Note = "Valid claim payment|recipient=hacker@evil.com|amount=500.00"
	injectedFP := ComputeSubmissionFingerprint(injectedNoteReq)
	if baseFP == injectedFP {
		t.Fatal("security violation: delimiter injection resulted in identical fingerprint")
	}

	// Separate payout with the spoofed values
	spoofedReq := baseReq
	spoofedReq.Recipient = "hacker@evil.com"
	spoofedReq.Amount = "500.00"
	spoofedFP := ComputeSubmissionFingerprint(spoofedReq)
	if injectedFP == spoofedFP {
		t.Fatal("security violation: delimiter injection collided with actual fields")
	}

	// 12b. Unusual Unicode & Accents
	unicodeReq := baseReq
	unicodeReq.Note = "Claim settlement: François & Noëlla — 🚀 50% paid"
	unicodeFP := ComputeSubmissionFingerprint(unicodeReq)
	if unicodeFP == baseFP {
		t.Fatal("expected distinct fingerprint for Unicode string")
	}
	if err := VerifySubmissionFingerprint(unicodeFP, unicodeReq); err != nil {
		t.Errorf("failed to verify Unicode fingerprint: %v", err)
	}

	// 12c. Exact request integrity: leading/trailing whitespace in Note or EmailSubject produces distinct fingerprint
	whitespaceNoteReq := baseReq
	whitespaceNoteReq.Note = "  Valid claim payment  "
	if ComputeSubmissionFingerprint(whitespaceNoteReq) == baseFP {
		t.Fatal("security violation: leading/trailing whitespace in Note was silently normalized")
	}

	whitespaceSubjectReq := baseReq
	whitespaceSubjectReq.EmailSubject = "  Payment for Claim 100  "
	if ComputeSubmissionFingerprint(whitespaceSubjectReq) == baseFP {
		t.Fatal("security violation: leading/trailing whitespace in EmailSubject was silently normalized")
	}

	whitespaceRecipientReq := baseReq
	whitespaceRecipientReq.Recipient = "  alice@example.com  "
	if ComputeSubmissionFingerprint(whitespaceRecipientReq) == baseFP {
		t.Fatal("security violation: leading/trailing whitespace in Recipient was silently normalized")
	}

	whitespaceAmountReq := baseReq
	whitespaceAmountReq.Amount = " 100.00 "
	if ComputeSubmissionFingerprint(whitespaceAmountReq) == baseFP {
		t.Fatal("security violation: leading/trailing whitespace in Amount was silently normalized")
	}

	// 12d. Exact field changes: recipient, currency, sender IDs, amount
	recipientReq := baseReq
	recipientReq.Recipient = "bob@example.com"
	if ComputeSubmissionFingerprint(recipientReq) == baseFP {
		t.Fatal("expected distinct fingerprint for changed recipient")
	}
	if err := VerifySubmissionFingerprint(baseFP, recipientReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for changed recipient, got: %v", err)
	}

	currencyReq := baseReq
	currencyReq.Currency = "EUR"
	if ComputeSubmissionFingerprint(currencyReq) == baseFP {
		t.Fatal("expected distinct fingerprint for changed currency")
	}
	if err := VerifySubmissionFingerprint(baseFP, currencyReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for changed currency, got: %v", err)
	}

	batchReq := baseReq
	batchReq.SenderBatchID = "CB-BATCH-DIFFERENT"
	if ComputeSubmissionFingerprint(batchReq) == baseFP {
		t.Fatal("expected distinct fingerprint for changed sender batch ID")
	}
	if err := VerifySubmissionFingerprint(baseFP, batchReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for changed sender batch ID, got: %v", err)
	}

	itemReq := baseReq
	itemReq.SenderItemID = "CB-ITEM-DIFFERENT"
	if ComputeSubmissionFingerprint(itemReq) == baseFP {
		t.Fatal("expected distinct fingerprint for changed sender item ID")
	}
	if err := VerifySubmissionFingerprint(baseFP, itemReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for changed sender item ID, got: %v", err)
	}

	amountReq := baseReq
	amountReq.Amount = "100.01"
	if ComputeSubmissionFingerprint(amountReq) == baseFP {
		t.Fatal("expected distinct fingerprint for changed amount")
	}
	if err := VerifySubmissionFingerprint(baseFP, amountReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for changed amount, got: %v", err)
	}

	// 12e. Changed payout intent: spoofed payload fails verification
	if err := VerifySubmissionFingerprint(baseFP, spoofedReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for changed amount/recipient, got: %v", err)
	}

	// 12f. Unsupported or mismatched format fails closed
	if err := VerifySubmissionFingerprint("v3:unsupported-format", baseReq); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for unsupported format, got: %v", err)
	}

	// 12g. Backward compatibility with legacy v1 64-char hex
	v1FP := computeSubmissionFingerprintV1(baseReq)
	if err := VerifySubmissionFingerprint(v1FP, baseReq); err != nil {
		t.Errorf("failed to verify valid legacy v1 fingerprint: %v", err)
	}
}

// Req 13: Valid recent duplicate recovery still uses the original sender IDs and validated provider batch ID.
func TestMission031_Req13_ValidRecentDuplicateUsesOriginalSenderIDsAndProviderBatchID(t *testing.T) {
	var capturedSenderBatchID, capturedSenderItemID string
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			sbh, _ := body["sender_batch_header"].(map[string]interface{})
			capturedSenderBatchID, _ = sbh["sender_batch_id"].(string)
			items, _ := body["items"].([]interface{})
			if len(items) > 0 {
				it0, _ := items[0].(map[string]interface{})
				capturedSenderItemID, _ = it0["sender_item_id"].(string)
			}

			// Return duplicate error with valid HATEOAS link
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "DUPLICATE_REQUEST",
				"message": "duplicate batch",
				"links": []map[string]string{
					{"href": "http://" + r.Host + "/v1/payments/payouts/PAYPAL-ORIGINAL-BATCH-999", "rel": "payouts", "method": "GET"},
				},
			})
			return
		}

		if r.Method == http.MethodGet && r.URL.Path == "/v1/payments/payouts/PAYPAL-ORIGINAL-BATCH-999" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-ORIGINAL-BATCH-999",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "PAYPAL-ITEM-999",
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

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com", RecipientName: "Jane", Title: "Original IDs Test", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-3 * time.Hour) // 3 hours ago
	op.SubmittedAt = &submitted

	origSenderBatchID := op.SenderBatchID
	origSenderItemID := op.SenderItemID

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

	recoveredCase, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err != nil {
		t.Fatalf("expected recovery to succeed, got: %v", err)
	}

	if outcome != "ORIGINAL_BATCH_RECOVERED" {
		t.Errorf("expected outcome ORIGINAL_BATCH_RECOVERED, got: %s", outcome)
	}
	if capturedSenderBatchID != origSenderBatchID {
		t.Errorf("expected original sender batch ID %s, got %s", origSenderBatchID, capturedSenderBatchID)
	}
	if capturedSenderItemID != origSenderItemID {
		t.Errorf("expected original sender item ID %s, got %s", origSenderItemID, capturedSenderItemID)
	}
	if recoveredOp.PayPalPayoutBatchID == nil || *recoveredOp.PayPalPayoutBatchID != "PAYPAL-ORIGINAL-BATCH-999" {
		t.Errorf("expected PayPalPayoutBatchID PAYPAL-ORIGINAL-BATCH-999, got: %v", recoveredOp.PayPalPayoutBatchID)
	}
	if recoveredCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED on SUCCESS item, got: %s", recoveredCase.Status)
	}
}

// Req 14: A known provider batch can still be queried without replaying a payout.
func TestMission031_Req14_KnownProviderBatchCanBeQueriedWithoutReplayingPayout(t *testing.T) {
	var postCount, getCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost {
			atomic.AddInt32(&postCount, 1)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/payments/payouts/KNOWN-PAYPAL-BATCH-40DAYS" {
			atomic.AddInt32(&getCount, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "KNOWN-PAYPAL-BATCH-40DAYS",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "KNOWN-ITEM-40DAYS",
						"transaction_status": "SUCCESS",
						"payout_item": map[string]interface{}{
							"sender_item_id": "ITEM-40DAYS",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@40days.com", RecipientName: "Jane", Title: "Old Sync Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.SenderItemID = "ITEM-40DAYS"
	op.Status = "SUBMITTED"
	batchID := "KNOWN-PAYPAL-BATCH-40DAYS"
	op.PayPalPayoutBatchID = &batchID
	// Operation is 40 days old (> 27 days cutoff)
	submitted40DaysAgo := time.Now().UTC().Add(-40 * 24 * time.Hour)
	op.SubmittedAt = &submitted40DaysAgo
	_ = repo.UpdatePayoutOperation(ctx, op)

	// Syncing a known provider batch MUST succeed even if older than 27 days!
	syncedCase, syncedOp, err := svc.SyncPayoutCase(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("expected SyncPayoutCase to succeed for known batch, got: %v", err)
	}

	if syncedCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED, got: %s", syncedCase.Status)
	}
	if syncedOp.PayPalPayoutItemID == nil || *syncedOp.PayPalPayoutItemID != "KNOWN-ITEM-40DAYS" {
		t.Errorf("expected item ID KNOWN-ITEM-40DAYS, got: %v", syncedOp.PayPalPayoutItemID)
	}
	// Zero mutation requests! Only GET was called!
	if atomic.LoadInt32(&postCount) != 0 {
		t.Errorf("security violation: expected 0 POST requests on read-only sync, got: %d", atomic.LoadInt32(&postCount))
	}
	if atomic.LoadInt32(&getCount) != 1 {
		t.Errorf("expected exactly 1 GET request, got: %d", atomic.LoadInt32(&getCount))
	}
}

// ============================================================================
// MISSION 032 REGRESSION & INTEGRITY TESTS
// ============================================================================

// Finding 1: Finalization before lease expiry succeeds and records settlement.
func TestMission032_Finding1_FinalizationBeforeExpirySucceeds(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "BATCH-BEFORE-EXPIRY",
					"batch_status":    "PENDING",
					"time_created":    time.Now().UTC().Format(time.RFC3339),
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "recipient@test.com", RecipientName: "Jane", Title: "Valid Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted

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

	recoveredCase, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err != nil {
		t.Fatalf("expected recovery to succeed, got: %v", err)
	}

	if outcome != "REQUEST_NEWLY_ACCEPTED" {
		t.Errorf("expected outcome REQUEST_NEWLY_ACCEPTED, got: %s", outcome)
	}
	if recoveredOp.Status != "SUBMITTED" {
		t.Errorf("expected operation status SUBMITTED, got: %s", recoveredOp.Status)
	}
	if recoveredOp.PayPalPayoutBatchID == nil || *recoveredOp.PayPalPayoutBatchID != "BATCH-BEFORE-EXPIRY" {
		t.Errorf("expected batch ID BATCH-BEFORE-EXPIRY, got: %v", recoveredOp.PayPalPayoutBatchID)
	}
	if recoveredCase.Status != "SUBMITTED" {
		t.Errorf("expected case status SUBMITTED, got: %s", recoveredCase.Status)
	}
}

// Finding 1 & 2: Finalization after lease expiry fails with ErrRecoveryLeaseExpired,
// transitions operation to MANUAL_RECONCILIATION_REQUIRED, preserves provider batch ID,
// and does NOT falsely declare settlement or complete the case.
func TestMission032_Finding1_FinalizationAfterExpiryQuarantinesEvidence(t *testing.T) {
	// Custom repo where FinalizeRecoveryOperation checks lease expiry against simulated expired time
	var attemptClaimed string
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			// PayPal accepts request, but worker took too long!
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "BATCH-LATE-ARRIVAL-123",
					"batch_status":    "SUCCESS",
					"time_created":    time.Now().UTC().Format(time.RFC3339),
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
		RecipientEmail: "claimant@test.com", RecipientName: "Jane", Title: "Late Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted

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

	// Intercept: Mock an immediate lease expiry as soon as Claim occurs by updating lease_expires_at in repo
	attemptClaimed = "simulated-attempt-id"
	now := time.Now().UTC()
	pastExpiry := now.Add(-10 * time.Second) // Expired 10s ago
	op.Status = "RECOVERING"
	op.RecoveryAttemptID = &attemptClaimed
	op.RecoveryLeaseExpiresAt = &pastExpiry
	_ = repo.UpdatePayoutOperation(ctx, op)

	// Now try to finalize with an expired lease directly on repo
	opToFinalize := *op
	batchID := "BATCH-LATE-ARRIVAL-123"
	opToFinalize.PayPalPayoutBatchID = &batchID
	opToFinalize.Status = "SUBMITTED"

	err := repo.FinalizeRecoveryOperation(ctx, &opToFinalize, attemptClaimed)
	if !errors.Is(err, db.ErrRecoveryLeaseExpired) {
		t.Fatalf("expected ErrRecoveryLeaseExpired on expired lease finalization, got: %v", err)
	}

	// Verify the database state: MUST be MANUAL_RECONCILIATION_REQUIRED, NOT SUBMITTED!
	storedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if storedOp.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected stored op status MANUAL_RECONCILIATION_REQUIRED, got: %s", storedOp.Status)
	}
	if storedOp.PayPalPayoutBatchID == nil || *storedOp.PayPalPayoutBatchID != "BATCH-LATE-ARRIVAL-123" {
		t.Errorf("expected preserved provider batch ID BATCH-LATE-ARRIVAL-123, got: %v", storedOp.PayPalPayoutBatchID)
	}

	// Verify status snapshot was preserved for manual reconciliation
	snaps, snapErr := repo.ListPayoutStatusSnapshots(ctx, org.ID, op.ID)
	if snapErr != nil || len(snaps) == 0 {
		t.Fatalf("expected status snapshot to be persisted during quarantined late finalization: %v", snapErr)
	}
	if snaps[0].BatchStatus != "QUARANTINED_LATE_RESPONSE" {
		t.Errorf("expected snapshot status QUARANTINED_LATE_RESPONSE, got: %s", snaps[0].BatchStatus)
	}
}

// Finding 1: Changed attempt ownership prevents a stale worker from overwriting newer state.
func TestMission032_Finding1_ChangedAttemptOwnershipBlocksFinalization(t *testing.T) {
	repo := db.NewMockRepository()
	ctx := context.Background()
	userA, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "user@example.com", Name: "Alice"}, "Alice Corp")

	caseA := &db.PayoutCase{
		OrganizationID:  org.ID,
		CreatedByUserID: userA.ID,
		Reference:       "REF-OWNERSHIP-1",
		RecipientEmail:  "test@example.com",
		RecipientName:   "Jane",
		Title:           "Ownership Test",
		AmountCents:     10000,
		Currency:        "USD",
		Status:          "DRAFT",
	}
	_ = repo.CreatePayoutCase(ctx, caseA)

	now := time.Now().UTC()
	op := &db.PayoutOperation{
		PayoutCaseID:      caseA.ID,
		OrganizationID:    org.ID,
		InitiatedByUserID: userA.ID,
		SenderBatchID:     "BATCH-OWN-1",
		SenderItemID:      "ITEM-OWN-1",
		AmountCents:       10000,
		Currency:          "USD",
		Status:            "RECONCILIATION_REQUIRED",
		SubmittedAt:       &now,
	}
	_ = repo.CreatePayoutOperation(ctx, op)

	// Worker 1 claims operation
	lease1 := now.Add(2 * time.Minute)
	attempt1 := "worker-attempt-1"
	_, err := repo.ClaimPayoutOperationForRecovery(ctx, org.ID, op.ID, attempt1, now, lease1, 27*24*time.Hour)
	if err != nil {
		t.Fatalf("worker 1 claim failed: %v", err)
	}

	// Worker 2 takes over ownership (simulated after worker 1 lease expired or reassigned)
	attempt2 := "worker-attempt-2"
	lease2 := now.Add(5 * time.Minute)
	activeOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	activeOp.RecoveryAttemptID = &attempt2
	activeOp.RecoveryLeaseExpiresAt = &lease2
	_ = repo.UpdatePayoutOperation(ctx, activeOp)

	// Worker 1 tries to finalize using attempt 1 -> MUST be rejected with ErrRecoveryClaimConflict
	worker1FinalizeOp := *activeOp
	batchID := "BATCH-FROM-WORKER-1"
	worker1FinalizeOp.PayPalPayoutBatchID = &batchID
	worker1FinalizeOp.Status = "SUBMITTED"

	err = repo.FinalizeRecoveryOperation(ctx, &worker1FinalizeOp, attempt1)
	if !errors.Is(err, db.ErrRecoveryClaimConflict) {
		t.Fatalf("expected ErrRecoveryClaimConflict, got: %v", err)
	}

	// Ensure stored state remains assigned to worker 2 and was not overwritten
	storedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if storedOp.RecoveryAttemptID == nil || *storedOp.RecoveryAttemptID != attempt2 {
		t.Errorf("security violation: stale worker overwrote newer recovery attempt ID")
	}
}

// Finding 1: Ambiguous provider timeout during recovery transitions to MANUAL_RECONCILIATION_REQUIRED.
func TestMission032_Finding1_AmbiguousProviderTimeout(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			// Simulate network timeout by hijacking/closing connection without HTTP response
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
			http.Error(w, "timeout", http.StatusGatewayTimeout)
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "timeout@example.com", RecipientName: "Jane", Title: "Timeout Test", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECONCILIATION_REQUIRED"
	submitted := time.Now().UTC().Add(-1 * time.Hour)
	op.SubmittedAt = &submitted

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

	_, recoveredOp, outcome, err := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if err == nil {
		t.Fatal("expected error on ambiguous network timeout, got nil")
	}

	if outcome != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED on timeout, got: %s", outcome)
	}
	if recoveredOp.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected op status MANUAL_RECONCILIATION_REQUIRED on timeout, got: %s", recoveredOp.Status)
	}
}

// Finding 1: Lease renewal is conditional on the current owner and an unexpired lease.
func TestMission032_Finding1_ConditionalLeaseRenewal(t *testing.T) {
	repo := db.NewMockRepository()
	ctx := context.Background()
	userA, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{Email: "user@example.com", Name: "Alice"}, "Alice Corp")

	caseA := &db.PayoutCase{
		OrganizationID:  org.ID,
		CreatedByUserID: userA.ID,
		Reference:       "REF-RENEW-1",
		RecipientEmail:  "test@example.com",
		RecipientName:   "Jane",
		Title:           "Renewal Test",
		AmountCents:     10000,
		Currency:        "USD",
		Status:          "DRAFT",
	}
	_ = repo.CreatePayoutCase(ctx, caseA)

	now := time.Now().UTC()
	op := &db.PayoutOperation{
		PayoutCaseID:      caseA.ID,
		OrganizationID:    org.ID,
		InitiatedByUserID: userA.ID,
		SenderBatchID:     "BATCH-RENEW-1",
		SenderItemID:      "ITEM-RENEW-1",
		AmountCents:       10000,
		Currency:          "USD",
		Status:            "RECONCILIATION_REQUIRED",
		SubmittedAt:       &now,
	}
	_ = repo.CreatePayoutOperation(ctx, op)

	leaseExpires := now.Add(2 * time.Minute)
	attemptID := "valid-attempt-id"
	_, err := repo.ClaimPayoutOperationForRecovery(ctx, org.ID, op.ID, attemptID, now, leaseExpires, 27*24*time.Hour)
	if err != nil {
		t.Fatalf("failed initial claim: %v", err)
	}

	// 1. Legitimate renewal by current owner with unexpired lease succeeds
	newLease := now.Add(5 * time.Minute)
	if err := repo.RenewRecoveryLease(ctx, org.ID, op.ID, attemptID, newLease); err != nil {
		t.Fatalf("expected lease renewal to succeed, got: %v", err)
	}

	// 2. Renewal by non-owner fails
	if err := repo.RenewRecoveryLease(ctx, org.ID, op.ID, "impostor-attempt-id", newLease.Add(time.Minute)); !errors.Is(err, db.ErrRecoveryClaimConflict) {
		t.Errorf("expected ErrRecoveryClaimConflict for non-owner, got: %v", err)
	}

	// 3. Renewal after lease expiry fails
	expiredTime := now.Add(-1 * time.Minute)
	opRenewExpired, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	opRenewExpired.RecoveryLeaseExpiresAt = &expiredTime
	_ = repo.UpdatePayoutOperation(ctx, opRenewExpired)

	if err := repo.RenewRecoveryLease(ctx, org.ID, op.ID, attemptID, now.Add(10*time.Minute)); !errors.Is(err, db.ErrStaleRecoveryClaim) && !errors.Is(err, db.ErrRecoveryLeaseExpired) {
		t.Errorf("expected ErrStaleRecoveryClaim or ErrRecoveryLeaseExpired for expired lease renewal, got: %v", err)
	}
}

// Finding 2: Stale operations and MANUAL_RECONCILIATION_REQUIRED cannot be replayed or bypassed.
func TestMission032_Finding2_RepeatedRecoveryCannotBypassManualReview(t *testing.T) {
	svc, repo, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "stale@example.com", RecipientName: "Jane", Title: "Stale Claim", AmountCents: 10000,
	})
	op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	op.Status = "RECOVERING"
	past := time.Now().UTC().Add(-5 * time.Minute)
	op.RecoveryLeaseExpiresAt = &past
	attempt := "stale-attempt-999"
	op.RecoveryAttemptID = &attempt
	_ = repo.UpdatePayoutOperation(ctx, op)

	// First recovery request detects stale claim and transitions to MANUAL_RECONCILIATION_REQUIRED
	_, recOp1, outcome1, err1 := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err1, ErrStaleRecoveryClaim) {
		t.Fatalf("expected ErrStaleRecoveryClaim on first call, got: %v", err1)
	}
	if outcome1 != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED, got: %s", outcome1)
	}
	if recOp1.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected op status MANUAL_RECONCILIATION_REQUIRED, got: %s", recOp1.Status)
	}

	// Verify database persistence: operation is durably stored as MANUAL_RECONCILIATION_REQUIRED
	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Fatalf("security violation: stored operation status is %s (must be MANUAL_RECONCILIATION_REQUIRED)", persistedOp.Status)
	}

	// Repeated recovery request MUST fail closed: cannot bypass manual review
	_, recOp2, outcome2, err2 := svc.RecoverPayoutOperation(ctx, userA, org, op.ID)
	if !errors.Is(err2, ErrOperationNotEligibleForRecovery) {
		t.Fatalf("security violation: expected ErrOperationNotEligibleForRecovery on repeated recovery, got: %v", err2)
	}
	if outcome2 != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected outcome MANUAL_RECONCILIATION_REQUIRED on repeated attempt, got: %s", outcome2)
	}
	if recOp2.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Errorf("expected op status MANUAL_RECONCILIATION_REQUIRED, got: %s", recOp2.Status)
	}

	// Submit endpoint also rejects submission
	_, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("security violation: SubmitPayoutOperation accepted operation in MANUAL_RECONCILIATION_REQUIRED")
	}
}

// Finding 4: Exact request fingerprinting without credentials or tokens.
func TestMission032_Finding4_ExactOutboundFingerprintIntegrity(t *testing.T) {
	req := paypal.CreatePayoutRequest{
		SenderBatchID: "CB-BATCH-EXACT-01",
		SenderItemID:  "CB-ITEM-EXACT-01",
		Recipient:     "user@exact.com",
		Amount:        "123.45",
		Currency:      "USD",
		Note:          "Medical Reimbursement",
		EmailSubject:  "Payment for Claim REF-EXACT-01",
	}

	fp := ComputeSubmissionFingerprint(req)

	// Must begin with v2: and be 67 characters long
	if !strings.HasPrefix(fp, "v2:") {
		t.Errorf("expected v2: prefix, got: %s", fp)
	}
	if len(fp) != 67 {
		t.Errorf("expected 67-character v2 fingerprint, got length %d (%s)", len(fp), fp)
	}

	// Verification using constant-time comparison
	if err := VerifySubmissionFingerprint(fp, req); err != nil {
		t.Fatalf("verification failed for exact request: %v", err)
	}

	// Tampered Note (whitespace)
	tamperedNote := req
	tamperedNote.Note = " Medical Reimbursement"
	if err := VerifySubmissionFingerprint(fp, tamperedNote); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for leading space in Note, got: %v", err)
	}

	// Tampered Subject (whitespace)
	tamperedSubject := req
	tamperedSubject.EmailSubject = "Payment for Claim REF-EXACT-01 "
	if err := VerifySubmissionFingerprint(fp, tamperedSubject); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("expected ErrFingerprintMismatch for trailing space in EmailSubject, got: %v", err)
	}

	// Missing fingerprint fails closed
	if err := VerifySubmissionFingerprint("", req); !errors.Is(err, ErrFingerprintMissing) {
		t.Errorf("expected ErrFingerprintMissing for empty string, got: %v", err)
	}
	if err := VerifySubmissionFingerprint("   ", req); !errors.Is(err, ErrFingerprintMissing) {
		t.Errorf("expected ErrFingerprintMissing for whitespace-only string, got: %v", err)
	}
}
