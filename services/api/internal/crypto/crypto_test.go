package crypto

import (
	"regexp"
	"strings"
	"testing"
)

func init() {
	SetFastTestMode()
}

func TestPasswordHashingAndVerification(t *testing.T) {
	password := "Correct-Horse-Battery-Staple-99!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("expected argon2id PHC prefix, got: %s", hash)
	}

	// Verify with correct password
	valid, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("unexpected error verifying valid password: %v", err)
	}
	if !valid {
		t.Errorf("expected password to verify as true, got false")
	}

	// Verify with incorrect password
	invalid, err := VerifyPassword("Wrong-Password-Attempt-123", hash)
	if err != nil {
		t.Fatalf("unexpected error verifying wrong password: %v", err)
	}
	if invalid {
		t.Errorf("expected wrong password to verify as false, got true")
	}
}

func TestPasswordLengthConstraints(t *testing.T) {
	// Too short (< 8 chars)
	_, err := HashPassword("short")
	if err == nil {
		t.Errorf("expected error for short password, got nil")
	}

	// Too long (> 128 chars)
	tooLong := strings.Repeat("A", 129)
	_, err = HashPassword(tooLong)
	if err == nil {
		t.Errorf("expected error for overly long password, got nil")
	}
}

func TestGenerateOpaqueTokenAndHash(t *testing.T) {
	rawToken, hashHex, err := GenerateOpaqueToken()
	if err != nil {
		t.Fatalf("failed to generate opaque token: %v", err)
	}

	if len(rawToken) == 0 {
		t.Errorf("raw token is empty")
	}

	if len(hashHex) != 64 {
		t.Errorf("expected 64-char sha256 hex string, got len %d: %s", len(hashHex), hashHex)
	}

	// Verify HashToken produces identical output
	expectedHash := HashToken(rawToken)
	if hashHex != expectedHash {
		t.Errorf("hash mismatch: %s vs %s", hashHex, expectedHash)
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"User@Example.com", "user@example.com"},
		{"  Claimant.Test@DOMAIN.ORG  ", "claimant.test@domain.org"},
		{"user+tag@gmail.com", "user+tag@gmail.com"},
	}

	for _, tt := range tests {
		normalized := NormalizeEmail(tt.input)
		if normalized != tt.expected {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.input, normalized, tt.expected)
		}
	}
}

func TestGenerateUUID(t *testing.T) {
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	id1 := GenerateUUID()
	id2 := GenerateUUID()

	if !uuidRegex.MatchString(id1) {
		t.Errorf("id1 is not a valid v4 UUID: %s", id1)
	}
	if !uuidRegex.MatchString(id2) {
		t.Errorf("id2 is not a valid v4 UUID: %s", id2)
	}
	if id1 == id2 {
		t.Errorf("expected distinct UUIDs, got duplicates: %s", id1)
	}
}
