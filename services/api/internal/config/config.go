package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const OfficialPayPalSandboxBaseURL = "https://api-m.sandbox.paypal.com"

var (
	ErrProductionBlocked      = errors.New("security violation: production environment is forbidden in Phase 1")
	ErrProductionHostBlocked  = errors.New("security violation: production PayPal host detected; only sandbox hosts are permitted")
	ErrInvalidSandboxEndpoint = errors.New("security violation: invalid PayPal Sandbox endpoint; must be https://api-m.sandbox.paypal.com")
	ErrMissingPayPalClientID  = errors.New("configuration error: PAYPAL_CLIENT_ID is required for PayPal operations")
	ErrMissingPayPalSecret    = errors.New("configuration error: PAYPAL_CLIENT_SECRET is required for PayPal operations")
)

// Config holds all validated application configuration.
type Config struct {
	AppEnv                 string
	Port                   string
	LogLevel               string
	DatabaseURL            string
	DatabaseAutoMigrate    bool
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
	AIContextSize          int
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

	payPalBaseURL := getEnv("PAYPAL_SANDBOX_BASE_URL", OfficialPayPalSandboxBaseURL)
	if err := ValidatePayPalSandboxEndpoint(payPalBaseURL); err != nil {
		return nil, err
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

	aiContextSize := 4096
	if rawCtx := os.Getenv("AI_CONTEXT_SIZE"); rawCtx != "" {
		val, err := strconv.Atoi(rawCtx)
		if err != nil || val <= 0 {
			return nil, fmt.Errorf("configuration error: AI_CONTEXT_SIZE must be a positive integer, got %q", rawCtx)
		}
		aiContextSize = val
	}

	cfg := &Config{
		AppEnv:                 appEnv,
		Port:                   getEnv("PORT", "8080"),
		LogLevel:               strings.ToLower(getEnv("LOG_LEVEL", "info")),
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://claimbridge:claimbridge_local_dev_only@localhost:5432/claimbridge_dev?sslmode=disable"),
		DatabaseAutoMigrate:    getEnvBool("DATABASE_AUTOMIGRATE", true),
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
		PayPalBaseURL:          OfficialPayPalSandboxBaseURL,
		PayPalRequestTimeout:   time.Duration(timeoutSec) * time.Second,
		PolicyEngineBinaryPath: getEnv("POLICY_ENGINE_BIN", ""),
		AIEnabled:              getEnvBool("AI_ENABLED", true),
		AIProvider:             strings.ToLower(getEnv("AI_PROVIDER", "ollama")),
		AIBaseURL:              strings.TrimRight(getEnv("AI_BASE_URL", "http://127.0.0.1:11434"), "/"),
		AIModel:                getEnv("AI_MODEL", "llama3.2"),
		AIAPIKey:               os.Getenv("AI_API_KEY"),
		AIRequestTimeout:       time.Duration(getEnvInt("AI_REQUEST_TIMEOUT_SECONDS", 30)) * time.Second,
		AIMaxOutputTokens:      getEnvInt("AI_MAX_OUTPUT_TOKENS", 2048),
		AIContextSize:          aiContextSize,
	}

	// Validate AI Base URL
	if cfg.AIEnabled {
		u, err := url.Parse(cfg.AIBaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("configuration error: AI_BASE_URL must be a valid http or https URL, got %q", cfg.AIBaseURL)
		}
		if cfg.AIContextSize <= 0 {
			return nil, fmt.Errorf("configuration error: AI_CONTEXT_SIZE must be a positive integer, got %d", cfg.AIContextSize)
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
	if err := ValidatePayPalSandboxEndpoint(c.PayPalBaseURL); err != nil {
		return err
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
		"Config{AppEnv: %s, Port: %s, LogLevel: %s, DatabaseURL: %s, AppBaseURL: %s, AllowedOrigins: %v, SessionTTL: %s, SMTP: %s:%d, PayPalEnv: %s, PayPalClientID: %s, PayPalSecret: %s, PayPalBaseURL: %s, Timeout: %s, AIEnabled: %t, AIProvider: %s, AIBaseURL: %s, AIModel: %s, AIKey: %s, AIContextSize: %d}",
		c.AppEnv, c.Port, c.LogLevel, redactedDB, c.AppBaseURL, c.AllowedOrigins, c.SessionTTL, c.SMTPHost, c.SMTPPort, c.PayPalEnv, maskedClientID, maskedSecret, c.PayPalBaseURL, c.PayPalRequestTimeout, c.AIEnabled, c.AIProvider, c.AIBaseURL, c.AIModel, maskedAIKey, c.AIContextSize,
	)
}

// ValidatePayPalSandboxEndpoint validates scheme, host, port, and URL structure strictly for PayPal Sandbox.
func ValidatePayPalSandboxEndpoint(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("%w: endpoint URL cannot be empty", ErrInvalidSandboxEndpoint)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: malformed URL: %w", ErrInvalidSandboxEndpoint, err)
	}

	// 1. Embedded credentials/userinfo check
	if u.User != nil {
		return fmt.Errorf("%w: embedded credentials/userinfo not allowed", ErrInvalidSandboxEndpoint)
	}

	// 2. Reject production hosts
	host := strings.ToLower(u.Hostname())
	if host == "api.paypal.com" || host == "api-m.paypal.com" || host == "www.paypal.com" {
		return fmt.Errorf("%w: %s is a production host", ErrProductionHostBlocked, host)
	}

	// 3. Scheme must be strictly HTTPS
	if strings.ToLower(u.Scheme) != "https" {
		return fmt.Errorf("%w: scheme must be https, got %q", ErrInvalidSandboxEndpoint, u.Scheme)
	}

	// 4. Hostname must be strictly api-m.sandbox.paypal.com
	if host != "api-m.sandbox.paypal.com" {
		return fmt.Errorf("%w: host must be api-m.sandbox.paypal.com, got %q", ErrInvalidSandboxEndpoint, host)
	}

	// 5. Port must be empty or standard 443
	port := u.Port()
	if port != "" && port != "443" {
		return fmt.Errorf("%w: unexpected port %q; only standard HTTPS (443) permitted", ErrInvalidSandboxEndpoint, port)
	}

	// 6. Path must be empty or root "/"
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%w: unexpected URL path %q; base URL must not contain a path", ErrInvalidSandboxEndpoint, u.Path)
	}

	// 7. Query and fragment must be empty
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: query parameters or fragments not permitted", ErrInvalidSandboxEndpoint)
	}

	return nil
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

// ResolveEnvFile searches for a valid .env file starting with customPath,
// then probing candidate paths (.env, ../../.env, ../.env).
func ResolveEnvFile(customPath ...string) string {
	if len(customPath) > 0 && strings.TrimSpace(customPath[0]) != "" {
		p := strings.TrimSpace(customPath[0])
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
		return p // Return path even if missing so caller can report specific error if desired
	}

	candidates := []string{
		".env",
		"../../.env", // Relative to services/api
		"../.env",    // Direct parent
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// LoadEnvFiles parses key=value pairs from the resolved .env file and sets them
// in os.Setenv if they are not already set. Returns the resolved path or empty string.
func LoadEnvFiles(customPath ...string) (string, error) {
	resolved := ResolveEnvFile(customPath...)
	if resolved == "" {
		return "", nil
	}

	file, err := os.Open(resolved)
	if err != nil {
		return resolved, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			v = strings.Trim(v, `"'`)
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
	return resolved, scanner.Err()
}
