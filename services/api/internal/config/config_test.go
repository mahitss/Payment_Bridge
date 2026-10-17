package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadFromEnv_DefaultSandbox(t *testing.T) {
	os.Clearenv()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if cfg.PayPalEnv != "sandbox" {
		t.Errorf("expected default PayPalEnv to be sandbox, got: %s", cfg.PayPalEnv)
	}

	if cfg.Port != "8080" {
		t.Errorf("expected default Port to be 8080, got: %s", cfg.Port)
	}

	if !strings.Contains(cfg.PayPalBaseURL, "sandbox.paypal.com") {
		t.Errorf("expected sandbox PayPalBaseURL, got: %s", cfg.PayPalBaseURL)
	}
}

func TestLoadFromEnv_RejectProductionAppEnv(t *testing.T) {
	os.Clearenv()
	os.Setenv("CLAIMBRIDGE_ENV", "production")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when CLAIMBRIDGE_ENV=production, got nil")
	}

	if !strings.Contains(err.Error(), "production environment is forbidden") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadFromEnv_RejectProductionPayPalEnv(t *testing.T) {
	os.Clearenv()
	os.Setenv("PAYPAL_ENV", "production")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when PAYPAL_ENV=production, got nil")
	}

	if !strings.Contains(err.Error(), "PAYPAL_ENV cannot be 'production'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadFromEnv_RejectProductionPayPalHost(t *testing.T) {
	os.Clearenv()
	os.Setenv("PAYPAL_SANDBOX_BASE_URL", "https://api-m.paypal.com")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when PAYPAL_SANDBOX_BASE_URL is a production host, got nil")
	}

	if !strings.Contains(err.Error(), "production host") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_RedactsSecretsInString(t *testing.T) {
	cfg := &Config{
		AppEnv:             "development",
		Port:               "8080",
		LogLevel:           "info",
		DatabaseURL:        "postgres://claimbridge:super_secret_db_pass@localhost:5432/claimbridge_dev",
		PayPalEnv:          "sandbox",
		PayPalClientID:     "AZ1234567890CLIENTID",
		PayPalClientSecret: "SECRET_VALUE_NEVER_LOG",
		PayPalBaseURL:      "https://api-m.sandbox.paypal.com",
	}

	str := cfg.String()

	if strings.Contains(str, "super_secret_db_pass") {
		t.Errorf("String() leaked database password: %s", str)
	}
	if strings.Contains(str, "SECRET_VALUE_NEVER_LOG") {
		t.Errorf("String() leaked PayPal secret: %s", str)
	}
	if !strings.Contains(str, "[REDACTED]") {
		t.Errorf("String() missing expected redacted placeholder: %s", str)
	}
}

func TestConfig_ValidatePayPalCredentials(t *testing.T) {
	cfg := &Config{
		PayPalEnv:          "sandbox",
		PayPalBaseURL:      "https://api-m.sandbox.paypal.com",
		PayPalClientID:     "",
		PayPalClientSecret: "",
	}

	if err := cfg.ValidatePayPalCredentials(); err != ErrMissingPayPalClientID {
		t.Errorf("expected ErrMissingPayPalClientID, got: %v", err)
	}

	cfg.PayPalClientID = "some_client_id"
	if err := cfg.ValidatePayPalCredentials(); err != ErrMissingPayPalSecret {
		t.Errorf("expected ErrMissingPayPalSecret, got: %v", err)
	}

	cfg.PayPalClientSecret = "some_client_secret"
	if err := cfg.ValidatePayPalCredentials(); err != nil {
		t.Errorf("expected valid credentials to pass, got: %v", err)
	}
}
