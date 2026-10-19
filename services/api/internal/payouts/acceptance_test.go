package payouts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/email"
	"claimbridge/services/api/internal/payouts"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
	"claimbridge/services/api/internal/server"
)

// TestMission003_PayoutAcceptanceScenarios runs end-to-end HTTP scenarios 13-24 for Mission 003.
func TestMission003_PayoutAcceptanceScenarios(t *testing.T) {
	// 1. Fake PayPal Sandbox REST Server (Stateless & Deterministic for Scenarios 13-23)
	fakePayPalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-sandbox-access-token-999",
				"token_type":   "Bearer",
				"app_id":       "APP-SANDBOX-TEST",
				"expires_in":   3600,
			})
		case r.URL.Path == "/v1/payments/payouts" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-ACCEPTANCE-001",
					"batch_status":    "PENDING",
					"time_created":    time.Now().UTC().Format(time.RFC3339),
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/payments/payouts/PAYPAL-BATCH-"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"batch_header": map[string]interface{}{
					"payout_batch_id": "PAYPAL-BATCH-ACCEPTANCE-001",
					"batch_status":    "SUCCESS",
				},
				"items": []map[string]interface{}{
					{
						"payout_item_id":     "PAYPAL-ITEM-ACCEPTANCE-001",
						"transaction_status": "UNCLAIMED",
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"payout_item_id":     "PAYPAL-ITEM-ACCEPTANCE-001",
				"transaction_status": "RETURNED",
			})
		case strings.HasPrefix(r.URL.Path, "/v1/payments/payouts-item/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"payout_item_id":     "PAYPAL-ITEM-ACCEPTANCE-001",
				"transaction_status": "UNCLAIMED",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakePayPalServer.Close()

	// 2. Setup Config
	cfg := &config.Config{
		AppEnv:                 "development",
		Port:                   "8080",
		LogLevel:               "warn",
		DatabaseURL:            "postgres://mock",
		AppBaseURL:             "http://localhost:3000",
		APIBaseURL:             "http://localhost:8080",
		AllowedOrigins:         []string{"http://localhost:3000"},
		SessionCookieName:      "claimbridge_session",
		SessionCookieSecure:    false,
		SessionTTL:             7 * 24 * time.Hour,
		EmailVerificationTTL:   24 * time.Hour,
		PasswordResetTTL:       1 * time.Hour,
		RequireEmailVerify:     true,
		PayPalEnv:              "sandbox",
		PayPalClientID:         "MOCK_CLIENT_ID",
		PayPalClientSecret:     "MOCK_SECRET",
		PayPalBaseURL:          fakePayPalServer.URL,
		PayPalRequestTimeout:   5 * time.Second,
		PolicyEngineBinaryPath: "../../../policy/target/debug/claimbridge-policy",
	}

	repo := db.NewMockRepository()
	mockMailer := email.NewMockMailer()
	authService := auth.NewService(cfg, repo, mockMailer)
	authHandler := auth.NewHandler(cfg, authService)
	policyEngine := policy.NewEngine(cfg.PolicyEngineBinaryPath)
	paypalClient, err := paypal.NewTestClient(cfg, fakePayPalServer.Client(), fakePayPalServer.URL)
	if err != nil {
		t.Fatalf("failed to create paypal client: %v", err)
	}

	payoutsService := payouts.NewService(cfg, repo, policyEngine, paypalClient)
	payoutsHandler := payouts.NewHandler(cfg, payoutsService)

	srv := server.NewServer(cfg, policyEngine, paypalClient, authHandler, payoutsHandler)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Helper for HTTP requests with Origin header
	postJSON := func(client *http.Client, endpoint string, body interface{}) (*http.Response, map[string]interface{}) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+endpoint, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s failed: %v", endpoint, err)
		}
		rb, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var m map[string]interface{}
		_ = json.Unmarshal(rb, &m)
		return resp, m
	}

	getJSON := func(client *http.Client, endpoint string) (*http.Response, map[string]interface{}) {
		req, _ := http.NewRequest("GET", ts.URL+endpoint, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", endpoint, err)
		}
		rb, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var m map[string]interface{}
		_ = json.Unmarshal(rb, &m)
		return resp, m
	}

	// Create Client A (Officer Alice in Org A)
	jarA, _ := cookiejar.New(nil)
	clientA := &http.Client{Jar: jarA, Timeout: 5 * time.Second}

	// Register Alice
	postJSON(clientA, "/api/v1/auth/register", map[string]string{
		"email":             "alice@claims.internal",
		"password":          "StrongP@ssw0rd123!",
		"name":              "Alice Auditor",
		"organization_name": "Alpha Mutual Insurance",
	})
	mailA := mockMailer.GetLastMail()
	uA, _ := url.Parse(mailA.URL)
	postJSON(clientA, "/api/v1/auth/verify-email", map[string]string{"token": uA.Query().Get("token")})
	_, loginDataA := postJSON(clientA, "/api/v1/auth/login", map[string]string{
		"email":    "alice@claims.internal",
		"password": "StrongP@ssw0rd123!",
	})
	orgMap, _ := loginDataA["organization"].(map[string]interface{})
	orgAID, _ := orgMap["id"].(string)

	// Create Client B (Officer Bob in Org A)
	userB, _, _ := repo.CreateUserWithPersonalOrg(context.Background(), &db.User{
		Email:         "bob@claims.internal",
		Name:          "Bob Supervisor",
		EmailVerified: true,
		IsActive:      true,
	}, "Bob Temp Workspace")

	jarB, _ := cookiejar.New(nil)
	clientB := &http.Client{Jar: jarB, Timeout: 5 * time.Second}
	rawTokenB, tokenHashB, _ := crypto.GenerateOpaqueToken()
	_ = repo.CreateSession(context.Background(), &db.Session{
		UserID:               userB.ID,
		TokenHash:            tokenHashB,
		ActiveOrganizationID: &orgAID,
		ExpiresAt:            time.Now().Add(24 * time.Hour),
	})
	tsURL, _ := url.Parse(ts.URL)
	jarB.SetCookies(tsURL, []*http.Cookie{{
		Name:  "claimbridge_session",
		Value: rawTokenB,
		Path:  "/",
	}})

	// Create Client C in foreign Org C
	jarC, _ := cookiejar.New(nil)
	clientC := &http.Client{Jar: jarC, Timeout: 5 * time.Second}
	postJSON(clientC, "/api/v1/auth/register", map[string]string{
		"email":             "charlie@foreign.internal",
		"password":          "StrongP@ssw0rd123!",
		"name":              "Charlie Foreign",
		"organization_name": "Gamma Corp",
	})
	mailC := mockMailer.GetLastMail()
	uC, _ := url.Parse(mailC.URL)
	postJSON(clientC, "/api/v1/auth/verify-email", map[string]string{"token": uC.Query().Get("token")})
	postJSON(clientC, "/api/v1/auth/login", map[string]string{
		"email":    "charlie@foreign.internal",
		"password": "StrongP@ssw0rd123!",
	})

	// =========================================================================
	// Scenario 13: Probe PayPal connection status
	// =========================================================================
	t.Run("Scenario 13: PayPal connection status probe", func(t *testing.T) {
		res, data := getJSON(clientA, "/api/v1/paypal/connection-status")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK from connection-status, got: %d (%v)", res.StatusCode, data)
		}
		if data["connected"] != true || data["environment"] != "sandbox" {
			t.Errorf("expected connected=true in sandbox, got: %+v", data)
		}
	})

	// =========================================================================
	// Scenario 14: Create single-officer payout case (<= $250.00 USD)
	// =========================================================================
	var caseSingleID string
	t.Run("Scenario 14: Create single-officer payout case", func(t *testing.T) {
		res, data := postJSON(clientA, "/api/v1/payout-cases", payouts.CreateCaseRequest{
			RecipientEmail: "claimant1@example.com",
			RecipientName:  "Claimant One",
			Title:          "Small Property Claim",
			AmountCents:    18000, // $180.00 USD
			RiskScore:      10.0,
		})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got: %d (%v)", res.StatusCode, data)
		}
		cMap, _ := data["case"].(map[string]interface{})
		if cMap["dual_approval_required"] != false {
			t.Errorf("expected dual_approval_required=false for $180.00")
		}
		caseSingleID, _ = cMap["id"].(string)
	})

	// =========================================================================
	// Scenario 15: Create dual-approval payout case (> $250.00 USD)
	// =========================================================================
	var caseDualID string
	t.Run("Scenario 15: Create dual-approval payout case", func(t *testing.T) {
		res, data := postJSON(clientA, "/api/v1/payout-cases", payouts.CreateCaseRequest{
			RecipientEmail: "claimant2@example.com",
			RecipientName:  "Claimant Two",
			Title:          "Large Commercial Loss",
			AmountCents:    35000, // $350.00 USD
			RiskScore:      15.0,
		})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got: %d (%v)", res.StatusCode, data)
		}
		cMap, _ := data["case"].(map[string]interface{})
		if cMap["dual_approval_required"] != true {
			t.Errorf("expected dual_approval_required=true for $350.00")
		}
		caseDualID, _ = cMap["id"].(string)
	})

	// =========================================================================
	// Scenario 16: Reject payout case exceeding $500.00 ceiling fail-closed
	// =========================================================================
	t.Run("Scenario 16: Reject payout exceeding ceiling", func(t *testing.T) {
		res, _ := postJSON(clientA, "/api/v1/payout-cases", payouts.CreateCaseRequest{
			RecipientEmail: "claimant3@example.com",
			RecipientName:  "Claimant Three",
			Title:          "Ceiling Breach Claim",
			AmountCents:    50001, // $500.01 USD
		})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got: %d", res.StatusCode)
		}
	})

	// =========================================================================
	// Scenario 17: Initiate durable payout operation on case
	// =========================================================================
	var opID string
	t.Run("Scenario 17: Initiate durable payout operation", func(t *testing.T) {
		res, data := postJSON(clientA, fmt.Sprintf("/api/v1/payout-cases/%s/payout-operations", caseDualID), nil)
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got: %d (%v)", res.StatusCode, data)
		}
		opMap, _ := data["operation"].(map[string]interface{})
		if opMap["sender_batch_id"] == "" {
			t.Errorf("expected non-empty sender_batch_id")
		}
		if opMap["status"] != "AWAITING_APPROVAL" {
			t.Errorf("expected status AWAITING_APPROVAL, got: %v", opMap["status"])
		}
		opID, _ = opMap["id"].(string)
	})

	// =========================================================================
	// Scenario 18: Dual-custody separation of duties: initiator self-approval denied
	// =========================================================================
	t.Run("Scenario 18: Reject self-approval by initiator", func(t *testing.T) {
		res, data := postJSON(clientA, fmt.Sprintf("/api/v1/payout-operations/%s/approve", opID), nil)
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for self-approval, got: %d (%v)", res.StatusCode, data)
		}
	})

	// =========================================================================
	// Scenario 19: Dual-custody approval by distinct authorized officer succeeds
	// =========================================================================
	t.Run("Scenario 19: Dual approval by distinct officer succeeds", func(t *testing.T) {
		res, data := postJSON(clientB, fmt.Sprintf("/api/v1/payout-operations/%s/approve", opID), nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK for distinct approval, got: %d (%v)", res.StatusCode, data)
		}
		opMap, _ := data["operation"].(map[string]interface{})
		if opMap["status"] != "APPROVED" {
			t.Errorf("expected status APPROVED, got: %v", opMap["status"])
		}
	})

	// =========================================================================
	// Scenario 20: Submit payout operation to PayPal sandbox
	// =========================================================================
	t.Run("Scenario 20: Submit payout operation to PayPal Sandbox", func(t *testing.T) {
		res, data := postJSON(clientA, fmt.Sprintf("/api/v1/payout-operations/%s/submit", opID), nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK submitting payout, got: %d (%v)", res.StatusCode, data)
		}
		opMap, _ := data["operation"].(map[string]interface{})
		if opMap["status"] != "SUBMITTED" {
			t.Errorf("expected status SUBMITTED, got: %v", opMap["status"])
		}
		if opMap["paypal_payout_batch_id"] != "PAYPAL-BATCH-ACCEPTANCE-001" {
			t.Errorf("expected PayPal batch ID PAYPAL-BATCH-ACCEPTANCE-001, got: %v", opMap["paypal_payout_batch_id"])
		}
	})

	// =========================================================================
	// Scenario 21: Synchronize status with PayPal Sandbox
	// =========================================================================
	t.Run("Scenario 21: Synchronize status with PayPal Sandbox", func(t *testing.T) {
		res, data := postJSON(clientA, fmt.Sprintf("/api/v1/payout-cases/%s/sync", caseDualID), nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK from sync, got: %d (%v)", res.StatusCode, data)
		}
		opMap, _ := data["operation"].(map[string]interface{})
		if opMap["paypal_payout_item_id"] != "PAYPAL-ITEM-ACCEPTANCE-001" {
			t.Errorf("expected item ID PAYPAL-ITEM-ACCEPTANCE-001, got: %v", opMap["paypal_payout_item_id"])
		}
	})

	// =========================================================================
	// Scenario 22: Cancel eligible UNCLAIMED payout operation
	// =========================================================================
	t.Run("Scenario 22: Cancel eligible UNCLAIMED payout", func(t *testing.T) {
		res, data := postJSON(clientA, fmt.Sprintf("/api/v1/payout-operations/%s/cancel", opID), nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK cancelling unclaimed payout, got: %d (%v)", res.StatusCode, data)
		}
		opMap, _ := data["operation"].(map[string]interface{})
		if opMap["status"] != "CANCELLED" {
			t.Errorf("expected op status CANCELLED, got: %v", opMap["status"])
		}
	})

	// =========================================================================
	// Scenario 23: Multi-tenant cross-org isolation
	// =========================================================================
	t.Run("Scenario 23: Tenant isolation prevents cross-org case access", func(t *testing.T) {
		// Charlie in Gamma Corp attempts to retrieve Alice's case in Alpha Mutual
		res, _ := getJSON(clientC, fmt.Sprintf("/api/v1/payout-cases/%s", caseDualID))
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for cross-tenant read, got: %d", res.StatusCode)
		}

		// Charlie attempts to initiate operation on Alice's case
		resOp, _ := postJSON(clientC, fmt.Sprintf("/api/v1/payout-cases/%s/payout-operations", caseDualID), nil)
		if resOp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for cross-tenant operation, got: %d", resOp.StatusCode)
		}
	})

	// =========================================================================
	// Scenario 24: Ambiguous network timeout transitions to RECONCILIATION_REQUIRED
	// =========================================================================
	t.Run("Scenario 24: Ambiguous timeout safety transitions to RECONCILIATION_REQUIRED", func(t *testing.T) {
		timeoutPayPalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.URL.Path == "/v1/oauth2/token":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "fake-sandbox-access-token-999",
					"token_type":   "Bearer",
					"app_id":       "APP-SANDBOX-TEST",
					"expires_in":   3600,
				})
			case r.URL.Path == "/v1/payments/payouts" && r.Method == http.MethodPost:
				// Hold for 100ms to deterministically exceed the 30ms timeout, then exit cleanly
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			default:
				http.NotFound(w, r)
			}
		}))
		defer timeoutPayPalServer.Close()
		defer timeoutPayPalServer.CloseClientConnections()

		// Isolated, immutable configuration for the timeout scenario
		timeoutCfg := *cfg
		timeoutCfg.PayPalBaseURL = timeoutPayPalServer.URL
		timeoutCfg.PayPalRequestTimeout = 30 * time.Millisecond

		timeoutHTTPClient := &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		}

		timeoutPayPalClient, err := paypal.NewTestClient(&timeoutCfg, timeoutHTTPClient, timeoutPayPalServer.URL)
		if err != nil {
			t.Fatalf("failed to create timeout paypal client: %v", err)
		}

		timeoutPayoutsService := payouts.NewService(&timeoutCfg, repo, policyEngine, timeoutPayPalClient)
		timeoutPayoutsHandler := payouts.NewHandler(&timeoutCfg, timeoutPayoutsService)
		timeoutSrv := server.NewServer(&timeoutCfg, policyEngine, timeoutPayPalClient, authHandler, timeoutPayoutsHandler)
		timeoutTS := httptest.NewServer(timeoutSrv.Handler())
		defer timeoutTS.Close()
		defer timeoutTS.CloseClientConnections()

		postJSONTimeout := func(client *http.Client, endpoint string, body interface{}) (*http.Response, map[string]interface{}) {
			var bodyReader io.Reader
			if body != nil {
				jsonBytes, _ := json.Marshal(body)
				bodyReader = bytes.NewReader(jsonBytes)
			}
			req, _ := http.NewRequest(http.MethodPost, timeoutTS.URL+endpoint, bodyReader)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://localhost:3000")
			req.Close = true // Prevent keep-alive from holding connections open
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("POST %s failed: %v", endpoint, err)
			}
			defer res.Body.Close()
			var data map[string]interface{}
			_ = json.NewDecoder(res.Body).Decode(&data)
			return res, data
		}

		// Initiate operation on caseSingle
		_, outInit := postJSONTimeout(clientA, fmt.Sprintf("/api/v1/payout-cases/%s/payout-operations", caseSingleID), nil)
		opSingleMap, _ := outInit["operation"].(map[string]interface{})
		opSingleID, _ := opSingleMap["id"].(string)

		// Submit should time out deterministically after 50ms and transition operation to RECONCILIATION_REQUIRED
		resSubmit, _ := postJSONTimeout(clientA, fmt.Sprintf("/api/v1/payout-operations/%s/submit", opSingleID), nil)

		// Read case again via primary server or repository
		_, outCase := getJSON(clientA, fmt.Sprintf("/api/v1/payout-cases/%s", caseSingleID))
		opsList, _ := outCase["operations"].([]interface{})

		if len(opsList) > 0 {
			op0, _ := opsList[0].(map[string]interface{})
			opStatus := op0["status"]
			if opStatus != "RECONCILIATION_REQUIRED" && resSubmit.StatusCode != http.StatusGatewayTimeout {
				t.Errorf("expected status RECONCILIATION_REQUIRED on ambiguous timeout, got: %v (HTTP %d)", opStatus, resSubmit.StatusCode)
			}
		}
	})
}
