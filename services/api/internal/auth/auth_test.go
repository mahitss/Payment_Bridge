package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/email"
)

func init() {
	crypto.SetFastTestMode()
}

func setupTestServer(t *testing.T) (*Service, *db.MockRepository, *email.MockMailer, *config.Config, http.Handler) {
	cfg := &config.Config{
		AppEnv:               "test",
		Port:                 "8080",
		AppBaseURL:           "http://localhost:3000",
		APIBaseURL:           "http://localhost:8080",
		AllowedOrigins:       []string{"http://localhost:3000"},
		SessionCookieName:    "claimbridge_session",
		SessionCookieSecure:  false,
		SessionTTL:           72 * time.Hour,
		EmailVerificationTTL: 24 * time.Hour,
		PasswordResetTTL:     30 * time.Minute,
		RequireEmailVerify:   true,
	}

	repo := db.NewMockRepository()
	mailer := email.NewMockMailer()
	service := NewService(cfg, repo, mailer)
	handler := NewHandler(cfg, service)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rootHandler := handler.OriginCheckMiddleware(handler.Authenticate(mux))
	return service, repo, mailer, cfg, rootHandler
}

func TestAuth_RegistrationAndVerificationFlow(t *testing.T) {
	_, _, mailer, _, handler := setupTestServer(t)

	// 1. Register a new user
	regPayload := `{"name": "Alice Smith", "email": "Alice@Example.COM", "password": "SecurePassword123!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(regPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got: %d: %s", rr.Code, rr.Body.String())
	}

	// Verify an email was sent via Mailer
	mail := mailer.GetLastMail()
	if mail == nil || mail.Type != "verification" {
		t.Fatalf("expected verification email to be sent, got: %+v", mail)
	}

	// Extract verification token from URL
	parts := strings.Split(mail.URL, "token=")
	if len(parts) != 2 {
		t.Fatalf("failed to extract token from URL: %s", mail.URL)
	}
	token := parts[1]

	// 2. Attempt Login Before Verification (Must Fail 403 Forbidden)
	loginPayload := `{"email": "alice@example.com", "password": "SecurePassword123!"}`
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginPayload))
	reqLogin.Header.Set("Content-Type", "application/json")
	reqLogin.Header.Set("Origin", "http://localhost:3000")
	rrLogin := httptest.NewRecorder()

	handler.ServeHTTP(rrLogin, reqLogin)

	if rrLogin.Code != http.StatusForbidden {
		t.Errorf("expected status 403 Forbidden before verification, got: %d", rrLogin.Code)
	}

	// 3. Verify Email using token
	verifyPayload := fmtJSON(map[string]string{"token": token})
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email", strings.NewReader(verifyPayload))
	reqVerify.Header.Set("Content-Type", "application/json")
	reqVerify.Header.Set("Origin", "http://localhost:3000")
	rrVerify := httptest.NewRecorder()

	handler.ServeHTTP(rrVerify, reqVerify)

	if rrVerify.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for email verification, got: %d: %s", rrVerify.Code, rrVerify.Body.String())
	}

	// 4. Token Single-Use Enforcement: Second call must fail
	rrVerify2 := httptest.NewRecorder()
	reqVerify2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email", strings.NewReader(verifyPayload))
	reqVerify2.Header.Set("Content-Type", "application/json")
	reqVerify2.Header.Set("Origin", "http://localhost:3000")
	handler.ServeHTTP(rrVerify2, reqVerify2)

	if rrVerify2.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request on second token use, got: %d", rrVerify2.Code)
	}

	// 5. Login After Verification (Must Succeed 200 OK + Set Session Cookie)
	rrLoginSuccess := httptest.NewRecorder()
	reqLoginSuccess := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginPayload))
	reqLoginSuccess.Header.Set("Content-Type", "application/json")
	reqLoginSuccess.Header.Set("Origin", "http://localhost:3000")

	handler.ServeHTTP(rrLoginSuccess, reqLoginSuccess)

	if rrLoginSuccess.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK on login, got: %d: %s", rrLoginSuccess.Code, rrLoginSuccess.Body.String())
	}

	// Verify Session Cookie
	cookies := rrLoginSuccess.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "claimbridge_session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("expected claimbridge_session cookie to be set")
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("expected session cookie to have HttpOnly=true")
	}

	// 6. Test /api/v1/auth/me with session cookie
	reqMe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	reqMe.AddCookie(sessionCookie)
	rrMe := httptest.NewRecorder()

	handler.ServeHTTP(rrMe, reqMe)

	if rrMe.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /me, got: %d: %s", rrMe.Code, rrMe.Body.String())
	}

	var meResp map[string]interface{}
	json.NewDecoder(rrMe.Body).Decode(&meResp)
	userData := meResp["user"].(map[string]interface{})
	if userData["email"] != "alice@example.com" {
		t.Errorf("unexpected user email in /me: %v", userData["email"])
	}

	// 7. Test Logout
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	reqLogout.AddCookie(sessionCookie)
	reqLogout.Header.Set("Origin", "http://localhost:3000")
	rrLogout := httptest.NewRecorder()

	handler.ServeHTTP(rrLogout, reqLogout)

	if rrLogout.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for logout, got: %d", rrLogout.Code)
	}

	// 8. Subsequent /api/v1/auth/me must fail (401 Unauthorized)
	rrMeAfterLogout := httptest.NewRecorder()
	reqMeAfterLogout := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	reqMeAfterLogout.AddCookie(sessionCookie)

	handler.ServeHTTP(rrMeAfterLogout, reqMeAfterLogout)

	if rrMeAfterLogout.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized after logout, got: %d", rrMeAfterLogout.Code)
	}
}

func TestAuth_PasswordResetFlowAndSessionInvalidation(t *testing.T) {
	_, repo, mailer, _, handler := setupTestServer(t)

	// Create user with verified email
	pwdHash, _ := crypto.HashPassword("InitialPassword123!")
	user, org, _ := repo.CreateUserWithPersonalOrg(context.Background(), &db.User{
		Email:         "charlie@example.com",
		PasswordHash:  pwdHash,
		Name:          "Charlie",
		EmailVerified: true,
		IsActive:      true,
	}, "Charlie's Org")

	// Create active session for Charlie
	_, sessionHash, _ := crypto.GenerateOpaqueToken()
	_ = repo.CreateSession(context.Background(), &db.Session{
		UserID:               user.ID,
		TokenHash:            sessionHash,
		ActiveOrganizationID: &org.ID,
		ExpiresAt:            time.Now().UTC().Add(24 * time.Hour),
	})

	// 1. Request Password Reset
	forgotPayload := `{"email": "charlie@example.com"}`
	reqForgot := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", strings.NewReader(forgotPayload))
	reqForgot.Header.Set("Content-Type", "application/json")
	reqForgot.Header.Set("Origin", "http://localhost:3000")
	rrForgot := httptest.NewRecorder()

	handler.ServeHTTP(rrForgot, reqForgot)

	if rrForgot.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for forgot-password, got: %d", rrForgot.Code)
	}

	mail := mailer.GetLastMail()
	if mail == nil || mail.Type != "password_reset" {
		t.Fatalf("expected password_reset email, got: %+v", mail)
	}

	parts := strings.Split(mail.URL, "token=")
	resetToken := parts[1]

	// 2. Reset Password using token
	resetPayload := fmtJSON(map[string]string{
		"token":        resetToken,
		"new_password": "NewSecurePassword456!",
	})
	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", strings.NewReader(resetPayload))
	reqReset.Header.Set("Content-Type", "application/json")
	reqReset.Header.Set("Origin", "http://localhost:3000")
	rrReset := httptest.NewRecorder()

	handler.ServeHTTP(rrReset, reqReset)

	if rrReset.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for password reset, got: %d: %s", rrReset.Code, rrReset.Body.String())
	}

	// 3. SECURITY ASSERTION: Old session MUST be invalidated
	_, _, _, err := repo.GetSessionByTokenHash(context.Background(), sessionHash)
	if err == nil {
		t.Errorf("expected old session to be invalidated after password reset, but it was found")
	}

	// 4. Old password must fail login
	loginOldPayload := `{"email": "charlie@example.com", "password": "InitialPassword123!"}`
	reqOld := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginOldPayload))
	reqOld.Header.Set("Content-Type", "application/json")
	reqOld.Header.Set("Origin", "http://localhost:3000")
	rrOld := httptest.NewRecorder()
	handler.ServeHTTP(rrOld, reqOld)

	if rrOld.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized with old password, got: %d", rrOld.Code)
	}

	// 5. New password must succeed login
	loginNewPayload := `{"email": "charlie@example.com", "password": "NewSecurePassword456!"}`
	reqNew := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginNewPayload))
	reqNew.Header.Set("Content-Type", "application/json")
	reqNew.Header.Set("Origin", "http://localhost:3000")
	rrNew := httptest.NewRecorder()
	handler.ServeHTTP(rrNew, reqNew)

	if rrNew.Code != http.StatusOK {
		t.Errorf("expected 200 OK with new password, got: %d", rrNew.Code)
	}
}

func TestAuth_OriginAndCSRFProtection(t *testing.T) {
	_, _, _, _, handler := setupTestServer(t)

	// Post from evil.com (Must be rejected with 403 Forbidden)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil-attacker.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for disallowed Origin, got: %d", rr.Code)
	}
}

func fmtJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
