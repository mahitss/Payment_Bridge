package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/seeder"
)

func cleanEnv(t *testing.T) {
	orig := os.Environ()
	tmp := os.Getenv("TMP")
	temp := os.Getenv("TEMP")
	sysRoot := os.Getenv("SystemRoot")

	os.Clearenv()
	if tmp != "" {
		os.Setenv("TMP", tmp)
	}
	if temp != "" {
		os.Setenv("TEMP", temp)
	}
	if sysRoot != "" {
		os.Setenv("SystemRoot", sysRoot)
	}

	t.Cleanup(func() {
		os.Clearenv()
		for _, e := range orig {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				os.Setenv(parts[0], parts[1])
			}
		}
	})
}

func setupTestRepo(t *testing.T) db.Repository {
	repo := db.NewMockRepository()
	s := seeder.New(repo)
	if err := s.Seed(context.Background(), seeder.Options{Reset: false}); err != nil {
		t.Fatalf("failed seeding test repo: %v", err)
	}
	return repo
}

type mockMigratorRepo struct {
	db.Repository
	migrationCalls *atomic.Int32
	migrationErr   error
	closeCalls     *atomic.Int32
}

func (m *mockMigratorRepo) RunMigrations(ctx context.Context) error {
	if m.migrationCalls != nil {
		m.migrationCalls.Add(1)
	}
	return m.migrationErr
}

func (m *mockMigratorRepo) Close() {
	if m.closeCalls != nil {
		m.closeCalls.Add(1)
	}
}

type mockNonMigratorRepo struct {
	db.Repository
	closeCalls *atomic.Int32
}

func (m *mockNonMigratorRepo) Close() {
	if m.closeCalls != nil {
		m.closeCalls.Add(1)
	}
}

func readJSONReport(t *testing.T, reportDir string) SandboxE2EReport {
	t.Helper()
	files, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatalf("failed reading report directory %s: %v", reportDir, err)
	}
	var jsonPath string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			jsonPath = filepath.Join(reportDir, f.Name())
			break
		}
	}
	if jsonPath == "" {
		t.Fatalf("expected JSON report file, none found in %s", reportDir)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed reading report %s: %v", jsonPath, err)
	}
	var rep SandboxE2EReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling report %s: %v", jsonPath, err)
	}
	return rep
}


// 1. Missing credentials -> BLOCKED (Exit code 3)
func TestRun_MissingCredentialsBlocked(t *testing.T) {
	cleanEnv(t)
	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Unsetenv("PAYPAL_CLIENT_ID")
	os.Unsetenv("PAYPAL_CLIENT_SECRET")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-env-file", "nonexistent.env", "-no-report-file"}, &stdout, &stderr)

	if code != ExitCodeBlocked {
		t.Fatalf("expected exit code %d (BLOCKED) for missing credentials, got: %d", ExitCodeBlocked, code)
	}

	out := stdout.String()
	if !strings.Contains(out, "[STATUS: BLOCKED]") {
		t.Errorf("expected output to contain '[STATUS: BLOCKED]', got:\n%s", out)
	}
}

// 2. Invalid environment (PAYPAL_ENV != sandbox) -> FAIL (Exit code 1)
func TestRun_InvalidEnvironmentRejected(t *testing.T) {
	cleanEnv(t)
	os.Setenv("PAYPAL_ENV", "production")
	os.Setenv("PAYPAL_CLIENT_ID", "test_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_client_secret")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-env-file", "nonexistent.env", "-no-report-file"}, &stdout, &stderr)

	if code != ExitCodeFailure {
		t.Fatalf("expected exit code %d (FAIL) for production environment, got: %d", ExitCodeFailure, code)
	}
	if !strings.Contains(stderr.String(), "SECURITY VIOLATION") && !strings.Contains(stderr.String(), "security validation") {
		t.Errorf("expected security violation error in stderr, got:\n%s", stderr.String())
	}
}

// 3. Production / arbitrary host rejection before credential dispatch
func TestRun_ProductionAndArbitraryHostRejected(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
	}{
		{"Production Host api.paypal.com", "https://api.paypal.com"},
		{"Production Host api-m.paypal.com", "https://api-m.paypal.com"},
		{"Arbitrary Host evil.com", "https://evil.com"},
		{"Insecure HTTP Scheme", "http://api-m.sandbox.paypal.com"},
		{"Invalid Custom Port", "https://api-m.sandbox.paypal.com:8443"},
		{"Userinfo In URL", "https://user:pass@api-m.sandbox.paypal.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			os.Setenv("PAYPAL_ENV", "sandbox")
			os.Setenv("PAYPAL_CLIENT_ID", "test_id")
			os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")
			os.Setenv("PAYPAL_SANDBOX_BASE_URL", tc.baseURL)

			var stdout, stderr bytes.Buffer
			code := run([]string{"-env-file", "nonexistent.env", "-no-report-file"}, &stdout, &stderr)

			if code != ExitCodeFailure {
				t.Fatalf("expected exit code %d (FAIL) for endpoint %q, got: %d", ExitCodeFailure, tc.baseURL, code)
			}
			if !strings.Contains(stderr.String(), "security validation") && !strings.Contains(stderr.String(), "security violation") {
				t.Errorf("expected security validation error in stderr, got:\n%s", stderr.String())
			}
		})
	}
}

// 4. Default read-only behavior (no payouts created)
func TestRun_DefaultReadOnlyBehavior(t *testing.T) {
	cleanEnv(t)
	var mutationCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v1/payments/payouts") {
			mutationCount.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}

	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{"-no-report-file"}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeSuccess {
		t.Fatalf("expected exit code %d (PASS) for read-only preflight, got: %d\nStderr: %s", ExitCodeSuccess, code, stderr.String())
	}

	if mutationCount.Load() != 0 {
		t.Fatalf("expected 0 payout mutations in read-only mode, got: %d", mutationCount.Load())
	}

	out := stdout.String()
	if !strings.Contains(out, "READ-ONLY PREFLIGHT") {
		t.Errorf("expected output to mention READ-ONLY PREFLIGHT, got:\n%s", out)
	}
	if !strings.Contains(out, "[STATUS: PASS]") {
		t.Errorf("expected output to report [STATUS: PASS], got:\n%s", out)
	}
}

// 5. Missing confirmation flag -> FAIL
func TestRun_MissingWriteConfirmationFlag(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{"-submit-payout", "-recipient", "sb-buyer@personal.example.com", "-no-report-file"}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeFailure {
		t.Fatalf("expected exit code %d (FAIL) when confirmation flag missing, got: %d", ExitCodeFailure, code)
	}
	if !strings.Contains(stderr.String(), "SAFETY ABORT") || !strings.Contains(stderr.String(), "-confirm-sandbox-only") {
		t.Errorf("expected safety abort message regarding -confirm-sandbox-only, got:\n%s", stderr.String())
	}
}

// 6. Test amount above USD 1.00 -> FAIL
func TestRun_AmountAboveOneDollarRejected(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}
	repo := setupTestRepo(t)

	testAmounts := []string{"1.01", "5.00", "50.00"}
	for _, amt := range testAmounts {
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-recipient", "sb-buyer@personal.example.com",
			"-amount", amt,
			"-confirm-sandbox-only",
			"-no-report-file",
		}, &stdout, &stderr, testClient, repo)

		if code != ExitCodeFailure {
			t.Fatalf("expected exit code %d (FAIL) for amount %s, got: %d", ExitCodeFailure, amt, code)
		}
		if !strings.Contains(stderr.String(), "SAFETY VIOLATION") || !strings.Contains(stderr.String(), "exceeds the hard sandbox test ceiling") {
			t.Errorf("expected safety violation for amount %s, got:\n%s", amt, stderr.String())
		}
	}
}

// 7. Invalid recipient email -> FAIL
func TestRun_InvalidRecipientRejected(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := setupTestRepo(t)

	invalidRecipients := []string{"", "invalid-email-no-at", "   "}
	for _, rec := range invalidRecipients {
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-recipient", rec,
			"-confirm-sandbox-only",
			"-no-report-file",
		}, &stdout, &stderr, testClient, repo)

		if code != ExitCodeFailure {
			t.Fatalf("expected exit code %d (FAIL) for recipient %q, got: %d", ExitCodeFailure, rec, code)
		}
		if !strings.Contains(stderr.String(), "INPUT ERROR") || !strings.Contains(stderr.String(), "-recipient") {
			t.Errorf("expected input error for recipient %q, got:\n%s", rec, stderr.String())
		}
	}
}

// 8. Exactly one payout mutation per authorized run + correlation verification
func TestRun_ExactlyOnePayoutMutationAndCorrelation(t *testing.T) {
	cleanEnv(t)
	var mutationCount atomic.Int32
	var capturedSenderBatchID string
	var capturedSenderItemID string

	batchID := "PAYPAL-BATCH-VERIFY-123"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			mutationCount.Add(1)
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if senderHeader, ok := req["sender_batch_header"].(map[string]interface{}); ok {
				capturedSenderBatchID = fmt.Sprintf("%v", senderHeader["sender_batch_id"])
			}
			if items, ok := req["items"].([]interface{}); ok && len(items) > 0 {
				if item0, ok := items[0].(map[string]interface{}); ok {
					capturedSenderItemID = fmt.Sprintf("%v", item0["sender_item_id"])
				}
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					TimeCreated:   time.Now().UTC().Format(time.RFC3339),
				},
			})
			return
		}

		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/") {
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					SenderBatchHeader: &paypal.SenderBatchHeader{
						SenderBatchID: capturedSenderBatchID,
					},
					Amount: &paypal.CurrencyAmount{
						Value:    "1.00",
						Currency: "USD",
					},
				},
				Items: []paypal.PayoutItem{
					{
						PayoutItemID:      "ITEM-001",
						TransactionStatus: "SUCCESS",
						SenderItemID:      capturedSenderItemID,
						Receiver:          "sb-buyer@personal.example.com",
						Amount: paypal.CurrencyAmount{
							Value:    "1.00",
							Currency: "USD",
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-buyer@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-no-report-file",
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeSuccess {
		t.Fatalf("expected exit code %d (PASS) for authorized test payout, got: %d\nStderr: %s\nStdout: %s", ExitCodeSuccess, code, stderr.String(), stdout.String())
	}

	if mutationCount.Load() != 1 {
		t.Fatalf("expected EXACTLY 1 payout mutation dispatched, got: %d", mutationCount.Load())
	}

	out := stdout.String()
	if !strings.Contains(out, "completed and settled successfully") {
		t.Errorf("expected success settlement in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[STATUS: PASS]") {
		t.Errorf("expected [STATUS: PASS] in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Correlation Verified    : true") {
		t.Errorf("expected correlation verified in summary, got:\n%s", out)
	}
}

// 9. Provider timeout without automatic replay -> UNRESOLVED (Exit code 4)
func TestRun_ProviderTimeoutWithoutAutomaticReplay(t *testing.T) {
	cleanEnv(t)
	var mutationCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			mutationCount.Add(1)
			// Induce client timeout by sleeping longer than request timeout
			time.Sleep(300 * time.Millisecond)
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	// Set a very short client timeout
	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 100 * time.Millisecond,
	}

	httpClient := &http.Client{Timeout: 100 * time.Millisecond}
	testClient, err := paypal.NewTestClient(cfg, httpClient, ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-buyer@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-no-report-file",
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeUnresolved {
		t.Fatalf("expected exit code %d (UNRESOLVED) on network timeout, got: %d", ExitCodeUnresolved, code)
	}

	if mutationCount.Load() != 1 {
		t.Fatalf("expected exactly 1 attempt without automatic replay, got: %d", mutationCount.Load())
	}

	out := stdout.String()
	if !strings.Contains(out, "[STATUS: UNRESOLVED]") {
		t.Errorf("expected output to contain '[STATUS: UNRESOLVED]', got:\n%s", out)
	}
	if !strings.Contains(out, "RECONCILIATION_REQUIRED") {
		t.Errorf("expected guidance mentioning RECONCILIATION_REQUIRED, got:\n%s", out)
	}
}

// 10. UNCLAIMED provider status reported honestly without claiming final settlement
func TestRun_UnclaimedProviderStatusHonestReporting(t *testing.T) {
	cleanEnv(t)
	var capturedSenderBatchID string
	var capturedSenderItemID string
	batchID := "PAYPAL-BATCH-UNCLAIMED"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if senderHeader, ok := req["sender_batch_header"].(map[string]interface{}); ok {
				capturedSenderBatchID = fmt.Sprintf("%v", senderHeader["sender_batch_id"])
			}
			if items, ok := req["items"].([]interface{}); ok && len(items) > 0 {
				if item0, ok := items[0].(map[string]interface{}); ok {
					capturedSenderItemID = fmt.Sprintf("%v", item0["sender_item_id"])
				}
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					TimeCreated:   time.Now().UTC().Format(time.RFC3339),
				},
			})
			return
		}

		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/") {
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					SenderBatchHeader: &paypal.SenderBatchHeader{
						SenderBatchID: capturedSenderBatchID,
					},
					Amount: &paypal.CurrencyAmount{
						Value:    "1.00",
						Currency: "USD",
					},
				},
				Items: []paypal.PayoutItem{
					{
						PayoutItemID:      "ITEM-UNCLAIMED-001",
						TransactionStatus: "UNCLAIMED",
						SenderItemID:      capturedSenderItemID,
						Receiver:          "sb-unregistered@personal.example.com",
						Amount: paypal.CurrencyAmount{
							Value:    "1.00",
							Currency: "USD",
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-unregistered@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-no-report-file",
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeSuccess {
		t.Fatalf("expected exit code %d (PASS) for UNCLAIMED response, got: %d", ExitCodeSuccess, code)
	}

	out := stdout.String()
	if !strings.Contains(out, "UNCLAIMED") {
		t.Errorf("expected output to clearly state UNCLAIMED, got:\n%s", out)
	}
	if strings.Contains(out, "settled successfully") {
		t.Errorf("UNCLAIMED status should never report settlement completed!")
	}
}

// 11. Sanitized reports and credential redaction
func TestRun_SanitizedReportAndRedaction(t *testing.T) {
	cleanEnv(t)
	reportDir := t.TempDir()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "super-secret-access-token-12345",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "sensitive_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "super_confidential_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "sensitive_client_id",
		PayPalClientSecret:   "super_confidential_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{"-report-dir", reportDir}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeSuccess {
		t.Fatalf("expected exit code %d (PASS) for preflight, got: %d", ExitCodeSuccess, code)
	}

	// Verify report files exist
	files, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatalf("failed reading report directory: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected at least 2 report files (.json and .md), found: %d", len(files))
	}

	for _, f := range files {
		content, rErr := os.ReadFile(filepath.Join(reportDir, f.Name()))
		if rErr != nil {
			t.Fatalf("failed reading report file %s: %v", f.Name(), rErr)
		}
		text := string(content)

		// Assert zero secrets in file
		forbiddenStrings := []string{
			"super-secret-access-token-12345",
			"super_confidential_secret",
		}
		for _, forbidden := range forbiddenStrings {
			if strings.Contains(text, forbidden) {
				t.Errorf("SECURITY LEAK: report file %s contains forbidden secret %q", f.Name(), forbidden)
			}
		}
	}
}

// 12. OAuth HTTP 401 Unauthorized -> ExitCodeOAuthError (2)
func TestRun_OAuthUnauthorized(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_client","error_description":"Client Authentication failed"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "bad_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "bad_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "bad_client_id",
		PayPalClientSecret:   "bad_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{"-no-report-file"}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeOAuthError {
		t.Fatalf("expected exit code %d (OAuth Error) for 401 Unauthorized, got: %d", ExitCodeOAuthError, code)
	}
	if !strings.Contains(stdout.String(), "Invalid client credentials (HTTP 401 Unauthorized)") {
		t.Errorf("expected 401 diagnostic message in output, got:\n%s", stdout.String())
	}
}

// 13. OAuth HTTP 403 Forbidden (Missing Payouts Feature) -> ExitCodeOAuthError (2)
func TestRun_OAuthForbidden(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"access_denied","error_description":"Account lacks Payouts permissions"}`, http.StatusForbidden)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "noperm_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "noperm_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "noperm_client_id",
		PayPalClientSecret:   "noperm_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{"-no-report-file"}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeOAuthError {
		t.Fatalf("expected exit code %d (OAuth Error) for 403 Forbidden, got: %d", ExitCodeOAuthError, code)
	}
	if !strings.Contains(stdout.String(), "PayPal account lacks Payouts permissions (HTTP 403 Forbidden)") {
		t.Errorf("expected 403 diagnostic message in output, got:\n%s", stdout.String())
	}
}

// 14. Correlation Mismatch (e.g. wrong batch ID returned) -> FAIL (1)
func TestRun_CorrelationMismatchFails(t *testing.T) {
	cleanEnv(t)
	batchID := "PAYPAL-BATCH-CORRELATION-TEST"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					TimeCreated:   time.Now().UTC().Format(time.RFC3339),
				},
			})
			return
		}

		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/") {
			// Deliberately return a different sender batch ID to trigger correlation mismatch
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					SenderBatchHeader: &paypal.SenderBatchHeader{
						SenderBatchID: "completely-different-sender-batch-id",
					},
					Amount: &paypal.CurrencyAmount{
						Value:    "1.00",
						Currency: "USD",
					},
				},
				Items: []paypal.PayoutItem{
					{
						PayoutItemID:      "ITEM-001",
						TransactionStatus: "SUCCESS",
						SenderItemID:      "completely-different-item-id",
						Receiver:          "sb-buyer@personal.example.com",
						Amount: paypal.CurrencyAmount{
							Value:    "1.00",
							Currency: "USD",
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, _ := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-buyer@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-no-report-file",
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeFailure {
		t.Fatalf("expected exit code %d (FAIL) on correlation mismatch, got: %d", ExitCodeFailure, code)
	}
	if !strings.Contains(stderr.String(), "Correlation verification failed") {
		t.Errorf("expected correlation failure message in stderr, got:\n%s", stderr.String())
	}
}

// 15. Database credential leakage prevention regression test with sentinel secrets
func TestRun_DatabaseCredentialRedactionSentinel(t *testing.T) {
	cleanEnv(t)
	reportDir := t.TempDir()

	sentinelDBPass := "SENTINEL_DB_SECRET_XYZ987"
	sentinelPPSecret := "SENTINEL_PP_SECRET_ABC123"

	// Configure environment with sentinel database credentials pointing to an unreachable port
	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", sentinelPPSecret)
	os.Setenv("DATABASE_URL", fmt.Sprintf("postgres://sentinel_user:%s@127.0.0.1:54329/claimbridge_test?sslmode=disable", sentinelDBPass))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token-xyz",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_client_id",
		PayPalClientSecret:   sentinelPPSecret,
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	// Attempt write mode against unreachable database to trigger database error handling path
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-recipient@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-report-dir", reportDir,
	}, &stdout, &stderr, testClient, nil)

	if code != ExitCodeFailure {
		t.Fatalf("expected exit code %d (FAIL) on DB failure, got: %d", ExitCodeFailure, code)
	}

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	// 1. Assert sentinel password never appears in stdout or stderr
	if strings.Contains(stdoutStr, sentinelDBPass) {
		t.Fatalf("SECURITY VIOLATION: Database password sentinel leaked to stdout:\n%s", stdoutStr)
	}
	if strings.Contains(stderrStr, sentinelDBPass) {
		t.Fatalf("SECURITY VIOLATION: Database password sentinel leaked to stderr:\n%s", stderrStr)
	}
	if strings.Contains(stdoutStr, sentinelPPSecret) {
		t.Fatalf("SECURITY VIOLATION: PayPal secret sentinel leaked to stdout:\n%s", stdoutStr)
	}
	if strings.Contains(stderrStr, sentinelPPSecret) {
		t.Fatalf("SECURITY VIOLATION: PayPal secret sentinel leaked to stderr:\n%s", stderrStr)
	}

	// 2. Assert sanitized endpoint is reported (safe host and db name without credentials)
	if !strings.Contains(stdoutStr, "127.0.0.1:54329/claimbridge_test") {
		t.Errorf("expected sanitized endpoint in stdout, got:\n%s", stdoutStr)
	}
	if strings.Contains(stdoutStr, "sentinel_user") {
		t.Errorf("stdout should not include database username, got:\n%s", stdoutStr)
	}

	// 3. Inspect generated report files for leaks
	files, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatalf("failed reading report directory: %v", err)
	}
	for _, f := range files {
		content, rErr := os.ReadFile(filepath.Join(reportDir, f.Name()))
		if rErr != nil {
			t.Fatalf("failed reading file %s: %v", f.Name(), rErr)
		}
		text := string(content)
		if strings.Contains(text, sentinelDBPass) {
			t.Errorf("SECURITY VIOLATION: report file %s leaked database password sentinel", f.Name())
		}
		if strings.Contains(text, sentinelPPSecret) {
			t.Errorf("SECURITY VIOLATION: report file %s leaked PayPal secret sentinel", f.Name())
		}
		if strings.Contains(text, "sentinel_user") {
			t.Errorf("SECURITY VIOLATION: report file %s leaked database username", f.Name())
		}
	}
}

// 16. Missing operator initialization in database blocks payout without mutations
func TestRun_MissingOperatorInitializationBlocksPayout(t *testing.T) {
	cleanEnv(t)
	var mutationCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v1/payments/payouts") {
			mutationCount.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}

	// Create an empty repository where operator user does NOT exist
	unseededRepo := db.NewMockRepository()

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-buyer@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-no-report-file",
	}, &stdout, &stderr, testClient, unseededRepo)

	if code != ExitCodeFailure {
		t.Fatalf("expected exit code %d (FAIL) when operator uninitialized, got: %d", ExitCodeFailure, code)
	}

	// Assert zero mutations dispatched
	if mutationCount.Load() != 0 {
		t.Fatalf("expected zero mutations dispatched when operator is uninitialized, got: %d", mutationCount.Load())
	}

	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "Operator context uninitialized") && !strings.Contains(stderrStr, "not found in database") {
		t.Errorf("expected error message describing uninitialized operator, got:\n%s", stderrStr)
	}
	if !strings.Contains(stderrStr, "go run ./cmd/seed") {
		t.Errorf("expected guidance directing operator to 'go run ./cmd/seed', got:\n%s", stderrStr)
	}
}

// 17. Exact sender-item correlation tests
func TestVerifyCorrelation_ExactSenderItemIDRequired(t *testing.T) {
	op := &db.PayoutOperation{
		SenderBatchID: "BATCH-100",
		SenderItemID:  "ITEM-EXPECTED-555",
	}
	expectedRecipient := "recipient@personal.example.com"
	expectedAmount := "1.00"
	expectedCurrency := "USD"

	cases := []struct {
		name              string
		batch             *paypal.PayoutBatchResponse
		expectOverall     bool
		expectItemMatched bool
	}{
		{
			name: "Exact sender item ID match",
			batch: &paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					SenderBatchHeader: &paypal.SenderBatchHeader{SenderBatchID: "BATCH-100"},
				},
				Items: []paypal.PayoutItem{
					{
						SenderItemID: "ITEM-EXPECTED-555",
						Receiver:     expectedRecipient,
						Amount:       paypal.CurrencyAmount{Value: expectedAmount, Currency: expectedCurrency},
					},
				},
			},
			expectOverall:     true,
			expectItemMatched: true,
		},
		{
			name: "Provider item has empty SenderItemID (must fail without fallback)",
			batch: &paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					SenderBatchHeader: &paypal.SenderBatchHeader{SenderBatchID: "BATCH-100"},
				},
				Items: []paypal.PayoutItem{
					{
						SenderItemID: "",
						Receiver:     expectedRecipient,
						Amount:       paypal.CurrencyAmount{Value: expectedAmount, Currency: expectedCurrency},
					},
				},
			},
			expectOverall:     false,
			expectItemMatched: false,
		},
		{
			name: "Provider item has mismatched SenderItemID",
			batch: &paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					SenderBatchHeader: &paypal.SenderBatchHeader{SenderBatchID: "BATCH-100"},
				},
				Items: []paypal.PayoutItem{
					{
						SenderItemID: "ITEM-WRONG-999",
						Receiver:     expectedRecipient,
						Amount:       paypal.CurrencyAmount{Value: expectedAmount, Currency: expectedCurrency},
					},
				},
			},
			expectOverall:     false,
			expectItemMatched: false,
		},
		{
			name: "Ambiguous item list: multiple items with same expected SenderItemID",
			batch: &paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					SenderBatchHeader: &paypal.SenderBatchHeader{SenderBatchID: "BATCH-100"},
				},
				Items: []paypal.PayoutItem{
					{
						SenderItemID: "ITEM-EXPECTED-555",
						Receiver:     expectedRecipient,
						Amount:       paypal.CurrencyAmount{Value: expectedAmount, Currency: expectedCurrency},
					},
					{
						SenderItemID: "ITEM-EXPECTED-555",
						Receiver:     expectedRecipient,
						Amount:       paypal.CurrencyAmount{Value: expectedAmount, Currency: expectedCurrency},
					},
				},
			},
			expectOverall:     false,
			expectItemMatched: false,
		},
		{
			name: "Multiple items with exactly one matching expected SenderItemID",
			batch: &paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					SenderBatchHeader: &paypal.SenderBatchHeader{SenderBatchID: "BATCH-100"},
				},
				Items: []paypal.PayoutItem{
					{
						SenderItemID: "ITEM-OTHER-001",
						Receiver:     "other@example.com",
						Amount:       paypal.CurrencyAmount{Value: "5.00", Currency: "USD"},
					},
					{
						SenderItemID: "ITEM-EXPECTED-555",
						Receiver:     expectedRecipient,
						Amount:       paypal.CurrencyAmount{Value: expectedAmount, Currency: expectedCurrency},
					},
				},
			},
			expectOverall:     true,
			expectItemMatched: true,
		},
		{
			name: "Empty items list in provider batch",
			batch: &paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					SenderBatchHeader: &paypal.SenderBatchHeader{SenderBatchID: "BATCH-100"},
				},
				Items: []paypal.PayoutItem{},
			},
			expectOverall:     false,
			expectItemMatched: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := verifyCorrelation(op, expectedRecipient, expectedAmount, expectedCurrency, tc.batch)
			if res.OverallCorrelated != tc.expectOverall {
				t.Errorf("expected OverallCorrelated=%t, got %t", tc.expectOverall, res.OverallCorrelated)
			}
			if res.SenderItemIDMatched != tc.expectItemMatched {
				t.Errorf("expected SenderItemIDMatched=%t, got %t", tc.expectItemMatched, res.SenderItemIDMatched)
			}
		})
	}
}

// 18. Table-driven provider outcome classification tests
func TestClassifyProviderOutcome_TableDriven(t *testing.T) {
	validCorrelation := &CorrelationVerification{OverallCorrelated: true}
	invalidCorrelation := &CorrelationVerification{OverallCorrelated: false}

	cases := []struct {
		name             string
		itemStatus       string
		batchStatus      string
		correlation      *CorrelationVerification
		persistenceValid bool
		auditValid       bool
		expectedOutcome  string
		expectedExit     int
	}{
		{"SUCCESS settles completely", "SUCCESS", "SUCCESS", validCorrelation, true, true, "PASS", ExitCodeSuccess},
		{"UNCLAIMED passes integration but is unsettled", "UNCLAIMED", "SUCCESS", validCorrelation, true, true, "PASS", ExitCodeSuccess},
		{"PENDING item accepted in progress", "PENDING", "PENDING", validCorrelation, true, true, "PASS", ExitCodeSuccess},
		{"PROCESSING item accepted in progress", "PROCESSING", "PROCESSING", validCorrelation, true, true, "PASS", ExitCodeSuccess},
		{"FAILED item terminal failure", "FAILED", "SUCCESS", validCorrelation, true, true, "FAIL", ExitCodeFailure},
		{"BLOCKED item terminal failure", "BLOCKED", "SUCCESS", validCorrelation, true, true, "FAIL", ExitCodeFailure},
		{"DENIED item terminal failure", "DENIED", "DENIED", validCorrelation, true, true, "FAIL", ExitCodeFailure},
		{"CANCELED item terminal failure", "CANCELED", "CANCELED", validCorrelation, true, true, "FAIL", ExitCodeFailure},
		{"RETURNED requires manual review", "RETURNED", "SUCCESS", validCorrelation, true, true, "UNRESOLVED", ExitCodeUnresolved},
		{"REFUNDED requires manual review", "REFUNDED", "SUCCESS", validCorrelation, true, true, "UNRESOLVED", ExitCodeUnresolved},
		{"REVERSED requires manual review", "REVERSED", "SUCCESS", validCorrelation, true, true, "UNRESOLVED", ExitCodeUnresolved},
		{"ONHOLD requires manual review", "ONHOLD", "ONHOLD", validCorrelation, true, true, "UNRESOLVED", ExitCodeUnresolved},
		{"Empty status unresolved", "", "", validCorrelation, true, true, "UNRESOLVED", ExitCodeUnresolved},
		{"Unknown status unresolved", "SOME_UNKNOWN_STATUS", "", validCorrelation, true, true, "UNRESOLVED", ExitCodeUnresolved},
		{"Failed correlation overrides SUCCESS", "SUCCESS", "SUCCESS", invalidCorrelation, true, true, "FAIL", ExitCodeFailure},
		{"Failed persistence overrides SUCCESS", "SUCCESS", "SUCCESS", validCorrelation, false, true, "FAIL", ExitCodeFailure},
		{"Failed audit verification overrides SUCCESS", "SUCCESS", "SUCCESS", validCorrelation, true, false, "FAIL", ExitCodeFailure},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, details, code := classifyProviderOutcome(tc.itemStatus, tc.batchStatus, tc.correlation, tc.persistenceValid, tc.auditValid)
			if outcome != tc.expectedOutcome {
				t.Errorf("expected outcome %s, got %s (details: %s)", tc.expectedOutcome, outcome, details)
			}
			if code != tc.expectedExit {
				t.Errorf("expected exit code %d, got %d", tc.expectedExit, code)
			}
		})
	}
}

// 19. Exact amount parsing table-driven tests
func TestParseAmountCents_TableDriven(t *testing.T) {
	validCases := []struct {
		input         string
		expectedCents int64
		expectedFmt   string
	}{
		{"0.01", 1, "0.01"},
		{"0.5", 50, "0.50"},
		{"0.50", 50, "0.50"},
		{"1.00", 100, "1.00"},
		{"1", 100, "1.00"},
		{" 1.00 ", 100, "1.00"},
	}

	for _, tc := range validCases {
		t.Run("Valid: "+tc.input, func(t *testing.T) {
			cents, fmtStr, err := parseAmountCents(tc.input)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if cents != tc.expectedCents {
				t.Errorf("expected %d cents, got %d", tc.expectedCents, cents)
			}
			if fmtStr != tc.expectedFmt {
				t.Errorf("expected formatted %q, got %q", tc.expectedFmt, fmtStr)
			}
		})
	}

	invalidCases := []struct {
		name  string
		input string
	}{
		{"Above ceiling: 1.01", "1.01"},
		{"Above ceiling: 2.00", "2.00"},
		{"Above ceiling: 100.00", "100.00"},
		{"Excessive decimals: 1.001", "1.001"},
		{"Excessive decimals: 0.009", "0.009"},
		{"Negative: -0.50", "-0.50"},
		{"Negative: -1.00", "-1.00"},
		{"Explicit plus sign: +1.00", "+1.00"},
		{"Exponent notation: 1e0", "1e0"},
		{"Exponent notation: 1e-2", "1e-2"},
		{"NaN", "NaN"},
		{"Inf", "Inf"},
		{"-Inf", "-Inf"},
		{"Alphabetic: abc", "abc"},
		{"Currency symbol: $1.00", "$1.00"},
		{"Multiple decimal points: 1.0.0", "1.0.0"},
		{"Only decimal point: .", "."},
		{"Trailing dot: 1.", "1."},
		{"Empty string", ""},
		{"Whitespace only", "   "},
		{"Zero: 0", "0"},
		{"Zero: 0.00", "0.00"},
		{"Very large number", "99999999999999999999999999999999"},
	}

	for _, tc := range invalidCases {
		t.Run("Invalid: "+tc.name, func(t *testing.T) {
			cents, _, err := parseAmountCents(tc.input)
			if err == nil {
				t.Errorf("expected error for input %q, got %d cents without error", tc.input, cents)
			}
		})
	}
}

// 20. Honest report semantics test
func TestRun_ReportHonestSemantics(t *testing.T) {
	cleanEnv(t)
	reportDir := t.TempDir()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}
	repo := setupTestRepo(t)

	// Case A: Missing confirmation flag -> SubmissionRequested=true, MutationDispatched=false, PayoutAttempted=false
	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb@personal.example.com",
		"-report-dir", reportDir,
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeFailure {
		t.Fatalf("expected exit code %d (FAIL), got %d", ExitCodeFailure, code)
	}

	// Verify report file contents
	files, err := os.ReadDir(reportDir)
	if err != nil || len(files) == 0 {
		t.Fatalf("expected report file generated")
	}

	var jsonReportPath string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			jsonReportPath = filepath.Join(reportDir, f.Name())
			break
		}
	}
	if jsonReportPath == "" {
		t.Fatalf("no json report file found in %s", reportDir)
	}

	data, err := os.ReadFile(jsonReportPath)
	if err != nil {
		t.Fatalf("failed reading json report: %v", err)
	}

	var rep SandboxE2EReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling json report: %v", err)
	}

	if !rep.SubmissionRequested {
		t.Errorf("expected SubmissionRequested=true when -submit-payout flag passed")
	}
	if rep.MutationDispatched {
		t.Errorf("MutationDispatched MUST be false when confirmation flag was missing")
	}
	if rep.PayoutAttempted {
		t.Errorf("PayoutAttempted MUST be false when confirmation flag was missing")
	}
	if rep.ProviderResponseReceived {
		t.Errorf("ProviderResponseReceived MUST be false when no mutation dispatched")
	}
}

// 21. Actual audit records verification test
func TestRun_AuditEvidenceVerification(t *testing.T) {
	cleanEnv(t)
	reportDir := t.TempDir()

	batchID := "PAYPAL-AUDIT-VERIFY-BATCH"
	var capturedSenderBatchID string
	var capturedSenderItemID string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-oauth-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        paypal.PayPalPayoutsScope,
			})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/payments/payouts" {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if senderHeader, ok := req["sender_batch_header"].(map[string]interface{}); ok {
				capturedSenderBatchID = fmt.Sprintf("%v", senderHeader["sender_batch_id"])
			}
			if items, ok := req["items"].([]interface{}); ok && len(items) > 0 {
				if item0, ok := items[0].(map[string]interface{}); ok {
					capturedSenderItemID = fmt.Sprintf("%v", item0["sender_item_id"])
				}
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					TimeCreated:   time.Now().UTC().Format(time.RFC3339),
				},
			})
			return
		}

		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/") {
			json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: batchID,
					BatchStatus:   "SUCCESS",
					SenderBatchHeader: &paypal.SenderBatchHeader{
						SenderBatchID: capturedSenderBatchID,
					},
					Amount: &paypal.CurrencyAmount{
						Value:    "1.00",
						Currency: "USD",
					},
				},
				Items: []paypal.PayoutItem{
					{
						PayoutItemID:      "ITEM-001",
						TransactionStatus: "SUCCESS",
						SenderItemID:      capturedSenderItemID,
						Receiver:          "sb-buyer@personal.example.com",
						Amount: paypal.CurrencyAmount{
							Value:    "1.00",
							Currency: "USD",
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}
	repo := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-recipient", "sb-buyer@personal.example.com",
		"-amount", "1.00",
		"-confirm-sandbox-only",
		"-report-dir", reportDir,
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeSuccess {
		t.Fatalf("expected exit code %d (PASS), got: %d\nStderr: %s", ExitCodeSuccess, code, stderr.String())
	}

	// Verify report file
	files, err := os.ReadDir(reportDir)
	if err != nil || len(files) == 0 {
		t.Fatalf("no reports generated in %s", reportDir)
	}

	var jsonReportPath string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			jsonReportPath = filepath.Join(reportDir, f.Name())
			break
		}
	}

	data, err := os.ReadFile(jsonReportPath)
	if err != nil {
		t.Fatalf("failed reading json report: %v", err)
	}

	var rep SandboxE2EReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling json report: %v", err)
	}

	if !rep.AuditVerificationPassed {
		t.Errorf("expected AuditVerificationPassed=true")
	}
	if rep.AuditEventsCount <= 0 {
		t.Errorf("expected AuditEventsCount > 0, got %d", rep.AuditEventsCount)
	}

	// Cross check against repo
	user, _ := repo.GetUserByEmail(context.Background(), seeder.OperatorEmail)
	memberships, _ := repo.GetOrganizationMemberships(context.Background(), user.ID)
	orgID := memberships[0].Organization.ID
	actualAuditCount, err := repo.CountAuditEvents(context.Background(), orgID, rep.CaseID, rep.OperationID)
	if err != nil {
		t.Fatalf("CountAuditEvents failed: %v", err)
	}
	if rep.AuditEventsCount != actualAuditCount {
		t.Errorf("reported audit events count (%d) does not match actual count from repository (%d)", rep.AuditEventsCount, actualAuditCount)
	}
}


func TestRun_HTTP403StructuredDiagnosticsCaptured(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "TEST-TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				AppID:       "APP-DIAG-TEST",
				Scope:       "openid " + paypal.PayPalPayoutsScope,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Paypal-Debug-Id", "dbg-header-403")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{
				"name": "USER_NOT_ELIGIBLE_FOR_PAYOUT",
				"message": "The account is not eligible for payouts",
				"debug_id": "dbg-body-403",
				"details": [
					{"issue": "PAYOUTS_RESTRICTED"}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "diag_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "diag_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "diag_client_id",
		PayPalClientSecret:   "diag_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}
	repo := setupTestRepo(t)

	reportDir := filepath.Join(t.TempDir(), "reports")
	var stdout, stderr bytes.Buffer
	code := runWithDeps([]string{
		"-submit-payout",
		"-confirm-sandbox-only",
		"-recipient", "receiver@example.com",
		"-amount", "1.00",
		"-report-dir", reportDir,
	}, &stdout, &stderr, testClient, repo)

	if code != ExitCodeFailure {
		t.Fatalf("expected ExitCodeFailure (1) for 403 Forbidden, got: %d", code)
	}

	files, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatalf("failed reading report directory: %v", err)
	}
	var jsonPath string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			jsonPath = filepath.Join(reportDir, f.Name())
			break
		}
	}
	if jsonPath == "" {
		t.Fatalf("expected JSON report file, none found in %s", reportDir)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed reading report: %v", err)
	}

	var rep SandboxE2EReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("failed unmarshaling report: %v", err)
	}

	if rep.ProviderErrorName != "USER_NOT_ELIGIBLE_FOR_PAYOUT" {
		t.Errorf("expected ProviderErrorName 'USER_NOT_ELIGIBLE_FOR_PAYOUT', got %q", rep.ProviderErrorName)
	}
	if rep.ProviderErrorIssue != "PAYOUTS_RESTRICTED" {
		t.Errorf("expected ProviderErrorIssue 'PAYOUTS_RESTRICTED', got %q", rep.ProviderErrorIssue)
	}
	if rep.ProviderDebugID != "dbg-body-403" {
		t.Errorf("expected ProviderDebugID 'dbg-body-403', got %q", rep.ProviderDebugID)
	}
}


// 23. Missing Payouts Scope Fail-Closed Pre-Database Invariant
func TestRun_MissingPayoutsScopeBlocked(t *testing.T) {
	cleanEnv(t)
	var mutationCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "mock-valid-token-no-payouts",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				AppID:       "APP-NO-PAYOUTS-SCOPE",
				Scope:       "openid profile email", // Valid OAuth scopes, but lacks Payouts scope
			})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v1/payments/payouts") {
			mutationCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_id",
		PayPalClientSecret:   "test_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}

	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed constructing test client: %v", err)
	}

	t.Run("MutationMode_NoDatabaseInitOrMigrations_ExitCodeBlocked", func(t *testing.T) {
		var dbFactoryCalls atomic.Int32
		var migrationCalls atomic.Int32

		mockFactory := func(ctx context.Context, databaseURL string) (db.Repository, error) {
			dbFactoryCalls.Add(1)
			return &mockMigratorRepo{
				Repository:     db.NewMockRepository(),
				migrationCalls: &migrationCalls,
			}, nil
		}

		reportDir := filepath.Join(t.TempDir(), "reports-no-db")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, testClient, nil, mockFactory)

		if code != ExitCodeBlocked {
			t.Fatalf("expected ExitCodeBlocked (%d), got: %d\nStderr: %s", ExitCodeBlocked, code, stderr.String())
		}

		// Verify database factory was NEVER called
		if dbFactoryCalls.Load() != 0 {
			t.Fatalf("expected 0 database factory calls on missing-scope path, got: %d", dbFactoryCalls.Load())
		}
		if migrationCalls.Load() != 0 {
			t.Fatalf("expected 0 migration calls on missing-scope path, got: %d", migrationCalls.Load())
		}

		// Verify zero calls to PayPal payout POST
		if mutationCount.Load() != 0 {
			t.Fatalf("expected zero calls to POST /v1/payments/payouts, got: %d", mutationCount.Load())
		}

		// Read and verify report artifact
		files, err := os.ReadDir(reportDir)
		if err != nil {
			t.Fatalf("failed reading report directory: %v", err)
		}
		var jsonPath string
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".json") {
				jsonPath = filepath.Join(reportDir, f.Name())
				break
			}
		}
		if jsonPath == "" {
			t.Fatalf("expected JSON report file, none found in %s", reportDir)
		}

		data, err := os.ReadFile(jsonPath)
		if err != nil {
			t.Fatalf("failed reading report: %v", err)
		}

		var rep SandboxE2EReport
		if err := json.Unmarshal(data, &rep); err != nil {
			t.Fatalf("failed unmarshaling report: %v", err)
		}

		if rep.Outcome != "BLOCKED" {
			t.Errorf("expected report Outcome 'BLOCKED', got %q", rep.Outcome)
		}
		if !rep.SubmissionRequested {
			t.Errorf("expected SubmissionRequested=true")
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
		if rep.PayoutAttempted {
			t.Errorf("expected PayoutAttempted=false")
		}
		if rep.ProviderResponseReceived {
			t.Errorf("expected ProviderResponseReceived=false")
		}
		if rep.HasPayoutsScope {
			t.Errorf("expected HasPayoutsScope=false")
		}
		if rep.DatabaseReachable {
			t.Errorf("expected DatabaseReachable=false when blocked before database initialization")
		}
		if rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=false when blocked before database initialization")
		}
		if rep.CaseID != "" {
			t.Errorf("expected empty CaseID in report, got %q", rep.CaseID)
		}
		if rep.OperationID != "" {
			t.Errorf("expected empty OperationID in report, got %q", rep.OperationID)
		}
		if !strings.Contains(rep.Details, "missing required Payouts permission scope") {
			t.Errorf("expected report Details to explain missing Payouts scope, got: %q", rep.Details)
		}
	})

	t.Run("MutationMode_WithInjectedRepo_ZeroRepoCalls", func(t *testing.T) {
		repo := setupTestRepo(t)

		user, err := repo.GetUserByEmail(context.Background(), seeder.OperatorEmail)
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}
		memberships, err := repo.GetOrganizationMemberships(context.Background(), user.ID)
		if err != nil || len(memberships) == 0 {
			t.Fatalf("GetOrganizationMemberships failed: %v", err)
		}
		orgID := memberships[0].Organization.ID

		initialCases, totalCasesBefore, err := repo.ListPayoutCasesByOrg(context.Background(), orgID, 100, 0)
		if err != nil {
			t.Fatalf("ListPayoutCasesByOrg failed: %v", err)
		}

		reportDir := filepath.Join(t.TempDir(), "reports-injected")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, testClient, repo)

		if code != ExitCodeBlocked {
			t.Fatalf("expected ExitCodeBlocked (%d), got: %d\nStderr: %s", ExitCodeBlocked, code, stderr.String())
		}

		// Verify no case was created
		afterCases, totalCasesAfter, err := repo.ListPayoutCasesByOrg(context.Background(), orgID, 100, 0)
		if err != nil {
			t.Fatalf("ListPayoutCasesByOrg after run failed: %v", err)
		}
		if totalCasesAfter != totalCasesBefore || len(afterCases) != len(initialCases) {
			t.Errorf("expected no payout case created (before=%d, after=%d)", totalCasesBefore, totalCasesAfter)
		}
		for _, c := range afterCases {
			if strings.HasPrefix(c.Reference, "SANDBOX-E2E") {
				t.Errorf("expected no SANDBOX-E2E case created in repository, found: %s", c.Reference)
			}
		}

		// Verify no operation was created
		for _, c := range afterCases {
			ops, err := repo.ListPayoutOperationsByCaseID(context.Background(), orgID, c.ID)
			if err != nil {
				t.Fatalf("ListPayoutOperationsByCaseID failed: %v", err)
			}
			for _, op := range ops {
				if strings.HasPrefix(op.SenderBatchID, "SANDBOX-E2E") {
					t.Errorf("expected no SANDBOX-E2E operation created, found: %s", op.SenderBatchID)
				}
			}
		}
	})

	t.Run("ReadOnlyPreflight_PassesWithoutScope", func(t *testing.T) {
		repo := setupTestRepo(t)
		var roStdout, roStderr bytes.Buffer
		roCode := runWithDeps([]string{"-no-report-file"}, &roStdout, &roStderr, testClient, repo)
		if roCode != ExitCodeSuccess {
			t.Fatalf("expected ExitCodeSuccess (0) for read-only preflight without payouts scope, got: %d", roCode)
		}
	})

	t.Run("ValidScope_MutationProceedsToDatabasePreflight", func(t *testing.T) {
		// Mock server that returns Payouts scope
		scopeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/oauth2/token" {
				json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
					AccessToken: "mock-valid-token-with-payouts",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
					AppID:       "APP-WITH-PAYOUTS-SCOPE",
					Scope:       "openid " + paypal.PayPalPayoutsScope,
				})
				return
			}
			http.NotFound(w, r)
		}))
		defer scopeServer.Close()

		scopeCfg := &config.Config{
			AppEnv:               "development",
			PayPalEnv:            "sandbox",
			PayPalClientID:       "test_id",
			PayPalClientSecret:   "test_secret",
			PayPalBaseURL:        scopeServer.URL,
			PayPalRequestTimeout: 5 * time.Second,
		}
		scopeClient, sErr := paypal.NewTestClient(scopeCfg, scopeServer.Client(), scopeServer.URL)
		if sErr != nil {
			t.Fatalf("failed constructing test client: %v", sErr)
		}

		var factoryCalls atomic.Int32
		mockFactory := func(ctx context.Context, databaseURL string) (db.Repository, error) {
			factoryCalls.Add(1)
			return nil, errors.New("simulated db connection failure after scope gate")
		}

		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-no-report-file",
		}, &stdout, &stderr, scopeClient, nil, mockFactory)

		// When scope is present, it MUST pass the scope gate and invoke the database factory!
		if factoryCalls.Load() != 1 {
			t.Fatalf("expected exactly 1 database factory call when Payouts scope is present, got: %d", factoryCalls.Load())
		}
		if code != ExitCodeFailure {
			t.Fatalf("expected ExitCodeFailure (1) due to database failure after passing scope gate, got: %d", code)
		}
	})
}

// 24. Truthful Reporting: SubmissionRequested and Mutation Lifecycle Tracking
func TestRun_SubmissionRequestedReportAccuracy(t *testing.T) {
	cleanEnv(t)

	t.Run("MutationMode_SubmitPayoutFlag_MissingCredentials_ReportsSubmissionRequested", func(t *testing.T) {
		cleanEnv(t)
		os.Setenv("PAYPAL_ENV", "sandbox")
		os.Unsetenv("PAYPAL_CLIENT_ID")
		os.Unsetenv("PAYPAL_CLIENT_SECRET")

		reportDir := filepath.Join(t.TempDir(), "reports-missing-creds-submit")
		var stdout, stderr bytes.Buffer
		code := run([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-env-file", "nonexistent.env",
			"-report-dir", reportDir,
		}, &stdout, &stderr)

		if code != ExitCodeBlocked {
			t.Fatalf("expected ExitCodeBlocked (%d), got: %d", ExitCodeBlocked, code)
		}

		rep := readJSONReport(t, reportDir)
		if !rep.SubmissionRequested {
			t.Errorf("expected SubmissionRequested=true when -submit-payout was passed")
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
		if rep.PayoutAttempted {
			t.Errorf("expected PayoutAttempted=false")
		}
		if rep.ProviderResponseReceived {
			t.Errorf("expected ProviderResponseReceived=false")
		}
		if rep.Outcome != "BLOCKED" {
			t.Errorf("expected Outcome='BLOCKED', got %q", rep.Outcome)
		}
	})

	t.Run("MutationMode_CreateTestPayoutFlag_MissingCredentials_ReportsSubmissionRequested", func(t *testing.T) {
		cleanEnv(t)
		os.Setenv("PAYPAL_ENV", "sandbox")
		os.Unsetenv("PAYPAL_CLIENT_ID")
		os.Unsetenv("PAYPAL_CLIENT_SECRET")

		reportDir := filepath.Join(t.TempDir(), "reports-missing-creds-create")
		var stdout, stderr bytes.Buffer
		code := run([]string{
			"-create-test-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-env-file", "nonexistent.env",
			"-report-dir", reportDir,
		}, &stdout, &stderr)

		if code != ExitCodeBlocked {
			t.Fatalf("expected ExitCodeBlocked (%d), got: %d", ExitCodeBlocked, code)
		}

		rep := readJSONReport(t, reportDir)
		if !rep.SubmissionRequested {
			t.Errorf("expected SubmissionRequested=true when -create-test-payout was passed")
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
		if rep.PayoutAttempted {
			t.Errorf("expected PayoutAttempted=false")
		}
		if rep.ProviderResponseReceived {
			t.Errorf("expected ProviderResponseReceived=false")
		}
		if rep.Outcome != "BLOCKED" {
			t.Errorf("expected Outcome='BLOCKED', got %q", rep.Outcome)
		}
	})

	t.Run("MutationMode_OAuthFailure_ReportsSubmissionRequested", func(t *testing.T) {
		cleanEnv(t)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_client",
				"error_description": "Client Authentication failed",
			})
		}))
		defer ts.Close()

		os.Setenv("PAYPAL_ENV", "sandbox")
		os.Setenv("PAYPAL_CLIENT_ID", "test_id")
		os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

		cfg := &config.Config{
			AppEnv:               "development",
			PayPalEnv:            "sandbox",
			PayPalClientID:       "test_id",
			PayPalClientSecret:   "test_secret",
			PayPalBaseURL:        ts.URL,
			PayPalRequestTimeout: 5 * time.Second,
		}

		testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("failed constructing test client: %v", err)
		}

		reportDir := filepath.Join(t.TempDir(), "reports-oauth-fail")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, testClient, nil)

		if code != ExitCodeOAuthError {
			t.Fatalf("expected ExitCodeOAuthError (%d), got: %d", ExitCodeOAuthError, code)
		}

		rep := readJSONReport(t, reportDir)
		if !rep.SubmissionRequested {
			t.Errorf("expected SubmissionRequested=true when mutation requested even if OAuth failed")
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
		if rep.PayoutAttempted {
			t.Errorf("expected PayoutAttempted=false")
		}
		if rep.ProviderResponseReceived {
			t.Errorf("expected ProviderResponseReceived=false")
		}
		if rep.OAuthTokenAcquired {
			t.Errorf("expected OAuthTokenAcquired=false")
		}
		if rep.Outcome != "FAIL" {
			t.Errorf("expected Outcome='FAIL', got %q", rep.Outcome)
		}
	})

	t.Run("ReadOnly_ReportsSubmissionRequestedFalse", func(t *testing.T) {
		cleanEnv(t)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/oauth2/token" {
				json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
					AccessToken: "mock-valid-token-ro",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
					AppID:       "APP-READ-ONLY",
					Scope:       "openid profile",
				})
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		os.Setenv("PAYPAL_ENV", "sandbox")
		os.Setenv("PAYPAL_CLIENT_ID", "test_id")
		os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

		cfg := &config.Config{
			AppEnv:               "development",
			PayPalEnv:            "sandbox",
			PayPalClientID:       "test_id",
			PayPalClientSecret:   "test_secret",
			PayPalBaseURL:        ts.URL,
			PayPalRequestTimeout: 5 * time.Second,
		}

		testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("failed constructing test client: %v", err)
		}

		reportDir := filepath.Join(t.TempDir(), "reports-ro")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-report-dir", reportDir,
		}, &stdout, &stderr, testClient, nil)

		if code != ExitCodeSuccess {
			t.Fatalf("expected ExitCodeSuccess (%d), got: %d", ExitCodeSuccess, code)
		}

		rep := readJSONReport(t, reportDir)
		if rep.SubmissionRequested {
			t.Errorf("expected SubmissionRequested=false in read-only mode")
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
		if rep.PayoutAttempted {
			t.Errorf("expected PayoutAttempted=false")
		}
		if rep.ProviderResponseReceived {
			t.Errorf("expected ProviderResponseReceived=false")
		}
		if rep.Outcome != "PASS" {
			t.Errorf("expected Outcome='PASS', got %q", rep.Outcome)
		}
	})
}

// 25. Migration Verification Fail-Closed Behavior
func TestRun_MigrationVerification(t *testing.T) {
	cleanEnv(t)

	setupPayoutServer := func(t *testing.T) (*httptest.Server, *paypal.Client, *atomic.Int32) {
		t.Helper()
		var mutationCount atomic.Int32
		var capturedSenderBatchID string
		var capturedSenderItemID string
		batchID := "PAYPAL-MIGRATION-TEST-123"

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/oauth2/token" {
				json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
					AccessToken: "mock-valid-token-with-payouts",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
					AppID:       "APP-MIGRATION-TEST",
					Scope:       "openid " + paypal.PayPalPayoutsScope,
				})
				return
			}
			if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v1/payments/payouts") {
				mutationCount.Add(1)
				var req map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if senderHeader, ok := req["sender_batch_header"].(map[string]interface{}); ok {
					capturedSenderBatchID = fmt.Sprintf("%v", senderHeader["sender_batch_id"])
				}
				if items, ok := req["items"].([]interface{}); ok && len(items) > 0 {
					if item0, ok := items[0].(map[string]interface{}); ok {
						capturedSenderItemID = fmt.Sprintf("%v", item0["sender_item_id"])
					}
				}

				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
					BatchHeader: paypal.PayoutBatchHeader{
						PayoutBatchID: batchID,
						BatchStatus:   "SUCCESS",
						TimeCreated:   time.Now().UTC().Format(time.RFC3339),
					},
				})
				return
			}
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/") {
				json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
					BatchHeader: paypal.PayoutBatchHeader{
						PayoutBatchID: batchID,
						BatchStatus:   "SUCCESS",
						SenderBatchHeader: &paypal.SenderBatchHeader{
							SenderBatchID: capturedSenderBatchID,
						},
						Amount: &paypal.CurrencyAmount{
							Value:    "1.00",
							Currency: "USD",
						},
					},
					Items: []paypal.PayoutItem{
						{
							PayoutItemID:      "ITEM-001",
							TransactionStatus: "SUCCESS",
							SenderItemID:      capturedSenderItemID,
							Receiver:          "receiver@example.com",
							Amount: paypal.CurrencyAmount{
								Value:    "1.00",
								Currency: "USD",
							},
						},
					},
				})
				return
			}
			http.NotFound(w, r)
		}))

		os.Setenv("PAYPAL_ENV", "sandbox")
		os.Setenv("PAYPAL_CLIENT_ID", "test_id")
		os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")

		cfg := &config.Config{
			AppEnv:               "development",
			PayPalEnv:            "sandbox",
			PayPalClientID:       "test_id",
			PayPalClientSecret:   "test_secret",
			PayPalBaseURL:        ts.URL,
			PayPalRequestTimeout: 5 * time.Second,
		}

		client, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("failed constructing test client: %v", err)
		}
		return ts, client, &mutationCount
	}

	t.Run("ValidScope_SuccessfulMigration_SetsDatabaseMigrationsTrue", func(t *testing.T) {
		ts, client, mutationCount := setupPayoutServer(t)
		defer ts.Close()

		baseRepo := setupTestRepo(t)
		var migrationCalls atomic.Int32
		var closeCalls atomic.Int32

		mockFactory := func(ctx context.Context, databaseURL string) (db.Repository, error) {
			return &mockMigratorRepo{
				Repository:     baseRepo,
				migrationCalls: &migrationCalls,
				closeCalls:     &closeCalls,
				migrationErr:   nil,
			}, nil
		}

		reportDir := filepath.Join(t.TempDir(), "reports-migration-pass")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, nil, mockFactory)

		if code != ExitCodeSuccess {
			t.Fatalf("expected ExitCodeSuccess (%d), got: %d\nStderr: %s", ExitCodeSuccess, code, stderr.String())
		}
		if migrationCalls.Load() != 1 {
			t.Fatalf("expected exactly 1 migration call, got: %d", migrationCalls.Load())
		}
		if closeCalls.Load() == 0 {
			t.Fatalf("expected repository Close() to be called")
		}
		if mutationCount.Load() != 1 {
			t.Fatalf("expected 1 payout mutation dispatched, got: %d", mutationCount.Load())
		}

		rep := readJSONReport(t, reportDir)
		if !rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=true upon successful migration")
		}
		if !rep.DatabaseReachable {
			t.Errorf("expected DatabaseReachable=true")
		}
		if rep.Outcome != "PASS" {
			t.Errorf("expected Outcome='PASS', got %q", rep.Outcome)
		}
	})

	t.Run("ValidScope_MigrationError_LeavesDatabaseMigrationsFalseAndFails", func(t *testing.T) {
		ts, client, mutationCount := setupPayoutServer(t)
		defer ts.Close()

		baseRepo := setupTestRepo(t)
		var migrationCalls atomic.Int32
		var closeCalls atomic.Int32

		mockFactory := func(ctx context.Context, databaseURL string) (db.Repository, error) {
			return &mockMigratorRepo{
				Repository:     baseRepo,
				migrationCalls: &migrationCalls,
				closeCalls:     &closeCalls,
				migrationErr:   errors.New("simulated migration schema error"),
			}, nil
		}

		reportDir := filepath.Join(t.TempDir(), "reports-migration-fail")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, nil, mockFactory)

		if code != ExitCodeFailure {
			t.Fatalf("expected ExitCodeFailure (%d), got: %d", ExitCodeFailure, code)
		}
		if migrationCalls.Load() != 1 {
			t.Fatalf("expected exactly 1 migration call, got: %d", migrationCalls.Load())
		}
		if closeCalls.Load() == 0 {
			t.Fatalf("expected repository Close() to be called on failure")
		}
		if mutationCount.Load() != 0 {
			t.Fatalf("expected 0 payout mutations dispatched on migration failure, got: %d", mutationCount.Load())
		}

		rep := readJSONReport(t, reportDir)
		if rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=false when migration fails")
		}
		if rep.Outcome != "FAIL" {
			t.Errorf("expected Outcome='FAIL', got %q", rep.Outcome)
		}
		if !strings.Contains(rep.Details, "Database schema migrations failed") {
			t.Errorf("expected details to report migration failure, got: %q", rep.Details)
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
		if rep.PayoutAttempted {
			t.Errorf("expected PayoutAttempted=false")
		}
	})

	t.Run("ValidScope_RepositoryWithoutRunMigrations_FailsClosedInMutationMode", func(t *testing.T) {
		ts, client, mutationCount := setupPayoutServer(t)
		defer ts.Close()

		baseRepo := setupTestRepo(t)
		var closeCalls atomic.Int32

		mockFactory := func(ctx context.Context, databaseURL string) (db.Repository, error) {
			return &mockNonMigratorRepo{
				Repository: baseRepo,
				closeCalls: &closeCalls,
			}, nil
		}

		reportDir := filepath.Join(t.TempDir(), "reports-no-migrator-mutation")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, nil, mockFactory)

		if code != ExitCodeFailure {
			t.Fatalf("expected ExitCodeFailure (%d), got: %d", ExitCodeFailure, code)
		}
		if closeCalls.Load() == 0 {
			t.Fatalf("expected repository Close() to be called when failing closed")
		}
		if mutationCount.Load() != 0 {
			t.Fatalf("expected 0 payout mutations dispatched, got: %d", mutationCount.Load())
		}

		rep := readJSONReport(t, reportDir)
		if rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=false when repo lacks RunMigrations")
		}
		if rep.Outcome != "FAIL" {
			t.Errorf("expected Outcome='FAIL', got %q", rep.Outcome)
		}
		if !strings.Contains(rep.Details, "repository does not implement RunMigrations") {
			t.Errorf("expected details to state missing RunMigrations implementation, got: %q", rep.Details)
		}
		if rep.MutationDispatched {
			t.Errorf("expected MutationDispatched=false")
		}
	})

	t.Run("ReadOnly_RepositoryWithoutRunMigrations_PassesPreflight", func(t *testing.T) {
		ts, client, _ := setupPayoutServer(t)
		defer ts.Close()

		baseRepo := setupTestRepo(t)
		var closeCalls atomic.Int32

		mockFactory := func(ctx context.Context, databaseURL string) (db.Repository, error) {
			return &mockNonMigratorRepo{
				Repository: baseRepo,
				closeCalls: &closeCalls,
			}, nil
		}

		reportDir := filepath.Join(t.TempDir(), "reports-no-migrator-ro")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, nil, mockFactory)

		if code != ExitCodeSuccess {
			t.Fatalf("expected ExitCodeSuccess (%d) for read-only preflight, got: %d", ExitCodeSuccess, code)
		}

		rep := readJSONReport(t, reportDir)
		if rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=false for repository without RunMigrations")
		}
		if !rep.DatabaseReachable {
			t.Errorf("expected DatabaseReachable=true")
		}
		if rep.Outcome != "PASS" {
			t.Errorf("expected Outcome='PASS', got %q", rep.Outcome)
		}
	})

	t.Run("InjectedRepo_WithoutRunMigrations_ReportsUnverified", func(t *testing.T) {
		cleanEnv(t)
		ts, client, _ := setupPayoutServer(t)
		defer ts.Close()

		injectedMock := db.NewMockRepository()

		reportDir := filepath.Join(t.TempDir(), "reports-injected-unverified")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, injectedMock)

		if code != ExitCodeSuccess {
			t.Fatalf("expected ExitCodeSuccess (%d) for read-only preflight, got: %d", ExitCodeSuccess, code)
		}

		rep := readJSONReport(t, reportDir)
		if rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=false for injected mock repository")
		}
		if !rep.DatabaseReachable {
			t.Errorf("expected DatabaseReachable=true")
		}
	})

	t.Run("InjectedRepo_WithRunMigrations_ReportsVerified", func(t *testing.T) {
		cleanEnv(t)
		ts, client, _ := setupPayoutServer(t)
		defer ts.Close()

		baseRepo := setupTestRepo(t)
		var migrationCalls atomic.Int32
		injectedMigrator := &mockMigratorRepo{
			Repository:     baseRepo,
			migrationCalls: &migrationCalls,
		}

		reportDir := filepath.Join(t.TempDir(), "reports-injected-verified")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, injectedMigrator)

		if code != ExitCodeSuccess {
			t.Fatalf("expected ExitCodeSuccess (%d), got: %d", ExitCodeSuccess, code)
		}
		if migrationCalls.Load() != 1 {
			t.Fatalf("expected 1 migration call for injected migrator, got: %d", migrationCalls.Load())
		}

		rep := readJSONReport(t, reportDir)
		if !rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=true for injected migrator")
		}
	})

	t.Run("InjectedRepo_WithRunMigrations_Error_FailsInMutationMode", func(t *testing.T) {
		cleanEnv(t)
		ts, client, mutationCount := setupPayoutServer(t)
		defer ts.Close()

		baseRepo := setupTestRepo(t)
		var migrationCalls atomic.Int32
		injectedMigrator := &mockMigratorRepo{
			Repository:     baseRepo,
			migrationCalls: &migrationCalls,
			migrationErr:   errors.New("injected migration error"),
		}

		reportDir := filepath.Join(t.TempDir(), "reports-injected-fail")
		var stdout, stderr bytes.Buffer
		code := runWithDeps([]string{
			"-submit-payout",
			"-confirm-sandbox-only",
			"-recipient", "receiver@example.com",
			"-amount", "1.00",
			"-report-dir", reportDir,
		}, &stdout, &stderr, client, injectedMigrator)

		if code != ExitCodeFailure {
			t.Fatalf("expected ExitCodeFailure (%d), got: %d", ExitCodeFailure, code)
		}
		if migrationCalls.Load() != 1 {
			t.Fatalf("expected 1 migration call, got: %d", migrationCalls.Load())
		}
		if mutationCount.Load() != 0 {
			t.Fatalf("expected 0 payout mutations dispatched, got: %d", mutationCount.Load())
		}

		rep := readJSONReport(t, reportDir)
		if rep.DatabaseMigrations {
			t.Errorf("expected DatabaseMigrations=false")
		}
		if rep.Outcome != "FAIL" {
			t.Errorf("expected Outcome='FAIL', got %q", rep.Outcome)
		}
	})
}

