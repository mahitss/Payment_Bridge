package paypal

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrMissingWebhookHeaders   = errors.New("missing required PayPal webhook signature headers")
	ErrMissingWebhookID        = errors.New("missing configured PayPal webhook ID")
	ErrInvalidAuthAlgo         = errors.New("unsupported PayPal webhook auth algorithm; must be SHA256withRSA")
	ErrInvalidCertURL          = errors.New("invalid PayPal webhook certificate URL")
	ErrSSRFPrevented           = errors.New("certificate URL rejected by security policy (SSRF prevention)")
	ErrExpiredCert             = errors.New("paypal webhook certificate has expired or is not yet valid")
	ErrInvalidWebhookSignature = errors.New("invalid PayPal webhook cryptographic signature")
	ErrInvalidTransmissionTime = errors.New("invalid or future transmission time")
)

const MaxCachedCertificates = 64

// WebhookHeaders bundles the 5 standard headers provided in a PayPal webhook delivery.
type WebhookHeaders struct {
	TransmissionID   string
	TransmissionTime string
	CertURL          string
	AuthAlgo         string
	TransmissionSig  string
}

// WebhookCertFetcher abstracts the retrieval and caching of PayPal X.509 certificates.
type WebhookCertFetcher interface {
	FetchCert(ctx context.Context, certURL string) (*x509.Certificate, error)
}

type cachedCert struct {
	cert      *x509.Certificate
	fetchedAt time.Time
}

// DefaultCertFetcher validates certificate URLs, fetches X.509 certs over HTTPS, and caches them in memory.
type DefaultCertFetcher struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]*cachedCert
}

// NewDefaultCertFetcher constructs a production-ready certificate fetcher with SSRF and redirect protection.
func NewDefaultCertFetcher() *DefaultCertFetcher {
	return &DefaultCertFetcher{
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return errors.New("redirects are prohibited for webhook certificate download")
			},
		},
		cache: make(map[string]*cachedCert),
	}
}

// ValidateCertURL verifies that the certificate URL strictly matches PayPal's official domains over HTTPS.
func ValidateCertURL(certURL string) error {
	trimmed := strings.TrimSpace(certURL)
	if trimmed == "" {
		return ErrInvalidCertURL
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCertURL, err)
	}

	// 1. Must use HTTPS
	if u.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be https", ErrSSRFPrevented)
	}

	// 2. Reject embedded userinfo
	if u.User != nil {
		return fmt.Errorf("%w: userinfo not permitted", ErrSSRFPrevented)
	}

	// 3. Strict host validation
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidCertURL)
	}

	isPayPalDomain := host == "paypal.com" || strings.HasSuffix(host, ".paypal.com")
	if !isPayPalDomain {
		return fmt.Errorf("%w: host %q is not a permitted PayPal domain", ErrSSRFPrevented, host)
	}

	// 4. Port validation: default 443 or none
	port := u.Port()
	if port != "" && port != "443" {
		return fmt.Errorf("%w: non-standard port %q rejected", ErrSSRFPrevented, port)
	}

	// 5. Path validation: no path traversal
	if strings.Contains(u.Path, "..") || strings.Contains(u.RawPath, "..") {
		return fmt.Errorf("%w: path traversal rejected", ErrSSRFPrevented)
	}

	return nil
}

// normalizeCertURL strips query parameters and fragments to prevent cache-key pollution.
func normalizeCertURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// FetchCert retrieves the X.509 certificate for certURL, using the bounded cache when valid.
func (f *DefaultCertFetcher) FetchCert(ctx context.Context, certURL string) (*x509.Certificate, error) {
	if err := ValidateCertURL(certURL); err != nil {
		return nil, err
	}

	normURL := normalizeCertURL(certURL)
	now := time.Now().UTC()

	// 1. Check in-memory cache
	f.mu.RLock()
	if cached, ok := f.cache[normURL]; ok && cached != nil && cached.cert != nil {
		if now.After(cached.cert.NotBefore) && now.Before(cached.cert.NotAfter) {
			f.mu.RUnlock()
			return cached.cert, nil
		}
	}
	f.mu.RUnlock()

	// 2. Perform HTTP GET
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch certificate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("certificate server returned HTTP %d", resp.StatusCode)
	}

	// 3. Limit read to 64KB
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("failed reading certificate body: %w", err)
	}

	// 4. Parse PEM block
	block, _ := pem.Decode(bodyBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("failed to decode PEM certificate block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed parsing X.509 certificate: %w", err)
	}

	// 5. Validity check
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, ErrExpiredCert
	}

	// 6. Cache the certificate with bounded memory pruning
	f.mu.Lock()
	if len(f.cache) >= MaxCachedCertificates {
		// Evict expired entries first
		for k, v := range f.cache {
			if now.After(v.cert.NotAfter) {
				delete(f.cache, k)
			}
		}
		// If still at capacity, evict oldest entry
		if len(f.cache) >= MaxCachedCertificates {
			var oldestKey string
			var oldestTime time.Time
			for k, v := range f.cache {
				if oldestKey == "" || v.fetchedAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.fetchedAt
				}
			}
			if oldestKey != "" {
				delete(f.cache, oldestKey)
			}
		}
	}

	f.cache[normURL] = &cachedCert{
		cert:      cert,
		fetchedAt: now,
	}
	f.mu.Unlock()

	return cert, nil
}

// WebhookVerifier performs cryptographic verification of PayPal webhook notifications.
type WebhookVerifier struct {
	fetcher WebhookCertFetcher
}

// NewWebhookVerifier constructs a WebhookVerifier. If no fetcher is provided, DefaultCertFetcher is used.
func NewWebhookVerifier(fetcher ...WebhookCertFetcher) *WebhookVerifier {
	if len(fetcher) > 0 && fetcher[0] != nil {
		return &WebhookVerifier{fetcher: fetcher[0]}
	}
	return &WebhookVerifier{fetcher: NewDefaultCertFetcher()}
}

// ComputeCRC32 calculates the IEEE 802.3 CRC-32 checksum of the raw body as an unsigned integer string.
func ComputeCRC32(rawBody []byte) string {
	crc := crc32.ChecksumIEEE(rawBody)
	return strconv.FormatUint(uint64(crc), 10)
}

// BuildStringToVerify constructs the canonical message string required by PayPal's signature verification specification:
// <transmission_id>|<transmission_time>|<webhook_id>|<crc32(request_body)>
func BuildStringToVerify(transmissionID, transmissionTime, webhookID string, rawBody []byte) string {
	crcStr := ComputeCRC32(rawBody)
	return fmt.Sprintf("%s|%s|%s|%s", transmissionID, transmissionTime, webhookID, crcStr)
}

// Verify validates that the signature in headers is valid for the rawBody and webhookID.
func (v *WebhookVerifier) Verify(ctx context.Context, headers WebhookHeaders, webhookID string, rawBody []byte) error {
	// 1. Check required headers
	if strings.TrimSpace(headers.TransmissionID) == "" ||
		strings.TrimSpace(headers.TransmissionTime) == "" ||
		strings.TrimSpace(headers.CertURL) == "" ||
		strings.TrimSpace(headers.AuthAlgo) == "" ||
		strings.TrimSpace(headers.TransmissionSig) == "" {
		return ErrMissingWebhookHeaders
	}

	// 2. Check webhook ID
	if strings.TrimSpace(webhookID) == "" {
		return ErrMissingWebhookID
	}

	// 3. Transmission time syntax and future-timestamp guard (10m clock skew)
	tTime, parseErr := time.Parse(time.RFC3339, headers.TransmissionTime)
	if parseErr != nil {
		return fmt.Errorf("%w: transmission time must be valid RFC3339 format: %v", ErrInvalidTransmissionTime, parseErr)
	}
	if tTime.After(time.Now().UTC().Add(10 * time.Minute)) {
		return fmt.Errorf("%w: timestamp is in the future beyond acceptable clock skew", ErrInvalidTransmissionTime)
	}

	// 4. Algorithm validation
	algo := strings.ToUpper(strings.TrimSpace(headers.AuthAlgo))
	if algo != "SHA256WITHRSA" {
		return fmt.Errorf("%w: got %s", ErrInvalidAuthAlgo, headers.AuthAlgo)
	}

	// 5. Retrieve and validate certificate
	cert, err := v.fetcher.FetchCert(ctx, headers.CertURL)
	if err != nil {
		return fmt.Errorf("%w: certificate retrieval failed: %w", ErrInvalidCertURL, err)
	}

	// 6. Extract RSA public key
	rsaPub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: certificate public key is not RSA", ErrInvalidAuthAlgo)
	}

	// 7. Decode base64 signature
	sigBytes, err := base64.StdEncoding.DecodeString(headers.TransmissionSig)
	if err != nil {
		return fmt.Errorf("%w: invalid base64 encoding", ErrInvalidWebhookSignature)
	}

	// 8. Construct string to verify
	strToVerify := BuildStringToVerify(headers.TransmissionID, headers.TransmissionTime, webhookID, rawBody)

	// 9. Compute SHA-256 hash
	hash := sha256.Sum256([]byte(strToVerify))

	// 10. Verify PKCS#1 v1.5 signature
	if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, hash[:], sigBytes); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWebhookSignature, err)
	}

	return nil
}

// SignWebhookPayload signs a payload using an RSA private key following PayPal's specification (used for tests and fixtures).
func SignWebhookPayload(privKey *rsa.PrivateKey, transmissionID, transmissionTime, webhookID string, rawBody []byte) (string, error) {
	strToVerify := BuildStringToVerify(transmissionID, transmissionTime, webhookID, rawBody)
	hash := sha256.Sum256([]byte(strToVerify))
	sigBytes, err := rsa.SignPKCS1v15(nil, privKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sigBytes), nil
}

// MockCertFetcher provides a memory-backed certificate fetcher for unit and acceptance tests.
type MockCertFetcher struct {
	Certs map[string]*x509.Certificate
}

// FetchCert returns the in-memory certificate for certURL or an error if not found.
func (m *MockCertFetcher) FetchCert(ctx context.Context, certURL string) (*x509.Certificate, error) {
	if err := ValidateCertURL(certURL); err != nil {
		return nil, err
	}
	cert, ok := m.Certs[certURL]
	if !ok {
		return nil, fmt.Errorf("mock cert not found for URL: %s", certURL)
	}
	now := time.Now().UTC()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, ErrExpiredCert
	}
	return cert, nil
}
