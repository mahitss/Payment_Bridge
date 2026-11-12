package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_NormalTrafficPermitted(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute, time.Minute)
	defer rl.Stop()

	handler := rl.Middleware("test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		rec := httptest.NewRecorder()

		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected HTTP 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimiter_ExcessiveAttemptsBlockedWith429(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute, time.Minute)
	defer rl.Stop()

	handler := rl.Middleware("login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 3 allowed
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "203.0.113.10:45678"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected HTTP 200, got %d", i+1, rec.Code)
		}
	}

	// 4th must be blocked with HTTP 429
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "203.0.113.10:45678"
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected HTTP 429 Too Many Requests, got %d", rec.Code)
	}

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Errorf("expected Retry-After header, got empty")
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode 429 response body: %v", err)
	}

	if body["error"] != "too many requests, please try again later" {
		t.Errorf("unexpected error message: %v", body["error"])
	}
	if retrySec, ok := body["retry_after_seconds"].(float64); !ok || retrySec <= 0 {
		t.Errorf("expected positive retry_after_seconds, got %v", body["retry_after_seconds"])
	}
}

func TestRateLimiter_DifferentIPsIsolated(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute, time.Minute)
	defer rl.Stop()

	handler := rl.Middleware("register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	// Max out IP A
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/register", nil)
		req.RemoteAddr = "198.51.100.1:1111"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
	}

	// IP A is now rate-limited
	reqA := httptest.NewRequest(http.MethodPost, "/register", nil)
	reqA.RemoteAddr = "198.51.100.1:1111"
	recA := httptest.NewRecorder()
	handler(recA, reqA)
	if recA.Code != http.StatusTooManyRequests {
		t.Fatalf("expected IP A to be rate-limited (429), got %d", recA.Code)
	}

	// IP B should NOT be affected
	reqB := httptest.NewRequest(http.MethodPost, "/register", nil)
	reqB.RemoteAddr = "198.51.100.2:2222"
	recB := httptest.NewRecorder()
	handler(recB, reqB)
	if recB.Code != http.StatusCreated {
		t.Fatalf("expected IP B to succeed (201), got %d", recB.Code)
	}
}

func TestRateLimiter_DirectConnectionIgnoresSpoofedXFF(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute, time.Minute)
	defer rl.Stop()

	// Direct untrusted public IP connecting with spoofed X-Forwarded-For
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "198.51.100.50:5555" // Public IP (not in trusted proxies)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")

	extracted := rl.ExtractClientIP(req)
	if extracted != "198.51.100.50" {
		t.Fatalf("expected direct untrusted connection to use RemoteAddr 198.51.100.50, got %q", extracted)
	}
}

func TestRateLimiter_TrustedProxyResolvesRealClientIP(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute, time.Minute)
	defer rl.Stop()

	// Connection from loopback (Next.js reverse proxy on 127.0.0.1)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "127.0.0.1:44444"
	req.Header.Set("X-Forwarded-For", "203.0.113.88")

	extracted := rl.ExtractClientIP(req)
	if extracted != "203.0.113.88" {
		t.Fatalf("expected trusted proxy to resolve XFF client IP 203.0.113.88, got %q", extracted)
	}
}

func TestRateLimiter_WindowResetAllowsSubsequentRequests(t *testing.T) {
	// Short 50ms window
	rl := NewRateLimiter(1, 50*time.Millisecond, 50*time.Millisecond)
	defer rl.Stop()

	handler := rl.Middleware("action", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/action", nil)
	req.RemoteAddr = "192.0.2.99:9999"

	rec1 := httptest.NewRecorder()
	handler(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rec1.Code)
	}

	// Immediate 2nd request blocked
	rec2 := httptest.NewRecorder()
	handler(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second immediate request expected 429, got %d", rec2.Code)
	}

	// Wait for window to elapse
	time.Sleep(60 * time.Millisecond)

	rec3 := httptest.NewRecorder()
	handler(rec3, req)
	if rec3.Code != http.StatusOK {
		t.Fatalf("request after window expiry expected 200, got %d", rec3.Code)
	}
}
