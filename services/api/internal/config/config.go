package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ErrProductionBlocked     = errors.New("security violation: production environment is forbidden in Phase 1")
	ErrProductionHostBlocked = errors.New("security violation: production PayPal host detected; only sandbox hosts are permitted")
	ErrMissingPayPalClientID = errors.New("configuration error: PAYPAL_CLIENT_ID is required for PayPal operations")
	ErrMissingPayPalSecret   = errors.New("configuration error: PAYPAL_CLIENT_SECRET is required for PayPal operations")
)

// Config holds all validated application configuration.
type Config struct {
	AppEnv                 string
	Port                   string
	LogLevel               string
	DatabaseURL            string
	AppBaseURL             string
	APIBaseURL             string
	AllowedOrigins         []string
	SessionCookieName      string
	SessionCookieSecure    bool
	SessionTTL             time.Duration
	EmailVerificationTTL   time.Duration
	PasswordResetTTL       time.Duration
	RequireEmailVerify     bool
	SMTPHost               string
	SMTPPort               int
	SMTPFrom               string
	SMTPUser               string
	SMTPPass               string
	PayPalEnv              string
	PayPalClientID         string
	PayPalClientSecret     string
	PayPalBaseURL          string
	PayPalRequestTimeout   time.Duration
	PolicyEngineBinaryPath string
	AIEnabled              bool
	AIProvider             string
	AIBaseURL              string
	AIModel                string
	AIAPIKey               string
	AIRequestTimeout       time.Duration
	AIMaxOutputTokens      int
}

// LoadFromEnv loads configuration from environment variables with strict validation.
func LoadFromEnv() (*Config, error) {
	appEnv := getEnv("CLAIMBRIDGE_ENV", "development")
	if strings.EqualFold(appEnv, "production") {
		return nil, fmt.Errorf("%w: CLAIMBRIDGE_ENV cannot be 'production' in this milestone", ErrProductionBlocked)
	}

	payPalEnv := strings.ToLower(getEnv("PAYPAL_ENV", "sandbox"))
	if payPalEnv == "production" {
		return nil, fmt.Errorf("%w: PAYPAL_ENV cannot be 'production' in this milestone", ErrProductionBlocked)
	}

	payPalBaseURL := getEnv("PAYPAL_SANDBOX_BASE_URL", "https://api-m.sandbox.paypal.com")
	if isProductionPayPalHost(payPalBaseURL) {
		return nil, fmt.Errorf("%w: %s is a production host", ErrProductionHostBlocked, payPalBaseURL)
	}

	timeoutSec := getEnvInt("PAYPAL_REQUEST_TIMEOUT_SECONDS", 15)
	if timeoutSec <= 0 || timeoutSec > 60 {
		timeoutSec = 15
	}

	// Session & Token TTLs
	sessionTTLHours := getEnvInt("SESSION_TTL_HOURS", 72)
	emailTTLHours := getEnvInt("EMAIL_VERIFICATION_TTL_HOURS", 24)
	resetTTLMinutes := getEnvInt("PASSWORD_RESET_TTL_MINUTES", 30)

	allowedOriginsRaw := getEnv("ALLOWED_ORIGINS", "http://localhost:3000")
	var allowedOrigins []string
	for _, o := range strings.Split(allowedOriginsRaw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowedOrigins = append(allowedOrigins, o)
		}
	}

	cookieSecure := getEnvBool("SESSION_COOKIE_SECURE", false)
	cookieName := getEnv("SESSION_COOKIE_NAME", "claimbridge_session")
	if appEnv == "production" && !strings.HasPrefix(cookieName, "__Host-") {
		cookieName = "__Host-" + cookieName
		cookieSecure = true
	}

	cfg := &Config{
		AppEnv:                 appEnv,
		Port:                   getEnv("PORT", "8080"),
		LogLevel:               strings.ToLower(getEnv("LOG_LEVEL", "info")),
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://claimbridge:claimbridge_local_dev_only@localhost:5432/claimbridge_dev?sslmode=disable"),
		AppBaseURL:             strings.TrimRight(getEnv("APP_BASE_URL", "http://localhost:3000"), "/"),
		APIBaseURL:             strings.TrimRight(getEnv("API_BASE_URL", "http://localhost:8080"), "/"),
		AllowedOrigins:         allowedOrigins,
		SessionCookieName:      cookieName,
		SessionCookieSecure:    cookieSecure,
		SessionTTL:             time.Duration(sessionTTLHours) * time.Hour,
		EmailVerificationTTL:   time.Duration(emailTTLHours) * time.Hour,
		PasswordResetTTL:       time.Duration(resetTTLMinutes) * time.Minute,
		RequireEmailVerify:     getEnvBool("REQUIRE_EMAIL_VERIFICATION", true),
		SMTPHost:               getEnv("SMTP_HOST", "localhost"),
		SMTPPort:               getEnvInt("SMTP_PORT", 1025),
		SMTPFrom:               getEnv("SMTP_FROM", "noreply@claimbridge.local"),
		SMTPUser:               os.Getenv("SMTP_USER"),
		SMTPPass:               os.Getenv("SMTP_PASS"),
		PayPalEnv:              payPalEnv,
		PayPalClientID:         os.Getenv("PAYPAL_CLIENT_ID"),
		PayPalClientSecret:     os.Getenv("PAYPAL_CLIENT_SECRET"),
		PayPalBaseURL:          strings.TrimRight(payPalBaseURL, "/"),
		PayPalRequestTimeout:   time.Duration(timeoutSec) * time.Second,
		PolicyEngineBinaryPath: getEnv("POLICY_ENGINE_BIN", ""),
		AIEnabled:              getEnvBool("AI_ENABLED", true),
		AIProvider:             strings.ToLower(getEnv("AI_PROVIDER", "ollama")),
		AIBaseURL:              strings.TrimRight(getEnv("AI_BASE_URL", "http://127.0.0.1:11434"), "/"),
		AIModel:                getEnv("AI_MODEL", "llama3.2"),
		AIAPIKey:               os.Getenv("AI_API_KEY"),
		AIRequestTimeout:       time.Duration(getEnvInt("AI_REQUEST_TIMEOUT_SECONDS", 30)) * time.Second,
		AIMaxOutputTokens:      getEnvInt("AI_MAX_OUTPUT_TOKENS", 2048),
	}

	// Validate AI Base URL
	if cfg.AIEnabled {
		u, err := url.Parse(cfg.AIBaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("configuration error: AI_BASE_URL must be a valid http or https URL, got %q", cfg.AIBaseURL)
		}
	}

	return cfg, nil
}

// ValidatePayPalCredentials checks if PayPal credentials are provided and non-empty.
func (c *Config) ValidatePayPalCredentials() error {
	if strings.TrimSpace(c.PayPalClientID) == "" {
		return ErrMissingPayPalClientID
	}
	if strings.TrimSpace(c.PayPalClientSecret) == "" {
		return ErrMissingPayPalSecret
	}
	if c.PayPalEnv != "sandbox" {
		return ErrProductionBlocked
	}
	if isProductionPayPalHost(c.PayPalBaseURL) {
		return ErrProductionHostBlocked
	}
	return nil
}

// String returns a redacted, safe representation of the config for logging.
func (c *Config) String() string {
	maskedSecret := "[NOT SET]"
	if len(c.PayPalClientSecret) > 0 {
		maskedSecret = "[REDACTED]"
	}

	maskedClientID := "[NOT SET]"
	if len(c.PayPalClientID) > 4 {
		maskedClientID = c.PayPalClientID[:4] + "...[REDACTED]"
	} else if len(c.PayPalClientID) > 0 {
		maskedClientID = "[REDACTED]"
	}

	maskedAIKey := "[NOT SET]"
	if len(c.AIAPIKey) > 0 {
		maskedAIKey = "[REDACTED]"
	}

	redactedDB := redactDatabaseURL(c.DatabaseURL)

	return fmt.Sprintf(
		"Config{AppEnv: %s, Port: %s, LogLevel: %s, DatabaseURL: %s, AppBaseURL: %s, AllowedOrigins: %v, SessionTTL: %s, SMTP: %s:%d, PayPalEnv: %s, PayPalClientID: %s, PayPalSecret: %s, PayPalBaseURL: %s, Timeout: %s, AIEnabled: %t, AIProvider: %s, AIBaseURL: %s, AIModel: %s, AIKey: %s}",
		c.AppEnv, c.Port, c.LogLevel, redactedDB, c.AppBaseURL, c.AllowedOrigins, c.SessionTTL, c.SMTPHost, c.SMTPPort, c.PayPalEnv, maskedClientID, maskedSecret, c.PayPalBaseURL, c.PayPalRequestTimeout, c.AIEnabled, c.AIProvider, c.AIBaseURL, c.AIModel, maskedAIKey,
	)
}

func isProductionPayPalHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "api.paypal.com" || host == "api-m.paypal.com" || host == "www.paypal.com"
}

func redactDatabaseURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[REDACTED_DB_URL]"
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "REDACTED")
	}
	return u.String()
}

func getEnv(key, defaultVal string) string {
	val := os.Getenv(key)
	if strings.TrimSpace(val) == "" {
		return defaultVal
	}
	return val
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return b
}
