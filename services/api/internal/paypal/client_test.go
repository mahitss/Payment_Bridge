package paypal

import (
	"bytes"
	"context"
	"fmt"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
)

func TestNewClient_AcceptOfficialSandbox(t *testing.T) {
	validHosts := []string{
		"https://api-m.sandbox.paypal.com",
		"https://api-m.sandbox.paypal.com/",
		"  https://api-m.sandbox.paypal.com  ",
	}

	for _, host := range validHosts {
		cfg := &config.Config{
			AppEnv:        "development",
			PayPalEnv:     "sandbox",
			PayPalBaseURL: host,
		}
		c, err := NewClient(cfg, nil)
		if err != nil {
			t.Errorf("expected valid sandbox endpoint %q to pass, got: %v", host, err)
		}
		if c.baseURL != OfficialSandboxBaseURL {
			t.Errorf("expected normalized baseURL %q, got %q", OfficialSandboxBaseURL, c.baseURL)
		}
	}
}

func TestNewClient_RejectProductionHosts(t *testing.T) {
	badHosts := []string{
		"https://api.paypal.com",
		"https://api-m.paypal.com",
		"https://www.paypal.com",
	}

	for _, host := range badHosts {
		cfg := &config.Config{
			PayPalEnv:     "sandbox",
			PayPalBaseURL: host,
		}
		_, err := NewClient(cfg, nil)
		if err == nil {
			t.Errorf("expected error for host %s, got nil", host)
		}
		if !errors.Is(err, ErrProductionHostBlocked) {
			t.Errorf("expected ErrProductionHostBlocked for host %s, got: %v", host, err)
		}
	}
}

func TestNewClient_RejectUnsafeEndpoints(t *testing.T) {
	unsafe := []struct {
		url     string
		desc    string
		errType error
	}{
		{"", "empty URL", ErrInvalidSandboxEndpoint},
		{"https://attacker.com", "arbitrary HTTPS host", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com.attacker.com", "subdomain suffix attempt", ErrInvalidSandboxEndpoint},
		{"http://api-m.sandbox.paypal.com", "plain HTTP scheme", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com:8443", "unexpected port 8443", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com:8080", "unexpected port 8080", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com/v1/payments", "unexpected path", ErrInvalidSandboxEndpoint},
		{"https://user:pass@api-m.sandbox.paypal.com", "embedded credentials", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com?query=1", "query parameters", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com#fragment", "fragment identifier", ErrInvalidSandboxEndpoint},
	}

	for _, tc := range unsafe {
		cfg := &config.Config{
			PayPalEnv:     "sandbox",
			PayPalBaseURL: tc.url,
		}
		_, err := NewClient(cfg, nil)
		if err == nil {
			t.Errorf("expected error for %s (%q), got nil", tc.desc, tc.url)
		}
		if tc.errType != nil && !errors.Is(err, tc.errType) {
			t.Errorf("expected %v for %s, got: %v", tc.errType, tc.desc, err)
		}
	}
}

func TestNewClient_FailClosedBeforeDispatch(t *testing.T) {
	// Proves that invalid endpoint configuration fails during NewClient,
	// BEFORE any HTTP connection or credential-bearing request can be dispatched.
	cfg := &config.Config{
		PayPalEnv:          "sandbox",
		PayPalBaseURL:      "https://malicious-collector.com",
		PayPalClientID:     "SECRET_CLIENT_ID_NEVER_LEAK",
		PayPalClientSecret: "SECRET_PASSWORD_NEVER_LEAK",
	}

	client, err := NewClient(cfg, nil)
	if err == nil {
		t.Fatal("expected NewClient to fail immediately for non-sandbox URL, got nil")
	}
	if client != nil {
		t.Fatal("expected returned client to be nil on validation error")
	}
}

func TestNewClient_SimulatedEndpointForbiddenInProduction(t *testing.T) {
	cfg := &config.Config{
		AppEnv:        "production",
		PayPalEnv:     "sandbox",
		PayPalBaseURL: OfficialSandboxBaseURL,
	}

	_, err := NewTestClient(cfg, nil, "http://127.0.0.1:54321")
	if err == nil {
		t.Fatal("expected error when WithSimulatedBaseURL used in production, got nil")
	}
	if !errors.Is(err, ErrSimulatedEndpointForbidden) {
		t.Errorf("expected ErrSimulatedEndpointForbidden, got: %v", err)
	}
}

func TestNewClient_RejectNonSandboxEnv(t *testing.T) {
	cfg := &config.Config{
		PayPalEnv:     "production",
		PayPalBaseURL: "https://api-m.sandbox.paypal.com",
	}
	_, err := NewClient(cfg, nil)
	if err == nil {
		t.Fatal("expected error for non-sandbox env, got nil")
	}
	if !errors.Is(err, ErrSandboxAssertionFailed) {
		t.Errorf("expected ErrSandboxAssertionFailed, got: %v", err)
	}
}

func TestClient_GetAccessToken_SuccessAndCaching(t *testing.T) {
	var tokenCalls int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			atomic.AddInt32(&tokenCalls, 1)

			// Verify request headers and auth
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Basic ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "A21AA_TEST_SANDBOX_TOKEN_XYZ",
				TokenType:   "Bearer",
				AppID:       "APP-SANDBOX-TEST",
				ExpiresIn:   3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "sandbox_test_client_id",
		PayPalClientSecret:   "sandbox_test_client_secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// Call 1: Fetches fresh token
	token1, err := client.GetAccessToken(ctx)
	if err != nil {
		t.Fatalf("unexpected error fetching token: %v", err)
	}
	if token1 != "A21AA_TEST_SANDBOX_TOKEN_XYZ" {
		t.Errorf("unexpected token value: %s", token1)
	}

	// Call 2: Uses cached token
	token2, err := client.GetAccessToken(ctx)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if token2 != token1 {
		t.Errorf("cached token mismatch: %s vs %s", token2, token1)
	}

	if calls := atomic.LoadInt32(&tokenCalls); calls != 1 {
		t.Errorf("expected 1 token call to server due to caching, got: %d", calls)
	}
}

func TestClient_GetPayoutBatch_SuccessAndNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "A21AA_TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}

		if r.URL.Path == "/v1/payments/payouts/BATCH-12345" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(PayoutBatchResponse{
				BatchHeader: PayoutBatchHeader{
					PayoutBatchID: "BATCH-12345",
					BatchStatus:   "SUCCESS",
					TimeCreated:   "2026-10-17T12:00:00Z",
				},
			})
			return
		}

		if r.URL.Path == "/v1/payments/payouts/BATCH-NONEXISTENT" {
			http.Error(w, `{"name":"ITEM_NOT_FOUND"}`, http.StatusNotFound)
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// Test 1: Successful retrieval
	batch, err := client.GetPayoutBatch(ctx, "BATCH-12345")
	if err != nil {
		t.Fatalf("unexpected error getting batch: %v", err)
	}
	if batch.BatchHeader.PayoutBatchID != "BATCH-12345" {
		t.Errorf("unexpected batch id: %s", batch.BatchHeader.PayoutBatchID)
	}
	if batch.BatchHeader.BatchStatus != "SUCCESS" {
		t.Errorf("unexpected batch status: %s", batch.BatchHeader.BatchStatus)
	}

	// Test 2: Not Found returns ErrPayoutNotFound
	_, err = client.GetPayoutBatch(ctx, "BATCH-NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for non-existent batch, got nil")
	}
	if !errors.Is(err, ErrPayoutNotFound) {
		t.Errorf("expected ErrPayoutNotFound, got: %v", err)
	}
}

func TestClient_CreatePayout_AmbiguousTimeoutSafety(t *testing.T) {
	// Server hangs to simulate timeout during payout dispatch
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "A21AA_TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			// Sleep longer than context timeout
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = client.CreatePayout(ctx, CreatePayoutRequest{
		SenderBatchID: "claimbridge-claim-test-timeout",
		Recipient:     "receiver@example.com",
		Amount:        "25.00",
		Currency:      "USD",
	})

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// SAFETY ASSERTION: Must be classified as ErrAmbiguousNetworkTimeout so caller does NOT retry
	if !errors.Is(err, ErrAmbiguousNetworkTimeout) {
		t.Errorf("expected ErrAmbiguousNetworkTimeout, got: %v", err)
	}
}

func TestClient_CancelPayoutItem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}

		if r.URL.Path == "/v1/payments/payouts-item/ITEM-123/cancel" {
			if r.Method != http.MethodPost {
				http.Error(w, "invalid method", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(PayoutItemResponse{
				PayoutItemID:      "ITEM-123",
				TransactionStatus: "CANCELED",
				PayoutBatchID:     "BATCH-123",
				Amount: CurrencyAmount{
					Currency: "USD",
					Value:    "10.00",
				},
				Receiver: "recipient@example.com",
			})
			return
		}

		if r.URL.Path == "/v1/payments/payouts-item/ITEM-404/cancel" {
			http.NotFound(w, r)
			return
		}

		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// 1. Success cancel
	res, err := client.CancelPayoutItem(ctx, "ITEM-123")
	if err != nil {
		t.Fatalf("expected cancel to succeed, got: %v", err)
	}
	if res.TransactionStatus != "CANCELED" {
		t.Errorf("expected transaction_status CANCELED, got: %s", res.TransactionStatus)
	}

	// 2. 404 cancel
	_, err404 := client.CancelPayoutItem(ctx, "ITEM-404")
	if !errors.Is(err404, ErrPayoutNotFound) {
		t.Errorf("expected ErrPayoutNotFound, got: %v", err404)
	}
}

func TestClient_CheckConnection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "VERIFIED_SANDBOX_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	status := client.CheckConnection(context.Background())
	if !status.Connected {
		t.Errorf("expected Connected true, got false with error: %s", status.Error)
	}
	if status.Environment != "sandbox" {
		t.Errorf("expected Environment sandbox, got: %s", status.Environment)
	}
}

func TestClient_GetAccessToken_UnauthorizedAndForbidden(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if strings.Contains(string(decoded), "FORBIDDEN") {
			http.Error(w, `{"error":"forbidden","error_description":"payouts permission missing"}`, http.StatusForbidden)
			return
		}
		http.Error(w, `{"error":"invalid_client","error_description":"Client Authentication failed"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	// 1. Test 401
	cfg401 := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "bad_id",
		PayPalClientSecret:   "bad_secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client401, _ := NewTestClient(cfg401, ts.Client(), ts.URL)
	_, err401 := client401.GetAccessToken(context.Background())
	if !errors.Is(err401, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized on 401, got: %v", err401)
	}

	// 2. Test 403
	cfg403 := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "FORBIDDEN_USER",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client403, _ := NewTestClient(cfg403, ts.Client(), ts.URL)
	_, err403 := client403.GetAccessToken(context.Background())
	if !errors.Is(err403, ErrForbidden) {
		t.Errorf("expected ErrForbidden on 403, got: %v", err403)
	}
}

func TestClient_GetPayoutBatch_UnauthorizedAndForbidden(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/BATCH-401" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/payments/payouts/BATCH-403" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client, _ := NewTestClient(cfg, ts.Client(), ts.URL)
	ctx := context.Background()

	_, err401 := client.GetPayoutBatch(ctx, "BATCH-401")
	if !errors.Is(err401, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized on batch 401, got: %v", err401)
	}

	_, err403 := client.GetPayoutBatch(ctx, "BATCH-403")
	if !errors.Is(err403, ErrForbidden) {
		t.Errorf("expected ErrForbidden on batch 403, got: %v", err403)
	}
}

func TestClient_GetPayoutBatch_OfficialNestedResponse(t *testing.T) {
	// Tests official nested PayPal structure
	nestedJSON := `{
		"batch_header": {
			"payout_batch_id": "OFFICIAL-BATCH-001",
			"batch_status": "SUCCESS",
			"time_created": "2026-10-25T10:00:00Z",
			"sender_batch_header": {
				"sender_batch_id": "CB-BATCH-REF-12345",
				"email_subject": "Payout Notification"
			},
			"amount": {
				"currency": "USD",
				"value": "150.00"
			}
		},
		"items": [
			{
				"payout_item_id": "ITEM-OFFICIAL-001",
				"transaction_id": "TRANS-001",
				"transaction_status": "SUCCESS",
				"payout_item": {
					"sender_item_id": "CB-ITEM-REF-12345",
					"receiver": "payee@example.com",
					"recipient_type": "EMAIL",
					"note": "Claim Reimbursement",
					"amount": {
						"currency": "USD",
						"value": "150.00"
					}
				}
			}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts/OFFICIAL-BATCH-001" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(nestedJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	resp, err := client.GetPayoutBatch(context.Background(), "OFFICIAL-BATCH-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.BatchHeader.SenderBatchID != "CB-BATCH-REF-12345" {
		t.Errorf("expected SenderBatchID CB-BATCH-REF-12345, got %q", resp.BatchHeader.SenderBatchID)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	item := resp.Items[0]
	if item.SenderItemID != "CB-ITEM-REF-12345" {
		t.Errorf("expected item.SenderItemID CB-ITEM-REF-12345, got %q", item.SenderItemID)
	}
	if item.Receiver != "payee@example.com" {
		t.Errorf("expected item.Receiver payee@example.com, got %q", item.Receiver)
	}
	if item.Amount.Value != "150.00" || item.Amount.Currency != "USD" {
		t.Errorf("expected item.Amount 150.00 USD, got %v", item.Amount)
	}
	if item.TransactionStatus != "SUCCESS" {
		t.Errorf("expected item.TransactionStatus SUCCESS, got %q", item.TransactionStatus)
	}
}

func TestClient_GetPayoutItem_OfficialNestedResponse(t *testing.T) {
	nestedItemJSON := `{
		"payout_item_id": "ITEM-OFFICIAL-DETAIL-001",
		"transaction_id": "TRANS-DETAIL-001",
		"transaction_status": "UNCLAIMED",
		"payout_batch_id": "BATCH-001",
		"payout_item": {
			"sender_item_id": "CB-ITEM-DETAIL-999",
			"receiver": "claimant@example.com",
			"recipient_type": "EMAIL",
			"note": "Detail Note",
			"amount": {
				"currency": "USD",
				"value": "75.00"
			}
		}
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "TEST_TOKEN",
				TokenType:   "Bearer",
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts-item/ITEM-OFFICIAL-DETAIL-001" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(nestedItemJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	itemResp, err := client.GetPayoutItem(context.Background(), "ITEM-OFFICIAL-DETAIL-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if itemResp.SenderItemID != "CB-ITEM-DETAIL-999" {
		t.Errorf("expected SenderItemID CB-ITEM-DETAIL-999, got %q", itemResp.SenderItemID)
	}
	if itemResp.Receiver != "claimant@example.com" {
		t.Errorf("expected Receiver claimant@example.com, got %q", itemResp.Receiver)
	}
	if itemResp.Amount.Value != "75.00" {
		t.Errorf("expected Amount value 75.00, got %q", itemResp.Amount.Value)
	}
	if itemResp.TransactionStatus != "UNCLAIMED" {
		t.Errorf("expected TransactionStatus UNCLAIMED, got %q", itemResp.TransactionStatus)
	}
}

func TestClient_CreatePayout_SendsSenderItemID(t *testing.T) {
	var capturedBody map[string]interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "TEST_TOKEN",
				TokenType:   "Bearer",
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "BATCH-NEW-123",
					"batch_status":    "PENDING",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	req := CreatePayoutRequest{
		SenderBatchID: "CB-BATCH-TEST-001",
		SenderItemID:  "CB-ITEM-TEST-001",
		Recipient:     "user@example.com",
		Amount:        "25.00",
		Currency:      "USD",
		Note:          "Test Note",
		EmailSubject:  "Test Subject",
	}

	resp, err := client.CreatePayout(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.BatchHeader.PayoutBatchID != "BATCH-NEW-123" {
		t.Errorf("expected batch ID BATCH-NEW-123, got %q", resp.BatchHeader.PayoutBatchID)
	}

	// Verify captured body has items[0].sender_item_id
	itemsRaw, ok := capturedBody["items"].([]interface{})
	if !ok || len(itemsRaw) == 0 {
		t.Fatalf("expected items array in payload, got %v", capturedBody)
	}
	firstItem, ok := itemsRaw[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected item map, got %v", itemsRaw[0])
	}
	if firstItem["sender_item_id"] != "CB-ITEM-TEST-001" {
		t.Errorf("expected sender_item_id CB-ITEM-TEST-001, got %v", firstItem["sender_item_id"])
	}
}

func TestExtractAndValidateBatchIDFromHATEOAS_Valid(t *testing.T) {
	baseURL := "https://api-m.sandbox.paypal.com"
	links := []LinkDescription{
		{
			Href:   "https://api-m.sandbox.paypal.com/v1/payments/payouts/RECOVERED-BATCH-999",
			Rel:    "payouts",
			Method: "GET",
		},
	}

	batchID, err := ExtractAndValidateBatchIDFromHATEOAS(links, baseURL)
	if err != nil {
		t.Fatalf("unexpected error extracting batch id: %v", err)
	}
	if batchID != "RECOVERED-BATCH-999" {
		t.Errorf("expected batch ID RECOVERED-BATCH-999, got %q", batchID)
	}
}

func TestExtractAndValidateBatchIDFromHATEOAS_UntrustedHost(t *testing.T) {
	baseURL := "https://api-m.sandbox.paypal.com"
	links := []LinkDescription{
		{
			Href:   "https://attacker-steals-tokens.com/v1/payments/payouts/RECOVERED-BATCH-999",
			Rel:    "payouts",
			Method: "GET",
		},
	}

	_, err := ExtractAndValidateBatchIDFromHATEOAS(links, baseURL)
	if err == nil {
		t.Fatal("expected error for untrusted HATEOAS host, got nil")
	}
	if !errors.Is(err, ErrUntrustedHATEOASHost) {
		t.Errorf("expected ErrUntrustedHATEOASHost, got: %v", err)
	}
}

func TestExtractAndValidateBatchIDFromHATEOAS_UntrustedScheme(t *testing.T) {
	baseURL := "https://api-m.sandbox.paypal.com"
	links := []LinkDescription{
		{
			Href:   "http://api-m.sandbox.paypal.com/v1/payments/payouts/RECOVERED-BATCH-999",
			Rel:    "payouts",
			Method: "GET",
		},
	}

	_, err := ExtractAndValidateBatchIDFromHATEOAS(links, baseURL)
	if err == nil {
		t.Fatal("expected error for insecure HTTP scheme, got nil")
	}
	if !errors.Is(err, ErrUntrustedHATEOASHost) {
		t.Errorf("expected ErrUntrustedHATEOASHost, got: %v", err)
	}
}

func TestExtractAndValidateBatchIDFromHATEOAS_MalformedPath(t *testing.T) {
	baseURL := "https://api-m.sandbox.paypal.com"
	badLinks := [][]LinkDescription{
		{{Href: "https://api-m.sandbox.paypal.com/v1/other/service/123", Rel: "self"}},
		{{Href: "https://api-m.sandbox.paypal.com/v1/payments/payouts/../traversal", Rel: "payouts"}},
		{{Href: "https://api-m.sandbox.paypal.com/v1/payments/payouts/", Rel: "payouts"}},
	}

	for _, links := range badLinks {
		_, err := ExtractAndValidateBatchIDFromHATEOAS(links, baseURL)
		if err == nil {
			t.Errorf("expected error for malformed link %v, got nil", links)
		}
		if !errors.Is(err, ErrMalformedHATEOASLink) {
			t.Errorf("expected ErrMalformedHATEOASLink, got: %v", err)
		}
	}
}

func TestClient_CreatePayout_DuplicateResponseWithHATEOAS(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "A21AA_TEST_TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(APIErrorResponse{
				Name:    "DUPLICATE_REQUEST",
				Message: "A payout request with the same sender_batch_id was already submitted.",
				Links: []LinkDescription{
					{
						Href:   ts.URL + "/v1/payments/payouts/ORIGINAL-BATCH-XYZ-789",
						Rel:    "payouts",
						Method: "GET",
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "id",
		PayPalClientSecret:   "secret",
		PayPalRequestTimeout: 5 * time.Second,
	}
	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req := CreatePayoutRequest{
		SenderBatchID: "CB-BATCH-DUPLICATE",
		SenderItemID:  "CB-ITEM-DUPLICATE",
		Recipient:     "user@example.com",
		Amount:        "50.00",
		Currency:      "USD",
		Note:          "Note",
		EmailSubject:  "Subject",
	}

	_, err = client.CreatePayout(context.Background(), req)
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}

	var dupErr *DuplicatePayoutError
	if !errors.As(err, &dupErr) {
		t.Fatalf("expected *DuplicatePayoutError, got %T: %v", err, err)
	}

	if dupErr.OriginalBatchID != "ORIGINAL-BATCH-XYZ-789" {
		t.Errorf("expected original batch ID ORIGINAL-BATCH-XYZ-789, got %q", dupErr.OriginalBatchID)
	}
}


func TestHasPayoutsScope(t *testing.T) {
	tests := []struct {
		name     string
		scopeStr string
		expected bool
	}{
		{
			name:     "exact single scope",
			scopeStr: "https://uri.paypal.com/payments/payouts",
			expected: true,
		},
		{
			name:     "multiple scopes including payouts",
			scopeStr: "openid https://uri.paypal.com/payments/payouts https://api.paypal.com/v1/vault/credit-card",
			expected: true,
		},
		{
			name:     "tab and newline delimited",
			scopeStr: "openid\t\nhttps://uri.paypal.com/payments/payouts\nhttps://api.paypal.com",
			expected: true,
		},
		{
			name:     "prefix match must not match",
			scopeStr: "https://uri.paypal.com/payments/payouts/extra",
			expected: false,
		},
		{
			name:     "suffix match must not match",
			scopeStr: "myhttps://uri.paypal.com/payments/payouts",
			expected: false,
		},
		{
			name:     "empty scope",
			scopeStr: "",
			expected: false,
		},
		{
			name:     "other scopes only",
			scopeStr: "openid profile email https://api.paypal.com/v1/payments/.*",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := HasPayoutsScope(tc.scopeStr)
			if actual != tc.expected {
				t.Errorf("HasPayoutsScope(%q) = %t, expected %t", tc.scopeStr, actual, tc.expected)
			}
		})
	}
}

func TestClient_GetAccessToken_PayoutsScopeDetection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(OAuthTokenResponse{
			AccessToken: "TOKEN-WITH-PAYOUTS",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			AppID:       "APP-SCOPE-TEST",
			Scope:       "openid https://uri.paypal.com/payments/payouts email",
		})
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "client_id",
		PayPalClientSecret:   "client_secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	token, err := client.GetAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	if token != "TOKEN-WITH-PAYOUTS" {
		t.Errorf("unexpected token: %s", token)
	}
	if !client.HasPayoutsScope() {
		t.Errorf("expected client.HasPayoutsScope() to be true")
	}
}

func TestAPIForbiddenError_StructuredDiagnostics(t *testing.T) {
	body := []byte(`{
		"name": "NOT_AUTHORIZED",
		"message": "Authorization error",
		"debug_id": "test-debug-999",
		"details": [
			{"issue": "PERMISSION_DENIED"}
		]
	}`)

	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Paypal-Debug-Id": []string{"header-debug-id"}},
		Request:    req,
	}

	err := parseForbiddenError(resp, body)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected errors.Is(err, ErrForbidden) to be true")
	}

	fe, ok := AsAPIForbiddenError(err)
	if !ok {
		t.Fatalf("expected AsAPIForbiddenError to return true")
	}

	if fe.Name != "NOT_AUTHORIZED" {
		t.Errorf("expected Name NOT_AUTHORIZED, got: %s", fe.Name)
	}
	if fe.Issue != "PERMISSION_DENIED" {
		t.Errorf("expected Issue PERMISSION_DENIED, got: %s", fe.Issue)
	}
	if fe.DebugID != "test-debug-999" {
		t.Errorf("expected DebugID test-debug-999, got: %s", fe.DebugID)
	}
	if !strings.Contains(fe.Error(), "name=NOT_AUTHORIZED") ||
		!strings.Contains(fe.Error(), "issue=PERMISSION_DENIED") ||
		!strings.Contains(fe.Error(), "debug_id=test-debug-999") {
		t.Errorf("unexpected fe.Error() string: %s", fe.Error())
	}
}

func TestClient_CreatePayout_HTTP403StructuredDiagnostics(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OAuthTokenResponse{
				AccessToken: "TEST-TOKEN",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				AppID:       "APP-TEST",
				Scope:       "openid",
			})
			return
		}
		if r.URL.Path == "/v1/payments/payouts" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Paypal-Debug-Id", "hdr-dbg-888")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{
				"name": "INSUFFICIENT_PERMISSIONS",
				"message": "User not authorized for payouts",
				"debug_id": "body-dbg-888",
				"details": [{"issue": "PAYOUTS_NOT_ENABLED"}]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := &config.Config{
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        ts.URL,
		PayPalClientID:       "client_id",
		PayPalClientSecret:   "client_secret",
		PayPalRequestTimeout: 5 * time.Second,
	}

	client, err := NewTestClient(cfg, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	req := CreatePayoutRequest{
		SenderBatchID: "BATCH-403-TEST",
		SenderItemID:  "ITEM-403-TEST",
		Recipient:     "test@example.com",
		Amount:        "1.00",
		Currency:      "USD",
	}

	_, pErr := client.CreatePayout(context.Background(), req)
	if pErr == nil {
		t.Fatal("expected CreatePayout to return error, got nil")
	}

	if !errors.Is(pErr, ErrForbidden) {
		t.Fatalf("expected errors.Is(pErr, ErrForbidden) to be true, got: %v", pErr)
	}

	fe, ok := AsAPIForbiddenError(pErr)
	if !ok {
		t.Fatalf("expected AsAPIForbiddenError to be true, got %T: %v", pErr, pErr)
	}

	if fe.Name != "INSUFFICIENT_PERMISSIONS" {
		t.Errorf("expected Name INSUFFICIENT_PERMISSIONS, got: %s", fe.Name)
	}
	if fe.Issue != "PAYOUTS_NOT_ENABLED" {
		t.Errorf("expected Issue PAYOUTS_NOT_ENABLED, got: %s", fe.Issue)
	}
	if fe.DebugID != "body-dbg-888" {
		t.Errorf("expected DebugID body-dbg-888, got: %s", fe.DebugID)
	}
}


func TestSanitizeDiagnosticField_ControlCharsAndLength(t *testing.T) {
	// Long string with control characters and newlines
	longInput := "  MALICIOUS_ERROR_NAME\r\n\t[INJECTION]\x00\x1b[31m" + strings.Repeat("A", 200)
	sanitized := sanitizeDiagnosticField(longInput, MaxDiagnosticFieldLength)

	if len(sanitized) > MaxDiagnosticFieldLength {
		t.Errorf("expected length <= %d, got %d", MaxDiagnosticFieldLength, len(sanitized))
	}
	if strings.ContainsAny(sanitized, "\r\n\t\x00\x1b") {
		t.Errorf("sanitized string contains control characters: %q", sanitized)
	}
}

func TestParseForbiddenError_MalformedAndOversized(t *testing.T) {
	// 1. Malformed JSON
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Paypal-Debug-Id": []string{"debug-header-only"}},
		Request:    req,
	}
	malformedBody := []byte(`{not valid json at all...`)
	err := parseForbiddenError(resp, malformedBody)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected errors.Is(err, ErrForbidden) on malformed body, got: %v", err)
	}
	fe, ok := AsAPIForbiddenError(err)
	if !ok {
		t.Fatalf("expected AsAPIForbiddenError to succeed from header debug id")
	}
	if fe.DebugID != "debug-header-only" {
		t.Errorf("expected DebugID from header, got: %s", fe.DebugID)
	}

	// 2. Completely empty body and headers
	emptyResp := &http.Response{
		StatusCode: http.StatusForbidden,
		Request:    req,
	}
	errEmpty := parseForbiddenError(emptyResp, nil)
	if errEmpty != ErrForbidden {
		t.Errorf("expected base ErrForbidden when body and headers are empty, got: %v", errEmpty)
	}

	// 3. Oversized fields in JSON
	hugeIssue := strings.Repeat("ISSUE_", 50)
	hugeName := strings.Repeat("NAME_", 50)
	oversizedBody := []byte(fmt.Sprintf(`{
		"name": %q,
		"details": [{"issue": %q}]
	}`, hugeName, hugeIssue))

	errOver := parseForbiddenError(resp, oversizedBody)
	feOver, ok := AsAPIForbiddenError(errOver)
	if !ok {
		t.Fatalf("expected AsAPIForbiddenError to succeed")
	}
	if len(feOver.Name) > MaxDiagnosticFieldLength {
		t.Errorf("expected Name length <= %d, got %d", MaxDiagnosticFieldLength, len(feOver.Name))
	}
	if len(feOver.Issue) > MaxDiagnosticFieldLength {
		t.Errorf("expected Issue length <= %d, got %d", MaxDiagnosticFieldLength, len(feOver.Issue))
	}
}

func TestReadResponseBody_StrictLimits(t *testing.T) {
	t.Run("nil reader returns nil", func(t *testing.T) {
		data, err := readResponseBody(nil)
		if err != nil {
			t.Fatalf("expected nil error for nil reader, got: %v", err)
		}
		if data != nil {
			t.Fatalf("expected nil data for nil reader, got: %v", data)
		}
	})

	t.Run("exact limit response is accepted", func(t *testing.T) {
		exactData := bytes.Repeat([]byte("A"), MaxResponseBodyBytes)
		reader := bytes.NewReader(exactData)

		data, err := readResponseBody(reader)
		if err != nil {
			t.Fatalf("expected success for exact limit (%d bytes), got error: %v", MaxResponseBodyBytes, err)
		}
		if len(data) != MaxResponseBodyBytes {
			t.Fatalf("expected %d bytes, got %d", MaxResponseBodyBytes, len(data))
		}
		if !bytes.Equal(data, exactData) {
			t.Fatal("data read does not match original exact data")
		}
	})

	t.Run("one byte over limit is rejected with typed error", func(t *testing.T) {
		oneByteOver := bytes.Repeat([]byte("B"), MaxResponseBodyBytes+1)
		reader := bytes.NewReader(oneByteOver)

		data, err := readResponseBody(reader)
		if err == nil {
			t.Fatal("expected error for response 1 byte over limit, got nil")
		}
		if data != nil {
			t.Fatalf("expected nil data on oversized error, got %d bytes", len(data))
		}
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("expected errors.Is(err, ErrOversizedResponse), got: %v", err)
		}
		var ore *OversizedResponseError
		if !errors.As(err, &ore) {
			t.Fatalf("expected error to be *OversizedResponseError, got: %T", err)
		}
		if ore.Limit != MaxResponseBodyBytes {
			t.Fatalf("expected Limit %d, got %d", MaxResponseBodyBytes, ore.Limit)
		}
	})

	t.Run("large response with valid JSON prefix is rejected without leak", func(t *testing.T) {
		sensitiveSecret := "SECRET_TOKEN_DO_NOT_LEAK_99999"
		prefix := fmt.Sprintf(`{"name":"NOT_AUTHORIZED","token":%q,"message":"forbidden"}`, sensitiveSecret)
		// Pad beyond limit
		padding := strings.Repeat(" ", MaxResponseBodyBytes+500)
		oversizedPayload := []byte(prefix + padding)

		reader := bytes.NewReader(oversizedPayload)
		data, err := readResponseBody(reader)
		if err == nil {
			t.Fatal("expected error for oversized response starting with JSON, got nil")
		}
		if data != nil {
			t.Fatalf("expected nil data on oversized error, got %d bytes", len(data))
		}
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("expected errors.Is(err, ErrOversizedResponse), got: %v", err)
		}

		// Ensure error string does not leak the sensitive secret, token, or JSON content
		errMsg := err.Error()
		if strings.Contains(errMsg, sensitiveSecret) {
			t.Fatalf("security violation: sensitive secret leaked in error string: %s", errMsg)
		}
		if strings.Contains(errMsg, "NOT_AUTHORIZED") {
			t.Fatalf("provider payload leaked in error string: %s", errMsg)
		}
		if strings.Contains(errMsg, "forbidden") {
			t.Fatalf("provider payload leaked in error string: %s", errMsg)
		}
	})

	t.Run("underlying io error is propagated", func(t *testing.T) {
		expectedErr := errors.New("simulated network read failure")
		errReader := &faultyReader{err: expectedErr}

		data, err := readResponseBody(errReader)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if data != nil {
			t.Fatalf("expected nil data on io error, got %d bytes", len(data))
		}
		if errors.Is(err, ErrOversizedResponse) {
			t.Fatal("io error should not be classified as ErrOversizedResponse")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected underlying error to be preserved, got: %v", err)
		}
	})
}

type faultyReader struct {
	err error
}

func (r *faultyReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

func TestClient_OversizedResponse_Endpoints(t *testing.T) {
	t.Run("GetAccessToken rejects oversized response and preserves no leak", func(t *testing.T) {
		sensitiveSecret := "LEAK_SENSITIVE_OAUTH_TOKEN_VALUE_42"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Send valid JSON start with secret, padded beyond MaxResponseBodyBytes
			w.Write([]byte(fmt.Sprintf(`{"access_token":%q,"token_type":"Bearer","expires_in":3600,"scope":"payouts",`, sensitiveSecret)))
			padding := make([]byte, MaxResponseBodyBytes+500)
			for i := range padding {
				padding[i] = ' '
			}
			w.Write(padding)
			w.Write([]byte(`"end":true}`))
		}))
		defer server.Close()

		cfg := &config.Config{
			AppEnv:             "development",
			PayPalEnv:          "sandbox",
			PayPalBaseURL:      server.URL,
			PayPalClientID:     "client123",
			PayPalClientSecret: "secret456",
		}
		client, err := NewTestClient(cfg, server.Client(), server.URL)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		token, err := client.GetAccessToken(context.Background())
		if err == nil {
			t.Fatal("expected GetAccessToken to fail on oversized response, got nil error")
		}
		if token != "" {
			t.Fatalf("expected empty token on error, got %q", token)
		}
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("expected errors.Is(err, ErrOversizedResponse), got: %v", err)
		}
		if strings.Contains(err.Error(), sensitiveSecret) {
			t.Fatalf("security violation: secret leaked in error string: %s", err.Error())
		}
	})

	t.Run("GetAccessToken rejects oversized HTTP 403 without parsing body", func(t *testing.T) {
		sensitiveMessage := "SECRET_INTERNAL_403_MESSAGE"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Paypal-Debug-Id", "dbg-403-hdr")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(fmt.Sprintf(`{"name":"NOT_AUTHORIZED","message":%q,"details":[{"issue":"RESTRICTED"}]}`, sensitiveMessage)))
			padding := make([]byte, MaxResponseBodyBytes+100)
			for i := range padding {
				padding[i] = ' '
			}
			w.Write(padding)
		}))
		defer server.Close()

		cfg := &config.Config{
			AppEnv:             "development",
			PayPalEnv:          "sandbox",
			PayPalBaseURL:      server.URL,
			PayPalClientID:     "client123",
			PayPalClientSecret: "secret456",
		}
		client, err := NewTestClient(cfg, server.Client(), server.URL)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.GetAccessToken(context.Background())
		if err == nil {
			t.Fatal("expected error on oversized 403 response, got nil")
		}
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("expected errors.Is(err, ErrOversizedResponse), got: %v", err)
		}
		if strings.Contains(err.Error(), sensitiveMessage) {
			t.Fatalf("security violation: message leaked in error string: %s", err.Error())
		}
		// Oversized body was aborted before parseForbiddenError, so ErrForbidden is NOT wrapped
		if errors.Is(err, ErrForbidden) {
			t.Fatalf("oversized body should fail with ErrOversizedResponse before reaching 403 parser")
		}
	})

	t.Run("GetPayoutBatch rejects oversized response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/oauth2/token" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(OAuthTokenResponse{
					AccessToken: "valid-token",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
					Scope:       PayPalPayoutsScope,
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			padding := make([]byte, MaxResponseBodyBytes+1000)
			for i := range padding {
				padding[i] = 'X'
			}
			w.Write(padding)
		}))
		defer server.Close()

		cfg := &config.Config{
			AppEnv:             "development",
			PayPalEnv:          "sandbox",
			PayPalBaseURL:      server.URL,
			PayPalClientID:     "client123",
			PayPalClientSecret: "secret456",
		}
		client, err := NewTestClient(cfg, server.Client(), server.URL)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		batch, err := client.GetPayoutBatch(context.Background(), "batch-123")
		if err == nil {
			t.Fatal("expected error on oversized batch response, got nil")
		}
		if batch != nil {
			t.Fatalf("expected nil batch on error, got %v", batch)
		}
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("expected errors.Is(err, ErrOversizedResponse), got: %v", err)
		}
	})

	t.Run("CreatePayout marks oversized response with ErrAmbiguousNetworkTimeout and ErrOversizedResponse", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/oauth2/token" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(OAuthTokenResponse{
					AccessToken: "valid-token",
					TokenType:   "Bearer",
					ExpiresIn:   3600,
					Scope:       PayPalPayoutsScope,
				})
				return
			}
			w.WriteHeader(http.StatusCreated)
			padding := make([]byte, MaxResponseBodyBytes+500)
			for i := range padding {
				padding[i] = 'Y'
			}
			w.Write(padding)
		}))
		defer server.Close()

		cfg := &config.Config{
			AppEnv:             "development",
			PayPalEnv:          "sandbox",
			PayPalBaseURL:      server.URL,
			PayPalClientID:     "client123",
			PayPalClientSecret: "secret456",
		}
		client, err := NewTestClient(cfg, server.Client(), server.URL)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.CreatePayout(context.Background(), CreatePayoutRequest{
			SenderBatchID: "sb-123",
			SenderItemID:  "item-123",
			Recipient:     "recipient@example.com",
			Amount:        "1.00",
			Currency:      "USD",
		})
		if err == nil {
			t.Fatal("expected CreatePayout to fail on oversized response, got nil error")
		}
		if resp != nil {
			t.Fatalf("expected nil response on error, got: %v", resp)
		}
		if !errors.Is(err, ErrAmbiguousNetworkTimeout) {
			t.Fatalf("expected errors.Is(err, ErrAmbiguousNetworkTimeout), got: %v", err)
		}
		if !errors.Is(err, ErrOversizedResponse) {
			t.Fatalf("expected errors.Is(err, ErrOversizedResponse), got: %v", err)
		}
	})

	t.Run("Exact limit response with valid HTTP 403 parses correctly", func(t *testing.T) {
		exactHeader := `{"name":"NOT_AUTHORIZED","details":[{"issue":"PAYOUTS_NOT_ELIGIBLE"}]`
		padPrefix := `,"pad":"`
		padSuffix := `"}`
		neededPad := MaxResponseBodyBytes - len(exactHeader) - len(padPrefix) - len(padSuffix)
		if neededPad < 0 {
			t.Fatalf("exactHeader is already longer than MaxResponseBodyBytes")
		}
		payload := []byte(exactHeader + padPrefix + strings.Repeat("x", neededPad) + padSuffix)
		if len(payload) != MaxResponseBodyBytes {
			t.Fatalf("expected exact payload length %d, got %d", MaxResponseBodyBytes, len(payload))
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Paypal-Debug-Id", "dbg-exact-limit")
			w.WriteHeader(http.StatusForbidden)
			w.Write(payload)
		}))
		defer server.Close()

		cfg := &config.Config{
			AppEnv:             "development",
			PayPalEnv:          "sandbox",
			PayPalBaseURL:      server.URL,
			PayPalClientID:     "client123",
			PayPalClientSecret: "secret456",
		}
		client, err := NewTestClient(cfg, server.Client(), server.URL)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.GetAccessToken(context.Background())
		if err == nil {
			t.Fatal("expected error on 403 exact limit response, got nil")
		}
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected errors.Is(err, ErrForbidden), got: %v", err)
		}
		fe, ok := AsAPIForbiddenError(err)
		if !ok {
			t.Fatalf("expected AsAPIForbiddenError(err) to succeed, got: %T (%v)", err, err)
		}
		if fe.Name != "NOT_AUTHORIZED" {
			t.Errorf("expected fe.Name 'NOT_AUTHORIZED', got %q", fe.Name)
		}
		if fe.Issue != "PAYOUTS_NOT_ELIGIBLE" {
			t.Errorf("expected fe.Issue 'PAYOUTS_NOT_ELIGIBLE', got %q", fe.Issue)
		}
		if fe.DebugID != "dbg-exact-limit" {
			t.Errorf("expected fe.DebugID 'dbg-exact-limit', got %q", fe.DebugID)
		}
	})
}
