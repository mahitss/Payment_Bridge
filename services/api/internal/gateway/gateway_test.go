package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGateway_TargetValidation_LoopbackRestriction(t *testing.T) {
	// 1. Valid loopback targets
	validTargets := []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080",
		"http://127.0.0.1",
		"", // Defaults to DefaultTargetURL
	}
	for _, target := range validTargets {
		gw, err := NewGateway(target)
		if err != nil {
			t.Errorf("expected valid target %q to succeed, got: %v", target, err)
		}
		if gw == nil {
			t.Errorf("expected non-nil gateway for target %q", target)
		}
	}

	// 2. Forbidden external targets
	forbiddenTargets := []string{
		"http://evil.com:8080",
		"http://192.168.1.50:8080",
		"http://10.0.0.1:8080",
		"https://api.sandbox.paypal.com",
	}
	for _, target := range forbiddenTargets {
		_, err := NewGateway(target)
		if err == nil {
			t.Errorf("expected target %q to be forbidden, but NewGateway succeeded", target)
		}
	}
}

func TestGateway_AllowedWebhookPOST_Preservation(t *testing.T) {
	var (
		receivedBody    []byte
		receivedCT      string
		receivedSig     string
		receivedTransID string
		receivedTime    string
		receivedCertURL string
		receivedAlgo    string
	)

	// Mock upstream API
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != AllowedPath {
			http.NotFound(w, r)
			return
		}
		receivedCT = r.Header.Get("Content-Type")
		receivedSig = r.Header.Get("PAYPAL-TRANSMISSION-SIG")
		receivedTransID = r.Header.Get("PAYPAL-TRANSMISSION-ID")
		receivedTime = r.Header.Get("PAYPAL-TRANSMISSION-TIME")
		receivedCertURL = r.Header.Get("PAYPAL-CERT-URL")
		receivedAlgo = r.Header.Get("PAYPAL-AUTH-ALGO")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		receivedBody = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"processed","paypal_event_id":"WH-EVT-100"}`))
	}))
	defer mockUpstream.Close()

	gw, err := NewGateway(mockUpstream.URL)
	if err != nil {
		t.Fatalf("failed creating gateway: %v", err)
	}

	rawPayload := []byte(`{"id":"WH-EVT-100","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)
	req := httptest.NewRequest(http.MethodPost, AllowedPath, bytes.NewReader(rawPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PAYPAL-TRANSMISSION-ID", "TRANS-12345")
	req.Header.Set("PAYPAL-TRANSMISSION-TIME", "2026-11-08T00:00:00Z")
	req.Header.Set("PAYPAL-CERT-URL", "https://api.sandbox.paypal.com/v1/notifications/certs/CERT.pem")
	req.Header.Set("PAYPAL-AUTH-ALGO", "SHA256withRSA")
	req.Header.Set("PAYPAL-TRANSMISSION-SIG", "base64sig==")

	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	// Check response code and body
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from gateway, got: %d, body: %s", rec.Code, rec.Body.String())
	}

	// Verify exact body preservation
	if !bytes.Equal(receivedBody, rawPayload) {
		t.Errorf("request body was mutated: expected %s, got %s", string(rawPayload), string(receivedBody))
	}

	// Verify header preservation
	if receivedCT != "application/json" {
		t.Errorf("expected Content-Type application/json, got: %s", receivedCT)
	}
	if receivedTransID != "TRANS-12345" {
		t.Errorf("expected transmission ID TRANS-12345, got: %s", receivedTransID)
	}
	if receivedTime != "2026-11-08T00:00:00Z" {
		t.Errorf("expected transmission time 2026-11-08T00:00:00Z, got: %s", receivedTime)
	}
	if receivedCertURL != "https://api.sandbox.paypal.com/v1/notifications/certs/CERT.pem" {
		t.Errorf("expected cert URL, got: %s", receivedCertURL)
	}
	if receivedAlgo != "SHA256withRSA" {
		t.Errorf("expected auth algo SHA256withRSA, got: %s", receivedAlgo)
	}
	if receivedSig != "base64sig==" {
		t.Errorf("expected signature base64sig==, got: %s", receivedSig)
	}
}

func TestGateway_ForbiddenMethods(t *testing.T) {
	gw, err := NewGateway("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("failed creating gateway: %v", err)
	}

	methods := []string{
		http.MethodGet,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodHead,
		http.MethodOptions,
	}

	for _, method := range methods {
		req := httptest.NewRequest(method, AllowedPath, nil)
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected HTTP 405 for method %s, got: %d", method, rec.Code)
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "METHOD_NOT_ALLOWED" {
			t.Errorf("expected code METHOD_NOT_ALLOWED for %s, got: %v", method, errResp["code"])
		}
	}
}

func TestGateway_ForbiddenPaths(t *testing.T) {
	gw, err := NewGateway("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("failed creating gateway: %v", err)
	}

	forbiddenPaths := []string{
		"/healthz",
		"/readyz",
		"/api/v1/payout-cases",
		"/api/v1/payout-operations/op-1/submit",
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/sandbox/status",
		"/api/v1/policy/evaluate",
		"/",
		"/api/v1/paypal/webhooks/", // trailing slash
		"/api/v1/paypal/webhooks/extra",
	}

	for _, path := range forbiddenPaths {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(method, path, strings.NewReader(`{"dummy":true}`))
			rec := httptest.NewRecorder()
			gw.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected HTTP 404 for path %s (method %s), got: %d", path, method, rec.Code)
			}

			var errResp map[string]string
			_ = json.NewDecoder(rec.Body).Decode(&errResp)
			if errResp["code"] != "ROUTE_NOT_ALLOWED" {
				t.Errorf("expected code ROUTE_NOT_ALLOWED for path %s, got: %v", path, errResp["code"])
			}
		}
	}
}

func TestGateway_ForbiddenQueryStrings(t *testing.T) {
	gw, err := NewGateway("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("failed creating gateway: %v", err)
	}

	queryURLs := []string{
		AllowedPath + "?token=secret",
		AllowedPath + "?id=123",
		AllowedPath + "?",
		AllowedPath + "?debug=true",
	}

	for _, rawURL := range queryURLs {
		req := httptest.NewRequest(http.MethodPost, rawURL, strings.NewReader(`{"id":"WH-1"}`))
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected HTTP 400 for query string URL %s, got: %d", rawURL, rec.Code)
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "QUERY_STRING_FORBIDDEN" {
			t.Errorf("expected code QUERY_STRING_FORBIDDEN, got: %v", errResp["code"])
		}
	}
}

func TestGateway_OversizedPayload_Rejection(t *testing.T) {
	gw, err := NewGateway("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("failed creating gateway: %v", err)
	}

	// 512 KB + 10 bytes
	oversized := make([]byte, 512*1024+10)
	req := httptest.NewRequest(http.MethodPost, AllowedPath, bytes.NewReader(oversized))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for oversized payload, got: %d", rec.Code)
	}

	var errResp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp["code"] != "PAYLOAD_TOO_LARGE" {
		t.Errorf("expected code PAYLOAD_TOO_LARGE, got: %v", errResp["code"])
	}
}

func TestGateway_UpstreamUnavailable_BadGateway(t *testing.T) {
	// Point to unused port on localhost
	gw, err := NewGateway("http://127.0.0.1:54399")
	if err != nil {
		t.Fatalf("failed creating gateway: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, AllowedPath, strings.NewReader(`{"id":"WH-1"}`))
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected HTTP 502 Bad Gateway when upstream is down, got: %d", rec.Code)
	}

	var errResp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp["code"] != "UPSTREAM_UNAVAILABLE" {
		t.Errorf("expected code UPSTREAM_UNAVAILABLE, got: %v", errResp["code"])
	}
}
