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
	AppEnv                string
	Port                  string
	LogLevel              string
	DatabaseURL           string
	PayPalEnv             string
	PayPalClientID        string
	PayPalClientSecret    string
	PayPalBaseURL         string
	PayPalRequestTimeout  time.Duration
	PolicyEngineBinaryPath string
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

	cfg := &Config{
		AppEnv:                 appEnv,
		Port:                   getEnv("PORT", "8080"),
		LogLevel:               strings.ToLower(getEnv("LOG_LEVEL", "info")),
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://claimbridge:claimbridge_local_dev_only@localhost:5432/claimbridge_dev?sslmode=disable"),
		PayPalEnv:              payPalEnv,
		PayPalClientID:         os.Getenv("PAYPAL_CLIENT_ID"),
		PayPalClientSecret:     os.Getenv("PAYPAL_CLIENT_SECRET"),
		PayPalBaseURL:          strings.TrimRight(payPalBaseURL, "/"),
		PayPalRequestTimeout:   time.Duration(timeoutSec) * time.Second,
		PolicyEngineBinaryPath: getEnv("POLICY_ENGINE_BIN", ""),
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

	redactedDB := redactDatabaseURL(c.DatabaseURL)

	return fmt.Sprintf(
		"Config{AppEnv: %s, Port: %s, LogLevel: %s, DatabaseURL: %s, PayPalEnv: %s, PayPalClientID: %s, PayPalSecret: %s, PayPalBaseURL: %s, Timeout: %s}",
		c.AppEnv, c.Port, c.LogLevel, redactedDB, c.PayPalEnv, maskedClientID, maskedSecret, c.PayPalBaseURL, c.PayPalRequestTimeout,
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
