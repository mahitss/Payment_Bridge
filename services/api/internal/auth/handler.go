package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
)

type contextKey string

const (
	UserContextKey         contextKey = "auth_user"
	SessionContextKey      contextKey = "auth_session"
	OrganizationContextKey contextKey = "auth_org"
)

// Handler handles all HTTP requests for authentication and session lifecycle.
type Handler struct {
	cfg     *config.Config
	service *Service
}

func NewHandler(cfg *config.Config, service *Service) *Handler {
	return &Handler{
		cfg:     cfg,
		service: service,
	}
}

// RegisterRoutes binds authentication routes to a ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/register", h.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/verify-email", h.handleVerifyEmail)
	mux.HandleFunc("POST /api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", h.handleLogout)
	mux.HandleFunc("POST /api/v1/auth/forgot-password", h.handleForgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", h.handleResetPassword)
	mux.HandleFunc("GET /api/v1/auth/me", h.handleMe)
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var input RegisterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ip := extractClientIP(r)
	ua := r.UserAgent()

	out, err := h.service.Register(r.Context(), input, ip, ua)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrNameRequired),
			errors.Is(err, ErrPasswordTooShort), errors.Is(err, ErrPasswordTooLong):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			slog.ErrorContext(r.Context(), "Registration error", slog.String("error", err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to complete registration"})
		}
		return
	}

	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var input VerifyEmailInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ip := extractClientIP(r)
	ua := r.UserAgent()

	if err := h.service.VerifyEmail(r.Context(), input, ip, ua); err != nil {
		if errors.Is(err, ErrInvalidToken) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Verification token is invalid, expired, or has already been used.",
			})
			return
		}
		slog.ErrorContext(r.Context(), "Email verification error", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify email"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Email verified successfully! You may now log in.",
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var input LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ip := extractClientIP(r)
	ua := r.UserAgent()

	out, err := h.service.Login(r.Context(), input, ip, ua)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid email or password."})
		case errors.Is(err, ErrEmailNotVerified):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "Please verify your email address before logging in.",
				"code":  "EMAIL_NOT_VERIFIED",
			})
		case errors.Is(err, ErrAccountDisabled):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "This account has been disabled."})
		default:
			slog.ErrorContext(r.Context(), "Login error", slog.String("error", err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "authentication failure"})
		}
		return
	}

	// Set HTTP-Only Session Cookie
	SetSessionCookie(w, out.SessionToken, time.Now().Add(h.cfg.SessionTTL), h.cfg.SessionCookieSecure, h.cfg.SessionCookieName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "Login successful",
		"user":         out.User,
		"organization": out.Organization,
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := ExtractSessionToken(r, h.cfg.SessionCookieName)
	if token != "" {
		ip := extractClientIP(r)
		ua := r.UserAgent()
		_ = h.service.Logout(r.Context(), token, ip, ua)
	}

	ClearSessionCookie(w, h.cfg.SessionCookieSecure, h.cfg.SessionCookieName)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

func (h *Handler) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var input ForgotPasswordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ip := extractClientIP(r)
	ua := r.UserAgent()

	_ = h.service.ForgotPassword(r.Context(), input, ip, ua)

	// Always return generic success to prevent account enumeration
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If an account matches that email address, password reset instructions have been sent.",
	})
}

func (h *Handler) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var input ResetPasswordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ip := extractClientIP(r)
	ua := r.UserAgent()

	if err := h.service.ResetPassword(r.Context(), input, ip, ua); err != nil {
		switch {
		case errors.Is(err, ErrInvalidToken):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reset link is invalid, expired, or has already been used."})
		case errors.Is(err, ErrPasswordTooShort), errors.Is(err, ErrPasswordTooLong):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			slog.ErrorContext(r.Context(), "Password reset error", slog.String("error", err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reset password"})
		}
		return
	}

	// Clear any residual session cookies
	ClearSessionCookie(w, h.cfg.SessionCookieSecure, h.cfg.SessionCookieName)

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password updated successfully. All previous sessions have been logged out. Please sign in with your new password.",
	})
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	user, okUser := r.Context().Value(UserContextKey).(*db.User)
	org, _ := r.Context().Value(OrganizationContextKey).(*db.Organization)

	if !okUser || user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user": SafeUser{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			EmailVerified: user.EmailVerified,
			CreatedAt:     user.CreatedAt,
		},
		"organization": org,
	})
}

// Middleware

// Authenticate extracts session cookie and populates context with user and organization.
func (h *Handler) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ExtractSessionToken(r, h.cfg.SessionCookieName)
		if token != "" {
			sess, user, org, err := h.service.ValidateSession(r.Context(), token)
			if err == nil && user != nil {
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				ctx = context.WithValue(ctx, SessionContextKey, sess)
				if org != nil {
					ctx = context.WithValue(ctx, OrganizationContextKey, org)
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// RequireAuth blocks unauthenticated requests.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(UserContextKey).(*db.User)
		if !ok || user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// OriginCheckMiddleware enforces Origin validation for state-changing requests.
func (h *Handler) OriginCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				for _, ao := range h.cfg.AllowedOrigins {
					if strings.EqualFold(origin, ao) {
						allowed = true
						break
					}
				}
				if !allowed {
					slog.WarnContext(r.Context(), "Origin rejected by CSRF/CORS check",
						slog.String("origin", origin),
						slog.String("path", r.URL.Path),
					)
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "Cross-origin request rejected"})
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Helper Functions

func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter, secure bool, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ExtractSessionToken(r *http.Request, cookieName string) string {
	cookie, err := r.Cookie(cookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Fallback to Bearer token header if present
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}

	return ""
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	return r.RemoteAddr
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
