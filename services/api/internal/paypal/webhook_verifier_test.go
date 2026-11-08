package paypal

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"
)

func generateTestCertAndKey(t *testing.T, notBefore, notAfter time.Time) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1337),
		Subject: pkix.Name{
			CommonName:   "api.sandbox.paypal.com",
			Organization: []string{"PayPal, Inc."},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	return cert, privKey
}

func TestValidateCertURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid sandbox cert url",
			url:     "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-1234.pem",
			wantErr: false,
		},
		{
			name:    "valid api-m sandbox cert url",
			url:     "https://api-m.sandbox.paypal.com/v1/notifications/certs/CERT-1234.pem",
			wantErr: false,
		},
		{
			name:    "valid production cert url",
			url:     "https://api.paypal.com/v1/notifications/certs/CERT-1234.pem",
			wantErr: false,
		},
		{
			name:    "valid notify paypal cert url",
			url:     "https://notify.paypal.com/v1/notifications/certs/CERT-1234.pem",
			wantErr: false,
		},
		{
			name:    "reject insecure http",
			url:     "http://api.sandbox.paypal.com/v1/notifications/certs/CERT-1234.pem",
			wantErr: true,
		},
		{
			name:    "reject evil domain suffix",
			url:     "https://evil-paypal.com/certs/cert.pem",
			wantErr: true,
		},
		{
			name:    "reject arbitrary domain",
			url:     "https://attacker.example.com/cert.pem",
			wantErr: true,
		},
		{
			name:    "reject userinfo",
			url:     "https://user:pass@api.paypal.com/cert.pem",
			wantErr: true,
		},
		{
			name:    "reject non-standard port",
			url:     "https://api.paypal.com:8443/cert.pem",
			wantErr: true,
		},
		{
			name:    "reject path traversal",
			url:     "https://api.paypal.com/v1/notifications/certs/../../secret.pem",
			wantErr: true,
		},
		{
			name:    "reject empty url",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCertURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCertURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestWebhookVerifier_ValidSignature(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}

	verifier := NewWebhookVerifier(fetcher)

	transmissionID := "123e4567-e89b-12d3-a456-426614174000"
	transmissionTime := "2026-11-07T02:00:00Z"
	webhookID := "WH-TEST-WEBHOOK-ID-12345"
	rawBody := []byte(`{"id":"WH-EVENT-1","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed to sign payload: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	ctx := context.Background()
	if err := verifier.Verify(ctx, headers, webhookID, rawBody); err != nil {
		t.Fatalf("expected valid signature verification, got error: %v", err)
	}
}

func TestWebhookVerifier_InvalidSignature_TamperedBody(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}

	verifier := NewWebhookVerifier(fetcher)

	transmissionID := "trans-123"
	transmissionTime := "2026-11-07T02:00:00Z"
	webhookID := "WH-123"
	rawBody := []byte(`{"id":"WH-EVENT-1","status":"original"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed to sign payload: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	tamperedBody := []byte(`{"id":"WH-EVENT-1","status":"tampered"}`)
	ctx := context.Background()
	err = verifier.Verify(ctx, headers, webhookID, tamperedBody)
	if err == nil {
		t.Fatalf("expected signature verification failure for tampered body, got nil")
	}
}

func TestWebhookVerifier_InvalidSignature_WrongWebhookID(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}

	verifier := NewWebhookVerifier(fetcher)

	transmissionID := "trans-123"
	transmissionTime := "2026-11-07T02:00:00Z"
	actualWebhookID := "WH-ACTUAL-ID"
	differentWebhookID := "WH-DIFFERENT-ID"
	rawBody := []byte(`{"id":"WH-EVENT-1"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, actualWebhookID, rawBody)
	if err != nil {
		t.Fatalf("failed to sign payload: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	ctx := context.Background()
	err = verifier.Verify(ctx, headers, differentWebhookID, rawBody)
	if err == nil {
		t.Fatalf("expected signature verification failure for mismatched webhook ID, got nil")
	}
}

func TestWebhookVerifier_MissingHeaders(t *testing.T) {
	verifier := NewWebhookVerifier(&MockCertFetcher{})

	ctx := context.Background()
	headers := WebhookHeaders{
		TransmissionID:   "",
		TransmissionTime: "2026-11-07T02:00:00Z",
		CertURL:          "https://api.sandbox.paypal.com/cert.pem",
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "abc",
	}

	err := verifier.Verify(ctx, headers, "WH-123", []byte(`{}`))
	if err != ErrMissingWebhookHeaders {
		t.Fatalf("expected ErrMissingWebhookHeaders, got %v", err)
	}
}

func TestWebhookVerifier_MissingWebhookID(t *testing.T) {
	verifier := NewWebhookVerifier(&MockCertFetcher{})

	ctx := context.Background()
	headers := WebhookHeaders{
		TransmissionID:   "trans-1",
		TransmissionTime: "2026-11-07T02:00:00Z",
		CertURL:          "https://api.sandbox.paypal.com/cert.pem",
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "abc",
	}

	err := verifier.Verify(ctx, headers, "", []byte(`{}`))
	if err != ErrMissingWebhookID {
		t.Fatalf("expected ErrMissingWebhookID, got %v", err)
	}
}

func TestWebhookVerifier_UnsupportedAlgorithm(t *testing.T) {
	verifier := NewWebhookVerifier(&MockCertFetcher{})

	ctx := context.Background()
	headers := WebhookHeaders{
		TransmissionID:   "trans-1",
		TransmissionTime: "2026-11-07T02:00:00Z",
		CertURL:          "https://api.sandbox.paypal.com/cert.pem",
		AuthAlgo:         "SHA1withRSA",
		TransmissionSig:  "abc",
	}

	err := verifier.Verify(ctx, headers, "WH-123", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error for unsupported algorithm SHA1withRSA, got nil")
	}
}

func TestWebhookVerifier_ExpiredCertificate(t *testing.T) {
	now := time.Now().UTC()
	// Certificate expired yesterday
	cert, privKey := generateTestCertAndKey(t, now.Add(-48*time.Hour), now.Add(-24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-EXPIRED.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}

	verifier := NewWebhookVerifier(fetcher)

	transmissionID := "trans-123"
	transmissionTime := "2026-11-07T02:00:00Z"
	webhookID := "WH-123"
	rawBody := []byte(`{"id":"WH-EVENT-1"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed to sign payload: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	ctx := context.Background()
	err = verifier.Verify(ctx, headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("expected error for expired certificate, got nil")
	}
}

func TestWebhookVerifier_MalformedBase64Signature(t *testing.T) {
	now := time.Now().UTC()
	cert, _ := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}

	verifier := NewWebhookVerifier(fetcher)

	headers := WebhookHeaders{
		TransmissionID:   "trans-123",
		TransmissionTime: "2026-11-07T02:00:00Z",
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "not-valid-base64!@#$%",
	}

	ctx := context.Background()
	err := verifier.Verify(ctx, headers, "WH-123", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error for malformed base64 signature, got nil")
	}
}

func TestWebhookVerifier_CorruptedSignatureBytes(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}

	verifier := NewWebhookVerifier(fetcher)

	transmissionID := "trans-123"
	transmissionTime := "2026-11-07T02:00:00Z"
	webhookID := "WH-123"
	rawBody := []byte(`{"id":"WH-EVENT-1"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed to sign payload: %v", err)
	}

	sigBytes, _ := base64.StdEncoding.DecodeString(sig)
	sigBytes[10] ^= 0xFF // Flip bits in signature
	corruptedSig := base64.StdEncoding.EncodeToString(sigBytes)

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  corruptedSig,
	}

	ctx := context.Background()
	err = verifier.Verify(ctx, headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("expected error for corrupted signature bytes, got nil")
	}
}

func TestWebhookVerifier_KnownGoodVector(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-KNOWN-GOOD.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}
	verifier := NewWebhookVerifier(fetcher)

	// Known raw payload
	rawBody := []byte(`{"id":"WH-VECTOR-001","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)

	// Verify CRC32 calculation independently
	crc := ComputeCRC32(rawBody)
	expectedCRC := "1575475497" // ChecksumIEEE for this exact byte sequence
	if crc != expectedCRC {
		t.Fatalf("CRC32 mismatch: got %s, expected %s", crc, expectedCRC)
	}

	transmissionID := "5b481976-1234-4567-89ab-cdef01234567"
	transmissionTime := "2026-11-07T12:00:00Z"
	webhookID := "WH-SANDBOX-VECTOR-123"

	// Verify canonical string to verify format
	strToVerify := BuildStringToVerify(transmissionID, transmissionTime, webhookID, rawBody)
	expectedStr := "5b481976-1234-4567-89ab-cdef01234567|2026-11-07T12:00:00Z|WH-SANDBOX-VECTOR-123|1575475497"
	if strToVerify != expectedStr {
		t.Fatalf("canonical string format mismatch:\ngot:  %s\nwant: %s", strToVerify, expectedStr)
	}

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed generating signature: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	ctx := context.Background()
	if err := verifier.Verify(ctx, headers, webhookID, rawBody); err != nil {
		t.Fatalf("known-good vector verification failed: %v", err)
	}
}

func TestWebhookVerifier_FutureTransmissionTime_Rejection(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-TEST.pem"
	fetcher := &MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}
	verifier := NewWebhookVerifier(fetcher)

	futureTime := now.Add(2 * time.Hour).Format(time.RFC3339)
	rawBody := []byte(`{"id":"WH-FUTURE"}`)
	sig, _ := SignWebhookPayload(privKey, "trans-future", futureTime, "WH-123", rawBody)

	headers := WebhookHeaders{
		TransmissionID:   "trans-future",
		TransmissionTime: futureTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err := verifier.Verify(context.Background(), headers, "WH-123", rawBody)
	if err == nil {
		t.Fatalf("expected error for future transmission time beyond 10m skew, got nil")
	}
}

func TestValidateHATEOASURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid sandbox payouts item",
			url:     "https://api-m.sandbox.paypal.com/v1/payments/payouts-item/123",
			wantErr: false,
		},
		{
			name:    "valid production payouts item",
			url:     "https://api-m.paypal.com/v1/payments/payouts-item/123",
			wantErr: false,
		},
		{
			name:    "reject http",
			url:     "http://api-m.sandbox.paypal.com/v1/payments/payouts/123",
			wantErr: true,
		},
		{
			name:    "reject evil host",
			url:     "https://evil-paypal.com/v1/payments/payouts/123",
			wantErr: true,
		},
		{
			name:    "reject userinfo",
			url:     "https://user:pass@api-m.sandbox.paypal.com/v1/payments/payouts/123",
			wantErr: true,
		},
		{
			name:    "reject path traversal",
			url:     "https://api-m.sandbox.paypal.com/v1/payments/../../secret",
			wantErr: true,
		},
		{
			name:    "reject empty",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHATEOASURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHATEOASURL(%q) err = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Mission 039.4 — Controlled RSA Signature Verification Proofs
// -----------------------------------------------------------------------------

func TestWebhookVerifier_Proof_ValidRSASignatureAccepted(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-01.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-001"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-PROOF-12345"
	rawBody := []byte(`{"id":"WH-EVT-PROOF-01","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed generating valid signature: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	if err := verifier.Verify(context.Background(), headers, webhookID, rawBody); err != nil {
		t.Fatalf("expected valid RSA signature to pass verification, got error: %v", err)
	}
}

func TestWebhookVerifier_Proof_ForgedSignatureRejected(t *testing.T) {
	now := time.Now().UTC()
	// Legitimate cert uses privKeyA
	certA, _ := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))
	// Attacker generates separate key pair privKeyB
	_, privKeyB := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-02.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: certA},
	})

	transmissionID := "trans-proof-002"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-PROOF-12345"
	rawBody := []byte(`{"id":"WH-EVT-PROOF-02","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)

	// Attacker signs with privKeyB
	forgedSig, err := SignWebhookPayload(privKeyB, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed generating forged signature: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  forgedSig,
	}

	err = verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: forged RSA signature was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Proof_ModifiedPayloadRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-03.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-003"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-PROOF-12345"
	rawBody := []byte(`{"id":"WH-EVT-PROOF-03","amount":100}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed generating signature: %v", err)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	// Payload modified by 1 character
	modifiedBody := []byte(`{"id":"WH-EVT-PROOF-03","amount":101}`)

	err = verifier.Verify(context.Background(), headers, webhookID, modifiedBody)
	if err == nil {
		t.Fatalf("security violation: tampered payload was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Proof_ModifiedTransmissionIDRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-04.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-004-original"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-PROOF-12345"
	rawBody := []byte(`{"id":"WH-EVT-PROOF-04"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed generating signature: %v", err)
	}

	// Header presents different transmission ID
	headers := WebhookHeaders{
		TransmissionID:   "trans-proof-004-tampered",
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err = verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: modified transmission ID was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Proof_ModifiedTransmissionTimeRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-05.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-005"
	signedTime := now.Add(-5 * time.Minute).Format(time.RFC3339)
	webhookID := "WH-PROOF-12345"
	rawBody := []byte(`{"id":"WH-EVT-PROOF-05"}`)

	sig, err := SignWebhookPayload(privKey, transmissionID, signedTime, webhookID, rawBody)
	if err != nil {
		t.Fatalf("failed generating signature: %v", err)
	}

	// Header presents different transmission time (1 second difference)
	tamperedTime := now.Add(-5*time.Minute + 1*time.Second).Format(time.RFC3339)
	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: tamperedTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err = verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: modified transmission time was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Proof_IncorrectCRC32Rejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-06.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-006"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-PROOF-12345"
	rawBody := []byte(`{"id":"WH-EVT-PROOF-06","status":"legitimate"}`)

	// Manually construct signature with deliberately wrong CRC32 decimal string
	wrongCRC := "999999999"
	canonicalStringWithWrongCRC := fmt.Sprintf("%s|%s|%s|%s", transmissionID, transmissionTime, webhookID, wrongCRC)
	hash := sha256.Sum256([]byte(canonicalStringWithWrongCRC))
	sigBytes, err := rsa.SignPKCS1v15(nil, privKey, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("failed signing test payload: %v", err)
	}
	sigWithWrongCRC := base64.StdEncoding.EncodeToString(sigBytes)

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sigWithWrongCRC,
	}

	err = verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: signature with incorrect CRC32 was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Proof_InvalidCertificateNonRSA(t *testing.T) {
	now := time.Now().UTC()

	// Generate ECDSA P-256 key pair instead of RSA
	privECDSA, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed generating ecdsa key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(7777),
		Subject: pkix.Name{
			CommonName:   "api.sandbox.paypal.com",
			Organization: []string{"PayPal, Inc."},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privECDSA.PublicKey, privECDSA)
	if err != nil {
		t.Fatalf("failed creating ecdsa certificate: %v", err)
	}
	ecdsaCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed parsing ecdsa certificate: %v", err)
	}

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-ECDSA.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: ecdsaCert},
	})

	headers := WebhookHeaders{
		TransmissionID:   "trans-proof-007a",
		TransmissionTime: now.Format(time.RFC3339),
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "c2ln",
	}

	err = verifier.Verify(context.Background(), headers, "WH-123", []byte(`{}`))
	if err == nil {
		t.Fatalf("security violation: non-RSA certificate was accepted!")
	}
	if !errors.Is(err, ErrInvalidAuthAlgo) || !strings.Contains(err.Error(), "not RSA") {
		t.Errorf("expected ErrInvalidAuthAlgo with 'not RSA', got: %v", err)
	}
}

func TestWebhookVerifier_Proof_InvalidCertificateNotYetValid(t *testing.T) {
	now := time.Now().UTC()
	// Certificate valid only starting 2 hours in the future
	cert, privKey := generateTestCertAndKey(t, now.Add(2*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-FUTURE.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-007b"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-123"
	rawBody := []byte(`{"id":"WH-EVT-FUTURE-CERT"}`)

	sig, _ := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)
	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err := verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: not-yet-valid certificate was accepted!")
	}
	if !errors.Is(err, ErrExpiredCert) && !strings.Contains(err.Error(), "expired or is not yet valid") {
		t.Errorf("expected ErrExpiredCert, got: %v", err)
	}
}

func TestWebhookVerifier_Proof_UnsupportedAlgorithmRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-PROOF-08.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-proof-008"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WH-123"
	rawBody := []byte(`{"id":"WH-EVT-08"}`)

	sig, _ := SignWebhookPayload(privKey, transmissionID, transmissionTime, webhookID, rawBody)

	unsupportedAlgos := []string{
		"SHA1withRSA",
		"SHA512withRSA",
		"RSASSA-PSS",
		"ECDSAwithSHA256",
		"HMAC-SHA256",
		"none",
		"md5",
		"SHA256", // missing withRSA
	}

	for _, algo := range unsupportedAlgos {
		headers := WebhookHeaders{
			TransmissionID:   transmissionID,
			TransmissionTime: transmissionTime,
			CertURL:          certURL,
			AuthAlgo:         algo,
			TransmissionSig:  sig,
		}

		err := verifier.Verify(context.Background(), headers, webhookID, rawBody)
		if err == nil {
			t.Errorf("security violation: unsupported algorithm %q was accepted!", algo)
		}
		if !errors.Is(err, ErrInvalidAuthAlgo) {
			t.Errorf("expected ErrInvalidAuthAlgo for algorithm %q, got: %v", algo, err)
		}
	}
}

func TestWebhookVerifier_Proof_CertificateRetrievalFailureHandled(t *testing.T) {
	// 1. Missing cert in fetcher
	emptyFetcher := &MockCertFetcher{Certs: make(map[string]*x509.Certificate)}
	verifier := NewWebhookVerifier(emptyFetcher)

	headers := WebhookHeaders{
		TransmissionID:   "trans-proof-009",
		TransmissionTime: time.Now().UTC().Format(time.RFC3339),
		CertURL:          "https://api.sandbox.paypal.com/v1/notifications/certs/NONEXISTENT.pem",
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "c2ln",
	}

	err := verifier.Verify(context.Background(), headers, "WH-123", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error for nonexistent cert, got nil")
	}
	if !errors.Is(err, ErrInvalidCertURL) && !strings.Contains(err.Error(), "certificate retrieval failed") {
		t.Errorf("expected certificate retrieval error wrapping ErrInvalidCertURL, got: %v", err)
	}

	// 2. Malicious cert URL violating SSRF policy
	evilHeaders := WebhookHeaders{
		TransmissionID:   "trans-proof-009b",
		TransmissionTime: time.Now().UTC().Format(time.RFC3339),
		CertURL:          "https://evil-attacker.com/certs/cert.pem",
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "c2ln",
	}

	errSSRF := verifier.Verify(context.Background(), evilHeaders, "WH-123", []byte(`{}`))
	if errSSRF == nil {
		t.Fatalf("security violation: SSRF attacker cert URL was accepted!")
	}
	if !errors.Is(errSSRF, ErrSSRFPrevented) {
		t.Errorf("expected ErrSSRFPrevented, got: %v", errSSRF)
	}
}

// -----------------------------------------------------------------------------
// Mission 040.5 — Independent Controlled Cryptographic Verification Proofs
// -----------------------------------------------------------------------------

// independentSignPayload independently calculates CRC32, formats the canonical string, and signs it
// without referencing any production helper functions.
func independentSignPayload(t *testing.T, privKey *rsa.PrivateKey, transmissionID, transmissionTime, webhookID string, rawBody []byte) (string, uint32) {
	t.Helper()
	// 1. Independent IEEE 802.3 CRC32 calculation
	crcValue := crc32.ChecksumIEEE(rawBody)
	crcDecimalStr := strconv.FormatUint(uint64(crcValue), 10)

	// 2. Independent canonical string construction: transmissionId|timeStamp|webhookId|crc32
	canonicalMsg := transmissionID + "|" + transmissionTime + "|" + webhookID + "|" + crcDecimalStr

	// 3. Independent SHA-256 digest
	digest := sha256.Sum256([]byte(canonicalMsg))

	// 4. Independent PKCS#1 v1.5 signing
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("failed independent signing: %v", err)
	}

	return base64.StdEncoding.EncodeToString(sig), crcValue
}

func TestWebhookVerifier_Mission040_5_Independent_ValidSignatureAccepted(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-INDEP-01.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-indep-001"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WEBHOOK_ID"
	rawBody := []byte(`{"id":"WH-SIM-001","event_type":"PAYMENT.PAYOUTSBATCH.PROCESSING"}`)

	sig, independentCRC := independentSignPayload(t, privKey, transmissionID, transmissionTime, webhookID, rawBody)

	// Assert independent CRC32 matches production ComputeCRC32
	prodCRC := ComputeCRC32(rawBody)
	if prodCRC != strconv.FormatUint(uint64(independentCRC), 10) {
		t.Fatalf("CRC32 mismatch: independent %d vs production %s", independentCRC, prodCRC)
	}

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	if err := verifier.Verify(context.Background(), headers, webhookID, rawBody); err != nil {
		t.Fatalf("expected independent signature to verify successfully, got: %v", err)
	}
}

func TestWebhookVerifier_Mission040_5_Independent_ModifiedBodyRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-INDEP-02.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-indep-002"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WEBHOOK_ID"
	originalBody := []byte(`{"id":"WH-SIM-002","amount":"10.00"}`)
	tamperedBody := []byte(`{"id":"WH-SIM-002","amount":"99.00"}`)

	sig, _ := independentSignPayload(t, privKey, transmissionID, transmissionTime, webhookID, originalBody)

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err := verifier.Verify(context.Background(), headers, webhookID, tamperedBody)
	if err == nil {
		t.Fatalf("security violation: modified body accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Mission040_5_Independent_WrongWebhookIDRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-INDEP-03.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-indep-003"
	transmissionTime := now.Format(time.RFC3339)
	signedWebhookID := "71R95625WE243291S" // Signed with real registered ID
	verifiedWebhookID := "WEBHOOK_ID"       // Verified with simulator dummy ID
	rawBody := []byte(`{"id":"WH-SIM-003"}`)

	sig, _ := independentSignPayload(t, privKey, transmissionID, transmissionTime, signedWebhookID, rawBody)

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err := verifier.Verify(context.Background(), headers, verifiedWebhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: signature signed with %q was accepted when verifying with %q!", signedWebhookID, verifiedWebhookID)
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Mission040_5_Independent_WrongTimestampRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-INDEP-04.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-indep-004"
	signedTime := now.Add(-5 * time.Minute).Format(time.RFC3339)
	headerTime := now.Format(time.RFC3339)
	webhookID := "WEBHOOK_ID"
	rawBody := []byte(`{"id":"WH-SIM-004"}`)

	sig, _ := independentSignPayload(t, privKey, transmissionID, signedTime, webhookID, rawBody)

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: headerTime, // Header differs from what was signed
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err := verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: mismatched timestamp was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Mission040_5_Independent_WrongTransmissionIDRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-INDEP-05.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	signedTransID := "trans-signed-005"
	headerTransID := "trans-tampered-005"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WEBHOOK_ID"
	rawBody := []byte(`{"id":"WH-SIM-005"}`)

	sig, _ := independentSignPayload(t, privKey, signedTransID, transmissionTime, webhookID, rawBody)

	headers := WebhookHeaders{
		TransmissionID:   headerTransID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}

	err := verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: mismatched transmission ID was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}

func TestWebhookVerifier_Mission040_5_Independent_ModifiedSignatureRejected(t *testing.T) {
	now := time.Now().UTC()
	cert, privKey := generateTestCertAndKey(t, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-INDEP-06.pem"
	verifier := NewWebhookVerifier(&MockCertFetcher{
		Certs: map[string]*x509.Certificate{certURL: cert},
	})

	transmissionID := "trans-indep-006"
	transmissionTime := now.Format(time.RFC3339)
	webhookID := "WEBHOOK_ID"
	rawBody := []byte(`{"id":"WH-SIM-006"}`)

	sig, _ := independentSignPayload(t, privKey, transmissionID, transmissionTime, webhookID, rawBody)

	// Tamper with the base64 signature by replacing last 4 characters
	tamperedSig := sig[:len(sig)-4] + "AAAA"

	headers := WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  tamperedSig,
	}

	err := verifier.Verify(context.Background(), headers, webhookID, rawBody)
	if err == nil {
		t.Fatalf("security violation: corrupted signature was accepted!")
	}
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Errorf("expected ErrInvalidWebhookSignature, got: %v", err)
	}
}
