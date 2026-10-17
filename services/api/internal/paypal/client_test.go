package paypal

import (
	"context"
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

	client, err := NewClient(cfg, ts.Client())
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

	client, err := NewClient(cfg, ts.Client())
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

	client, err := NewClient(cfg, ts.Client())
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

	client, err := NewClient(cfg, ts.Client())
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

	client, err := NewClient(cfg, ts.Client())
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

