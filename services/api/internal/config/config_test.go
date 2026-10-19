package config

import (
	"os"
	"strings"
	"testing"
)

func cleanEnv(t *testing.T) {
	orig := os.Environ()
	tmp := os.Getenv("TMP")
	temp := os.Getenv("TEMP")
	sysRoot := os.Getenv("SystemRoot")

	os.Clearenv()
	if tmp != "" {
		os.Setenv("TMP", tmp)
	}
	if temp != "" {
		os.Setenv("TEMP", temp)
	}
	if sysRoot != "" {
		os.Setenv("SystemRoot", sysRoot)
	}

	t.Cleanup(func() {
		os.Clearenv()
		for _, e := range orig {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				os.Setenv(parts[0], parts[1])
			}
		}
	})
}

func TestLoadFromEnv_DefaultSandbox(t *testing.T) {
	cleanEnv(t)

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
	cleanEnv(t)
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
	cleanEnv(t)
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
	cleanEnv(t)
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

func TestValidatePayPalSandboxEndpoint(t *testing.T) {
	// Valid variations
	valid := []string{
		"https://api-m.sandbox.paypal.com",
		"https://api-m.sandbox.paypal.com/",
		"  https://api-m.sandbox.paypal.com  ",
	}
	for _, v := range valid {
		if err := ValidatePayPalSandboxEndpoint(v); err != nil {
			t.Errorf("expected valid URL %q to pass, got: %v", v, err)
		}
	}

	// Invalid variations
	invalid := []struct {
		url     string
		desc    string
		errType error
	}{
		{"", "empty URL", ErrInvalidSandboxEndpoint},
		{"https://api.paypal.com", "production host api.paypal.com", ErrProductionHostBlocked},
		{"https://api-m.paypal.com", "production host api-m.paypal.com", ErrProductionHostBlocked},
		{"https://www.paypal.com", "production host www.paypal.com", ErrProductionHostBlocked},
		{"https://attacker.com", "arbitrary host", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com.evil.com", "subdomain takeover attempt", ErrInvalidSandboxEndpoint},
		{"http://api-m.sandbox.paypal.com", "plain HTTP scheme", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com:8443", "unexpected port 8443", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com:80", "port 80", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com/v1/payments", "unexpected path", ErrInvalidSandboxEndpoint},
		{"https://user:pass@api-m.sandbox.paypal.com", "embedded userinfo", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com?query=1", "query parameters", ErrInvalidSandboxEndpoint},
		{"https://api-m.sandbox.paypal.com#fragment", "fragment identifier", ErrInvalidSandboxEndpoint},
	}

	for _, tc := range invalid {
		err := ValidatePayPalSandboxEndpoint(tc.url)
		if err == nil {
			t.Errorf("expected error for %s (%q), got nil", tc.desc, tc.url)
		}
	}
}

func TestResolveAndLoadEnvFiles(t *testing.T) {
	// Create temporary env file using t.TempDir()
	tempDir := t.TempDir()
	tmpFile, err := os.CreateTemp(tempDir, "claimbridge-test-*.env")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "# Test Env\nTEST_ENV_VAR_1=hello\nTEST_ENV_VAR_2=\"world\"\n"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed writing temp env file: %v", err)
	}
	tmpFile.Close()

	os.Unsetenv("TEST_ENV_VAR_1")
	os.Unsetenv("TEST_ENV_VAR_2")

	resolved, err := LoadEnvFiles(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadEnvFiles failed: %v", err)
	}
	if resolved != tmpFile.Name() {
		t.Errorf("expected resolved path %q, got %q", tmpFile.Name(), resolved)
	}

	if val := os.Getenv("TEST_ENV_VAR_1"); val != "hello" {
		t.Errorf("expected TEST_ENV_VAR_1 to be 'hello', got %q", val)
	}
	if val := os.Getenv("TEST_ENV_VAR_2"); val != "world" {
		t.Errorf("expected TEST_ENV_VAR_2 to be 'world' (quotes stripped), got %q", val)
	}
}
