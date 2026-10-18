package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/investigations"
	"claimbridge/services/api/internal/payouts"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

// Server encapsulates the HTTP handler and service dependencies.
type Server struct {
	cfg                   *config.Config
	repo                  db.Repository
	policyEngine          policy.PolicyEngine
	paypalClient          *paypal.Client
	authHandler           *auth.Handler
	payoutsHandler        *payouts.Handler
	investigationsHandler *investigations.Handler
	mux                   *http.ServeMux
}

// NewServer constructs the HTTP server with router, auth handlers, and middleware.
func NewServer(cfg *config.Config, policyEngine policy.PolicyEngine, paypalClient *paypal.Client, authHandler *auth.Handler, payoutsHandlers ...*payouts.Handler) *Server {
	var ph *payouts.Handler
	if len(payoutsHandlers) > 0 {
		ph = payoutsHandlers[0]
	}

	s := &Server{
		cfg:            cfg,
		policyEngine:   policyEngine,
		paypalClient:   paypalClient,
		authHandler:    authHandler,
		payoutsHandler: ph,
		mux:            http.NewServeMux(),
	}

	s.registerRoutes()
	return s
}

// SetRepository configures the database repository for readiness probes and storage health checks.
func (s *Server) SetRepository(repo db.Repository) {
	s.repo = repo
}

// SetInvestigationsHandler attaches the AI investigations handler and registers its routes.
func (s *Server) SetInvestigationsHandler(ih *investigations.Handler) {
	s.investigationsHandler = ih
	if ih != nil {
		ih.RegisterRoutes(s.mux)
	}
}

// Handler returns the root HTTP handler wrapped in standard middleware.
func (s *Server) Handler() http.Handler {
	var handler http.Handler = s.mux

	if s.authHandler != nil {
		handler = s.authHandler.Authenticate(handler)
		handler = s.authHandler.OriginCheckMiddleware(handler)
	}

	return s.recoveryMiddleware(
		s.securityHeadersMiddleware(
			s.corsMiddleware(
				s.bodyLimitMiddleware(
					s.loggingMiddleware(
						s.timeoutMiddleware(handler),
					),
				),
			),
		),
	)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("POST /api/v1/policy/evaluate", s.handlePolicyEvaluate)
	s.mux.HandleFunc("GET /api/v1/sandbox/status", s.handleSandboxStatus)

	if s.authHandler != nil {
		s.authHandler.RegisterRoutes(s.mux)
	}

	if s.payoutsHandler != nil {
		s.payoutsHandler.RegisterRoutes(s.mux)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"service":   "claimbridge-api",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	hasPayPalCreds := s.cfg.PayPalClientID != "" && s.cfg.PayPalClientSecret != ""
	dbStatus := "connected"
	isReady := true

	if s.repo != nil {
		if err := s.repo.Ping(r.Context()); err != nil {
			dbStatus = "unavailable"
			isReady = false
		}
	}

	statusCode := http.StatusOK
	statusStr := "ready"
	if !isReady {
		statusCode = http.StatusServiceUnavailable
		statusStr = "degraded"
	}

	writeJSON(w, statusCode, map[string]interface{}{
		"status":                 statusStr,
		"service":                "claimbridge-api",
		"database":               dbStatus,
		"paypal_env":             s.cfg.PayPalEnv,
		"paypal_creds_available": hasPayPalCreds,
		"paypal_base_url":        s.cfg.PayPalBaseURL,
		"timestamp":              time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handlePolicyEvaluate(w http.ResponseWriter, r *http.Request) {
	var req policy.EvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "malformed JSON request payload",
		})
		return
	}

	resp, err := s.policyEngine.Evaluate(r.Context(), req)
	if err != nil {
		slog.ErrorContext(r.Context(), "Policy evaluation failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal policy evaluation error",
		})
		return
	}

	statusCode := http.StatusOK
	if resp.Decision == policy.DecisionDenied {
		statusCode = http.StatusForbidden
	}

	writeJSON(w, statusCode, resp)
}

func (s *Server) handleSandboxStatus(w http.ResponseWriter, r *http.Request) {
	if s.paypalClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "PayPal client not initialized",
		})
		return
	}

	token, err := s.paypalClient.GetAccessToken(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "PayPal sandbox status check failed", slog.String("error", err.Error()))
		statusMsg := "Authentication failed"
		if errors.Is(err, paypal.ErrMissingCredentials) {
			statusMsg = "Credentials not set in environment (PAYPAL_CLIENT_ID / PAYPAL_CLIENT_SECRET missing)"
		} else if errors.Is(err, paypal.ErrUnauthorized) {
			statusMsg = "Invalid credentials rejected by PayPal sandbox"
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status":   "disconnected",
			"error":    statusMsg,
			"endpoint": s.cfg.PayPalBaseURL,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "authenticated",
		"token_valid":  len(token) > 0,
		"paypal_env":   s.cfg.PayPalEnv,
		"endpoint":     s.cfg.PayPalBaseURL,
		"access_token": "[REDACTED_ACCESS_TOKEN]",
	})
}

// Middleware

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, allowed := range s.cfg.AllowedOrigins {
				if strings.EqualFold(origin, allowed) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
					break
				}
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) timeoutMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeout := s.cfg.PayPalRequestTimeout
		if strings.Contains(r.URL.Path, "/investigations") {
			timeout = s.cfg.AIRequestTimeout
			if timeout < 30*time.Second {
				timeout = 30 * time.Second
			}
		}
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		slog.InfoContext(r.Context(), "HTTP Request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.statusCode),
			slog.Duration("duration", duration),
			slog.String("remote_addr", r.RemoteAddr),
		)
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "Panic recovered in HTTP handler",
					slog.Any("error", rec),
					slog.String("stack", string(debug.Stack())),
				)
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1MB maximum payload
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none';")
		next.ServeHTTP(w, r)
	})
}

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
