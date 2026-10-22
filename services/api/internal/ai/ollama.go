package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"claimbridge/services/api/internal/config"
)

const maxResponseBytes = 1024 * 1024 // 1 MB

// OllamaClient provides inference via local Ollama or compatible OpenAI endpoints.
type OllamaClient struct {
	provider   string
	baseURL    string
	model      string
	apiKey     string
	timeout    time.Duration
	maxTokens  int
	httpClient *http.Client
}

// NewOllamaClient creates an OpenAI-compatible inference client.
func NewOllamaClient(cfg *config.Config) *OllamaClient {
	var provider string
	if cfg != nil {
		provider = strings.ToLower(strings.TrimSpace(cfg.AIProvider))
	}
	return &OllamaClient{
		provider:  provider,
		baseURL:   cfg.AIBaseURL,
		model:     cfg.AIModel,
		apiKey:    cfg.AIAPIKey,
		timeout:   cfg.AIRequestTimeout,
		maxTokens: cfg.AIMaxOutputTokens,
		httpClient: &http.Client{
			Timeout: cfg.AIRequestTimeout,
		},
	}
}

func (c *OllamaClient) Name() string {
	return "ollama-openai-compatible"
}

func (c *OllamaClient) Model() string {
	return c.model
}

func (c *OllamaClient) IsAvailable(ctx context.Context) bool {
	return c.CheckHealth(ctx).ModelAvailable
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// CheckHealth probes provider liveness and verifies if the configured model is installed.
func (c *OllamaClient) CheckHealth(ctx context.Context) AIHealthStatus {
	status := AIHealthStatus{
		ConfiguredModel: c.model,
		ProviderURL:     c.baseURL,
	}

	probeTimeout := 5 * time.Second
	if c.timeout > 0 && c.timeout < probeTimeout {
		probeTimeout = c.timeout
	}

	// 1. Try Ollama native /api/tags endpoint with bounded probe context
	tagsCtx, tagsCancel := context.WithTimeout(ctx, probeTimeout)
	tagsURL := strings.TrimRight(c.baseURL, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(tagsCtx, http.MethodGet, tagsURL, nil)
	if err != nil {
		tagsCancel()
		status.ErrorCategory = "REQUEST_CREATION_FAILED"
		status.ErrorMessage = err.Error()
		return status
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, tagsErr := c.httpClient.Do(req)
	tagsCancel()
	if tagsErr == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return c.parseModelsFromOllama(resp.Body, status)
		}
	}

	// 2. Fallback to OpenAI-compatible /v1/models endpoint with fresh probe context
	modelsCtx, modelsCancel := context.WithTimeout(ctx, probeTimeout)
	defer modelsCancel()

	modelsURL := strings.TrimRight(c.baseURL, "/") + "/v1/models"
	modelsReq, fbErr := http.NewRequestWithContext(modelsCtx, http.MethodGet, modelsURL, nil)
	if fbErr == nil {
		if c.apiKey != "" {
			modelsReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		modelsResp, fbDoErr := c.httpClient.Do(modelsReq)
		if fbDoErr == nil {
			defer modelsResp.Body.Close()
			if modelsResp.StatusCode == http.StatusOK {
				return c.parseModelsFromOpenAI(modelsResp.Body, status)
			}
			status.Reachable = true
			status.ModelAvailable = false
			status.ErrorCategory = fmt.Sprintf("HTTP_STATUS_%d", modelsResp.StatusCode)
			status.ErrorMessage = fmt.Sprintf("Inference provider returned HTTP %d on /v1/models", modelsResp.StatusCode)
			return status
		}
		// If tagsErr was nil but status was not OK, the host was reached
		if tagsErr == nil && resp != nil {
			status.Reachable = true
			status.ModelAvailable = false
			status.ErrorCategory = fmt.Sprintf("HTTP_STATUS_%d", resp.StatusCode)
			status.ErrorMessage = fmt.Sprintf("Inference provider returned HTTP %d on /api/tags", resp.StatusCode)
			return status
		}
		// Connection or timeout error
		status.Reachable = false
		status.ModelAvailable = false
		status.ErrorCategory = classifyNetworkError(fbDoErr)
		status.ErrorMessage = fmt.Sprintf("Cannot connect to inference daemon at %s: %v", c.baseURL, fbDoErr)
		return status
	}

	// Fallback request creation failed or tags failed
	status.Reachable = false
	status.ModelAvailable = false
	if tagsErr != nil {
		status.ErrorCategory = classifyNetworkError(tagsErr)
		status.ErrorMessage = fmt.Sprintf("Cannot connect to inference daemon at %s: %v", c.baseURL, tagsErr)
	} else if resp != nil {
		status.Reachable = true
		status.ErrorCategory = fmt.Sprintf("HTTP_STATUS_%d", resp.StatusCode)
		status.ErrorMessage = fmt.Sprintf("Inference provider returned HTTP %d on /api/tags", resp.StatusCode)
	} else {
		status.ErrorCategory = "OLLAMA_UNREACHABLE"
		status.ErrorMessage = fmt.Sprintf("Cannot connect to inference daemon at %s. Ensure daemon is running ('ollama serve').", c.baseURL)
	}
	return status
}

func classifyNetworkError(err error) string {
	if err == nil {
		return "UNKNOWN_ERROR"
	}
	errStr := strings.ToLower(err.Error())
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errStr, "deadline exceeded") || strings.Contains(errStr, "client.timeout") {
		return "TIMEOUT"
	}
	if errors.Is(err, context.Canceled) || strings.Contains(errStr, "context canceled") {
		return "CONTEXT_CANCELED"
	}
	if strings.Contains(errStr, "no such host") {
		return "DNS_RESOLUTION_FAILED"
	}
	return "OLLAMA_UNREACHABLE"
}

func (c *OllamaClient) parseModelsFromOllama(body io.Reader, status AIHealthStatus) AIHealthStatus {
	status.Reachable = true
	var tagsResp ollamaTagsResponse
	if err := json.NewDecoder(body).Decode(&tagsResp); err != nil {
		status.ErrorCategory = "MALFORMED_TAGS_RESPONSE"
		status.ErrorMessage = "Failed to parse model list from Ollama /api/tags"
		return status
	}

	targetModel := strings.ToLower(strings.TrimSpace(c.model))
	var modelNames []string
	found := false

	for _, m := range tagsResp.Models {
		modelNames = append(modelNames, m.Name)
		if matchModelName(m.Name, targetModel) {
			found = true
		}
	}

	status.AvailableModels = modelNames
	status.ModelAvailable = found
	if !found {
		status.ErrorCategory = "MODEL_NOT_FOUND"
		status.ErrorMessage = fmt.Sprintf("Configured model %q is not installed in Ollama. Operator action: run 'ollama pull %s'", c.model, c.model)
	}
	return status
}

func (c *OllamaClient) parseModelsFromOpenAI(body io.Reader, status AIHealthStatus) AIHealthStatus {
	status.Reachable = true
	var modelsResp openAIModelsResponse
	if err := json.NewDecoder(body).Decode(&modelsResp); err != nil {
		status.ErrorCategory = "MALFORMED_MODELS_RESPONSE"
		status.ErrorMessage = "Failed to parse model list from /v1/models"
		return status
	}

	targetModel := strings.ToLower(strings.TrimSpace(c.model))
	var modelNames []string
	found := false

	for _, m := range modelsResp.Data {
		modelNames = append(modelNames, m.ID)
		if matchModelName(m.ID, targetModel) {
			found = true
		}
	}

	status.AvailableModels = modelNames
	status.ModelAvailable = found
	if !found {
		status.ErrorCategory = "MODEL_NOT_FOUND"
		status.ErrorMessage = fmt.Sprintf("Configured model %q is not installed in inference provider. Operator action: run 'ollama pull %s'", c.model, c.model)
	}
	return status
}

func matchModelName(availableName, target string) bool {
	avail := strings.ToLower(strings.TrimSpace(availableName))
	tgt := strings.ToLower(strings.TrimSpace(target))
	if avail == "" || tgt == "" {
		return false
	}
	if avail == tgt {
		return true
	}
	availClean := stripModelNamespace(avail)
	tgtClean := stripModelNamespace(tgt)
	if availClean == tgtClean {
		return true
	}
	if strings.HasPrefix(availClean, tgtClean+":") {
		return true
	}
	availNoTag := strings.TrimSuffix(availClean, ":latest")
	tgtNoTag := strings.TrimSuffix(tgtClean, ":latest")
	if availNoTag == tgtNoTag {
		return true
	}
	return false
}

func stripModelNamespace(name string) string {
	parts := strings.Split(name, "/")
	return parts[len(parts)-1]
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model           string              `json:"model"`
	Messages        []openAIChatMessage `json:"messages"`
	Temperature     float64             `json:"temperature"`
	MaxTokens       int                 `json:"max_tokens"`
	ResponseFormat  *responseFormat     `json:"response_format,omitempty"`
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (c *OllamaClient) Investigate(ctx context.Context, snapshot *InvestigationSnapshot) (*InvestigationResult, error) {
	userPrompt, err := BuildUserPrompt(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to construct user prompt: %w", err)
	}

	var reasoningEffort string
	if c.provider == "ollama" {
		reasoningEffort = "none"
	}

	chatReq := openAIChatRequest{
		Model: c.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: BuildSystemPrompt()},
			{Role: "user", Content: userPrompt},
		},
		Temperature:     0.1,
		MaxTokens:       c.maxTokens,
		ResponseFormat:  &responseFormat{Type: "json_object"},
		ReasoningEffort: reasoningEffort,
	}

	reqBytes, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to encode chat request: %w", err)
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	slog.InfoContext(ctx, "Invoking AI investigation provider",
		slog.String("provider", c.Name()),
		slog.String("model", c.model),
		slog.String("case_id", snapshot.CaseID),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrAITimeout
		}
		slog.WarnContext(ctx, "AI provider connection failed",
			slog.String("error_class", "network_transport_failure"),
			slog.String("error", err.Error()),
			slog.String("case_id", snapshot.CaseID),
		)
		return nil, ErrAIUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrAIRateLimited
	}

	if resp.StatusCode >= 500 {
		slog.WarnContext(ctx, "AI provider returned server error",
			slog.String("error_class", "provider_server_error"),
			slog.Int("status", resp.StatusCode),
			slog.String("case_id", snapshot.CaseID),
		)
		return nil, ErrAIUnavailable
	}

	if resp.StatusCode != http.StatusOK {
		lr := io.LimitReader(resp.Body, 1024)
		body, _ := io.ReadAll(lr)
		slog.WarnContext(ctx, "AI provider returned non-200 status",
			slog.String("error_class", "provider_http_error"),
			slog.Int("status", resp.StatusCode),
			slog.Int("response_bytes", len(body)),
			slog.String("case_id", snapshot.CaseID),
		)
		return nil, fmt.Errorf("%w: http status %d", ErrAIUnavailable, resp.StatusCode)
	}

	// Limit response size
	lr := io.LimitReader(resp.Body, maxResponseBytes+1)
	respBody, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("failed to read ai provider response: %w", err)
	}
	if len(respBody) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("%w: invalid provider envelope", ErrInvalidSchema)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return nil, fmt.Errorf("%w: provider error: %s", ErrAIUnavailable, chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("%w: provider returned empty choices", ErrInvalidSchema)
	}

	rawContent := chatResp.Choices[0].Message.Content
	cleanedJSON := CleanJSONResponse(rawContent)

	var result InvestigationResult
	if err := json.Unmarshal([]byte(cleanedJSON), &result); err != nil {
		slog.WarnContext(ctx, "Failed to parse model output as JSON",
			slog.String("error_class", "malformed_model_json"),
			slog.Int("raw_length", len(rawContent)),
			slog.String("case_id", snapshot.CaseID),
		)
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchema, err)
	}

	result.RawResponse = map[string]interface{}{
		"provider":      c.Name(),
		"model":         c.model,
		"finish_reason": chatResp.Choices[0].FinishReason,
	}

	// Validate grounding and constraints
	if err := ValidateGrounding(&result, snapshot); err != nil {
		return nil, err
	}

	return &result, nil
}
