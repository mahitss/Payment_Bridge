package paypal

import (
	"context"
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
