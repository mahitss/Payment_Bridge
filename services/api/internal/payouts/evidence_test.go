package payouts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/db"
)

func TestEvidenceResolution_Success(t *testing.T) {
	_, repo, userA, _, orgA, mux := setupTestHandler(t)
	ctx := context.Background()

	// 1. Seed Case
	pCase := &db.PayoutCase{
		ID:                   "case-ev-001",
		OrganizationID:       orgA.ID,
		CreatedByUserID:      userA.ID,
		Reference:            "DEMO-W9821",
		RecipientEmail:       "test.claimant@example.com",
		RecipientName:        "Test Claimant",
		Title:                "Synthetic Warranty Claim",
		Description:          "Grounded evidence test fixture",
		Currency:             "USD",
		AmountCents:          12500,
		Status:               "SUBMITTED",
		RiskScore:            0.15,
		DualApprovalRequired: false,
		IsSyntheticFixture:   true,
		CreatedAt:            time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:            time.Now().UTC().Add(-1 * time.Hour),
	}
	repo.Cases[pCase.ID] = pCase

	// 2. Seed Operation
	batchID := "PAYPAL-BATCH-999"
	itemID := "PAYPAL-ITEM-888"
	batchStatus := "SUCCESS"
	itemStatus := "UNCLAIMED"
	op := &db.PayoutOperation{
		ID:                   "op-ev-001",
		PayoutCaseID:         pCase.ID,
		OrganizationID:       orgA.ID,
		InitiatedByUserID:    userA.ID,
		SenderBatchID:        "BATCH-CLAIM-001",
		SenderItemID:         "ITEM-CLAIM-001",
		AmountCents:          12500,
		Currency:             "USD",
		Status:               "SUBMITTED",
		PayPalPayoutBatchID:  &batchID,
		PayPalBatchStatus:    &batchStatus,
		PayPalPayoutItemID:   &itemID,
		PayPalItemStatus:     &itemStatus,
		RequiresDualApproval: false,
		IsSyntheticFixture:   true,
		RawProviderResponse:  map[string]interface{}{"sensitive_token": "SECRET-TOKEN-DO-NOT-LEAK"},
		CreatedAt:            time.Now().UTC().Add(-90 * time.Minute),
		UpdatedAt:            time.Now().UTC().Add(-30 * time.Minute),
	}
	repo.Operations[op.ID] = op

	// 3. Seed Status Snapshot
	snap := &db.PayoutStatusSnapshot{
		ID:                 "snap-ev-001",
		PayoutOperationID:  op.ID,
		OrganizationID:     orgA.ID,
		Provider:           "PAYPAL",
		BatchStatus:        "SUCCESS",
		ItemStatus:         &itemStatus,
		RawResponse:        map[string]interface{}{"raw_jwt": "SUPER_SECRET_PAYPAL_SIGNATURE"},
		IsSyntheticFixture: true,
		RecordedAt:         time.Now().UTC().Add(-45 * time.Minute),
	}
	if err := repo.CreatePayoutStatusSnapshot(ctx, snap); err != nil {
		t.Fatalf("failed to seed snapshot: %v", err)
	}

	authCtx := context.WithValue(context.Background(), auth.UserContextKey, userA)
	authCtx = context.WithValue(authCtx, auth.OrganizationContextKey, orgA)

	// Test 1: Resolve Case by ID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+pCase.ID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for case resolution, got %d, body: %s", rr.Code, rr.Body.String())
		}
		var res EvidenceResolution
		if err := json.NewDecoder(rr.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if res.ReferenceID != pCase.ID {
			t.Errorf("expected reference_id %q, got %q", pCase.ID, res.ReferenceID)
		}
		if res.SourceType != "PAYOUT_CASE" {
			t.Errorf("expected source_type PAYOUT_CASE, got %q", res.SourceType)
		}
		if !res.IsSyntheticFixture {
			t.Errorf("expected is_synthetic_fixture true")
		}
		if res.ResolutionStatus != "RESOLVED" {
			t.Errorf("expected resolution_status RESOLVED, got %q", res.ResolutionStatus)
		}
		// Verify email masking
		maskedFound := false
		for _, f := range res.Fields {
			if f.Name == "Recipient (Masked)" {
				maskedFound = true
				if f.Value == pCase.RecipientEmail || !strings.Contains(f.Value, "***") {
					t.Errorf("expected masked email, got raw %q", f.Value)
				}
			}
		}
		if !maskedFound {
			t.Errorf("expected Recipient (Masked) field in response")
		}
	}

	// Test 2: Resolve Operation by ID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+op.ID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for op ID resolution, got %d, body: %s", rr.Code, rr.Body.String())
		}
		var res EvidenceResolution
		if err := json.NewDecoder(rr.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if res.SourceType != "PAYOUT_OPERATION" {
			t.Errorf("expected source_type PAYOUT_OPERATION, got %q", res.SourceType)
		}
	}

	// Test 3: Resolve Operation by SenderBatchID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+op.SenderBatchID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for sender batch ID resolution, got %d", rr.Code)
		}
		var res EvidenceResolution
		_ = json.NewDecoder(rr.Body).Decode(&res)
		if res.SourceType != "PAYOUT_OPERATION" {
			t.Errorf("expected PAYOUT_OPERATION, got %s", res.SourceType)
		}
	}

	// Test 4: Resolve Operation by SenderItemID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+op.SenderItemID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for sender item ID resolution, got %d", rr.Code)
		}
	}

	// Test 5: Resolve Operation by PayPalPayoutBatchID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+*op.PayPalPayoutBatchID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for paypal batch ID resolution, got %d", rr.Code)
		}
	}

	// Test 6: Resolve Operation by PayPalPayoutItemID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+*op.PayPalPayoutItemID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for paypal item ID resolution, got %d", rr.Code)
		}
	}

	// Test 7: Resolve Status Snapshot by ID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+snap.ID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for snapshot ID resolution, got %d, body: %s", rr.Code, rr.Body.String())
		}
		var res EvidenceResolution
		if err := json.NewDecoder(rr.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if res.SourceType != "STATUS_SNAPSHOT" {
			t.Errorf("expected source_type STATUS_SNAPSHOT, got %q", res.SourceType)
		}
		if res.ReferenceID != snap.ID {
			t.Errorf("expected reference_id %q, got %q", snap.ID, res.ReferenceID)
		}
		if !res.IsSyntheticFixture {
			t.Errorf("expected is_synthetic_fixture true for snapshot")
		}
		// Verify raw JWT is NOT in any field
		for _, f := range res.Fields {
			if strings.Contains(f.Value, "SUPER_SECRET") {
				t.Errorf("raw secret leaked in field %s: %s", f.Name, f.Value)
			}
		}
	}
}

func TestEvidenceResolution_Security_DenialAndIsolation(t *testing.T) {
	_, repo, userA, userB, orgA, mux := setupTestHandler(t)

	// Org B setup
	orgB := &db.Organization{ID: "org-bob-isolated", Name: "Bob Isolation Inc", Slug: "bob-iso"}
	repo.Orgs[orgB.ID] = orgB

	// Case 1 in Org A
	caseA := &db.PayoutCase{
		ID:             "case-a-001",
		OrganizationID: orgA.ID,
		Reference:      "CASE-A",
		Status:         "DRAFT",
	}
	repo.Cases[caseA.ID] = caseA

	// Case 2 in Org A
	caseA2 := &db.PayoutCase{
		ID:             "case-a-002",
		OrganizationID: orgA.ID,
		Reference:      "CASE-A2",
		Status:         "DRAFT",
	}
	repo.Cases[caseA2.ID] = caseA2

	// Operation on Case 2 in Org A
	opA2 := &db.PayoutOperation{
		ID:             "op-a2-999",
		PayoutCaseID:   caseA2.ID,
		OrganizationID: orgA.ID,
		SenderBatchID:  "BATCH-A2-999",
		SenderItemID:   "ITEM-A2-999",
		Status:         "PREPARED",
	}
	repo.Operations[opA2.ID] = opA2

	// Case in Org B
	caseB := &db.PayoutCase{
		ID:             "case-b-secret",
		OrganizationID: orgB.ID,
		Reference:      "CASE-B-CONFIDENTIAL",
		Status:         "SUBMITTED",
	}
	repo.Cases[caseB.ID] = caseB

	opB := &db.PayoutOperation{
		ID:             "op-b-secret",
		PayoutCaseID:   caseB.ID,
		OrganizationID: orgB.ID,
		SenderBatchID:  "BATCH-B-SECRET",
		SenderItemID:   "ITEM-B-SECRET",
		Status:         "SUBMITTED",
	}
	repo.Operations[opB.ID] = opB

	authCtxA := context.WithValue(context.Background(), auth.UserContextKey, userA)
	authCtxA = context.WithValue(authCtxA, auth.OrganizationContextKey, orgA)

	// Test 1: Cross-Organization Attempt (User A attempts to inspect Org B's case or op)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+caseB.ID+"/evidence/"+caseB.ID, nil).WithContext(authCtxA)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for cross-org case query, got %d", rr.Code)
		}
		var errResp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&errResp)
		if errResp["resolution_status"] != "UNAVAILABLE" {
			t.Errorf("expected resolution_status UNAVAILABLE, got %v", errResp["resolution_status"])
		}
		// Confirm zero disclosure about Org B
		if strings.Contains(rr.Body.String(), "CONFIDENTIAL") || strings.Contains(rr.Body.String(), "Bob") {
			t.Errorf("cross-org information leaked in response body: %s", rr.Body.String())
		}
	}

	// Test 2: Cross-Case Attempt within Same Org (Querying op from Case 2 against Case 1)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+caseA.ID+"/evidence/"+opA2.ID, nil).WithContext(authCtxA)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for wrong-case evidence lookup, got %d", rr.Code)
		}
		var errResp map[string]interface{}
		_ = json.NewDecoder(rr.Body).Decode(&errResp)
		if errResp["resolution_status"] != "UNAVAILABLE" {
			t.Errorf("expected resolution_status UNAVAILABLE, got %v", errResp["resolution_status"])
		}
	}

	// Test 3: Completely Unknown / Fabricated Identifier
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+caseA.ID+"/evidence/non-existent-evidence-id", nil).WithContext(authCtxA)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for fabricated identifier, got %d", rr.Code)
		}
	}

	// Test 4: Unauthenticated Request
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+caseA.ID+"/evidence/"+caseA.ID, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated request, got %d", rr.Code)
		}
	}

	// Test 5: Missing Active Organization
	{
		noOrgCtx := context.WithValue(context.Background(), auth.UserContextKey, userB)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+caseA.ID+"/evidence/"+caseA.ID, nil).WithContext(noOrgCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for missing organization context, got %d", rr.Code)
		}
	}
}

func TestEvidenceResolution_ImmutabilityAndNoPayPalCalls(t *testing.T) {
	_, repo, userA, _, orgA, mux := setupTestHandler(t)

	// Create fixture case and operation
	pCase := &db.PayoutCase{
		ID:                 "case-immut-001",
		OrganizationID:     orgA.ID,
		Reference:          "DEMO-IMMUT",
		Status:             "SUBMITTED",
		IsSyntheticFixture: true,
	}
	repo.Cases[pCase.ID] = pCase

	op := &db.PayoutOperation{
		ID:                 "op-immut-001",
		PayoutCaseID:       pCase.ID,
		OrganizationID:     orgA.ID,
		SenderBatchID:      "BATCH-IMMUT-001",
		SenderItemID:       "ITEM-IMMUT-001",
		Status:             "SUBMITTED",
		IsSyntheticFixture: true,
	}
	repo.Operations[op.ID] = op

	authCtx := context.WithValue(context.Background(), auth.UserContextKey, userA)
	authCtx = context.WithValue(authCtx, auth.OrganizationContextKey, orgA)

	// Perform 10 resolution requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/evidence/"+op.ID, nil).WithContext(authCtx)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 on iteration %d, got %d", i, rr.Code)
		}
	}

	// Invariant checks:
	// 1. Case status must remain completely unchanged
	if repo.Cases[pCase.ID].Status != "SUBMITTED" {
		t.Errorf("case status was mutated from SUBMITTED to %s", repo.Cases[pCase.ID].Status)
	}
	// 2. Operation status must remain completely unchanged
	if repo.Operations[op.ID].Status != "SUBMITTED" {
		t.Errorf("op status was mutated from SUBMITTED to %s", repo.Operations[op.ID].Status)
	}
	// 3. No new snapshots or operations were created
	if len(repo.Snapshots) != 0 {
		t.Errorf("expected 0 snapshots created by inspector, got %d", len(repo.Snapshots))
	}
	if len(repo.Operations) != 1 {
		t.Errorf("expected 1 operation, got %d", len(repo.Operations))
	}
}
