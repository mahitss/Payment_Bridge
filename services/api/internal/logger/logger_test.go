package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSanitizeText_BearerTokens(t *testing.T) {
	input := "Request failed with header Authorization: Bearer A21AAL_9876543210verySecretTokenValue"
	output := SanitizeText(input)

	if strings.Contains(output, "A21AAL_9876543210verySecretTokenValue") {
		t.Errorf("SanitizeText failed to redact Bearer token: %s", output)
	}
	if !strings.Contains(output, "Bearer [REDACTED_ACCESS_TOKEN]") {
		t.Errorf("Expected Bearer redaction placeholder, got: %s", output)
	}
}

func TestSanitizeText_JsonAccessToken(t *testing.T) {
	input := `{"scope": "openid", "access_token": "A21AAMockToken123456", "token_type": "Bearer"}`
	output := SanitizeText(input)

	if strings.Contains(output, "A21AAMockToken123456") {
		t.Errorf("SanitizeText leaked JSON access_token: %s", output)
	}
	if !strings.Contains(output, `"access_token": "[REDACTED]"`) {
		t.Errorf("Expected JSON access_token redaction, got: %s", output)
	}
}

func TestInitLogger_StructuredRedaction(t *testing.T) {
	buf := &bytes.Buffer{}
	l := InitLogger("info", buf)

	l.InfoContext(context.Background(), "Dispatching PayPal call",
		slog.String("client_secret", "SuperSecret123"),
		slog.String("access_token", "Bearer A21AAToken"),
		slog.String("safe_key", "public_value"),
	)

	logOutput := buf.String()

	if strings.Contains(logOutput, "SuperSecret123") {
		t.Errorf("Logger leaked client_secret attribute: %s", logOutput)
	}
	if strings.Contains(logOutput, "A21AAToken") {
		t.Errorf("Logger leaked access_token attribute: %s", logOutput)
	}
	if !strings.Contains(logOutput, "public_value") {
		t.Errorf("Logger dropped safe attribute: %s", logOutput)
	}
	if !strings.Contains(logOutput, "[REDACTED]") {
		t.Errorf("Logger output missing [REDACTED] tag: %s", logOutput)
	}
}
