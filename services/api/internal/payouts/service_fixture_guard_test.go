package payouts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

func TestFixtureGuard_ExternalOperationsBlocked(t *testing.T) {
	var requestCount int64

	// Counting test double server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"simulated_token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		AppEnv:               "test",
		DatabaseURL:          "postgres://mock",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_client_id",
		PayPalClientSecret:   "test_client_secret",
		PayPalBaseURL:        server.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	repo := db.NewMockRepository()
	policyEngine := policy.NewEngine("")
	ppClient, err := paypal.NewTestClient(cfg, server.Client(), server.URL)
	if err != nil {
		t.Fatalf("failed constructing test paypal client: %v", err)
	}

	svc := NewService(cfg, repo, policyEngine, ppClient)
	ctx := context.Background()

	// Seed user and org
	now := time.Now().UTC()
	hash, _ := crypto.HashPassword("TestPass123!")
	user := &db.User{
		Email:           "operator@claimbridge.internal",
		PasswordHash:    hash,
		Name:            "Operator",
		EmailVerified:   true,
		EmailVerifiedAt: &now,
		IsActive:        true,
	}
	user, org, err := repo.CreateUserWithPersonalOrg(ctx, user, "Test Org")
	if err != nil {
		t.Fatalf("failed creating user and org: %v", err)
	}

	// Create synthetic fixture case
	fixtureCase := &db.PayoutCase{
		OrganizationID:       org.ID,
		CreatedByUserID:      user.ID,
		Reference:            "DEMO-FIXTURE-001",
		RecipientEmail:       "fixture@example.com",
		RecipientName:        "Fixture Recipient",
		Title:                "[DEMO TEST FIXTURE] Synthetic Test Payout",
		Currency:             "USD",
		AmountCents:          15000,
		Status:               "SUBMITTED",
		DualApprovalRequired: false,
		IsSyntheticFixture:   true, // FIXTURE
	}
	if err := repo.CreatePayoutCase(ctx, fixtureCase); err != nil {
		t.Fatalf("failed creating fixture case: %v", err)
	}

	batchID := "SIMULATED-DEMO-BATCH-001"
	itemID := "SIMULATED-DEMO-ITEM-001"
	batchStatus := "SUCCESS"
	itemStatus := "UNCLAIMED"
	fixtureOp := &db.PayoutOperation{
		PayoutCaseID:         fixtureCase.ID,
		OrganizationID:       org.ID,
		InitiatedByUserID:    user.ID,
		SenderBatchID:        "CB-BATCH-FIXTURE-001",
		SenderItemID:         "CB-ITEM-FIXTURE-001",
		AmountCents:          15000,
		Currency:             "USD",
		Status:               "PREPARED",
		PayPalPayoutBatchID:  &batchID,
		PayPalBatchStatus:    &batchStatus,
		PayPalPayoutItemID:   &itemID,
		PayPalItemStatus:     &itemStatus,
		RequiresDualApproval: false,
		IsSyntheticFixture:   true, // FIXTURE
	}
	if err := repo.CreatePayoutOperation(ctx, fixtureOp); err != nil {
		t.Fatalf("failed creating fixture op: %v", err)
	}

	// 1. Test SubmitPayoutOperation on fixture case/operation
	t.Run("SubmitBlockedOnFixture", func(t *testing.T) {
		op, err := svc.SubmitPayoutOperation(ctx, user, org, fixtureCase.ID, fixtureOp.ID)
		if op != nil {
			t.Errorf("expected returned op to be nil on blocked submission")
		}
		if !errors.Is(err, ErrFixtureExternalOperationForbidden) {
			t.Fatalf("expected ErrFixtureExternalOperationForbidden, got: %v", err)
		}
		if atomic.LoadInt64(&requestCount) != 0 {
			t.Errorf("expected zero outbound requests to PayPal, got: %d", atomic.LoadInt64(&requestCount))
		}
	})

	// 2. Test SyncPayoutCase on fixture case
	t.Run("SyncBlockedOnFixture", func(t *testing.T) {
		_, _, err := svc.SyncPayoutCase(ctx, org.ID, fixtureCase.ID)
		if !errors.Is(err, ErrFixtureExternalOperationForbidden) {
			t.Fatalf("expected ErrFixtureExternalOperationForbidden, got: %v", err)
		}
		if atomic.LoadInt64(&requestCount) != 0 {
			t.Errorf("expected zero outbound requests to PayPal, got: %d", atomic.LoadInt64(&requestCount))
		}
	})

	// 3. Test CancelPayoutOperation on fixture operation
	t.Run("CancelBlockedOnFixture", func(t *testing.T) {
		op, err := svc.CancelPayoutOperation(ctx, user, org, fixtureOp.ID)
		if op != nil {
			t.Errorf("expected returned op to be nil on blocked cancellation")
		}
		if !errors.Is(err, ErrFixtureExternalOperationForbidden) {
			t.Fatalf("expected ErrFixtureExternalOperationForbidden, got: %v", err)
		}
		if atomic.LoadInt64(&requestCount) != 0 {
			t.Errorf("expected zero outbound requests to PayPal, got: %d", atomic.LoadInt64(&requestCount))
		}
	})

	// 4. Test that normal non-fixture operations are NOT blocked by the fixture guard
	t.Run("GenuineNonFixtureAllowed", func(t *testing.T) {
		realCase := &db.PayoutCase{
			OrganizationID:       org.ID,
			CreatedByUserID:      user.ID,
			Reference:            "REAL-CLAIM-002",
			RecipientEmail:       "real@example.com",
			RecipientName:        "Real Recipient",
			Title:                "Real Legitimate Claim",
			Currency:             "USD",
			AmountCents:          10000,
			Status:               "DRAFT",
			DualApprovalRequired: false,
			IsSyntheticFixture:   false, // GENUINE
		}
		if err := repo.CreatePayoutCase(ctx, realCase); err != nil {
			t.Fatalf("failed creating real case: %v", err)
		}

		realOp, err := svc.InitiatePayoutOperation(ctx, user, org, realCase.ID)
		if err != nil {
			t.Fatalf("failed initiating real op: %v", err)
		}
		if realOp.IsSyntheticFixture {
			t.Errorf("real op should NOT be marked IsSyntheticFixture")
		}

		// When submitting real op, it should pass fixture guard and attempt PayPal call
		// (which will increment our requestCount)
		_, _ = svc.SubmitPayoutOperation(ctx, user, org, realCase.ID, realOp.ID)

		if atomic.LoadInt64(&requestCount) == 0 {
			t.Errorf("expected real operation to dispatch outbound request to PayPal")
		}
	})
}
