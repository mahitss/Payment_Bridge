package payouts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

func setupTestHandler(t *testing.T) (*Handler, *db.MockRepository, *db.User, *db.User, *db.Organization, *http.ServeMux) {
	t.Helper()

	repo := db.NewMockRepository()
	ctx := context.Background()

	userA, org, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "userA@example.com",
		Name:          "User Alice",
		EmailVerified: true,
	}, "ClaimCorp")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}

	userB, _, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         "userB@example.com",
		Name:          "User Bob",
		EmailVerified: true,
	}, "BobCorp")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "sandbox-id",
		PayPalClientSecret:   "sandbox-secret",
		PayPalBaseURL:        "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout: 2 * time.Second,
	}

	ppClient, _ := paypal.NewClient(cfg, http.DefaultClient)
	policyEngine := policy.NewEngine("")
	svc := NewService(cfg, repo, policyEngine, ppClient)
	h := NewHandler(cfg, svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	return h, repo, userA, userB, org, mux
}

func authenticatedRequest(method, path string, body []byte, user *db.User, org *db.Organization) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user))
	}
	if org != nil {
		req = req.WithContext(context.WithValue(req.Context(), auth.OrganizationContextKey, org))
	}
	return req
}

func TestPayoutHandler_UnauthorizedRequests(t *testing.T) {
	_, _, _, _, _, mux := setupTestHandler(t)

	// Unauthenticated request to /api/v1/payout-cases
	req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got: %d", rec.Code)
	}
}

func TestPayoutHandler_CreateAndGetPayoutCase(t *testing.T) {
	_, _, userA, _, org, mux := setupTestHandler(t)

	// 1. Create Payout Case
	createBody, _ := json.Marshal(CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Fire Damage Payout",
		AmountCents:    24000,
		Currency:       "USD",
		RiskScore:      5.0,
	})
	req := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases", createBody, userA, org)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		Case    db.PayoutCase `json:"case"`
		Message string        `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	caseID := createResp.Case.ID
	if caseID == "" {
		t.Fatalf("expected non-empty case ID")
	}

	// 2. Get Case Details
	reqGet := authenticatedRequest(http.MethodGet, "/api/v1/payout-cases/"+caseID, nil, userA, org)
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", recGet.Code, recGet.Body.String())
	}

	var getResp struct {
		Case       db.PayoutCase        `json:"case"`
		Operations []db.PayoutOperation `json:"operations"`
	}
	if err := json.NewDecoder(recGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}
	if getResp.Case.Title != "Fire Damage Payout" {
		t.Errorf("expected title 'Fire Damage Payout', got '%s'", getResp.Case.Title)
	}
}

func TestPayoutHandler_DualApproval_SelfApprovalForbidden(t *testing.T) {
	_, _, userA, userB, org, mux := setupTestHandler(t)

	// 1. Create dual-approval case ($350.00 USD)
	createBody, _ := json.Marshal(CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Water Damage Claim",
		AmountCents:    35000,
		Currency:       "USD",
	})
	req := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases", createBody, userA, org)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		Case db.PayoutCase `json:"case"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&createResp)
	caseID := createResp.Case.ID

	// 2. Initiate payout operation
	reqInit := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases/"+caseID+"/payout-operations", nil, userA, org)
	recInit := httptest.NewRecorder()
	mux.ServeHTTP(recInit, reqInit)
	if recInit.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recInit.Code, recInit.Body.String())
	}

	var initResp struct {
		Operation db.PayoutOperation `json:"operation"`
	}
	_ = json.NewDecoder(recInit.Body).Decode(&initResp)
	opID := initResp.Operation.ID

	// 3. User A attempts self-approval -> MUST return 403 Forbidden
	reqApproveSelf := authenticatedRequest(http.MethodPost, "/api/v1/payout-operations/"+opID+"/approve", nil, userA, org)
	recApproveSelf := httptest.NewRecorder()
	mux.ServeHTTP(recApproveSelf, reqApproveSelf)

	if recApproveSelf.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden on self-approval, got: %d (%s)", recApproveSelf.Code, recApproveSelf.Body.String())
	}

	// 4. User B approves -> MUST return 200 OK
	reqApproveB := authenticatedRequest(http.MethodPost, "/api/v1/payout-operations/"+opID+"/approve", nil, userB, org)
	recApproveB := httptest.NewRecorder()
	mux.ServeHTTP(recApproveB, reqApproveB)

	if recApproveB.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on distinct user approval, got: %d (%s)", recApproveB.Code, recApproveB.Body.String())
	}
}

func TestPayoutHandler_RejectDuplicateActiveOperation(t *testing.T) {
	_, _, userA, _, org, mux := setupTestHandler(t)

	// 1. Create payout case
	createBody, _ := json.Marshal(CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Duplicate Test",
		AmountCents:    12000,
		Currency:       "USD",
	})
	req := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases", createBody, userA, org)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var createResp struct {
		Case db.PayoutCase `json:"case"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&createResp)
	caseID := createResp.Case.ID

	// 2. Initiate first operation -> 201 Created
	reqInit1 := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases/"+caseID+"/payout-operations", nil, userA, org)
	recInit1 := httptest.NewRecorder()
	mux.ServeHTTP(recInit1, reqInit1)
	if recInit1.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", recInit1.Code)
	}

	// 3. Initiate second operation on the same case -> 409 Conflict
	reqInit2 := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases/"+caseID+"/payout-operations", nil, userA, org)
	recInit2 := httptest.NewRecorder()
	mux.ServeHTTP(recInit2, reqInit2)
	if recInit2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on duplicate operation initiation, got: %d (%s)", recInit2.Code, recInit2.Body.String())
	}
}

func TestPayoutHandler_RejectSubmitCancelledOperation(t *testing.T) {
	_, _, userA, _, org, mux := setupTestHandler(t)

	// 1. Create single-officer case
	createBody, _ := json.Marshal(CreateCaseRequest{
		RecipientEmail: "claimant@example.com",
		RecipientName:  "Jane Claimant",
		Title:          "Submit Cancelled Test",
		AmountCents:    12000,
		Currency:       "USD",
	})
	req := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases", createBody, userA, org)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var createResp struct {
		Case db.PayoutCase `json:"case"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&createResp)
	caseID := createResp.Case.ID

	// 2. Initiate operation
	reqInit := authenticatedRequest(http.MethodPost, "/api/v1/payout-cases/"+caseID+"/payout-operations", nil, userA, org)
	recInit := httptest.NewRecorder()
	mux.ServeHTTP(recInit, reqInit)
	if recInit.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", recInit.Code)
	}

	var initResp struct {
		Operation db.PayoutOperation `json:"operation"`
	}
	_ = json.NewDecoder(recInit.Body).Decode(&initResp)
	opID := initResp.Operation.ID

	// 3. Cancel operation locally -> 200 OK
	reqCancel := authenticatedRequest(http.MethodPost, "/api/v1/payout-operations/"+opID+"/cancel", nil, userA, org)
	recCancel := httptest.NewRecorder()
	mux.ServeHTTP(recCancel, reqCancel)
	if recCancel.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recCancel.Code)
	}

	// 4. Attempt to submit cancelled operation -> MUST be 409 Conflict
	reqSubmit := authenticatedRequest(http.MethodPost, "/api/v1/payout-operations/"+opID+"/submit", nil, userA, org)
	recSubmit := httptest.NewRecorder()
	mux.ServeHTTP(recSubmit, reqSubmit)
	if recSubmit.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when submitting cancelled operation, got: %d (%s)", recSubmit.Code, recSubmit.Body.String())
	}
}

