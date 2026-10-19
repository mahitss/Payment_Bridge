package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/paypal"
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

func TestRun_MissingCredentialsBlocked(t *testing.T) {
	cleanEnv(t)
	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Unsetenv("PAYPAL_CLIENT_ID")
	os.Unsetenv("PAYPAL_CLIENT_SECRET")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-env-file", "nonexistent.env"}, &stdout, &stderr)

	if code != 3 {
		t.Fatalf("expected exit code 3 (BLOCKED) for missing credentials, got: %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "[STATUS: BLOCKED]") {
		t.Errorf("expected output to contain '[STATUS: BLOCKED]', got:\n%s", out)
	}
	if strings.Contains(out, "FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY") {
		t.Errorf("blocked run should never print success banner")
	}
}

func TestRun_UnsafeEndpointRejected(t *testing.T) {
	cleanEnv(t)
	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_secret")
	os.Setenv("PAYPAL_SANDBOX_BASE_URL", "http://insecure-http.com")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-env-file", "nonexistent.env"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for unsafe endpoint, got: %d", code)
	}
	if !strings.Contains(stderr.String(), "Configuration failed security validation") {
		t.Errorf("expected security validation error in stderr, got:\n%s", stderr.String())
	}
}

func TestRun_OAuthFailure(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
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
	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runWithClient([]string{"-env-file", "nonexistent.env"}, &stdout, &stderr, testClient)

	if code != 2 {
		t.Fatalf("expected exit code 2 (FAILED) on OAuth failure, got: %d (stderr: %s)", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "[STATUS: FAILED]") {
		t.Errorf("expected output to contain '[STATUS: FAILED]', got:\n%s", out)
	}
	if strings.Contains(out, "FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY") {
		t.Errorf("OAuth failure should never conclude with success banner")
	}
}

func TestRun_LookupFailureReturnsNonZero(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "TEST_SECRET_TOKEN_12345",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/BATCH-MISSING" {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_client_id",
		PayPalClientSecret:   "test_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}
	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runWithClient([]string{"-env-file", "nonexistent.env", "-batch-id", "BATCH-MISSING"}, &stdout, &stderr, testClient)

	if code != 1 {
		t.Fatalf("expected exit code 1 on failed batch lookup, got: %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "[STATUS: FAILED]") {
		t.Errorf("expected output to contain '[STATUS: FAILED]', got:\n%s", out)
	}
	if strings.Contains(out, "FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY") {
		t.Errorf("failed lookup must never conclude with success banner")
	}
}

func TestRun_OAuthSuccessReadOnly(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "TEST_SECRET_TOKEN_56789",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_client_id",
		PayPalClientSecret:   "test_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}
	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runWithClient([]string{"-env-file", "nonexistent.env"}, &stdout, &stderr, testClient)

	if code != 0 {
		t.Fatalf("expected exit code 0 on successful OAuth verification, got: %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "[SUCCESS] OAuth2 Authentication Verified!") {
		t.Errorf("expected success message in output, got:\n%s", out)
	}
	if !strings.Contains(out, "FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY") {
		t.Errorf("expected final success banner in output")
	}
	if strings.Contains(out, "TEST_SECRET_TOKEN_56789") {
		t.Errorf("sensitive access token was leaked into output stdout:\n%s", out)
	}
}

func TestRun_SuccessfulBatchLookup(t *testing.T) {
	cleanEnv(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "TEST_TOKEN_111",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/BATCH-EXISTING" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paypal.PayoutBatchResponse{
				BatchHeader: paypal.PayoutBatchHeader{
					PayoutBatchID: "BATCH-EXISTING",
					BatchStatus:   "SUCCESS",
					TimeCreated:   "2026-10-18T12:00:00Z",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_client_id",
		PayPalClientSecret:   "test_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}
	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runWithClient([]string{"-env-file", "nonexistent.env", "-batch-id", "BATCH-EXISTING"}, &stdout, &stderr, testClient)

	if code != 0 {
		t.Fatalf("expected exit code 0 on successful batch lookup, got: %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "BATCH-EXISTING") {
		t.Errorf("expected batch details in output, got:\n%s", out)
	}
	if !strings.Contains(out, "FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY") {
		t.Errorf("expected success banner in output")
	}
}

func TestRun_UnrequestedPayoutNeverExecutes(t *testing.T) {
	cleanEnv(t)
	var payoutPostCalls int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" && r.Method == http.MethodPost {
			atomic.AddInt32(&payoutPostCalls, 1)
			w.WriteHeader(http.StatusCreated)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	os.Setenv("PAYPAL_ENV", "sandbox")
	os.Setenv("PAYPAL_CLIENT_ID", "test_client_id")
	os.Setenv("PAYPAL_CLIENT_SECRET", "test_client_secret")

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "test_client_id",
		PayPalClientSecret:   "test_client_secret",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}
	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	// Run without payout flag
	code := runWithClient([]string{"-env-file", "nonexistent.env"}, &stdout, &stderr, testClient)
	if code != 0 {
		t.Fatalf("expected exit code 0, got: %d", code)
	}

	if calls := atomic.LoadInt32(&payoutPostCalls); calls != 0 {
		t.Fatalf("SECURITY VIOLATION: unrequested payout executed %d times", calls)
	}
}

func TestRun_EnvFileResolution(t *testing.T) {
	cleanEnv(t)
	tempDir := t.TempDir()
	envPath := tempDir + "/test_custom.env"
	content := "PAYPAL_ENV=sandbox\nPAYPAL_CLIENT_ID=custom_client_from_file\nPAYPAL_CLIENT_SECRET=custom_secret_from_file\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed writing temp env file: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paypal.OAuthTokenResponse{
				AccessToken: "FILE_RESOLVED_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalClientID:       "custom_client_from_file",
		PayPalClientSecret:   "custom_secret_from_file",
		PayPalBaseURL:        ts.URL,
		PayPalRequestTimeout: 5 * time.Second,
	}
	testClient, err := paypal.NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed creating test client: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runWithClient([]string{"-env-file", envPath}, &stdout, &stderr, testClient)

	if code != 0 {
		t.Fatalf("expected exit code 0 with resolved env file, got: %d with output:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, envPath) {
		t.Errorf("expected loaded path in output, got:\n%s", out)
	}
	if !strings.Contains(out, "FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY") {
		t.Errorf("expected success banner")
	}
}
