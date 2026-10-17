package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/policy"
)

func TestServer_Healthz(t *testing.T) {
	cfg := &config.Config{
		AppEnv:               "test",
		Port:                 "8080",
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout: 5 * time.Second,
	}

	srv := NewServer(cfg, policy.NewEngine(""), nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got: %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if data["status"] != "ok" {
		t.Errorf("expected status 'ok', got: %v", data["status"])
	}
}

func TestServer_Readyz(t *testing.T) {
	cfg := &config.Config{
		AppEnv:               "test",
		Port:                 "8080",
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout: 5 * time.Second,
	}

	srv := NewServer(cfg, policy.NewEngine(""), nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got: %d", resp.StatusCode)
	}
}

func TestServer_PolicyEvaluate(t *testing.T) {
	cfg := &config.Config{
		AppEnv:               "test",
		Port:                 "8080",
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout: 5 * time.Second,
	}

	srv := NewServer(cfg, policy.NewEngine(""), nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Approved payload
	reqBody := `{
		"claim_id": "c-001",
		"claimant_email": "user@example.com",
		"amount_cents": 10000,
		"currency": "USD",
		"risk_score": 10.0,
		"current_state": "AwaitingPolicyCheck"
	}`

	resp, err := http.Post(ts.URL+"/api/v1/policy/evaluate", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to post policy request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for approved claim, got: %d", resp.StatusCode)
	}

	var evalResp policy.EvaluationResponse
	if err := json.NewDecoder(resp.Body).Decode(&evalResp); err != nil {
		t.Fatalf("failed to decode policy response: %v", err)
	}

	if evalResp.Decision != policy.DecisionApproved {
		t.Errorf("expected DecisionApproved, got: %s", evalResp.Decision)
	}

	// 2. Denied payload (amount > $500 ceiling)
	badBody := `{
		"claim_id": "c-002",
		"claimant_email": "user@example.com",
		"amount_cents": 90000,
		"currency": "USD",
		"risk_score": 5.0,
		"current_state": "AwaitingPolicyCheck"
	}`

	respBad, err := http.Post(ts.URL+"/api/v1/policy/evaluate", "application/json", bytes.NewReader([]byte(badBody)))
	if err != nil {
		t.Fatalf("failed to post bad policy request: %v", err)
	}
	defer respBad.Body.Close()

	if respBad.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 Forbidden for denied claim, got: %d", respBad.StatusCode)
	}
}
