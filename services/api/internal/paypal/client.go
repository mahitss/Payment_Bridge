package paypal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/logger"
)

var (
	ErrProductionHostBlocked      = errors.New("security violation: production PayPal host detected; only sandbox permitted")
	ErrInvalidSandboxEndpoint     = errors.New("security violation: invalid PayPal Sandbox endpoint; only https://api-m.sandbox.paypal.com is allowed")
	ErrSimulatedEndpointForbidden = errors.New("security violation: simulated PayPal endpoint is forbidden in production environment")
	ErrMissingCredentials         = errors.New("paypal credentials missing: client_id and client_secret are required")
	ErrUnauthorized               = errors.New("paypal authentication failed: invalid client credentials")
	ErrForbidden                  = errors.New("paypal operation forbidden: insufficient permissions or account restricted")
	ErrPayoutNotFound             = errors.New("paypal payout batch or item not found")
	ErrAmbiguousNetworkTimeout    = errors.New("ambiguous network timeout during payout mutation: DO NOT RETRY WITH NEW ID; run reconciliation")
	ErrAmbiguousServerResponse    = errors.New("ambiguous server error (HTTP 5xx): transaction state unknown")
	ErrInvalidPayoutRequest       = errors.New("invalid payout request: missing sender_batch_id or items")
	ErrSandboxAssertionFailed     = errors.New("paypal environment is not sandbox")
	ErrUntrustedHATEOASHost       = errors.New("security violation: untrusted HATEOAS host in paypal response")
	ErrMalformedHATEOASLink       = errors.New("malformed HATEOAS link in paypal response")
	ErrOversizedResponse          = errors.New("paypal response exceeded maximum allowable size limit")
)

// OfficialSandboxBaseURL is the immutable official PayPal sandbox API endpoint.
const OfficialSandboxBaseURL = "https://api-m.sandbox.paypal.com"

// PayPalPayoutsScope is the required OAuth2 scope for PayPal Payouts operations.
const PayPalPayoutsScope = "https://uri.paypal.com/payments/payouts"

// HasPayoutsScope checks whether the exact Payouts scope exists in a whitespace-delimited scope string.
func HasPayoutsScope(scopeStr string) bool {
	for _, s := range strings.Fields(scopeStr) {
		if s == PayPalPayoutsScope {
			return true
		}
	}
	return false
}

// IsAmbiguousOutcome returns whether an error represents an ambiguous outcome where PayPal may have accepted or processed the payout.
func IsAmbiguousOutcome(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrAmbiguousNetworkTimeout) || errors.Is(err, ErrAmbiguousServerResponse)
}

// Client is a server-side PayPal REST API client configured strictly for Sandbox.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	tokenMu         sync.RWMutex
	cachedToken     string
	tokenExpiry     time.Time
	hasPayoutsScope bool
}

// BaseURL returns the configured base URL for the client.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// HasPayoutsScope returns whether the currently cached access token was granted the payouts scope.
func (c *Client) HasPayoutsScope() bool {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.hasPayoutsScope
}

// OAuthTokenResponse represents the response from PayPal OAuth2 token endpoint.
type OAuthTokenResponse struct {
	Scope       string `json:"scope"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	AppID       string `json:"app_id"`
	ExpiresIn   int    `json:"expires_in"`
	Nonce       string `json:"nonce"`
}

// SenderBatchHeader represents sender-level batch metadata.
type SenderBatchHeader struct {
	SenderBatchID string `json:"sender_batch_id,omitempty"`
	EmailSubject  string `json:"email_subject,omitempty"`
	RecipientType string `json:"recipient_type,omitempty"`
}

// PayoutBatchHeader represents metadata about a PayPal Payout Batch.
type PayoutBatchHeader struct {
	PayoutBatchID     string             `json:"payout_batch_id"`
	BatchStatus       string             `json:"batch_status"`
	TimeCreated       string             `json:"time_created"`
	TimeCompleted     string             `json:"time_completed,omitempty"`
	SenderBatchHeader *SenderBatchHeader `json:"sender_batch_header,omitempty"`
	SenderBatchID     string             `json:"sender_batch_id,omitempty"`
	FundingSource     string             `json:"funding_source,omitempty"`
	Amount            *CurrencyAmount    `json:"amount,omitempty"`
	Fees              *CurrencyAmount    `json:"fees,omitempty"`
}

// Normalize ensures SenderBatchID and SenderBatchHeader are synchronized.
func (h *PayoutBatchHeader) Normalize() {
	if h.SenderBatchID == "" && h.SenderBatchHeader != nil {
		h.SenderBatchID = h.SenderBatchHeader.SenderBatchID
	} else if h.SenderBatchID != "" && h.SenderBatchHeader == nil {
		h.SenderBatchHeader = &SenderBatchHeader{
			SenderBatchID: h.SenderBatchID,
		}
	}
}

func (h *PayoutBatchHeader) UnmarshalJSON(data []byte) error {
	type Alias PayoutBatchHeader
	var aux struct {
		Alias
		LegacySenderBatchID string `json:"sender_batch_header.sender_batch_id"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*h = PayoutBatchHeader(aux.Alias)
	if h.SenderBatchID == "" && aux.LegacySenderBatchID != "" {
		h.SenderBatchID = aux.LegacySenderBatchID
	}
	h.Normalize()
	return nil
}

// CurrencyAmount represents a PayPal currency & value pair.
type CurrencyAmount struct {
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

// PayoutItemDetail represents the official nested payout_item structure.
type PayoutItemDetail struct {
	RecipientType string          `json:"recipient_type,omitempty"`
	Amount        *CurrencyAmount `json:"amount,omitempty"`
	Note          string          `json:"note,omitempty"`
	Receiver      string          `json:"receiver,omitempty"`
	SenderItemID  string          `json:"sender_item_id,omitempty"`
}

// PayoutItem represents a single item inside a payout batch.
type PayoutItem struct {
	PayoutItemID      string            `json:"payout_item_id"`
	TransactionID     string            `json:"transaction_id,omitempty"`
	TransactionStatus string            `json:"transaction_status"`
	PayoutItemFee     *CurrencyAmount   `json:"payout_item_fee,omitempty"`
	PayoutBatchID     string            `json:"payout_batch_id"`
	PayoutItem        *PayoutItemDetail `json:"payout_item,omitempty"`
	SenderItemID      string            `json:"sender_item_id,omitempty"`
	Amount            CurrencyAmount    `json:"amount"`
	Receiver          string            `json:"receiver"`
	RecipientType     string            `json:"recipient_type,omitempty"`
	Note              string            `json:"note,omitempty"`
	TimeProcessed     string            `json:"time_processed,omitempty"`
	Errors            json.RawMessage   `json:"errors,omitempty"`
	Links             []LinkDescription `json:"links,omitempty"`
}

// Normalize synchronizes official nested payout_item fields with flat fields.
func (pi *PayoutItem) Normalize() {
	if pi.PayoutItem != nil {
		if pi.SenderItemID == "" && pi.PayoutItem.SenderItemID != "" {
			pi.SenderItemID = pi.PayoutItem.SenderItemID
		}
		if pi.Receiver == "" && pi.PayoutItem.Receiver != "" {
			pi.Receiver = pi.PayoutItem.Receiver
		}
		if pi.RecipientType == "" && pi.PayoutItem.RecipientType != "" {
			pi.RecipientType = pi.PayoutItem.RecipientType
		}
		if pi.Note == "" && pi.PayoutItem.Note != "" {
			pi.Note = pi.PayoutItem.Note
		}
		if (pi.Amount.Value == "" || pi.Amount.Currency == "") && pi.PayoutItem.Amount != nil {
			pi.Amount = *pi.PayoutItem.Amount
		}
	} else {
		var amt *CurrencyAmount
		if pi.Amount.Value != "" || pi.Amount.Currency != "" {
			amt = &CurrencyAmount{
				Currency: pi.Amount.Currency,
				Value:    pi.Amount.Value,
			}
		}
		pi.PayoutItem = &PayoutItemDetail{
			RecipientType: pi.RecipientType,
			Amount:        amt,
			Note:          pi.Note,
			Receiver:      pi.Receiver,
			SenderItemID:  pi.SenderItemID,
		}
	}
}

func (pi *PayoutItem) UnmarshalJSON(data []byte) error {
	type Alias PayoutItem
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*pi = PayoutItem(aux)
	pi.Normalize()
	return nil
}

// PayoutBatchResponse represents the payload returned by GET /v1/payments/payouts/{id}.
type PayoutBatchResponse struct {
	BatchHeader PayoutBatchHeader `json:"batch_header"`
	Items       []PayoutItem      `json:"items,omitempty"`
	Links       []LinkDescription `json:"links,omitempty"`
	TotalItems  int               `json:"total_items,omitempty"`
	TotalPages  int               `json:"total_pages,omitempty"`
}

// Normalize normalizes the batch header and all nested items.
func (pbr *PayoutBatchResponse) Normalize() {
	pbr.BatchHeader.Normalize()
	for i := range pbr.Items {
		pbr.Items[i].Normalize()
	}
}

func (pbr *PayoutBatchResponse) UnmarshalJSON(data []byte) error {
	type Alias PayoutBatchResponse
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*pbr = PayoutBatchResponse(aux)
	pbr.Normalize()
	return nil
}

// PayoutItemResponse represents the payload returned by GET /v1/payments/payouts-item/{id}
// and POST /v1/payments/payouts-item/{id}/cancel.
type PayoutItemResponse struct {
	PayoutItemID      string            `json:"payout_item_id"`
	TransactionID     string            `json:"transaction_id,omitempty"`
	TransactionStatus string            `json:"transaction_status"`
	PayoutBatchID     string            `json:"payout_batch_id"`
	PayoutItemFee     *CurrencyAmount   `json:"payout_item_fee,omitempty"`
	PayoutItem        *PayoutItemDetail `json:"payout_item,omitempty"`
	SenderItemID      string            `json:"sender_item_id,omitempty"`
	SenderBatchID     string            `json:"sender_batch_id,omitempty"`
	Amount            CurrencyAmount    `json:"amount"`
	Receiver          string            `json:"receiver"`
	TimeProcessed     string            `json:"time_processed,omitempty"`
	Errors            json.RawMessage   `json:"errors,omitempty"`
	Links             []LinkDescription `json:"links,omitempty"`
}

// Normalize synchronizes official nested payout_item fields with flat fields.
func (pir *PayoutItemResponse) Normalize() {
	if pir.PayoutItem != nil {
		if pir.SenderItemID == "" && pir.PayoutItem.SenderItemID != "" {
			pir.SenderItemID = pir.PayoutItem.SenderItemID
		}
		if pir.Receiver == "" && pir.PayoutItem.Receiver != "" {
			pir.Receiver = pir.PayoutItem.Receiver
		}
		if (pir.Amount.Value == "" || pir.Amount.Currency == "") && pir.PayoutItem.Amount != nil {
			pir.Amount = *pir.PayoutItem.Amount
		}
	} else {
		var amt *CurrencyAmount
		if pir.Amount.Value != "" || pir.Amount.Currency != "" {
			amt = &CurrencyAmount{
				Currency: pir.Amount.Currency,
				Value:    pir.Amount.Value,
			}
		}
		pir.PayoutItem = &PayoutItemDetail{
			Amount:       amt,
			Receiver:     pir.Receiver,
			SenderItemID: pir.SenderItemID,
		}
	}
}

func (pir *PayoutItemResponse) UnmarshalJSON(data []byte) error {
	type Alias PayoutItemResponse
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*pir = PayoutItemResponse(aux)
	pir.Normalize()
	return nil
}

// LinkDescription represents PayPal HATEOAS links.
type LinkDescription struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

// APIErrorDetail represents field-level validation errors from PayPal REST APIs.
type APIErrorDetail struct {
	Field string `json:"field,omitempty"`
	Issue string `json:"issue,omitempty"`
}

// APIErrorResponse represents a standard PayPal REST API error body.
type APIErrorResponse struct {
	Name            string            `json:"name"`
	Message         string            `json:"message"`
	DebugID         string            `json:"debug_id,omitempty"`
	InformationLink string            `json:"information_link,omitempty"`
	Details         []APIErrorDetail  `json:"details,omitempty"`
	Links           []LinkDescription `json:"links,omitempty"`
}

const (
	// MaxResponseBodyBytes limits the amount of data read from PayPal REST API endpoints (64 KB).
	MaxResponseBodyBytes = 64 * 1024

	// MaxDiagnosticFieldLength limits error field string lengths to prevent unbounded log/persistence output.
	MaxDiagnosticFieldLength = 128
)

// sanitizeDiagnosticField strips control chars, sanitizes via logger rules, and caps length.
func sanitizeDiagnosticField(s string, maxLen int) string {
	cleaned := logger.SanitizeText(s)
	cleaned = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, cleaned)
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen]
	}
	return cleaned
}

// OversizedResponseError indicates that the provider response body exceeded MaxResponseBodyBytes.
type OversizedResponseError struct {
	Limit int64
}

func (e *OversizedResponseError) Error() string {
	return fmt.Sprintf("paypal response exceeded maximum allowable size limit (%d bytes)", e.Limit)
}

func (e *OversizedResponseError) Unwrap() error {
	return ErrOversizedResponse
}

// readResponseBody safely reads an HTTP response body up to MaxResponseBodyBytes.
// If the response body exceeds MaxResponseBodyBytes, it returns an OversizedResponseError
// and never returns partial or truncated data to callers.
func readResponseBody(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	// Read at most MaxResponseBodyBytes + 1 bytes to detect oversized responses
	// without unbounded memory allocation.
	bodyBytes, err := io.ReadAll(io.LimitReader(r, MaxResponseBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if int64(len(bodyBytes)) > MaxResponseBodyBytes {
		return nil, &OversizedResponseError{Limit: MaxResponseBodyBytes}
	}
	return bodyBytes, nil
}

// APIForbiddenError represents a structured HTTP 403 Forbidden error from PayPal REST API.
type APIForbiddenError struct {
	Name    string `json:"name,omitempty"`
	DebugID string `json:"debug_id,omitempty"`
	Issue   string `json:"issue,omitempty"`
}

func (e *APIForbiddenError) Error() string {
	parts := []string{ErrForbidden.Error()}
	if e.Name != "" {
		parts = append(parts, fmt.Sprintf("name=%s", e.Name))
	}
	if e.Issue != "" {
		parts = append(parts, fmt.Sprintf("issue=%s", e.Issue))
	}
	if e.DebugID != "" {
		parts = append(parts, fmt.Sprintf("debug_id=%s", e.DebugID))
	}
	if len(parts) == 1 {
		return ErrForbidden.Error()
	}
	return strings.Join(parts, " | ")
}

func (e *APIForbiddenError) Unwrap() error {
	return ErrForbidden
}

// AsAPIForbiddenError returns *APIForbiddenError if err wraps one.
func AsAPIForbiddenError(err error) (*APIForbiddenError, bool) {
	var fe *APIForbiddenError
	if errors.As(err, &fe) {
		return fe, true
	}
	return nil, false
}

func parseForbiddenError(resp *http.Response, bodyBytes []byte) error {
	debugID := ""
	if resp != nil && resp.Header != nil {
		debugID = resp.Header.Get("Paypal-Debug-Id")
		if debugID == "" {
			debugID = resp.Header.Get("PayPal-Debug-Id")
		}
	}

	var apiErr APIErrorResponse
	if len(bodyBytes) > 0 {
		if jsonErr := json.Unmarshal(bodyBytes, &apiErr); jsonErr == nil {
			if apiErr.DebugID != "" {
				debugID = apiErr.DebugID
			}
		}
	}

	issue := ""
	if len(apiErr.Details) > 0 {
		issue = apiErr.Details[0].Issue
	}

	name := sanitizeDiagnosticField(apiErr.Name, MaxDiagnosticFieldLength)
	issue = sanitizeDiagnosticField(issue, MaxDiagnosticFieldLength)
	debugID = sanitizeDiagnosticField(debugID, MaxDiagnosticFieldLength)

	if name == "" && issue == "" && debugID == "" {
		return ErrForbidden
	}

	return &APIForbiddenError{
		Name:    name,
		DebugID: debugID,
		Issue:   issue,
	}
}

// DuplicatePayoutError represents a PayPal response indicating sender_batch_id was already submitted.
type DuplicatePayoutError struct {
	OriginalBatchID string
	RawResponse     string
	Links           []LinkDescription
	Message         string
}

func (e *DuplicatePayoutError) Error() string {
	if e.OriginalBatchID != "" {
		return fmt.Sprintf("paypal duplicate payout request: original batch id %s: %s", e.OriginalBatchID, e.Message)
	}
	return fmt.Sprintf("paypal duplicate payout request: %s", e.Message)
}

// ExtractAndValidateBatchIDFromHATEOAS extracts and verifies the original PayPal batch ID
// from HATEOAS links in a duplicate payout error response.
//
// Invariants:
// - Does not blindly trust provider-supplied URLs.
// - Asserts that the host matches the client's configured Sandbox base URL host.
// - Asserts that the scheme matches the client's configured base URL scheme.
// - Validates that the path conforms to /v1/payments/payouts/{id}.
// - Rejects path traversal or invalid characters in the batch ID.
func ExtractAndValidateBatchIDFromHATEOAS(links []LinkDescription, expectedBaseURL string) (string, error) {
	if len(links) == 0 {
		return "", fmt.Errorf("%w: no links provided in response", ErrMalformedHATEOASLink)
	}

	expectedU, err := url.Parse(strings.TrimSpace(expectedBaseURL))
	if err != nil {
		return "", fmt.Errorf("invalid expected base URL %q: %w", expectedBaseURL, err)
	}
	expectedHost := strings.ToLower(expectedU.Host)
	expectedScheme := strings.ToLower(expectedU.Scheme)

	for _, link := range links {
		href := strings.TrimSpace(link.Href)
		if href == "" {
			continue
		}

		u, err := url.Parse(href)
		if err != nil {
			return "", fmt.Errorf("%w: invalid href %q: %v", ErrMalformedHATEOASLink, href, err)
		}

		// Security assertion: Host must strictly match expected base URL host if present
		if u.Host != "" {
			if strings.ToLower(u.Host) != expectedHost {
				return "", fmt.Errorf("%w: link host %q does not match expected host %q", ErrUntrustedHATEOASHost, u.Host, expectedHost)
			}
		}

		// Security assertion: Scheme must match expected base URL scheme if present
		if u.Scheme != "" {
			if strings.ToLower(u.Scheme) != expectedScheme {
				return "", fmt.Errorf("%w: link scheme %q does not match expected scheme %q", ErrUntrustedHATEOASHost, u.Scheme, expectedScheme)
			}
		}

		// Validate path: must be /v1/payments/payouts/{batch_id}
		cleanPath := strings.Trim(u.Path, "/")
		parts := strings.Split(cleanPath, "/")

		var batchID string
		if len(parts) == 4 && parts[0] == "v1" && parts[1] == "payments" && parts[2] == "payouts" {
			batchID = parts[3]
		} else if len(parts) == 3 && parts[0] == "payments" && parts[1] == "payouts" {
			batchID = parts[2]
		}

		if batchID != "" {
			if strings.ContainsAny(batchID, "/\\?%#") || strings.Contains(batchID, "..") {
				return "", fmt.Errorf("%w: invalid characters or traversal in batch ID %q", ErrMalformedHATEOASLink, batchID)
			}
			return batchID, nil
		}
	}

	return "", fmt.Errorf("%w: no matching payout batch route found in links", ErrMalformedHATEOASLink)
}

// CreatePayoutRequest specifies payout creation parameters.
type CreatePayoutRequest struct {
	SenderBatchID string
	SenderItemID  string
	Recipient     string
	Amount        string
	Currency      string
	Note          string
	EmailSubject  string
}

// CreatePayoutResponse represents PayPal's response to payout submission.
type CreatePayoutResponse struct {
	BatchHeader PayoutBatchHeader `json:"batch_header"`
	Links       []LinkDescription `json:"links,omitempty"`
}

func (cpr *CreatePayoutResponse) UnmarshalJSON(data []byte) error {
	type Alias CreatePayoutResponse
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*cpr = CreatePayoutResponse(aux)
	cpr.BatchHeader.Normalize()
	return nil
}

// ValidateSandboxBaseURL verifies that the URL strictly targets the official PayPal sandbox.
func ValidateSandboxBaseURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("%w: endpoint URL cannot be empty", ErrInvalidSandboxEndpoint)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: malformed URL: %w", ErrInvalidSandboxEndpoint, err)
	}

	if u.User != nil {
		return fmt.Errorf("%w: embedded credentials/userinfo not allowed", ErrInvalidSandboxEndpoint)
	}

	host := strings.ToLower(u.Hostname())
	if host == "api.paypal.com" || host == "api-m.paypal.com" || host == "www.paypal.com" {
		return fmt.Errorf("%w: %s is a production host", ErrProductionHostBlocked, host)
	}

	if strings.ToLower(u.Scheme) != "https" {
		return fmt.Errorf("%w: scheme must be https, got %q", ErrInvalidSandboxEndpoint, u.Scheme)
	}

	if host != "api-m.sandbox.paypal.com" {
		return fmt.Errorf("%w: host must be api-m.sandbox.paypal.com, got %q", ErrInvalidSandboxEndpoint, host)
	}

	port := u.Port()
	if port != "" && port != "443" {
		return fmt.Errorf("%w: unexpected port %q; only standard HTTPS (443) permitted", ErrInvalidSandboxEndpoint, port)
	}

	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%w: unexpected URL path %q; base URL must not contain a path", ErrInvalidSandboxEndpoint, u.Path)
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: query parameters or fragments not permitted", ErrInvalidSandboxEndpoint)
	}

	return nil
}

// ClientOption configures optional behavior on Client construction.
type ClientOption func(*clientOptions)

type clientOptions struct {
	simulatedBaseURL string
}

// WithSimulatedBaseURL is an explicit testing option allowing test suites to target
// simulated local HTTP test servers (e.g. httptest.Server).
func WithSimulatedBaseURL(testURL string) ClientOption {
	return func(o *clientOptions) {
		o.simulatedBaseURL = strings.TrimRight(testURL, "/")
	}
}

// NewTestClient is a convenience test constructor that configures a simulated endpoint.
func NewTestClient(cfg *config.Config, httpClient *http.Client, simulatedURL string) (*Client, error) {
	return NewClient(cfg, httpClient, WithSimulatedBaseURL(simulatedURL))
}

// NewClient initializes a PayPal sandbox client with strict security assertions.
// By default, it strictly validates that the endpoint is https://api-m.sandbox.paypal.com.
func NewClient(cfg *config.Config, httpClient *http.Client, opts ...ClientOption) (*Client, error) {
	if cfg.PayPalEnv != "sandbox" {
		return nil, ErrSandboxAssertionFailed
	}

	var options clientOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	targetURL := OfficialSandboxBaseURL
	if options.simulatedBaseURL != "" {
		if strings.EqualFold(cfg.AppEnv, "production") {
			return nil, ErrSimulatedEndpointForbidden
		}
		targetURL = options.simulatedBaseURL
	} else {
		if err := ValidateSandboxBaseURL(cfg.PayPalBaseURL); err != nil {
			return nil, err
		}
		targetURL = OfficialSandboxBaseURL
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.PayPalRequestTimeout,
		}
	}

	return &Client{
		baseURL:      targetURL,
		clientID:     cfg.PayPalClientID,
		clientSecret: cfg.PayPalClientSecret,
		httpClient:   httpClient,
	}, nil
}

// GetAccessToken acquires or returns a cached OAuth2 Bearer token using client_credentials.
func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	if c.clientID == "" || c.clientSecret == "" {
		return "", ErrMissingCredentials
	}

	c.tokenMu.RLock()
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		token := c.cachedToken
		c.tokenMu.RUnlock()
		return token, nil
	}
	c.tokenMu.RUnlock()

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double check after obtaining write lock
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.cachedToken, nil
	}

	tokenURL := fmt.Sprintf("%s/v1/oauth2/token", c.baseURL)
	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create oauth token request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", c.clientID, c.clientSecret)))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth token request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := readResponseBody(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read oauth token response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return "", parseForbiddenError(resp, bodyBytes)
	}
	if resp.StatusCode != http.StatusOK {
		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return "", fmt.Errorf("oauth token endpoint returned status %d: %s", resp.StatusCode, sanitizedBody)
	}

	var tokenResp OAuthTokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse oauth token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", errors.New("empty access_token received from paypal oauth")
	}

	c.cachedToken = tokenResp.AccessToken
	c.hasPayoutsScope = HasPayoutsScope(tokenResp.Scope)
	// Buffer expiry by 60 seconds to avoid race conditions
	buffer := 60 * time.Second
	if time.Duration(tokenResp.ExpiresIn)*time.Second > buffer {
		c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn)*time.Second - buffer)
	} else {
		c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	slog.InfoContext(ctx, "Acquired PayPal sandbox OAuth2 access token successfully",
		slog.String("token_type", tokenResp.TokenType),
		slog.Int("expires_in", tokenResp.ExpiresIn),
		slog.String("app_id", tokenResp.AppID),
		slog.Bool("has_payouts_scope", c.hasPayoutsScope),
	)

	return c.cachedToken, nil
}

// GetPayoutBatch fetches payout batch details given a payout_batch_id.
func (c *Client) GetPayoutBatch(ctx context.Context, payoutBatchID string) (*PayoutBatchResponse, error) {
	if strings.TrimSpace(payoutBatchID) == "" {
		return nil, errors.New("payout_batch_id cannot be empty")
	}

	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	urlStr := fmt.Sprintf("%s/v1/payments/payouts/%s", c.baseURL, url.PathEscape(payoutBatchID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct payout batch request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get payout batch request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read payout batch response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: batch id %s", ErrPayoutNotFound, payoutBatchID)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, parseForbiddenError(resp, bodyBytes)
	}
	if resp.StatusCode != http.StatusOK {
		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return nil, fmt.Errorf("paypal get payout batch failed with status %d: %s", resp.StatusCode, sanitizedBody)
	}

	var batchResp PayoutBatchResponse
	if err := json.Unmarshal(bodyBytes, &batchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payout batch response: %w", err)
	}

	return &batchResp, nil
}

// GetPayoutItem fetches payout item details given a payout_item_id.
func (c *Client) GetPayoutItem(ctx context.Context, payoutItemID string) (*PayoutItemResponse, error) {
	if strings.TrimSpace(payoutItemID) == "" {
		return nil, errors.New("payout_item_id cannot be empty")
	}

	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	urlStr := fmt.Sprintf("%s/v1/payments/payouts-item/%s", c.baseURL, url.PathEscape(payoutItemID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct payout item request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get payout item request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read payout item response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: item id %s", ErrPayoutNotFound, payoutItemID)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, parseForbiddenError(resp, bodyBytes)
	}
	if resp.StatusCode != http.StatusOK {
		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return nil, fmt.Errorf("paypal get payout item failed with status %d: %s", resp.StatusCode, sanitizedBody)
	}

	var itemResp PayoutItemResponse
	if err := json.Unmarshal(bodyBytes, &itemResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payout item response: %w", err)
	}

	return &itemResp, nil
}

// CreatePayout submits a single-item payout batch using direct REST call.
// SAFETY: Never automatically retries with a new idempotency key on ambiguous timeout.
func (c *Client) CreatePayout(ctx context.Context, req CreatePayoutRequest) (*CreatePayoutResponse, error) {
	if strings.TrimSpace(req.SenderBatchID) == "" || strings.TrimSpace(req.Recipient) == "" || strings.TrimSpace(req.Amount) == "" {
		return nil, ErrInvalidPayoutRequest
	}

	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	itemMap := map[string]interface{}{
		"recipient_type": "EMAIL",
		"amount": map[string]string{
			"value":    req.Amount,
			"currency": req.Currency,
		},
		"receiver": req.Recipient,
		"note":     req.Note,
	}
	if req.SenderItemID != "" {
		itemMap["sender_item_id"] = req.SenderItemID
	}

	payload := map[string]interface{}{
		"sender_batch_header": map[string]interface{}{
			"sender_batch_id": req.SenderBatchID,
			"email_subject":   req.EmailSubject,
		},
		"items": []map[string]interface{}{
			itemMap,
		},
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payout payload: %w", err)
	}

	payoutURL := fmt.Sprintf("%s/v1/payments/payouts", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, payoutURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to construct payout request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// If context deadline exceeded or network error, mark as ambiguous timeout.
		// DO NOT RETRY WITH NEW BATCH ID!
		return nil, fmt.Errorf("%w: %v", ErrAmbiguousNetworkTimeout, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: response read failed: %w", ErrAmbiguousNetworkTimeout, err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, parseForbiddenError(resp, bodyBytes)
	}

	// Ambiguous HTTP 5xx responses (500, 502, 503, 504) per ADR-005
	if resp.StatusCode == http.StatusInternalServerError ||
		resp.StatusCode == http.StatusBadGateway ||
		resp.StatusCode == http.StatusServiceUnavailable ||
		resp.StatusCode == http.StatusGatewayTimeout {
		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return nil, fmt.Errorf("%w: paypal create payout server error (status %d): %s", ErrAmbiguousServerResponse, resp.StatusCode, sanitizedBody)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		var apiErr APIErrorResponse
		if jsonErr := json.Unmarshal(bodyBytes, &apiErr); jsonErr == nil {
			isDup := strings.EqualFold(apiErr.Name, "DUPLICATE_REQUEST") ||
				strings.EqualFold(apiErr.Name, "DUPLICATE_TRANSACTION") ||
				strings.Contains(strings.ToUpper(apiErr.Message), "DUPLICATE")
			if !isDup {
				for _, detail := range apiErr.Details {
					if strings.Contains(strings.ToUpper(detail.Issue), "DUPLICATE") ||
						strings.Contains(strings.ToUpper(detail.Issue), "ALREADY SUBMITTED") {
						isDup = true
						break
					}
				}
			}

			if isDup {
				batchID, extractErr := ExtractAndValidateBatchIDFromHATEOAS(apiErr.Links, c.baseURL)
				if extractErr != nil {
					return nil, &DuplicatePayoutError{
						OriginalBatchID: "",
						RawResponse:     string(bodyBytes),
						Links:           apiErr.Links,
						Message:         fmt.Sprintf("%s (link validation error: %v)", apiErr.Message, extractErr),
					}
				}
				return nil, &DuplicatePayoutError{
					OriginalBatchID: batchID,
					RawResponse:     string(bodyBytes),
					Links:           apiErr.Links,
					Message:         apiErr.Message,
				}
			}
		}

		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return nil, fmt.Errorf("paypal create payout failed with status %d: %s", resp.StatusCode, sanitizedBody)
	}

	var payoutResp CreatePayoutResponse
	if err := json.Unmarshal(bodyBytes, &payoutResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payout creation response: %w", err)
	}

	return &payoutResp, nil
}

// CancelPayoutItem cancels an unclaimed payout item via POST /v1/payments/payouts-item/{id}/cancel.
func (c *Client) CancelPayoutItem(ctx context.Context, payoutItemID string) (*PayoutItemResponse, error) {
	if strings.TrimSpace(payoutItemID) == "" {
		return nil, errors.New("payout_item_id cannot be empty")
	}

	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	cancelURL := fmt.Sprintf("%s/v1/payments/payouts-item/%s/cancel", c.baseURL, url.PathEscape(payoutItemID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cancelURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct cancel payout request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cancel payout item request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read cancel payout item response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: item id %s", ErrPayoutNotFound, payoutItemID)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, parseForbiddenError(resp, bodyBytes)
	}
	if resp.StatusCode != http.StatusOK {
		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return nil, fmt.Errorf("paypal cancel payout item failed with status %d: %s", resp.StatusCode, sanitizedBody)
	}

	var itemResp PayoutItemResponse
	if err := json.Unmarshal(bodyBytes, &itemResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cancel item response: %w", err)
	}

	return &itemResp, nil
}

// ConnectionStatus represents the live verified status of the PayPal Sandbox connection.
type ConnectionStatus struct {
	Connected      bool      `json:"connected"`
	Environment    string    `json:"environment"`
	BaseURL        string    `json:"base_url"`
	AppID          string    `json:"app_id,omitempty"`
	TokenExpiresIn int       `json:"token_expires_in,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
	Error          string    `json:"error,omitempty"`
}

// CheckConnection probes PayPal Sandbox by acquiring an actual OAuth2 token.
func (c *Client) CheckConnection(ctx context.Context) *ConnectionStatus {
	now := time.Now().UTC()
	status := &ConnectionStatus{
		Environment: "sandbox",
		BaseURL:     c.baseURL,
		CheckedAt:   now,
	}

	if c.clientID == "" || c.clientSecret == "" {
		status.Connected = false
		status.Error = "PayPal credentials missing: PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET are not configured"
		return status
	}

	// Force fresh token check if needed or probe sandbox
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		status.Connected = false
		status.Error = err.Error()
		return status
	}

	status.Connected = token != ""
	c.tokenMu.RLock()
	if c.tokenExpiry.After(now) {
		status.TokenExpiresIn = int(time.Until(c.tokenExpiry).Seconds())
	}
	c.tokenMu.RUnlock()

	return status
}
