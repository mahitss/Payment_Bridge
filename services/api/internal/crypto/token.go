package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateOpaqueToken generates 32 bytes of cryptographically secure random bytes
// and returns both the URL-safe raw token (for the client/cookie/email) and the SHA-256
// hex hash (for persistent database storage).
func GenerateOpaqueToken() (rawToken string, hashHex string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random token bytes: %w", err)
	}

	rawToken = base64.RawURLEncoding.EncodeToString(bytes)
	hashHex = HashToken(rawToken)
	return rawToken, hashHex, nil
}

// HashToken computes the SHA-256 hex string of a given raw token.
func HashToken(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(h[:])
}

// GenerateUUID generates a cryptographically secure RFC 4122 v4 UUID string.
func GenerateUUID() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16],
	)
}

// NormalizeEmail converts an email address to lowercase and strips leading/trailing spaces.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
