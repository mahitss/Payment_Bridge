package ai

import (
	"context"
	"encoding/json"
	"sync"
)

// MockProvider provides deterministic, controllable AI inference for testing.
type MockProvider struct {
	mu           sync.Mutex
	Available    bool
	ModelName    string
	ProviderName string
	CustomError  error
	CustomResult *InvestigationResult
	RawOutput    string // If set, this raw JSON string will be parsed and validated
	CallCount    int
	LastSnapshot *InvestigationSnapshot
}

// NewMockProvider constructs a default passing mock provider.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		Available:    true,
		ModelName:    "mock-llama3.2",
		ProviderName: "mock",
	}
}

func (m *MockProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

func (m *MockProvider) Model() string {
	if m.ModelName != "" {
		return m.ModelName
	}
	return "mock-llama3.2"
}

func (m *MockProvider) IsAvailable(ctx context.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Available
}

func (m *MockProvider) Investigate(ctx context.Context, snapshot *InvestigationSnapshot) (*InvestigationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CallCount++
	m.LastSnapshot = snapshot

	if !m.Available {
		return nil, ErrAIUnavailable
	}

	if m.CustomError != nil {
		return nil, m.CustomError
	}

	if m.RawOutput != "" {
		cleaned := CleanJSONResponse(m.RawOutput)
		var res InvestigationResult
		if err := json.Unmarshal([]byte(cleaned), &res); err != nil {
			return nil, ErrInvalidSchema
		}
		if err := ValidateGrounding(&res, snapshot); err != nil {
			return nil, err
		}
		return &res, nil
	}

	if m.CustomResult != nil {
		if err := ValidateGrounding(m.CustomResult, snapshot); err != nil {
			return nil, err
		}
		return m.CustomResult, nil
	}

	// Default well-grounded result
	evidenceRef := snapshot.CaseID
	if len(snapshot.Operations) > 0 {
		evidenceRef = snapshot.Operations[0].ID
	}

	defaultResult := &InvestigationResult{
		Summary: "Payout case analyzed successfully with grounded evidence.",
		ObservedFacts: []string{
			"Case reference: " + snapshot.Reference,
			"Current status: " + snapshot.CaseStatus,
		},
		Findings: []InvestigationFinding{
			{
				Explanation:        "Operation status verified against stored state",
				EvidenceReferences: []string{evidenceRef},
			},
		},
		RecommendedNextSteps: []InvestigationRecommendation{
			{
				Action:                       ActionCheckProviderStatus,
				Reason:                       "Confirm settlement status with PayPal",
				RequiresHumanReview:          false,
				DeterministicPolicyPermitted: true,
			},
		},
		MissingInformation:  []string{},
		RiskFlags:           []string{},
		Confidence:          ConfidenceHigh,
		Limitations:         []string{"Based solely on snapshot records at time of analysis"},
		RequiresHumanReview: false,
		RawResponse: map[string]interface{}{
			"provider": "mock",
			"model":    "mock-llama3.2",
		},
	}

	return defaultResult, nil
}
