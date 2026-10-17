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
	ErrProductionHostBlocked     = errors.New("security violation: production PayPal host detected; only sandbox permitted")
	ErrMissingCredentials        = errors.New("paypal credentials missing: client_id and client_secret are required")
	ErrUnauthorized              = errors.New("paypal authentication failed: invalid client credentials")
	ErrPayoutNotFound            = errors.New("paypal payout batch or item not found")
	ErrAmbiguousNetworkTimeout   = errors.New("ambiguous network timeout during payout mutation: DO NOT RETRY WITH NEW ID; run reconciliation")
	ErrInvalidPayoutRequest      = errors.New("invalid payout request: missing sender_batch_id or items")
	ErrSandboxAssertionFailed    = errors.New("paypal environment is not sandbox")
)

// Client is a server-side PayPal REST API client configured strictly for Sandbox.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	tokenMu     sync.RWMutex
	cachedToken string
	tokenExpiry time.Time
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

// PayoutBatchHeader represents metadata about a PayPal Payout Batch.
type PayoutBatchHeader struct {
	PayoutBatchID   string `json:"payout_batch_id"`
	BatchStatus     string `json:"batch_status"`
	TimeCreated     string `json:"time_created"`
	TimeCompleted   string `json:"time_completed,omitempty"`
	SenderBatchID   string `json:"sender_batch_header.sender_batch_id,omitempty"`
	FundingSource   string `json:"funding_source,omitempty"`
	Amount          *CurrencyAmount `json:"amount,omitempty"`
	Fees            *CurrencyAmount `json:"fees,omitempty"`
}

// CurrencyAmount represents a PayPal currency & value pair.
type CurrencyAmount struct {
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

// PayoutItem represents a single item inside a payout batch.
type PayoutItem struct {
	PayoutItemID      string          `json:"payout_item_id"`
	TransactionID     string          `json:"transaction_id,omitempty"`
	TransactionStatus string          `json:"transaction_status"`
	PayoutItemFee     *CurrencyAmount `json:"payout_item_fee,omitempty"`
	PayoutBatchID     string          `json:"payout_batch_id"`
	SenderItemID      string          `json:"sender_item_id,omitempty"`
	Amount            CurrencyAmount  `json:"amount"`
	Receiver          string          `json:"receiver"`
	RecipientType     string          `json:"recipient_type,omitempty"`
	Note              string          `json:"note,omitempty"`
	Errors            json.RawMessage `json:"errors,omitempty"`
}

// PayoutBatchResponse represents the payload returned by GET /v1/payments/payouts/{id}.
type PayoutBatchResponse struct {
	BatchHeader PayoutBatchHeader `json:"batch_header"`
	Items       []PayoutItem      `json:"items,omitempty"`
	Links       []LinkDescription `json:"links,omitempty"`
}

// PayoutItemResponse represents the payload returned by GET /v1/payments/payouts-item/{id}.
type PayoutItemResponse struct {
	PayoutItemID      string          `json:"payout_item_id"`
	TransactionID     string          `json:"transaction_id,omitempty"`
	TransactionStatus string          `json:"transaction_status"`
	PayoutBatchID     string          `json:"payout_batch_id"`
	Amount            CurrencyAmount  `json:"amount"`
	Receiver          string          `json:"receiver"`
	Errors            json.RawMessage `json:"errors,omitempty"`
}

// LinkDescription represents PayPal HATEOAS links.
type LinkDescription struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

// CreatePayoutRequest specifies payout creation parameters.
type CreatePayoutRequest struct {
	SenderBatchID string
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

// NewClient initializes a PayPal sandbox client with strict security assertions.
func NewClient(cfg *config.Config, httpClient *http.Client) (*Client, error) {
	if cfg.PayPalEnv != "sandbox" {
		return nil, ErrSandboxAssertionFailed
	}

	u, err := url.Parse(cfg.PayPalBaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid paypal base URL: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	if host == "api.paypal.com" || host == "api-m.paypal.com" || host == "www.paypal.com" {
		return nil, fmt.Errorf("%w: cannot use production host %s", ErrProductionHostBlocked, host)
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.PayPalRequestTimeout,
		}
	}

	return &Client{
		baseURL:      strings.TrimRight(cfg.PayPalBaseURL, "/"),
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read oauth token response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read payout batch response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: batch id %s", ErrPayoutNotFound, payoutBatchID)
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read payout item response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: item id %s", ErrPayoutNotFound, payoutItemID)
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

	payload := map[string]interface{}{
		"sender_batch_header": map[string]interface{}{
			"sender_batch_id": req.SenderBatchID,
			"email_subject":   req.EmailSubject,
		},
		"items": []map[string]interface{}{
			{
				"recipient_type": "EMAIL",
				"amount": map[string]string{
					"value":    req.Amount,
					"currency": req.Currency,
				},
				"receiver": req.Recipient,
				"note":     req.Note,
			},
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: response read failed: %v", ErrAmbiguousNetworkTimeout, err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		sanitizedBody := logger.SanitizeText(string(bodyBytes))
		return nil, fmt.Errorf("paypal create payout failed with status %d: %s", resp.StatusCode, sanitizedBody)
	}

	var payoutResp CreatePayoutResponse
	if err := json.Unmarshal(bodyBytes, &payoutResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payout creation response: %w", err)
	}

	return &payoutResp, nil
}
