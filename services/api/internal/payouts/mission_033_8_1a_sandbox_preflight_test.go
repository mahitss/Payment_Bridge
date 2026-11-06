package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

// Helper to create test service with custom mock PayPal handler
func setupPreflightTestService(t *testing.T, paypalHandler http.HandlerFunc) (*Service, *db.MockRepository, *db.User, *db.User, *db.Organization) {
	t.Helper()
	repo := db.NewMockRepository()
	ctx := context.Background()

	userA, orgA, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "initiator@example.com",
		Name:          "Alice Initiator",
		EmailVerified: true,
	}, "Preflight Test Org")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}

	userB, _, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "approver@example.com",
		Name:          "Bob Approver",
		EmailVerified: true,
	}, "Approver Org")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "sandbox-test-client-id",
		PayPalClientSecret:   "sandbox-test-client-secret",
		PayPalBaseURL:        "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout: 2 * time.Second,
	}

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

// 1. Missing Payouts scope prevents payout dispatch and does NOT transition operation to SUBMITTING
func TestPreflight_MissingPayoutsScope_FailsClosedBeforeSubmitting(t *testing.T) {
	var postPayoutCount int32

	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			// Returns valid token but lacks https://uri.paypal.com/payments/payouts
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "valid-token-no-scope",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        "openid email profile",
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postPayoutCount, 1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "SHOULD_NEVER_BE_CALLED",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupPreflightTestService(t, paypalHandler)
	ctx := context.Background()

	// Create single-officer case ($100.00 USD -> status PREPARED)
	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Preflight Scope Test Claim",
		AmountCents:    10000,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}
	if op.Status != "PREPARED" {
		t.Fatalf("expected op status PREPARED before submit, got %s", op.Status)
	}

	// Attempt submit
	submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected SubmitPayoutOperation to fail closed due to missing payouts scope, got nil error")
	}

	if !errors.Is(submitErr, paypal.ErrMissingPayoutsScope) {
		t.Fatalf("expected error to wrap ErrMissingPayoutsScope, got: %v", submitErr)
	}

	// Invariant 1: Zero outbound mutations sent to /v1/payments/payouts
	if count := atomic.LoadInt32(&postPayoutCount); count != 0 {
		t.Errorf("security violation: dispatched %d POST /v1/payments/payouts mutations despite missing payouts scope", count)
	}

	// Invariant 2: Operation must NOT be in SUBMITTING or FAILED
	persistedOp, err := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if err != nil {
		t.Fatalf("failed fetching persisted op: %v", err)
	}
	if persistedOp.Status != "PREPARED" {
		t.Errorf("expected operation status to remain PREPARED, got: %s", persistedOp.Status)
	}
	if submittedOp != nil {
		t.Errorf("expected submittedOp to be nil on preflight rejection, got %v", submittedOp)
	}

	// Invariant 3: Audit event recorded
	foundPreflightAudit := false
	for _, e := range repo.AuditEvents {
		if e.EventType == "PAYOUT_AUTHORIZATION_PREFLIGHT_FAILED" {
			foundPreflightAudit = true
			break
		}
	}
	if !foundPreflightAudit {
		t.Errorf("expected PAYOUT_AUTHORIZATION_PREFLIGHT_FAILED audit event to be recorded")
	}
}

// 2. OAuth 401 is distinguished from HTTP 403 and does not transition row to SUBMITTING
func TestPreflight_OAuth401_DistinguishedAndFailsClosed(t *testing.T) {
	var postPayoutCount int32

	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":             "invalid_client",
				"error_description": "Client authentication failed",
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postPayoutCount, 1)
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupPreflightTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "OAuth 401 Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed creating case: %v", err)
	}
	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed initiating op: %v", err)
	}

	_, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected error on OAuth 401, got nil")
	}

	// Must be ErrUnauthorized, NOT ErrForbidden or ambiguous timeout
	if !errors.Is(submitErr, paypal.ErrUnauthorized) {
		t.Fatalf("expected error to wrap ErrUnauthorized, got: %v", submitErr)
	}
	if errors.Is(submitErr, paypal.ErrForbidden) {
		t.Errorf("OAuth 401 error must not be classified as ErrForbidden")
	}

	// Invariant: Zero mutations and status preserved
	if count := atomic.LoadInt32(&postPayoutCount); count != 0 {
		t.Errorf("dispatched %d payouts mutations on OAuth 401", count)
	}
	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "PREPARED" {
		t.Errorf("expected op status PREPARED, got: %s", persistedOp.Status)
	}
}

// 3. Scoped token followed by provider HTTP 403 preserves structured provider diagnostics and classifies account restriction
func TestPreflight_ScopedTokenFollowedByProvider403_AccountRestricted(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			// Genuine OAuth success with payouts scope
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "scoped-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        "openid " + paypal.PayPalPayoutsScope,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			w.Header().Set("Paypal-Debug-Id", "f1628730219ad")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "PAYOUT_NOT_AVAILABLE",
				"message":  "The account is not eligible for payouts",
				"debug_id": "f1628730219ad",
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupPreflightTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Provider 403 Account Restriction Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed creating case: %v", err)
	}
	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed initiating op: %v", err)
	}

	failedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected error on provider 403, got nil")
	}

	// Must be classified as account restricted and forbidden
	if !errors.Is(submitErr, ErrPayPalAccountRestricted) && !errors.Is(submitErr, paypal.ErrAccountRestricted) {
		t.Fatalf("expected ErrPayPalAccountRestricted, got: %v", submitErr)
	}
	if !errors.Is(submitErr, paypal.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got: %v", submitErr)
	}

	// Structured diagnostics extracted via AsAPIForbiddenError
	fe, ok := paypal.AsAPIForbiddenError(submitErr)
	if !ok {
		t.Fatal("expected AsAPIForbiddenError to return true")
	}
	if fe.Name != "PAYOUT_NOT_AVAILABLE" {
		t.Errorf("expected error Name 'PAYOUT_NOT_AVAILABLE', got %q", fe.Name)
	}
	if fe.DebugID != "f1628730219ad" {
		t.Errorf("expected DebugID 'f1628730219ad', got %q", fe.DebugID)
	}
	if !fe.IsAccountRestricted() {
		t.Errorf("expected fe.IsAccountRestricted() to return true")
	}

	// Invariant: Operation is transitioned to FAILED and captures diagnostics in DB
	if failedOp.Status != "FAILED" {
		t.Errorf("expected returned op status FAILED, got %s", failedOp.Status)
	}
	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "FAILED" {
		t.Errorf("expected persisted op status FAILED, got %s", persistedOp.Status)
	}
	if persistedOp.RawProviderResponse == nil {
		t.Fatal("expected RawProviderResponse to be populated")
	}
	if persistedOp.RawProviderResponse["error_name"] != "PAYOUT_NOT_AVAILABLE" {
		t.Errorf("expected error_name 'PAYOUT_NOT_AVAILABLE', got %v", persistedOp.RawProviderResponse["error_name"])
	}
	if persistedOp.RawProviderResponse["debug_id"] != "f1628730219ad" {
		t.Errorf("expected debug_id 'f1628730219ad', got %v", persistedOp.RawProviderResponse["debug_id"])
	}

	// Invariant: Audit event PAYOUT_SUBMISSION_REJECTED recorded with diagnostics
	var rejectEvent *db.AuditEvent
	for _, e := range repo.AuditEvents {
		if e.EventType == "PAYOUT_SUBMISSION_REJECTED" {
			rejectEvent = e
			break
		}
	}
	if rejectEvent == nil {
		t.Fatal("expected PAYOUT_SUBMISSION_REJECTED audit event")
	}
	if rejectEvent.Metadata["provider_error_name"] != "PAYOUT_NOT_AVAILABLE" {
		t.Errorf("expected audit provider_error_name 'PAYOUT_NOT_AVAILABLE', got %v", rejectEvent.Metadata["provider_error_name"])
	}
	if rejectEvent.Metadata["provider_debug_id"] != "f1628730219ad" {
		t.Errorf("expected audit provider_debug_id 'f1628730219ad', got %v", rejectEvent.Metadata["provider_debug_id"])
	}
}

// 4. Successful mock Payouts API response persists returned batch ID and transitions to SUBMITTED
func TestPreflight_SuccessfulPayout_PersistsBatchID(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "valid-payouts-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "BATCH-PROVEN-9999",
					"batch_status":    "PENDING",
					"time_created":    time.Now().UTC().Format(time.RFC3339),
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	svc, repo, userA, _, org := setupPreflightTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Success Batch Persistence Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed creating case: %v", err)
	}
	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed initiating op: %v", err)
	}

	submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr != nil {
		t.Fatalf("unexpected error on valid submit: %v", submitErr)
	}

	if submittedOp.Status != "SUBMITTED" {
		t.Errorf("expected status SUBMITTED, got %s", submittedOp.Status)
	}
	if submittedOp.PayPalPayoutBatchID == nil || *submittedOp.PayPalPayoutBatchID != "BATCH-PROVEN-9999" {
		t.Errorf("expected PayPalPayoutBatchID BATCH-PROVEN-9999, got %v", submittedOp.PayPalPayoutBatchID)
	}

	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "SUBMITTED" {
		t.Errorf("expected persisted status SUBMITTED, got %s", persistedOp.Status)
	}
	if persistedOp.PayPalPayoutBatchID == nil || *persistedOp.PayPalPayoutBatchID != "BATCH-PROVEN-9999" {
		t.Errorf("expected persisted batch ID BATCH-PROVEN-9999, got %v", persistedOp.PayPalPayoutBatchID)
	}

	persCase, _ := repo.GetPayoutCaseByID(ctx, org.ID, pCase.ID)
	if persCase.Status != "SUBMITTED" {
		t.Errorf("expected persisted case status SUBMITTED, got %s", persCase.Status)
	}
}

// 5. Synthetic demo fixtures cannot trigger external payout mutations
func TestPreflight_SyntheticDemoFixture_BlockedFromExternalMutation(t *testing.T) {
	var outboundCallCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&outboundCallCount, 1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})

	svc, repo, userA, _, org := setupPreflightTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Demo Synthetic Fixture Test",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed creating case: %v", err)
	}
	// Mark case as synthetic demo fixture in mock repository
	pCase.IsSyntheticFixture = true
	repo.Cases[pCase.ID].IsSyntheticFixture = true

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed initiating op: %v", err)
	}
	op.IsSyntheticFixture = true
	repo.Operations[op.ID].IsSyntheticFixture = true

	_, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected ErrFixtureExternalOperationForbidden, got nil")
	}

	if !errors.Is(submitErr, ErrFixtureExternalOperationForbidden) {
		t.Fatalf("expected ErrFixtureExternalOperationForbidden, got: %v", submitErr)
	}

	// Invariant: Zero outbound calls
	if count := atomic.LoadInt32(&outboundCallCount); count != 0 {
		t.Errorf("security violation: synthetic demo fixture dispatched %d outbound calls", count)
	}

	// Invariant: Operation remains in PREPARED status
	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "PREPARED" {
		t.Errorf("expected op status PREPARED, got %s", persistedOp.Status)
	}
}
