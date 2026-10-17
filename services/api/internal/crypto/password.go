package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidHashFormat = errors.New("invalid Argon2id hash format")
	ErrIncompatibleArgon = errors.New("incompatible Argon2id version")
)

// Argon2idParams holds parameters for password hashing.
type Argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2idParams represents safe production defaults for interactive login.
var DefaultArgon2idParams = &Argon2idParams{
	Memory:      64 * 1024, // 64 MiB
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// FastTestArgon2idParams is tuned for fast automated unit test suites.
var FastTestArgon2idParams = &Argon2idParams{
	Memory:      16 * 1024, // 16 MiB
	Iterations:  1,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

var activeParams = DefaultArgon2idParams

// SetFastTestMode configures the hasher for fast test execution.
func SetFastTestMode() {
	activeParams = FastTestArgon2idParams
}

// HashPassword hashes a plain text password using Argon2id.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters long")
	}
	if len(password) > 128 {
		return "", errors.New("password cannot exceed 128 characters")
	}

	salt := make([]byte, activeParams.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		activeParams.Iterations,
		activeParams.Memory,
		activeParams.Parallelism,
		activeParams.KeyLength,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		activeParams.Memory,
		activeParams.Iterations,
		activeParams.Parallelism,
		b64Salt,
		b64Hash,
	)

	return encoded, nil
}

// VerifyPassword verifies a password against an encoded Argon2id hash in constant time.
func VerifyPassword(password, encodedHash string) (bool, error) {
	vals := strings.Split(encodedHash, "$")
	if len(vals) != 6 {
		return false, ErrInvalidHashFormat
	}

	if vals[1] != "argon2id" {
		return false, ErrInvalidHashFormat
	}

	var version int
	_, err := fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil || version != argon2.Version {
		return false, ErrIncompatibleArgon
	}

	params := strings.Split(vals[3], ",")
	if len(params) != 3 {
		return false, ErrInvalidHashFormat
	}

	var memory, iterations uint32
	var parallelism uint8

	for _, p := range params {
		kv := strings.Split(p, "=")
		if len(kv) != 2 {
			return false, ErrInvalidHashFormat
		}
		switch kv[0] {
		case "m":
			m, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return false, ErrInvalidHashFormat
			}
			memory = uint32(m)
		case "t":
			t, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return false, ErrInvalidHashFormat
			}
			iterations = uint32(t)
		case "p":
			pVal, err := strconv.ParseUint(kv[1], 10, 8)
			if err != nil {
				return false, ErrInvalidHashFormat
			}
			parallelism = uint8(pVal)
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(vals[4])
	if err != nil {
		return false, ErrInvalidHashFormat
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(vals[5])
	if err != nil {
		return false, ErrInvalidHashFormat
	}

	actualHash := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memory,
		parallelism,
		uint32(len(expectedHash)),
	)

	if subtle.ConstantTimeCompare(actualHash, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}

// Pre-computed dummy hash to run constant-time comparisons when email does not exist.
var dummyHashOnce = "$argon2id$v=19$m=16384,t=1,p=2$bW9ja3NhbHQxMjM0NTY3OA$9W/k4f796iVdE6ZkJwS2t1O0a4o"

// DummyVerifyPassword performs a constant-time hash comparison to defeat account enumeration timing attacks.
func DummyVerifyPassword(password string) {
	_, _ = VerifyPassword(password, dummyHashOnce)
}
