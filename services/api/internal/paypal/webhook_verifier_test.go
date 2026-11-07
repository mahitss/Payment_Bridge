package paypal

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
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

