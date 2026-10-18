package readiness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

// fakeAIProvider implements ai.AIProvider for deterministic unit testing.
type fakeAIProvider struct {
	providerName  string
	modelName     string
	health        ai.AIHealthStatus
	investigateFn func(ctx context.Context, snapshot *ai.InvestigationSnapshot) (*ai.InvestigationResult, error)
}

func (f *fakeAIProvider) Name() string {
	if f.providerName != "" {
		return f.providerName
	}
	return "ollama-openai-compatible"
}

func (f *fakeAIProvider) Model() string {
	if f.modelName != "" {
		return f.modelName
	}
	return "llama3.2"
}

func (f *fakeAIProvider) IsAvailable(ctx context.Context) bool {
	return f.health.Reachable
}

func (f *fakeAIProvider) CheckHealth(ctx context.Context) ai.AIHealthStatus {
	return f.health
}

func (f *fakeAIProvider) Investigate(ctx context.Context, snapshot *ai.InvestigationSnapshot) (*ai.InvestigationResult, error) {
	if f.investigateFn != nil {
		return f.investigateFn(ctx, snapshot)
	}
	return &ai.InvestigationResult{
		Summary:    "Automated synthetic investigation probe finding.",
		Confidence: "HIGH",
		Findings: []ai.InvestigationFinding{
			{
				Explanation:        "Valid finding citing existing snapshot ID",
				EvidenceReferences: []string{"PROBE-CASE-00000000"},
			},
		},
		RecommendedNextSteps: []ai.InvestigationRecommendation{
			{
				Action: ai.ActionEscalateToOperator,
				Reason: "Test synthetic escalation to operator",
			},
		},
	}, nil
}

func setupTestServers(t *testing.T) (apiServer *httptest.Server, webServer *httptest.Server, paypalServer *httptest.Server) {
	// 1. Fake Go API Server
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	apiMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready","database":"connected"}`))
	})
	apiMux.HandleFunc("/api/v1/policy/evaluate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := policy.EvaluationResponse{
			ClaimID:  "readiness-probe-001",
			Decision: policy.DecisionApproved,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	apiServer = httptest.NewServer(apiMux)

	// 2. Fake Web Server
	webServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>ClaimBridge Web</body></html>"))
	}))

	// 3. Fake PayPal Sandbox Server
	payPalMux := http.NewServeMux()
	payPalMux.HandleFunc("/v1/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"fake-sandbox-token","token_type":"Bearer","expires_in":3600}`))
	})
	paypalServer = httptest.NewServer(payPalMux)

	return apiServer, webServer, paypalServer
}

func testConfig(paypalURL string) *config.Config {
	return &config.Config{
		Port:                 "8080",
		AppEnv:               "development",
		DatabaseURL:          "postgres://localhost:5432/test",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "sandbox-client-id",
		PayPalClientSecret:   "sandbox-client-secret",
		PayPalBaseURL:        paypalURL,
		PayPalRequestTimeout: 5 * time.Second,
		AIEnabled:            true,
		AIProvider:           "ollama",
		AIModel:              "llama3.2",
		AIBaseURL:            "http://localhost:11434",
		AIRequestTimeout:     5 * time.Second,
	}
}

// 1. TestAllRequiredChecksPass_SubmissionProfile
func TestAllRequiredChecksPass_SubmissionProfile(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	ppClient, _ := paypal.NewClient(cfg, ppSrv.Client())

	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: true,
		},
	}

	opts := RunnerOptions{
		Profile:      "submission",
		Config:       cfg,
		BaseURL:      apiSrv.URL,
		WebURL:       webSrv.URL,
		HTTPClient:   apiSrv.Client(),
		PayPalClient: ppClient,
		AIProvider:   fakeAI,
		DBPingFn:     func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !summary.IsReady {
		t.Errorf("expected IsReady = true, got false. Output:\n%s", out.String())
	}
	if summary.ExitCode != 0 {
		t.Errorf("expected ExitCode = 0, got %d", summary.ExitCode)
	}
	if summary.FailedCount != 0 || summary.BlockedCount != 0 {
		t.Errorf("expected 0 failed and 0 blocked, got failed=%d blocked=%d", summary.FailedCount, summary.BlockedCount)
	}
}

// 2. TestPostgresUnavailable
func TestPostgresUnavailable(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	opts := RunnerOptions{
		Profile:    "core",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return errors.New("connection to server on socket failed") },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected IsReady = false when PostgreSQL is unavailable")
	}
	if summary.ExitCode != 1 {
		t.Errorf("expected ExitCode = 1, got %d", summary.ExitCode)
	}
	if summary.Results[0].Status != StatusFail {
		t.Errorf("expected Check 1 StatusFail, got %s", summary.Results[0].Status)
	}
}

// 3. TestAPIHealthFailure
func TestAPIHealthFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	apiSrv := httptest.NewServer(mux)
	defer apiSrv.Close()

	cfg := testConfig("http://localhost:9999")
	opts := RunnerOptions{
		Profile:    "core",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected IsReady = false when /healthz fails")
	}
	if summary.Results[1].Status != StatusFail {
		t.Errorf("expected Check 2 to FAIL, got %s", summary.Results[1].Status)
	}
}

// 4. TestAPIReadinessDegraded
func TestAPIReadinessDegraded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded","database":"unavailable"}`))
	})
	apiSrv := httptest.NewServer(mux)
	defer apiSrv.Close()

	cfg := testConfig("http://localhost:9999")
	opts := RunnerOptions{
		Profile:    "core",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected IsReady = false when /readyz is degraded")
	}
	if summary.Results[2].Status != StatusFail {
		t.Errorf("expected Check 3 to FAIL, got %s", summary.Results[2].Status)
	}
}

// 5. TestPolicyEngineFailure
func TestPolicyEngineFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/api/v1/policy/evaluate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"engine crashed"}`))
	})
	apiSrv := httptest.NewServer(mux)
	defer apiSrv.Close()

	cfg := testConfig("http://localhost:9999")
	opts := RunnerOptions{
		Profile:    "core",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected IsReady = false when policy engine returns 500")
	}
	if summary.Results[3].Status != StatusFail {
		t.Errorf("expected Check 4 to FAIL, got %s", summary.Results[3].Status)
	}
}

// 6. TestFrontendUnavailable
func TestFrontendUnavailable(t *testing.T) {
	apiSrv, _, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	opts := RunnerOptions{
		Profile:    "core",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     "http://127.0.0.1:54321", // non-existent web port
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected IsReady = false when frontend is offline")
	}
	if summary.Results[4].Status != StatusFail {
		t.Errorf("expected Check 5 to FAIL, got %s", summary.Results[4].Status)
	}
}

// 7. TestPayPalCredentialsMissing
func TestPayPalCredentialsMissing(t *testing.T) {
	apiSrv, webSrv, _ := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()

	cfg := testConfig("https://api-m.sandbox.paypal.com")
	cfg.PayPalClientID = ""
	cfg.PayPalClientSecret = ""

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to be NOT ready when PayPal credentials missing")
	}
	if summary.Results[5].Status != StatusBlocked {
		t.Errorf("expected Check 6 to be BLOCKED, got %s", summary.Results[5].Status)
	}
}

// 8. TestPayPalOAuthRejection
func TestPayPalOAuthRejection(t *testing.T) {
	apiSrv, webSrv, _ := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()

	// Server returning 401 Unauthorized
	ppMux := http.NewServeMux()
	ppMux.HandleFunc("/v1/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client Authentication failed"}`))
	})
	ppSrv := httptest.NewServer(ppMux)
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	ppClient, _ := paypal.NewClient(cfg, ppSrv.Client())

	opts := RunnerOptions{
		Profile:      "submission",
		Config:       cfg,
		BaseURL:      apiSrv.URL,
		WebURL:       webSrv.URL,
		HTTPClient:   apiSrv.Client(),
		PayPalClient: ppClient,
		DBPingFn:     func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to be NOT ready when PayPal auth fails")
	}
	if summary.Results[5].Status != StatusFail {
		t.Errorf("expected Check 6 to FAIL, got %s", summary.Results[5].Status)
	}
}

// 9. TestPayPalOAuthPass_LookupNotRun
func TestPayPalOAuthPass_LookupNotRun(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	ppClient, _ := paypal.NewClient(cfg, ppSrv.Client())

	opts := RunnerOptions{
		Profile:      "core",
		Config:       cfg,
		BaseURL:      apiSrv.URL,
		WebURL:       webSrv.URL,
		HTTPClient:   apiSrv.Client(),
		PayPalClient: ppClient,
		DBPingFn:     func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// OAuth passes
	if summary.Results[5].Status != StatusPass {
		t.Errorf("expected Check 6 PASS, got %s", summary.Results[5].Status)
	}
	// Lookup is SKIPPED
	if summary.Results[6].Status != StatusSkipped {
		t.Errorf("expected Check 7 SKIPPED, got %s", summary.Results[6].Status)
	}
}

// 10. TestAIDisabled
func TestAIDisabled(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	cfg.AIEnabled = false

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to be blocked when AI is disabled")
	}
	if summary.Results[7].Status != StatusBlocked {
		t.Errorf("expected Check 8 to be BLOCKED, got %s", summary.Results[7].Status)
	}
}

// 11. TestAIProviderUnreachable
func TestAIProviderUnreachable(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      false,
			ModelAvailable: false,
			ErrorMessage:   "connection refused to localhost:11434",
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to fail when AI daemon is unreachable")
	}
	if summary.Results[7].Status != StatusBlocked {
		t.Errorf("expected Check 8 to be BLOCKED, got %s", summary.Results[7].Status)
	}
}

// 12. TestAIModelMissing
func TestAIModelMissing(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: false,
			ErrorMessage:   "model llama3.2 not found in catalog",
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to fail when model is missing")
	}
	if summary.Results[7].Status != StatusBlocked {
		t.Errorf("expected Check 8 to be BLOCKED, got %s", summary.Results[7].Status)
	}
}

// 13. TestMockProviderConfigured_FailsSubmission
func TestMockProviderConfigured_FailsSubmission(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	fakeAI := &fakeAIProvider{
		providerName: "mock",
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: true,
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to FAIL when provider is mock")
	}
	if summary.Results[8].Status != StatusFail {
		t.Errorf("expected Check 9 to FAIL on mock provider, got %s", summary.Results[8].Status)
	}
}

// 14. TestGenuineInferenceTimeout
func TestGenuineInferenceTimeout(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	cfg.AIRequestTimeout = 50 * time.Millisecond
	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: true,
		},
		investigateFn: func(ctx context.Context, snapshot *ai.InvestigationSnapshot) (*ai.InvestigationResult, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to fail when inference times out")
	}
	if summary.Results[8].Status != StatusFail {
		t.Errorf("expected Check 9 to FAIL on timeout, got %s", summary.Results[8].Status)
	}
}

// 15. TestGenuineInferenceMalformedJSON
func TestGenuineInferenceMalformedJSON(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: true,
		},
		investigateFn: func(ctx context.Context, snapshot *ai.InvestigationSnapshot) (*ai.InvestigationResult, error) {
			return nil, fmt.Errorf("%w: failed to parse json completion", ai.ErrInvalidSchema)
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to fail when model returns malformed JSON")
	}
	if summary.Results[8].Status != StatusFail {
		t.Errorf("expected Check 9 to FAIL on malformed JSON, got %s", summary.Results[8].Status)
	}
}

// 16. TestGenuineInferenceFabricatedEvidence
func TestGenuineInferenceFabricatedEvidence(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: true,
		},
		investigateFn: func(ctx context.Context, snapshot *ai.InvestigationSnapshot) (*ai.InvestigationResult, error) {
			return nil, fmt.Errorf("%w: finding references unknown or fabricated identifier 'FAKE-ID-999'", ai.ErrInvalidEvidenceRef)
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to fail when model references fabricated evidence")
	}
	if summary.Results[8].Status != StatusFail {
		t.Errorf("expected Check 9 to FAIL on fabricated evidence, got %s", summary.Results[8].Status)
	}
}

// 17. TestGenuineInferencePassesSchemaAndGrounding
func TestGenuineInferencePassesSchemaAndGrounding(t *testing.T) {
	apiSrv, webSrv, ppSrv := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()
	defer ppSrv.Close()

	cfg := testConfig(ppSrv.URL)
	fakeAI := &fakeAIProvider{
		health: ai.AIHealthStatus{
			Reachable:      true,
			ModelAvailable: true,
		},
	}

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		AIProvider: fakeAI,
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.Results[8].Status != StatusPass {
		t.Errorf("expected Check 9 to PASS, got %s", summary.Results[8].Status)
	}
}

// 18. TestCoreProfileReadyWhileIntegrationsBlocked
func TestCoreProfileReadyWhileIntegrationsBlocked(t *testing.T) {
	apiSrv, webSrv, _ := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()

	cfg := testConfig("https://api-m.sandbox.paypal.com")
	cfg.PayPalClientID = ""
	cfg.PayPalClientSecret = ""
	cfg.AIEnabled = false

	opts := RunnerOptions{
		Profile:    "core",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !summary.IsReady {
		t.Errorf("expected core profile to be READY when core services pass, but got not ready. Output:\n%s", out.String())
	}
	if summary.ExitCode != 0 {
		t.Errorf("expected ExitCode = 0 for core profile, got %d", summary.ExitCode)
	}
	// PayPal should be BLOCKED (check 6)
	if summary.Results[5].Status != StatusBlocked {
		t.Errorf("expected PayPal Check 6 to be BLOCKED, got %s", summary.Results[5].Status)
	}
	// AI should be BLOCKED (check 8)
	if summary.Results[7].Status != StatusBlocked {
		t.Errorf("expected AI Check 8 to be BLOCKED, got %s", summary.Results[7].Status)
	}
}

// 19. TestSubmissionProfileBlockedWhenIntegrationUnverified
func TestSubmissionProfileBlockedWhenIntegrationUnverified(t *testing.T) {
	apiSrv, webSrv, _ := setupTestServers(t)
	defer apiSrv.Close()
	defer webSrv.Close()

	cfg := testConfig("https://api-m.sandbox.paypal.com")
	cfg.PayPalClientID = "" // Missing credentials
	cfg.PayPalClientSecret = ""

	opts := RunnerOptions{
		Profile:    "submission",
		Config:     cfg,
		BaseURL:    apiSrv.URL,
		WebURL:     webSrv.URL,
		HTTPClient: apiSrv.Client(),
		DBPingFn:   func(ctx context.Context) error { return nil },
	}

	out := &bytes.Buffer{}
	summary, err := Run(context.Background(), opts, out, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.IsReady {
		t.Error("expected submission profile to be BLOCKED when PayPal is unverified")
	}
	if summary.ExitCode != 1 {
		t.Errorf("expected ExitCode = 1, got %d", summary.ExitCode)
	}
}

// 20. TestExitCodesAndSummariesMatchReadinessState
func TestExitCodesAndSummariesMatchReadinessState(t *testing.T) {
	// Case A: Success -> ExitCode 0, IsReady true
	{
		apiSrv, webSrv, ppSrv := setupTestServers(t)
		cfg := testConfig(ppSrv.URL)
		ppClient, _ := paypal.NewClient(cfg, ppSrv.Client())
		fakeAI := &fakeAIProvider{
			health: ai.AIHealthStatus{Reachable: true, ModelAvailable: true},
		}

		opts := RunnerOptions{
			Profile:      "submission",
			Config:       cfg,
			BaseURL:      apiSrv.URL,
			WebURL:       webSrv.URL,
			HTTPClient:   apiSrv.Client(),
			PayPalClient: ppClient,
			AIProvider:   fakeAI,
			DBPingFn:     func(ctx context.Context) error { return nil },
		}

		out := &bytes.Buffer{}
		summary, _ := Run(context.Background(), opts, out, out)
		apiSrv.Close()
		webSrv.Close()
		ppSrv.Close()

		if summary.ExitCode != 0 || !summary.IsReady {
			t.Errorf("expected ExitCode 0 & IsReady true, got ExitCode=%d IsReady=%v", summary.ExitCode, summary.IsReady)
		}
	}

	// Case B: Failure -> ExitCode 1, IsReady false
	{
		cfg := testConfig("http://localhost:9999")
		opts := RunnerOptions{
			Profile:  "core",
			Config:   cfg,
			DBPingFn: func(ctx context.Context) error { return errors.New("db down") },
		}

		out := &bytes.Buffer{}
		summary, _ := Run(context.Background(), opts, out, out)
		if summary.ExitCode != 1 || summary.IsReady {
			t.Errorf("expected ExitCode 1 & IsReady false, got ExitCode=%d IsReady=%v", summary.ExitCode, summary.IsReady)
		}
	}
}
