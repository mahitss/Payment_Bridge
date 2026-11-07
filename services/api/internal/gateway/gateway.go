package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	AllowedPath       = "/api/v1/paypal/webhooks"
	MaxBodyBytes      = 512 * 1024 // 512 KB
	DefaultListenAddr = "127.0.0.1:8787"
	DefaultTargetURL  = "http://127.0.0.1:8080"
)

var (
	ErrForbiddenTarget = errors.New("security violation: target URL must be strictly local (127.0.0.1 or localhost)")
)

// Gateway is an ingress reverse proxy restricted exclusively to forwarding PayPal webhook POST requests.
type Gateway struct {
	targetURL  *url.URL
	httpClient *http.Client
}

// NewGateway constructs a new webhook-only ingress gateway pointing to the specified local API target.
func NewGateway(targetBaseURL string) (*Gateway, error) {
	if targetBaseURL == "" {
		targetBaseURL = DefaultTargetURL
	}

	parsed, err := url.Parse(targetBaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	// Security Rule: Target host must strictly be a loopback address
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("%w: got host %q", ErrForbiddenTarget, host)
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	return &Gateway{
		targetURL:  parsed,
		httpClient: client,
	}, nil
}

// ServeHTTP enforces exact route allowlisting and forwards valid webhook requests to the local API.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// 1. Strict Path Allowlist: exact match required
	if r.URL.Path != AllowedPath {
		writeError(w, http.StatusNotFound, "not found: route is not allowed on webhook gateway", "ROUTE_NOT_ALLOWED")
		return
	}

	// 2. Strict Method Allowlist: only POST is allowed
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: webhook gateway only accepts POST", "METHOD_NOT_ALLOWED")
		return
	}

	// 3. Strict Query String Rejection: query parameters are strictly forbidden
	if r.URL.RawQuery != "" || strings.Contains(r.RequestURI, "?") || len(r.URL.Query()) > 0 {
		writeError(w, http.StatusBadRequest, "query strings are not permitted on webhook ingestion", "QUERY_STRING_FORBIDDEN")
		return
	}

	// 4. Enforce Maximum Request Body Size (512 KB)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body exceeds 512KB limit or failed reading", "PAYLOAD_TOO_LARGE")
		return
	}

	// 5. Construct Target Request (Strictly fixed destination: targetBaseURL + AllowedPath)
	targetEndpoint := fmt.Sprintf("%s%s", strings.TrimRight(g.targetURL.String(), "/"), AllowedPath)
	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed preparing upstream request", "INTERNAL_GATEWAY_ERROR")
		return
	}

	// 6. Preserve Headers Safely
	// Preserve Content-Type
	if ct := r.Header.Get("Content-Type"); ct != "" {
		outReq.Header.Set("Content-Type", ct)
	}

	// Preserve PayPal signature headers
	for k, v := range r.Header {
		if strings.HasPrefix(strings.ToUpper(k), "PAYPAL-") {
			for _, val := range v {
				outReq.Header.Add(k, val)
			}
		}
	}

	// 7. Forward to Local API
	resp, err := g.httpClient.Do(outReq)
	if err != nil {
		slog.ErrorContext(r.Context(), "Upstream API request failed", slog.String("error", err.Error()))
		writeError(w, http.StatusBadGateway, "upstream API server unavailable", "UPSTREAM_UNAVAILABLE")
		return
	}
	defer resp.Body.Close()

	// 8. Copy Upstream Response Headers & Status
	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	// 9. Safe Structured Logging (Zero Sensitive Payload or Signature Exposure)
	slog.InfoContext(r.Context(), "Webhook Gateway Ingress Forwarded",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int("body_bytes", len(bodyBytes)),
		slog.Duration("duration", time.Since(start)),
	)
}

func writeError(w http.ResponseWriter, code int, message, errCode string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
		"code":  errCode,
	})
}
