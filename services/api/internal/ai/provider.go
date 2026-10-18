package ai

import (
	"context"
	"fmt"
	"strings"

	"claimbridge/services/api/internal/config"
)

// AIHealthStatus captures granular diagnostics of the inference backend.
type AIHealthStatus struct {
	Reachable       bool     `json:"reachable"`
	ModelAvailable  bool     `json:"model_available"`
	ConfiguredModel string   `json:"configured_model"`
	AvailableModels []string `json:"available_models,omitempty"`
	ProviderURL     string   `json:"provider_url"`
	ErrorCategory   string   `json:"error_category,omitempty"`
	ErrorMessage    string   `json:"error_message,omitempty"`
}

// AIProvider is the replaceable server-side inference interface for payout investigations.
type AIProvider interface {
	// Investigate performs an evidence-grounded investigation on the supplied snapshot.
	Investigate(ctx context.Context, snapshot *InvestigationSnapshot) (*InvestigationResult, error)

	// Name returns the provider's friendly name (e.g. "ollama-openai-compatible", "mock").
	Name() string

	// Model returns the configured model identifier (e.g. "llama3.2").
	Model() string

	// IsAvailable performs a non-blocking liveness check against the provider endpoint.
	IsAvailable(ctx context.Context) bool

	// CheckHealth performs a detailed probe of connectivity and model availability.
	CheckHealth(ctx context.Context) AIHealthStatus
}

// NewProvider constructs the configured AIProvider based on application configuration.
func NewProvider(cfg *config.Config) (AIProvider, error) {
	if !cfg.AIEnabled {
		return &DisabledProvider{}, nil
	}

	providerType := strings.ToLower(strings.TrimSpace(cfg.AIProvider))
	switch providerType {
	case "ollama", "openai", "openai-compatible":
		return NewOllamaClient(cfg), nil
	case "mock":
		return NewMockProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported AI_PROVIDER: %q (supported: ollama, openai-compatible, mock)", cfg.AIProvider)
	}
}

// DisabledProvider returns ErrAIUnavailable for all calls when AI_ENABLED=false.
type DisabledProvider struct{}

func (d *DisabledProvider) Investigate(ctx context.Context, snapshot *InvestigationSnapshot) (*InvestigationResult, error) {
	return nil, ErrAIUnavailable
}

func (d *DisabledProvider) Name() string {
	return "disabled"
}

func (d *DisabledProvider) Model() string {
	return "none"
}

func (d *DisabledProvider) IsAvailable(ctx context.Context) bool {
	return false
}

func (d *DisabledProvider) CheckHealth(ctx context.Context) AIHealthStatus {
	return AIHealthStatus{
		Reachable:       false,
		ModelAvailable:  false,
		ConfiguredModel: "none",
		ProviderURL:     "none",
		ErrorCategory:   "AI_DISABLED",
		ErrorMessage:    "AI investigation assistant is disabled (AI_ENABLED=false)",
	}
}
