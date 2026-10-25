package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	if !strings.Contains(out, "Payout completed and settled successfully in PayPal Sandbox") {
		t.Errorf("expected success settlement in output, got:\n%s", out)
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
	if !strings.Contains(out, "Item Status: UNCLAIMED") {
		t.Errorf("expected output to clearly state Item Status: UNCLAIMED, got:\n%s", out)
	}
	if strings.Contains(out, "Settlement Completed") {
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
