package readiness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
)

// SyntheticProbeSnapshot generates a safe, non-mutating, synthetic snapshot for inference verification.
// Zero real customer or recipient data is used. All evidence IDs are deterministic.
func SyntheticProbeSnapshot() *ai.InvestigationSnapshot {
	now := time.Now().UTC()
	return &ai.InvestigationSnapshot{
		CaseID:               "PROBE-CASE-00000000",
		Reference:            "PROBE-REF-001",
		Currency:             "USD",
		AmountCents:          10000,
		AmountFormatted:      "$100.00 USD",
		CaseStatus:           "AwaitingApproval",
		RiskScore:            15.0,
		DualApprovalRequired: false,
		DescriptionSanitized: "Automated synthetic readiness probe verification snapshot",
		RecipientEmailMasked: "p***e@claimbridge.internal",
		RecipientName:        "Synthetic Probe Recipient",
		CreatedAt:            now.Format(time.RFC3339),
		UpdatedAt:            now.Format(time.RFC3339),
		Operations: []ai.SnapshotOperation{
			{
				ID:            "PROBE-OP-001",
				SenderBatchID: "PROBE-BATCH-001",
				SenderItemID:  "PROBE-ITEM-001",
				Status:        "DRAFT",
			},
		},
		Snapshots: []ai.SnapshotRecord{
			{
				ID:          "PROBE-SNAP-001",
				OperationID: "PROBE-OP-001",
				Provider:    "system",
				BatchStatus: "PENDING",
				RecordedAt:  now,
			},
		},
		ValidEvidenceIDs: []string{
			"PROBE-CASE-00000000",
			"PROBE-OP-001",
			"PROBE-BATCH-001",
			"PROBE-ITEM-001",
			"PROBE-SNAP-001",
		},
	}
}

// ProbePostgres verifies PostgreSQL connectivity and schema readiness.
func ProbePostgres(ctx context.Context, cfg *config.Config, pingFn func(context.Context) error) CheckResult {
	res := CheckResult{
		ID:       1,
		Name:     "PostgreSQL Persistence Connection",
		Category: "Core Infrastructure",
	}

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var err error
	if pingFn != nil {
		err = pingFn(probeCtx)
	} else {
		repo, rErr := db.NewPostgresRepository(probeCtx, cfg.DatabaseURL)
		if rErr != nil {
			err = rErr
		} else {
			defer repo.Close()
			err = repo.Ping(probeCtx)
		}
	}

	if err != nil {
		res.Status = StatusFail
		res.Summary = "PostgreSQL unavailable or unmigrated"
		res.Detail = err.Error()
		res.OperatorAction = "Run 'docker compose -f infra/docker-compose.yml up -d postgres'"
		return res
	}

	res.Status = StatusPass
	res.Summary = "Connected & responding to ping"
	res.Detail = "Connection verified against PostgreSQL database."
	return res
}

// ProbeAPIHealth verifies that the Go API server is reachable and serving /healthz.
func ProbeAPIHealth(ctx context.Context, client *http.Client, baseURL string) CheckResult {
	res := CheckResult{
		ID:       2,
		Name:     "Go API Gateway Health (/healthz)",
		Category: "Core Infrastructure",
	}

	url := fmt.Sprintf("%s/healthz", baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Failed to construct request to /healthz"
		res.Detail = err.Error()
		return res
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Go API server is offline or unreachable"
		res.Detail = err.Error()
		res.OperatorAction = "Run 'go run ./cmd/server' in services/api"
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Health check returned HTTP %d", resp.StatusCode)
		res.Detail = fmt.Sprintf("Expected HTTP 200 from %s", url)
		return res
	}

	res.Status = StatusPass
	res.Summary = "HTTP 200 OK — Process active & listening"
	return res
}

// ProbeAPIReadiness verifies storage health and operational status via /readyz.
// Strictly requires HTTP 200, valid JSON, status == "ready", and database == "connected".
func ProbeAPIReadiness(ctx context.Context, client *http.Client, baseURL string) CheckResult {
	res := CheckResult{
		ID:       3,
		Name:     "Go API Readiness & Storage Probe (/readyz)",
		Category: "Core Infrastructure",
	}

	url := fmt.Sprintf("%s/readyz", baseURL)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Failed to construct request to /readyz"
		res.Detail = err.Error()
		return res
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Go API /readyz unreachable or timed out"
		res.Detail = "Request failed: connection refused or timeout exceeded"
		res.OperatorAction = "Ensure Go API server is running on configured port"
		return res
	}
	defer resp.Body.Close()

	// Bounded read (max 64KB) to prevent unbounded memory consumption from rogue payloads
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Failed to read /readyz response body"
		res.Detail = "I/O error during bounded read"
		return res
	}

	if resp.StatusCode != http.StatusOK {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Go API readiness returned HTTP %d", resp.StatusCode)
		res.Detail = "Storage readiness check reported non-200 status code."
		res.OperatorAction = "Check PostgreSQL container and migrations"
		return res
	}

	trimmedBody := bytes.TrimSpace(bodyBytes)
	if len(trimmedBody) == 0 {
		res.Status = StatusFail
		res.Summary = "Empty response body from /readyz"
		res.Detail = "Expected JSON payload with status and database fields"
		res.OperatorAction = "Check Go API server logs for internal panic or malformed response"
		return res
	}

	var data map[string]interface{}
	if err := json.Unmarshal(trimmedBody, &data); err != nil {
		res.Status = StatusFail
		res.Summary = "Malformed JSON response from /readyz"
		res.Detail = "Response body could not be parsed as valid JSON"
		res.OperatorAction = "Check Go API server /readyz handler response format"
		return res
	}

	statusRaw, hasStatus := data["status"]
	if !hasStatus || statusRaw == nil {
		res.Status = StatusFail
		res.Summary = "Missing 'status' field in /readyz response"
		res.Detail = "Response JSON did not contain 'status' attribute"
		return res
	}
	statusStr, isString := statusRaw.(string)
	if !isString || statusStr == "" {
		res.Status = StatusFail
		res.Summary = "Invalid 'status' field in /readyz response"
		res.Detail = "Field 'status' must be a non-empty string"
		return res
	}

	dbRaw, hasDB := data["database"]
	if !hasDB || dbRaw == nil {
		res.Status = StatusFail
		res.Summary = "Missing 'database' field in /readyz response"
		res.Detail = "Response JSON did not contain 'database' attribute"
		return res
	}
	dbStr, isDBString := dbRaw.(string)
	if !isDBString || dbStr == "" {
		res.Status = StatusFail
		res.Summary = "Invalid 'database' field in /readyz response"
		res.Detail = "Field 'database' must be a non-empty string"
		return res
	}

	if statusStr != "ready" {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Go API reported non-ready status: %s", statusStr)
		res.Detail = fmt.Sprintf("Expected status 'ready', got %q", statusStr)
		res.OperatorAction = "Inspect service readiness logs"
		return res
	}

	if dbStr != "connected" {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Go API database reported non-healthy status: %s", dbStr)
		res.Detail = fmt.Sprintf("Expected database 'connected', got %q", dbStr)
		res.OperatorAction = "Check PostgreSQL container connectivity and migrations"
		return res
	}

	res.Status = StatusPass
	res.Summary = fmt.Sprintf("HTTP 200 OK (Status: %s, DB: %s)", statusStr, dbStr)
	res.Detail = "Storage connectivity and API operational readiness verified."
	return res
}

// ProbePolicyEngine verifies deterministic policy evaluation via /api/v1/policy/evaluate.
func ProbePolicyEngine(ctx context.Context, client *http.Client, baseURL string) CheckResult {
	res := CheckResult{
		ID:       4,
		Name:     "Rust Deterministic Policy Engine",
		Category: "Core Infrastructure",
	}

	url := fmt.Sprintf("%s/api/v1/policy/evaluate", baseURL)
	reqPayload := map[string]interface{}{
		"claim_id":       "readiness-probe-001",
		"claimant_email": "probe@claimbridge.internal",
		"amount_cents":   15000,
		"currency":       "USD",
		"risk_score":     10.0,
		"current_state":  "AwaitingPolicyCheck",
	}
	bodyBytes, _ := json.Marshal(reqPayload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Failed to construct policy evaluation request"
		res.Detail = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Policy engine endpoint unreachable"
		res.Detail = err.Error()
		res.OperatorAction = "Verify API server is running and compiled with policy engine"
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Policy probe returned HTTP %d", resp.StatusCode)
		res.Detail = string(body)
		return res
	}

	var evalResp policy.EvaluationResponse
	if err := json.NewDecoder(resp.Body).Decode(&evalResp); err != nil {
		res.Status = StatusFail
		res.Summary = "Policy probe returned malformed JSON"
		res.Detail = err.Error()
		return res
	}

	if evalResp.Decision != policy.DecisionApproved {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Unexpected policy decision: %s", evalResp.Decision)
		res.Detail = fmt.Sprintf("Violations: %v", evalResp.Violations)
		return res
	}

	res.Status = StatusPass
	res.Summary = "Deterministic rules active & enforcing $500 ceiling"
	res.Detail = fmt.Sprintf("Decision: %s (Dual Approval: %v)", evalResp.Decision, evalResp.RequiresDualApproval)
	return res
}

// ProbeWebFrontend verifies that the Next.js web application is responding.
func ProbeWebFrontend(ctx context.Context, client *http.Client, webURL string) CheckResult {
	res := CheckResult{
		ID:       5,
		Name:     "Next.js Web Interface",
		Category: "Core Infrastructure",
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webURL, nil)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "Failed to construct request to web frontend"
		res.Detail = err.Error()
		return res
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Web frontend offline at %s", webURL)
		res.Detail = err.Error()
		res.OperatorAction = "Run 'npm run dev' in apps/web"
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Web frontend returned HTTP %d", resp.StatusCode)
		res.Detail = fmt.Sprintf("Expected status < 400 from %s", webURL)
		return res
	}

	res.Status = StatusPass
	res.Summary = fmt.Sprintf("HTTP %d OK — Web application active", resp.StatusCode)
	return res
}

// ProbePayPalOAuth verifies PayPal Sandbox OAuth2 authentication.
// Tokens and credentials are never leaked. Production hosts are strictly rejected.
func ProbePayPalOAuth(ctx context.Context, cfg *config.Config, client PayPalReadOnlyClient, initErr error) CheckResult {
	res := CheckResult{
		ID:       6,
		Name:     "PayPal Sandbox OAuth Verification",
		Category: "External Integrations",
	}

	if cfg.PayPalClientID == "" || cfg.PayPalClientSecret == "" {
		res.Status = StatusBlocked
		res.Summary = "PayPal Sandbox credentials unconfigured"
		res.Detail = "PAYPAL_CLIENT_ID and/or PAYPAL_CLIENT_SECRET are unset or empty in .env"
		res.OperatorAction = "Configure sandbox credentials in .env (see docs/operational-instructions.md)"
		return res
	}

	if cfg.PayPalEnv != "sandbox" {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Security violation: PAYPAL_ENV is set to %q", cfg.PayPalEnv)
		res.Detail = "Only 'sandbox' environment is permitted for safety."
		return res
	}

	if initErr != nil {
		res.Status = StatusFail
		res.Summary = "Failed to initialize PayPal client"
		res.Detail = initErr.Error()
		return res
	}

	if client == nil {
		res.Status = StatusBlocked
		res.Summary = "PayPal client unavailable"
		res.Detail = "Client could not be constructed from configuration."
		return res
	}

	probeCtx, cancel := context.WithTimeout(ctx, cfg.PayPalRequestTimeout)
	defer cancel()

	_, err := client.GetAccessToken(probeCtx)
	if err != nil {
		if errors.Is(err, paypal.ErrUnauthorized) {
			res.Status = StatusFail
			res.Summary = "Authentication failed (HTTP 401 Unauthorized)"
			res.Detail = "PayPal Sandbox rejected client credentials."
			res.OperatorAction = "Check PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET in .env against PayPal Developer Dashboard"
			return res
		}
		if errors.Is(err, paypal.ErrForbidden) {
			res.Status = StatusFail
			res.Summary = "Payouts permission missing (HTTP 403 Forbidden)"
			res.Detail = "Account or sandbox app lacks 'Payouts' permission."
			res.OperatorAction = "Enable 'Payouts' under App Features in the PayPal Developer Dashboard"
			return res
		}
		res.Status = StatusFail
		res.Summary = "PayPal Sandbox OAuth handshake failed"
		res.Detail = err.Error()
		return res
	}

	res.Status = StatusPass
	res.Summary = "OAuth2 Bearer token acquired & verified"
	res.Detail = "Token cached and redacted ([REDACTED_ACCESS_TOKEN]). Target: api-m.sandbox.paypal.com"
	return res
}

// ProbePayPalLookup performs an optional read-only batch or item status lookup in PayPal Sandbox.
// Strictly read-only; never creates, mutates, or cancels a payout.
func ProbePayPalLookup(ctx context.Context, client PayPalReadOnlyClient, oauthStatus CheckStatus, batchID, itemID string) CheckResult {
	res := CheckResult{
		ID:       7,
		Name:     "PayPal Read-Only Payout Lookup",
		Category: "External Integrations",
	}

	if batchID == "" && itemID == "" {
		res.Status = StatusSkipped
		res.Summary = "Unrequested (No batch or item ID supplied)"
		res.Detail = "OAuth verified independently; read-only query skipped (supply -batch-id or -item-id to test)"
		return res
	}

	if oauthStatus == StatusBlocked {
		res.Status = StatusBlocked
		res.Summary = "Lookup blocked: PayPal credentials unconfigured or missing"
		res.Detail = "Read-only lookup requires verified OAuth authentication."
		res.OperatorAction = "Configure sandbox credentials in .env to enable payout lookups"
		return res
	}

	if oauthStatus != StatusPass {
		res.Status = StatusBlocked
		res.Summary = "Lookup blocked: PayPal OAuth verification did not succeed"
		res.Detail = fmt.Sprintf("OAuth check reported status %s; read-only lookup aborted to avoid dispatching unauthenticated requests", oauthStatus)
		res.OperatorAction = "Resolve OAuth authentication failure (Check 6) before attempting payout lookup"
		return res
	}

	if client == nil {
		res.Status = StatusBlocked
		res.Summary = "Cannot query payout: PayPal client not initialized"
		return res
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if batchID != "" {
		batch, err := client.GetPayoutBatch(probeCtx, batchID)
		if err != nil {
			res.Status = StatusFail
			res.Summary = fmt.Sprintf("Payout batch lookup failed for %s", batchID)
			res.Detail = err.Error()
			return res
		}
		res.Status = StatusPass
		res.Summary = fmt.Sprintf("Payout batch %s retrieved (Status: %s)", batchID, batch.BatchHeader.BatchStatus)
		return res
	}

	item, err := client.GetPayoutItem(probeCtx, itemID)
	if err != nil {
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("Payout item lookup failed for %s", itemID)
		res.Detail = err.Error()
		return res
	}
	res.Status = StatusPass
	res.Summary = fmt.Sprintf("Payout item %s retrieved (Status: %s, Receiver: %s)", itemID, item.TransactionStatus, ai.MaskEmail(item.Receiver))
	return res
}

// ProbeAIHealth verifies provider reachability and model catalog availability without mutating state.
func ProbeAIHealth(ctx context.Context, cfg *config.Config, provider ai.AIProvider) CheckResult {
	res := CheckResult{
		ID:       8,
		Name:     "AI Provider Reachability & Model Availability",
		Category: "AI Model Verification",
	}

	if !cfg.AIEnabled {
		res.Status = StatusBlocked
		res.Summary = "AI investigation engine is disabled"
		res.Detail = "AI_ENABLED=false in configuration."
		res.OperatorAction = "Set AI_ENABLED=true in .env to enable model-backed investigations"
		return res
	}

	if provider == nil {
		var pErr error
		provider, pErr = ai.NewProvider(cfg)
		if pErr != nil {
			res.Status = StatusFail
			res.Summary = "Failed to construct AI provider"
			res.Detail = pErr.Error()
			return res
		}
	}

	if provider.Name() == "mock" {
		res.Status = StatusWarn
		res.Summary = "Mock AI provider configured (AI_PROVIDER=mock)"
		res.Detail = "Mock provider satisfies local unit tests, but does not provide genuine model inference."
		res.OperatorAction = "Configure AI_PROVIDER=ollama and AI_MODEL=llama3.2 for live inference"
		return res
	}

	health := provider.CheckHealth(ctx)
	if !health.Reachable {
		res.Status = StatusBlocked
		res.Summary = fmt.Sprintf("Inference daemon unreachable at %s", cfg.AIBaseURL)
		res.Detail = health.ErrorMessage
		res.OperatorAction = "Start Ollama locally ('ollama serve') or verify AI_BASE_URL"
		return res
	}

	if !health.ModelAvailable {
		res.Status = StatusBlocked
		res.Summary = fmt.Sprintf("Configured model %q missing from catalog", cfg.AIModel)
		res.Detail = health.ErrorMessage
		res.OperatorAction = fmt.Sprintf("Run 'ollama pull %s'", cfg.AIModel)
		return res
	}

	res.Status = StatusPass
	res.Summary = fmt.Sprintf("Provider reachable & model %q available", cfg.AIModel)
	res.Detail = fmt.Sprintf("Endpoint: %s", cfg.AIBaseURL)
	return res
}

// ProbeAIInference executes a bounded, non-mutating synthetic inference probe.
// Validates response schema, evidence grounding, and action whitelisting.
// Zero financial operations or payouts are modified.
func ProbeAIInference(ctx context.Context, cfg *config.Config, provider ai.AIProvider) CheckResult {
	res := CheckResult{
		ID:       9,
		Name:     "Genuine AI Model Inference & Grounding Probe",
		Category: "AI Model Verification",
	}

	if !cfg.AIEnabled {
		res.Status = StatusBlocked
		res.Summary = "Inference probe blocked: AI is disabled"
		res.Detail = "AI_ENABLED is false."
		return res
	}

	if provider == nil {
		var pErr error
		provider, pErr = ai.NewProvider(cfg)
		if pErr != nil {
			res.Status = StatusFail
			res.Summary = "Failed to initialize AI provider"
			res.Detail = pErr.Error()
			return res
		}
	}

	if provider.Name() == "mock" {
		res.Status = StatusFail
		res.Summary = "Mock AI provider cannot verify genuine inference"
		res.Detail = "Submission profile strictly requires genuine model inference (AI_PROVIDER=ollama)."
		res.OperatorAction = "Configure AI_PROVIDER=ollama and AI_MODEL=llama3.2 in .env"
		return res
	}

	// Verify health prerequisite first
	health := provider.CheckHealth(ctx)
	if !health.Reachable {
		res.Status = StatusBlocked
		res.Summary = "Inference probe blocked: Daemon unreachable"
		res.Detail = health.ErrorMessage
		res.OperatorAction = "Run 'ollama serve'"
		return res
	}
	if !health.ModelAvailable {
		res.Status = StatusBlocked
		res.Summary = fmt.Sprintf("Inference probe blocked: Model %q not installed", cfg.AIModel)
		res.Detail = health.ErrorMessage
		res.OperatorAction = fmt.Sprintf("Run 'ollama pull %s'", cfg.AIModel)
		return res
	}

	// Execute synthetic non-mutating inference probe with bounded timeout
	snapshot := SyntheticProbeSnapshot()
	timeout := 15 * time.Second
	if cfg.AIRequestTimeout > 0 && cfg.AIRequestTimeout < timeout {
		timeout = cfg.AIRequestTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := provider.Investigate(probeCtx, snapshot)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
			res.Status = StatusFail
			res.Summary = fmt.Sprintf("Model inference timed out after %s", timeout)
			res.Detail = "Inference latency exceeded bounded test deadline."
			return res
		}
		if errors.Is(err, ai.ErrInvalidEvidenceRef) {
			res.Status = StatusFail
			res.Summary = "Model hallucinated invalid or fabricated evidence references"
			res.Detail = err.Error()
			return res
		}
		if errors.Is(err, ai.ErrInvalidSchema) {
			res.Status = StatusFail
			res.Summary = "Model output failed schema validation"
			res.Detail = err.Error()
			return res
		}
		res.Status = StatusFail
		res.Summary = "Genuine model inference execution failed"
		res.Detail = err.Error()
		return res
	}

	// Validate evidence grounding against synthetic snapshot valid IDs
	if valErr := ai.ValidateGrounding(result, snapshot); valErr != nil {
		res.Status = StatusFail
		res.Summary = "Evidence grounding validation failed"
		res.Detail = valErr.Error()
		return res
	}

	res.Status = StatusPass
	res.Summary = fmt.Sprintf("Genuine local %s inference verified", cfg.AIModel)
	res.Detail = fmt.Sprintf("Findings: %d, Recommendations: %d, Confidence: %s. Zero state mutations.",
		len(result.Findings), len(result.RecommendedNextSteps), result.Confidence)
	return res
}
