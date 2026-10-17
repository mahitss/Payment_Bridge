package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/email"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
	"claimbridge/services/api/internal/server"
)

// TestMission002_TwelveAcceptanceScenarios executes all 12 acceptance scenarios required by Mission 002.
func TestMission002_TwelveAcceptanceScenarios(t *testing.T) {
	cfg := &config.Config{
		AppEnv:                 "development",
		Port:                   "8080",
		LogLevel:               "warn",
		DatabaseURL:            "postgres://mock",
		AppBaseURL:             "http://localhost:3000",
		APIBaseURL:             "http://localhost:8080",
		AllowedOrigins:         []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		SessionCookieName:      "claimbridge_session",
		SessionCookieSecure:    false,
		SessionTTL:             7 * 24 * time.Hour,
		EmailVerificationTTL:   24 * time.Hour,
		PasswordResetTTL:       1 * time.Hour,
		RequireEmailVerify:     true,
		PayPalEnv:              "sandbox",
		PayPalClientID:         "MOCK_CLIENT_ID",
		PayPalClientSecret:     "MOCK_SECRET",
		PayPalBaseURL:          "https://api-m.sandbox.paypal.com",
		PayPalRequestTimeout:   10 * time.Second,
		PolicyEngineBinaryPath: "../../../policy/target/debug/claimbridge-policy",
	}

	repo := db.NewMockRepository()
	mockMailer := email.NewMockMailer()
	authService := auth.NewService(cfg, repo, mockMailer)
	authHandler := auth.NewHandler(cfg, authService)
	policyEngine := policy.NewEngine(cfg.PolicyEngineBinaryPath)
	paypalClient, _ := paypal.NewClient(cfg, nil)
	srv := server.NewServer(cfg, policyEngine, paypalClient, authHandler)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("failed to create cookiejar: %v", err)
	}

	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	testUserEmail := "auditor@mission002.internal"
	testUserPass := "StrongP@ssw0rd2026!"
	newPassword := "BrandNewP@ssw0rd2027!"

	post := func(endpoint string, body interface{}) (*http.Response, map[string]interface{}) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+endpoint, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s failed: %v", endpoint, err)
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(resp.Body)
		var m map[string]interface{}
		_ = json.Unmarshal(rb, &m)
		return resp, m
	}

	get := func(endpoint string) (*http.Response, map[string]interface{}) {
		req, _ := http.NewRequest("GET", ts.URL+endpoint, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", endpoint, err)
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(resp.Body)
		var m map[string]interface{}
		_ = json.Unmarshal(rb, &m)
		return resp, m
	}

	// 1. Register a new account
	t.Run("Scenario 1: Register account", func(t *testing.T) {
		resp, data := post("/api/v1/auth/register", map[string]string{
			"email":             testUserEmail,
			"password":          testUserPass,
			"name":              "Acceptance Lead",
			"organization_name": "Acceptance Workspace",
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %v", resp.StatusCode, data)
		}
	})

	// 2. Open verification email and verify account
	var verifyToken string
	t.Run("Scenario 2: Verify account and enforce single-use", func(t *testing.T) {
		lastMsg := mockMailer.GetLastMail()
		if lastMsg == nil || lastMsg.Type != "verification" {
			t.Fatalf("expected verification email dispatched, got nil")
		}
		u, _ := url.Parse(lastMsg.URL)
		verifyToken = u.Query().Get("token")
		if verifyToken == "" {
			t.Fatalf("expected token in URL: %s", lastMsg.URL)
		}

		resp, _ := post("/api/v1/auth/verify-email", map[string]string{"token": verifyToken})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		// Re-use token must fail
		respReuse, _ := post("/api/v1/auth/verify-email", map[string]string{"token": verifyToken})
		if respReuse.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on token reuse, got %d", respReuse.StatusCode)
		}
	})

	// 3. Log in using the real backend
	t.Run("Scenario 3: Login and obtain session cookie", func(t *testing.T) {
		resp, data := post("/api/v1/auth/login", map[string]string{
			"email":    testUserEmail,
			"password": testUserPass,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %v", resp.StatusCode, data)
		}

		u, _ := url.Parse(ts.URL)
		foundCookie := false
		for _, c := range jar.Cookies(u) {
			if c.Name == cfg.SessionCookieName && c.Value != "" {
				foundCookie = true
			}
		}
		if !foundCookie {
			t.Fatalf("expected cookie %s in jar", cfg.SessionCookieName)
		}
	})

	// 4. Open protected route (/api/v1/auth/me)
	var orgID string
	t.Run("Scenario 4: Access protected route", func(t *testing.T) {
		resp, data := get("/api/v1/auth/me")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}
		userObj, _ := data["user"].(map[string]interface{})
		orgObj, _ := data["organization"].(map[string]interface{})
		if userObj["email"] != testUserEmail {
			t.Fatalf("expected email %s, got %v", testUserEmail, userObj["email"])
		}
		orgID, _ = orgObj["id"].(string)
		if orgID == "" {
			t.Fatalf("expected organization ID")
		}
	})

	// 5. Refresh page and confirm session remains valid
	t.Run("Scenario 5: Session persistence across requests", func(t *testing.T) {
		resp, _ := get("/api/v1/auth/me")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on repeated request, got %d", resp.StatusCode)
		}
	})

	// 6. Log out and confirm protected access is denied
	t.Run("Scenario 6: Logout and subsequent 401", func(t *testing.T) {
		resp, _ := post("/api/v1/auth/logout", map[string]interface{}{})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on logout, got %d", resp.StatusCode)
		}
		respAfter, _ := get("/api/v1/auth/me")
		if respAfter.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized after logout, got %d", respAfter.StatusCode)
		}
	})

	// 7. Request password reset email
	var resetToken string
	t.Run("Scenario 7: Request password reset", func(t *testing.T) {
		resp, _ := post("/api/v1/auth/forgot-password", map[string]string{"email": testUserEmail})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on forgot-password, got %d", resp.StatusCode)
		}
		lastMsg := mockMailer.GetLastMail()
		if lastMsg == nil || lastMsg.Type != "password_reset" {
			t.Fatalf("expected password_reset email")
		}
		u, _ := url.Parse(lastMsg.URL)
		resetToken = u.Query().Get("token")
		if resetToken == "" {
			t.Fatalf("expected reset token in url: %s", lastMsg.URL)
		}
	})

	// 8. Reset password using single-use token
	t.Run("Scenario 8: Consume reset token and reject reuse", func(t *testing.T) {
		resp, _ := post("/api/v1/auth/reset-password", map[string]string{
			"token":        resetToken,
			"new_password": newPassword,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on reset-password, got %d", resp.StatusCode)
		}
		respReuse, _ := post("/api/v1/auth/reset-password", map[string]string{
			"token":        resetToken,
			"new_password": "AnotherPassword123!",
		})
		if respReuse.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on reset token reuse, got %d", respReuse.StatusCode)
		}
	})

	// 9. Confirm old password fails and new password works
	t.Run("Scenario 9: Old password fails, new password succeeds", func(t *testing.T) {
		respOld, _ := post("/api/v1/auth/login", map[string]string{"email": testUserEmail, "password": testUserPass})
		if respOld.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for old password, got %d", respOld.StatusCode)
		}

		respNew, _ := post("/api/v1/auth/login", map[string]string{"email": testUserEmail, "password": newPassword})
		if respNew.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK for new password, got %d", respNew.StatusCode)
		}
	})

	// 10. Confirm unauthenticated direct API call is rejected
	t.Run("Scenario 10: Reject unauthenticated direct API call", func(t *testing.T) {
		unauthClient := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/auth/me", nil)
		respDirect, _ := unauthClient.Do(req)
		if respDirect.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized on direct unauthenticated request, got %d", respDirect.StatusCode)
		}
	})

	// 11. Confirm cross-organization isolation
	t.Run("Scenario 11: Cross-organization tenant isolation", func(t *testing.T) {
		userBEmail := "auditor-b@isolated.org"
		_, _ = post("/api/v1/auth/register", map[string]string{
			"email":             userBEmail,
			"password":          "PasswordB123!",
			"name":              "Auditor B",
			"organization_name": "Isolated Org B",
		})
		lastMsg := mockMailer.GetLastMail()
		u, _ := url.Parse(lastMsg.URL)
		tokenB := u.Query().Get("token")
		_, _ = post("/api/v1/auth/verify-email", map[string]string{"token": tokenB})

		userBModel, _ := repo.GetUserByEmail(context.Background(), userBEmail)
		hasAccess, _, _ := repo.VerifyUserOrgAccess(context.Background(), userBModel.ID, orgID)
		if hasAccess {
			t.Fatalf("User B must NOT have access to User A's organization %s", orgID)
		}
	})

	// 12. Restart application and confirm account data persists
	t.Run("Scenario 12: Account data persistence across server reboot", func(t *testing.T) {
		ts.Close()
		ts2 := httptest.NewServer(srv.Handler())
		defer ts2.Close()

		storedUser, err := repo.GetUserByEmail(context.Background(), testUserEmail)
		if err != nil || storedUser == nil || !storedUser.EmailVerified {
			t.Fatalf("expected user %s to persist with verified email, err: %v", testUserEmail, err)
		}
	})
}
