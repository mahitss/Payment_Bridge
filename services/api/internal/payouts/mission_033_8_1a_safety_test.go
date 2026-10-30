package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

// Case A: Initial submission receives HTTP 500
// - Operation transitions to RECONCILIATION_REQUIRED
// - Audit event recorded
// - No replacement sender IDs generated
// - No blind retry performed
func TestMission033_8_1A_CaseA_InitialSubmissionHTTP500(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
				"scope":        paypal.PayPalPayoutsScope,
			})
		case "/v1/payments/payouts":
			atomic.AddInt32(&postCount, 1)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "INTERNAL_SERVER_ERROR",
				"message": "PayPal system temporarily unavailable",
			})
		default:
			http.NotFound(w, r)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Alice Claimant",
		Title:          "Claim Case A",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	origBatchID := op.SenderBatchID
	origItemID := op.SenderItemID

	submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected error on HTTP 500 submission, got nil")
	}

	if !paypal.IsAmbiguousOutcome(submitErr) {
		t.Fatalf("expected ambiguous outcome error, got: %v", submitErr)
	}

	if submittedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected status RECONCILIATION_REQUIRED, got: %s", submittedOp.Status)
	}

	if submittedOp.SenderBatchID != origBatchID {
		t.Errorf("sender_batch_id changed: expected %s, got %s", origBatchID, submittedOp.SenderBatchID)
	}
	if submittedOp.SenderItemID != origItemID {
		t.Errorf("sender_item_id changed: expected %s, got %s", origItemID, submittedOp.SenderItemID)
	}

	if count := atomic.LoadInt32(&postCount); count != 1 {
		t.Errorf("expected exactly 1 outbound POST request (no blind retry), got: %d", count)
	}

	// Verify repo state
	persistedOp, err := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if err != nil {
		t.Fatalf("failed to get op from repo: %v", err)
	}
	if persistedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected persisted status RECONCILIATION_REQUIRED, got: %s", persistedOp.Status)
	}

	// Verify audit event
	auditEvents := repo.AuditEvents
	foundAudit := false
	for _, a := range auditEvents {
		if a.EventType == "PAYOUT_AMBIGUOUS_TIMEOUT" {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Errorf("expected PAYOUT_AMBIGUOUS_TIMEOUT audit event to be recorded")
	}
}

// Case B: Initial submission receives HTTP 503
// - Operation transitions to RECONCILIATION_REQUIRED
// - Audit event recorded
// - Identifiers preserved
func TestMission033_8_1A_CaseB_InitialSubmissionHTTP503(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
				"scope":        paypal.PayPalPayoutsScope,
			})
		case "/v1/payments/payouts":
			atomic.AddInt32(&postCount, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "SERVICE_UNAVAILABLE",
				"message": "PayPal service down for maintenance",
			})
		default:
			http.NotFound(w, r)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Bob Claimant",
		Title:          "Claim Case B",
		AmountCents:    12000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	origBatchID := op.SenderBatchID
	origItemID := op.SenderItemID

	submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected error on HTTP 503 submission, got nil")
	}

	if !paypal.IsAmbiguousOutcome(submitErr) {
		t.Fatalf("expected ambiguous outcome error, got: %v", submitErr)
	}

	if submittedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected status RECONCILIATION_REQUIRED, got: %s", submittedOp.Status)
	}
	if submittedOp.SenderBatchID != origBatchID || submittedOp.SenderItemID != origItemID {
		t.Errorf("identifiers altered on HTTP 503 ambiguous response")
	}
	if count := atomic.LoadInt32(&postCount); count != 1 {
		t.Errorf("expected exactly 1 outbound POST, got: %d", count)
	}

	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected persisted status RECONCILIATION_REQUIRED, got: %s", persistedOp.Status)
	}
}

// Case C: Initial submission network timeout
// - Operation transitions to RECONCILIATION_REQUIRED
// - Audit event recorded
// - Identifiers preserved
func TestMission033_8_1A_CaseC_InitialSubmissionNetworkTimeout(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
				"scope":        paypal.PayPalPayoutsScope,
			})
		case "/v1/payments/payouts":
			// Simulate slow response exceeding timeout
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	// Set a very short request timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	pCase, err := svc.CreatePayoutCase(context.Background(), userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Charlie Claimant",
		Title:          "Claim Case C",
		AmountCents:    8000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(context.Background(), userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	origBatchID := op.SenderBatchID
	origItemID := op.SenderItemID

	submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected timeout error on submission, got nil")
	}

	if !paypal.IsAmbiguousOutcome(submitErr) {
		t.Fatalf("expected ambiguous timeout error, got: %v", submitErr)
	}

	if submittedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected status RECONCILIATION_REQUIRED, got: %s", submittedOp.Status)
	}
	if submittedOp.SenderBatchID != origBatchID || submittedOp.SenderItemID != origItemID {
		t.Errorf("identifiers altered on timeout")
	}

	persistedOp, _ := repo.GetPayoutOperationByID(context.Background(), org.ID, op.ID)
	if persistedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected persisted status RECONCILIATION_REQUIRED, got: %s", persistedOp.Status)
	}
}

// Case D: Response body read failure
// - Treated as ambiguous
// - Operation transitions to RECONCILIATION_REQUIRED
func TestMission033_8_1A_CaseD_ResponseBodyReadFailure(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
				"scope":        paypal.PayPalPayoutsScope,
			})
		case "/v1/payments/payouts":
			// Announce 10000 bytes then immediately close raw connection
			w.Header().Set("Content-Length", "10000")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, _ := hijacker.Hijack()
				conn.Close()
			}
		default:
			http.NotFound(w, r)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Dana Claimant",
		Title:          "Claim Case D",
		AmountCents:    9500,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected error on response body read failure, got nil")
	}

	if !paypal.IsAmbiguousOutcome(submitErr) {
		t.Fatalf("expected ambiguous outcome error on read failure, got: %v", submitErr)
	}

	if submittedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected status RECONCILIATION_REQUIRED, got: %s", submittedOp.Status)
	}

	persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedOp.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("expected persisted status RECONCILIATION_REQUIRED, got: %s", persistedOp.Status)
	}
}

// Case E: Definitive provider rejection (400, 403)
// - Operation transitions to FAILED
// - Must NOT transition to RECONCILIATION_REQUIRED
// - Clear error details preserved
func TestMission033_8_1A_CaseE_DefinitiveProviderRejection(t *testing.T) {
	t.Run("HTTP 400 Bad Request transitions to FAILED", func(t *testing.T) {
		paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v1/oauth2/token":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "fake-token",
					"token_type":   "Bearer",
					"expires_in":   3600,
					"app_id":       "APP-SANDBOX-TEST",
					"scope":        paypal.PayPalPayoutsScope,
				})
			case "/v1/payments/payouts":
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"name":    "INVALID_PARAMETER_VALUE",
					"message": "The value of a field is invalid",
				})
			default:
				http.NotFound(w, r)
			}
		})

		svc, repo, userA, _, org := setupTestService(t, paypalHandler)
		ctx := context.Background()

		pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
			RecipientEmail: "bad@example.com", RecipientName: "Bad", Title: "Case 400", AmountCents: 5000,
		})
		op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)

		submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
		if submitErr == nil {
			t.Fatal("expected error on 400, got nil")
		}

		if paypal.IsAmbiguousOutcome(submitErr) {
			t.Errorf("400 must NOT be classified as ambiguous outcome")
		}
		if submittedOp.Status != "FAILED" {
			t.Errorf("expected status FAILED, got: %s", submittedOp.Status)
		}
		if submittedOp.Status == "RECONCILIATION_REQUIRED" {
			t.Errorf("400 must NOT transition to RECONCILIATION_REQUIRED")
		}
		if submittedOp.ErrorMessage == nil || !strings.Contains(*submittedOp.ErrorMessage, "400") {
			t.Errorf("expected error message to preserve status 400 details, got: %v", submittedOp.ErrorMessage)
		}

		persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
		if persistedOp.Status != "FAILED" {
			t.Errorf("expected persisted status FAILED, got: %s", persistedOp.Status)
		}
	})

	t.Run("HTTP 403 Forbidden transitions to FAILED with details", func(t *testing.T) {
		paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v1/oauth2/token":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "fake-token",
					"token_type":   "Bearer",
					"expires_in":   3600,
					"app_id":       "APP-SANDBOX-TEST",
					"scope":        paypal.PayPalPayoutsScope,
				})
			case "/v1/payments/payouts":
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"name":     "NOT_AUTHORIZED",
					"message":  "Authorization failed",
					"debug_id": "dbg-12345",
					"details": []map[string]interface{}{
						{"issue": "PAYER_ACCOUNT_RESTRICTED"},
					},
				})
			default:
				http.NotFound(w, r)
			}
		})

		svc, repo, userA, _, org := setupTestService(t, paypalHandler)
		ctx := context.Background()

		pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
			RecipientEmail: "forbidden@example.com", RecipientName: "Forbidden", Title: "Case 403", AmountCents: 5000,
		})
		op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)

		submittedOp, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
		if submitErr == nil {
			t.Fatal("expected error on 403, got nil")
		}

		if paypal.IsAmbiguousOutcome(submitErr) {
			t.Errorf("403 must NOT be classified as ambiguous outcome")
		}
		if submittedOp.Status != "FAILED" {
			t.Errorf("expected status FAILED, got: %s", submittedOp.Status)
		}
		if submittedOp.RawProviderResponse == nil {
			t.Errorf("expected RawProviderResponse to contain forbidden error details")
		} else {
			if submittedOp.RawProviderResponse["debug_id"] != "dbg-12345" {
				t.Errorf("expected debug_id dbg-12345, got %v", submittedOp.RawProviderResponse["debug_id"])
			}
		}

		persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
		if persistedOp.Status != "FAILED" {
			t.Errorf("expected persisted status FAILED, got: %s", persistedOp.Status)
		}
	})
}

// Case F: Two concurrent initiation attempts for same payout case
// - Exactly one succeeds and creates active operation
// - Second attempt is rejected with conflict error
// - No duplicate active operations exist
func TestMission033_8_1A_CaseF_ConcurrentInitiationConflict(t *testing.T) {
	svc, repo, userA, _, org := setupTestService(t, nil)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Eva Claimant",
		Title:          "Claim Case F",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, initErr := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
			if initErr == nil {
				atomic.AddInt32(&successCount, 1)
			} else if errors.Is(initErr, ErrActiveOperationExists) {
				atomic.AddInt32(&conflictCount, 1)
			}
		}()
	}

	close(startGate)
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful initiation, got: %d", successCount)
	}
	if conflictCount != 1 {
		t.Errorf("expected exactly 1 conflict rejection, got: %d", conflictCount)
	}

	ops, err := repo.ListPayoutOperationsByCaseID(ctx, org.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed listing ops: %v", err)
	}
	activeOps := 0
	for _, o := range ops {
		if o.Status != "CANCELLED" && o.Status != "FAILED" {
			activeOps++
		}
	}
	if activeOps != 1 {
		t.Errorf("expected exactly 1 active operation in repository, got: %d", activeOps)
	}
}

// Case G: Database operation-list failure blocks initiation
// - Must fail closed
// - Must not assume no active operation exists
type failListRepo struct {
	*db.MockRepository
}

func (r *failListRepo) ListPayoutOperationsByCaseID(ctx context.Context, orgID, caseID string) ([]db.PayoutOperation, error) {
	return nil, errors.New("simulated database operation listing failure")
}

func TestMission033_8_1A_CaseG_OperationListFailureFailsClosed(t *testing.T) {
	mockRepo := db.NewMockRepository()
	ctx := context.Background()

	userA, org, err := mockRepo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "user@example.com",
		Name:          "User",
		EmailVerified: true,
	}, "Test Org")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	pCase := &db.PayoutCase{
		OrganizationID:  org.ID,
		CreatedByUserID: userA.ID,
		Reference:       "CASE-G-1",
		RecipientEmail:  "user@example.com",
		RecipientName:   "User",
		Title:           "Case G",
		AmountCents:     10000,
		Status:          "DRAFT",
	}
	_ = mockRepo.CreatePayoutCase(ctx, pCase)

	failingRepo := &failListRepo{MockRepository: mockRepo}
	cfg := &config.Config{AppEnv: "development", PayPalEnv: "sandbox"}
	svc := NewService(cfg, failingRepo, policy.NewEngine(""), nil)

	_, initErr := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if initErr == nil {
		t.Fatal("expected failure when database listing fails, got nil (failed open)")
	}

	if !strings.Contains(initErr.Error(), "failed listing existing payout operations") {
		t.Errorf("expected error to explain list failure, got: %v", initErr)
	}

	// Verify no operation was created in underlying repo
	mockRepoOps, _ := mockRepo.ListPayoutOperationsByCaseID(ctx, org.ID, pCase.ID)
	if len(mockRepoOps) != 0 {
		t.Errorf("expected 0 operations created on list failure, got: %d", len(mockRepoOps))
	}
}

// Case H: Two concurrent submission requests for same operation
// - Exactly one succeeds in claiming SUBMITTING status
// - Exactly one outbound payout POST is issued
// - Losing request receives conflict error
// - Zero duplicate payout POSTs
func TestMission033_8_1A_CaseH_ConcurrentSubmissionClaimSingleOutboundPOST(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
				"scope":        paypal.PayPalPayoutsScope,
			})
		case "/v1/payments/payouts":
			atomic.AddInt32(&postCount, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "BATCH-WINNER-1",
					"batch_status":    "PENDING",
					"time_created":    time.Now().UTC().Format(time.RFC3339),
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	svc, _, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Fiona Claimant",
		Title:          "Claim Case H",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed to create case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed to initiate op: %v", err)
	}

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
			if submitErr == nil {
				atomic.AddInt32(&successCount, 1)
			} else if errors.Is(submitErr, ErrOperationAlreadySubmitted) || errors.Is(submitErr, db.ErrSubmissionClaimConflict) {
				atomic.AddInt32(&conflictCount, 1)
			}
		}()
	}

	close(startGate)
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 winning submission, got: %d", successCount)
	}
	if conflictCount != 1 {
		t.Errorf("expected exactly 1 conflict rejection, got: %d", conflictCount)
	}
	if count := atomic.LoadInt32(&postCount); count != 1 {
		t.Errorf("expected exactly 1 outbound POST request to PayPal, got: %d (duplicate mutation violation)", count)
	}
}

// Case I: Submission repository claim failure
// - Outbound payout POST is never dispatched
// - Case and operation state remain protected
type failClaimRepo struct {
	*db.MockRepository
}

func (r *failClaimRepo) ClaimPayoutOperationForSubmission(
	ctx context.Context, orgID, opID, fingerprint string, submittedAt time.Time,
) (*db.PayoutOperation, error) {
	return nil, errors.New("simulated database claim failure")
}

func TestMission033_8_1A_CaseI_SubmissionClaimFailureZeroPOSTs(t *testing.T) {
	var postCount int32
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			atomic.AddInt32(&postCount, 1)
		}
	})

	mockRepo := db.NewMockRepository()
	ctx := context.Background()

	userA, org, err := mockRepo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "userI@example.com",
		Name:          "User I",
		EmailVerified: true,
	}, "Org I")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	pCase := &db.PayoutCase{
		OrganizationID:  org.ID,
		CreatedByUserID: userA.ID,
		Reference:       "CASE-I-1",
		RecipientEmail:  "userI@example.com",
		RecipientName:   "User I",
		Title:           "Case I",
		AmountCents:     10000,
		Status:          "DRAFT",
	}
	_ = mockRepo.CreatePayoutCase(ctx, pCase)

	op := &db.PayoutOperation{
		PayoutCaseID:      pCase.ID,
		OrganizationID:    org.ID,
		InitiatedByUserID: userA.ID,
		SenderBatchID:     "CB-BATCH-I-1",
		SenderItemID:      "CB-ITEM-I-1",
		AmountCents:       10000,
		Currency:          "USD",
		Status:            "PREPARED",
	}
	_ = mockRepo.CreatePayoutOperation(ctx, op)

	ts := httptest.NewServer(paypalHandler)
	t.Cleanup(ts.Close)

	cfg := &config.Config{
		AppEnv:        "development",
		PayPalEnv:     "sandbox",
		PayPalBaseURL: ts.URL,
	}
	ppClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)

	failingRepo := &failClaimRepo{MockRepository: mockRepo}
	svc := NewService(cfg, failingRepo, policy.NewEngine(""), ppClient)

	_, submitErr := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)
	if submitErr == nil {
		t.Fatal("expected error when claim fails, got nil")
	}

	if count := atomic.LoadInt32(&postCount); count != 0 {
		t.Errorf("expected ZERO outbound POSTs when claim fails, got: %d", count)
	}

	// Operation status remains PREPARED
	opInRepo, _ := mockRepo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if opInRepo.Status != "PREPARED" {
		t.Errorf("expected op to remain PREPARED, got: %s", opInRepo.Status)
	}
}

// Case J: Concurrent approval and cancellation or invalid transitions
// - Cannot reopen cancelled operation
// - Cannot transition from invalid state
// - Enforces single winner
func TestMission033_8_1A_CaseJ_ApprovalCancellationStateSafety(t *testing.T) {
	t.Run("Cannot reopen cancelled operation through approval", func(t *testing.T) {
		svc, repo, userA, userB, org := setupTestService(t, nil)
		ctx := context.Background()

		pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
			RecipientEmail: "claimant@example.com",
			RecipientName:  "Grace",
			Title:          "Case J1",
			AmountCents:    30000, // Requires dual approval
		})

		op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
		if op.Status != "AWAITING_APPROVAL" {
			t.Fatalf("expected status AWAITING_APPROVAL, got: %s", op.Status)
		}

		// Cancel the operation locally
		cancelledOp, cancelErr := svc.CancelPayoutOperation(ctx, userA, org, op.ID)
		if cancelErr != nil {
			t.Fatalf("cancel failed: %v", cancelErr)
		}
		if cancelledOp.Status != "CANCELLED" {
			t.Fatalf("expected CANCELLED, got: %s", cancelledOp.Status)
		}

		// Now attempt to approve the cancelled operation
		_, approveErr := svc.ApprovePayoutOperation(ctx, userB, org, op.ID)
		if approveErr == nil {
			t.Fatal("expected error approving cancelled operation, got nil (reopened cancelled op!)")
		}
		if !errors.Is(approveErr, db.ErrInvalidOperationTransition) && !errors.Is(approveErr, ErrCaseInTerminalState) {
			t.Errorf("expected ErrInvalidOperationTransition or ErrCaseInTerminalState, got: %v", approveErr)
		}

		// Verify op remains CANCELLED
		persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
		if persistedOp.Status != "CANCELLED" {
			t.Errorf("persisted op status mutated from CANCELLED to %s", persistedOp.Status)
		}
	})

	t.Run("Two concurrent approvals enforce single winner", func(t *testing.T) {
		svc, repo, userA, userB, org := setupTestService(t, nil)
		ctx := context.Background()

		// Create third user for second approver
		userC, _, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
			Email:         "approver2@example.com",
			Name:          "Charlie Approver",
			EmailVerified: true,
		}, "Charlie Org")
		if err != nil {
			t.Fatalf("failed creating user C: %v", err)
		}

		pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
			RecipientEmail: "claimant@example.com",
			RecipientName:  "Grace",
			Title:          "Case J2",
			AmountCents:    30000,
		})

		op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)

		startGate := make(chan struct{})
		var wg sync.WaitGroup
		var successCount int32
		var conflictCount int32

		// Approver 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, err := svc.ApprovePayoutOperation(ctx, userB, org, op.ID)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			} else if errors.Is(err, db.ErrInvalidOperationTransition) {
				atomic.AddInt32(&conflictCount, 1)
			}
		}()

		// Approver 2
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, err := svc.ApprovePayoutOperation(ctx, userC, org, op.ID)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			} else if errors.Is(err, db.ErrInvalidOperationTransition) {
				atomic.AddInt32(&conflictCount, 1)
			}
		}()

		close(startGate)
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 winning approval, got: %d", successCount)
		}
		if conflictCount != 1 {
			t.Errorf("expected exactly 1 approval conflict rejection, got: %d", conflictCount)
		}

		persistedOp, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
		if persistedOp.Status != "APPROVED" {
			t.Errorf("expected op to be APPROVED, got: %s", persistedOp.Status)
		}
	})

	t.Run("Cannot approve from SUBMITTED or SUBMITTING", func(t *testing.T) {
		svc, repo, userA, userB, org := setupTestService(t, nil)
		ctx := context.Background()

		pCase, _ := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
			RecipientEmail: "claimant@example.com", RecipientName: "Grace", Title: "Case J3", AmountCents: 10000,
		})
		op, _ := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
		op.Status = "SUBMITTED"
		_ = repo.UpdatePayoutOperation(ctx, op)

		_, approveErr := svc.ApprovePayoutOperation(ctx, userB, org, op.ID)
		if approveErr == nil {
			t.Fatal("expected error approving SUBMITTED operation, got nil")
		}
		if !errors.Is(approveErr, db.ErrInvalidOperationTransition) {
			t.Errorf("expected ErrInvalidOperationTransition, got: %v", approveErr)
		}
	})
}

// Case K: Sender identifiers remain immutable
// - sender_batch_id and sender_item_id unchanged throughout ambiguous response and recovery lifecycle
func TestMission033_8_1A_CaseK_SenderIdentifiersRemainImmutable(t *testing.T) {
	paypalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"app_id":       "APP-SANDBOX-TEST",
				"scope":        paypal.PayPalPayoutsScope,
			})
		case "/v1/payments/payouts":
			// Ambiguous 500 error
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    "INTERNAL_SERVER_ERROR",
				"message": "Temporary failure",
			})
		default:
			http.NotFound(w, r)
		}
	})

	svc, repo, userA, _, org := setupTestService(t, paypalHandler)
	ctx := context.Background()

	pCase, err := svc.CreatePayoutCase(ctx, userA, org, CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Hannah Claimant",
		Title:          "Claim Case K",
		AmountCents:    10000,
	})
	if err != nil {
		t.Fatalf("failed creating case: %v", err)
	}

	op, err := svc.InitiatePayoutOperation(ctx, userA, org, pCase.ID)
	if err != nil {
		t.Fatalf("failed initiating op: %v", err)
	}

	initialBatchID := op.SenderBatchID
	initialItemID := op.SenderItemID

	// Submit operation -> ambiguous 500
	submittedOp, _ := svc.SubmitPayoutOperation(ctx, userA, org, pCase.ID, op.ID)

	if submittedOp.SenderBatchID != initialBatchID {
		t.Errorf("sender_batch_id mutated after submission: expected %s, got %s", initialBatchID, submittedOp.SenderBatchID)
	}
	if submittedOp.SenderItemID != initialItemID {
		t.Errorf("sender_item_id mutated after submission: expected %s, got %s", initialItemID, submittedOp.SenderItemID)
	}

	persistedOp, err := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if err != nil {
		t.Fatalf("failed to retrieve op: %v", err)
	}
	if persistedOp.SenderBatchID != initialBatchID {
		t.Errorf("persisted sender_batch_id mutated: expected %s, got %s", initialBatchID, persistedOp.SenderBatchID)
	}
	if persistedOp.SenderItemID != initialItemID {
		t.Errorf("persisted sender_item_id mutated: expected %s, got %s", initialItemID, persistedOp.SenderItemID)
	}
}
