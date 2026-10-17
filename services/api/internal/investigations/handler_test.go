package investigations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/policy"
)

func setupTestHandler(t *testing.T) (*Handler, *db.MockRepository, *ai.MockProvider, *db.User, *db.Organization) {
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
	handler := NewHandler(cfg, svc)

	org := &db.Organization{ID: "org-h-1", Name: "Handler Org", Slug: "handler-org"}
	user := &db.User{ID: "user-h-1", Email: "handler@claim.com", Name: "Handler User"}
	repo.Orgs[org.ID] = org
	repo.Users[user.ID] = user

	return handler, repo, aiMock, user, org
}

func TestHandler_CreateAndGetInvestigation(t *testing.T) {
	handler, repo, _, user, org := setupTestHandler(t)

	pCase := &db.PayoutCase{
		ID:              "case-h-1",
		OrganizationID:  org.ID,
		CreatedByUserID: user.ID,
		Reference:       "REF-H-100",
		RecipientEmail:  "test@example.com",
		AmountCents:     5000,
		Status:          "SUBMITTED",
	}
	repo.Cases[pCase.ID] = pCase

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. POST Investigation
	reqBody := `{"force_sync": false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payout-cases/"+pCase.ID+"/investigations", strings.NewReader(reqBody))
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	ctx = context.WithValue(ctx, auth.OrganizationContextKey, org)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d, body: %s", rr.Code, rr.Body.String())
	}

	var created db.PayoutInvestigation
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected investigation ID in response")
	}

	// 2. GET Investigation by ID
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/"+created.ID, nil)
	getReq = getReq.WithContext(ctx)
	getRR := httptest.NewRecorder()
	mux.ServeHTTP(getRR, getReq)

	if getRR.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d, body: %s", getRR.Code, getRR.Body.String())
	}

	// 3. GET Cross-Tenant Access Check
	otherOrg := &db.Organization{ID: "org-h-2", Name: "Other Org", Slug: "other-org"}
	repo.Orgs[otherOrg.ID] = otherOrg
	crossCtx := context.WithValue(req.Context(), auth.UserContextKey, user)
	crossCtx = context.WithValue(crossCtx, auth.OrganizationContextKey, otherOrg)

	crossReq := httptest.NewRequest(http.MethodGet, "/api/v1/investigations/"+created.ID, nil)
	crossReq = crossReq.WithContext(crossCtx)
	crossRR := httptest.NewRecorder()
	mux.ServeHTTP(crossRR, crossReq)

	if crossRR.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 Not Found for cross-tenant request, got %d", crossRR.Code)
	}

	// 4. GET List Investigations
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/"+pCase.ID+"/investigations", nil)
	listReq = listReq.WithContext(ctx)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)

	if listRR.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK on list, got %d", listRR.Code)
	}

	var listResp struct {
		Investigations []db.PayoutInvestigation `json:"investigations"`
		Count          int                      `json:"count"`
	}
	if err := json.NewDecoder(listRR.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if listResp.Count != 1 {
		t.Errorf("expected 1 investigation in list, got %d", listResp.Count)
	}
}

func TestHandler_UnauthenticatedRejected(t *testing.T) {
	handler, _, _, _, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payout-cases/case-123/investigations", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated request, got %d", rr.Code)
	}
}
