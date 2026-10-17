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
	baseURL    string
	model      string
	apiKey     string
	timeout    time.Duration
	maxTokens  int
	httpClient *http.Client
}

// NewOllamaClient creates an OpenAI-compatible inference client.
func NewOllamaClient(cfg *config.Config) *OllamaClient {
	return &OllamaClient{
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
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Try checking /v1/models or root endpoint
	endpoint := c.baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Fallback check to root baseURL
		rootReq, rootErr := http.NewRequestWithContext(checkCtx, http.MethodGet, c.baseURL, nil)
		if rootErr != nil {
			return false
		}
		rootResp, rootDoErr := c.httpClient.Do(rootReq)
		if rootDoErr != nil {
			return false
		}
		_ = rootResp.Body.Close()
		return rootResp.StatusCode < 500
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model          string              `json:"model"`
	Messages       []openAIChatMessage `json:"messages"`
	Temperature    float64             `json:"temperature"`
	MaxTokens      int                 `json:"max_tokens"`
	ResponseFormat *responseFormat     `json:"response_format,omitempty"`
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

	chatReq := openAIChatRequest{
		Model: c.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: BuildSystemPrompt()},
			{Role: "user", Content: userPrompt},
		},
		Temperature:    0.1,
		MaxTokens:      c.maxTokens,
		ResponseFormat: &responseFormat{Type: "json_object"},
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
		slog.WarnContext(ctx, "AI provider connection failed", slog.String("error", err.Error()))
		return nil, ErrAIUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrAIRateLimited
	}

	if resp.StatusCode >= 500 {
		slog.WarnContext(ctx, "AI provider returned server error", slog.Int("status", resp.StatusCode))
		return nil, ErrAIUnavailable
	}

	if resp.StatusCode != http.StatusOK {
		lr := io.LimitReader(resp.Body, 1024)
		body, _ := io.ReadAll(lr)
		slog.WarnContext(ctx, "AI provider returned non-200 status", slog.Int("status", resp.StatusCode), slog.String("body", string(body)))
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
		slog.WarnContext(ctx, "Failed to parse model output as JSON", slog.String("raw", rawContent))
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
