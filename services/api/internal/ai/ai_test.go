package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
)

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"alice@example.com", "a***e@example.com"},
		{"bob@domain.org", "b***b@domain.org"},
		{"ab@xyz.com", "*@xyz.com"},
		{"invalid", "[REDACTED_EMAIL]"},
	}

	for _, tc := range tests {
		got := MaskEmail(tc.input)
		if got != tc.expected {
			t.Errorf("MaskEmail(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestCleanJSONResponse(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"summary":"ok"}`, `{"summary":"ok"}`},
		{"```json\n{\"summary\":\"ok\"}\n```", `{"summary":"ok"}`},
		{"```\n{\"summary\":\"ok\"}\n```", `{"summary":"ok"}`},
		{"Here is your output:\n{\"summary\":\"ok\"}\nHope this helps!", `{"summary":"ok"}`},
	}

	for _, tc := range tests {
		got := CleanJSONResponse(tc.input)
		if got != tc.expected {
			t.Errorf("CleanJSONResponse(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestValidateGrounding(t *testing.T) {
	snap := &InvestigationSnapshot{
		CaseID:           "case-100",
		ValidEvidenceIDs: []string{"case-100", "op-200", "batch-300"},
	}

	t.Run("Valid Grounded Result", func(t *testing.T) {
		res := &InvestigationResult{
			Summary:    "Valid investigation",
			Confidence: ConfidenceHigh,
			Findings: []InvestigationFinding{
				{Explanation: "Case observed", EvidenceReferences: []string{"case-100"}},
				{Explanation: "Operation verified", EvidenceReferences: []string{"op-200"}},
			},
			RecommendedNextSteps: []InvestigationRecommendation{
				{Action: ActionCheckProviderStatus, Reason: "Reconcile state"},
			},
		}
		if err := ValidateGrounding(res, snap); err != nil {
			t.Fatalf("expected valid grounding, got %v", err)
		}
	})

	t.Run("Fabricated Evidence ID Rejected", func(t *testing.T) {
		res := &InvestigationResult{
			Summary:    "Fabricated evidence test",
			Confidence: ConfidenceHigh,
			Findings: []InvestigationFinding{
				{Explanation: "Fake batch reference", EvidenceReferences: []string{"fabricated-batch-id-999"}},
			},
			RecommendedNextSteps: []InvestigationRecommendation{
				{Action: ActionCheckProviderStatus, Reason: "Check"},
			},
		}
		err := ValidateGrounding(res, snap)
		if !errors.Is(err, ErrInvalidEvidenceRef) {
			t.Fatalf("expected ErrInvalidEvidenceRef, got %v", err)
		}
	})

	t.Run("Disallowed Action Rejected", func(t *testing.T) {
		res := &InvestigationResult{
			Summary:    "Disallowed action test",
			Confidence: ConfidenceHigh,
			Findings: []InvestigationFinding{
				{Explanation: "Valid finding", EvidenceReferences: []string{"case-100"}},
			},
			RecommendedNextSteps: []InvestigationRecommendation{
				{Action: "EXECUTE_ARBITRARY_TRANSFER", Reason: "Unsafe"},
			},
		}
		err := ValidateGrounding(res, snap)
		if !errors.Is(err, ErrInvalidRecommendation) {
			t.Fatalf("expected ErrInvalidRecommendation, got %v", err)
		}
	})

	t.Run("Empty Summary Rejected", func(t *testing.T) {
		res := &InvestigationResult{
			Summary:    "",
			Confidence: ConfidenceHigh,
		}
		err := ValidateGrounding(res, snap)
		if !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("expected ErrInvalidSchema for empty summary, got %v", err)
		}
	})

	t.Run("Invalid Confidence Level Rejected", func(t *testing.T) {
		res := &InvestigationResult{
			Summary:    "Test summary",
			Confidence: "99.9% PROBABILITY", // Arbitrary/uncalibrated string
		}
		err := ValidateGrounding(res, snap)
		if !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("expected ErrInvalidSchema for invalid confidence, got %v", err)
		}
	})
}

func TestOllamaClient_WithFakeServer(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-test-1",
		ValidEvidenceIDs: []string{"case-test-1", "op-test-1"},
	}

	t.Run("Successful Grounded Response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := `{
				"id": "chat-123",
				"choices": [{
					"message": {
						"role": "assistant",
						"content": "{\"summary\": \"Analysis complete\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Case recorded\", \"evidence_references\": [\"case-test-1\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"Verify with provider\"}]}"
					},
					"finish_reason": "stop"
				}]
			}`
			_, _ = w.Write([]byte(resp))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:            srv.URL,
			AIModel:              "llama3.2",
			AIRequestTimeout:     5 * time.Second,
			AIMaxOutputTokens:    1024,
		}
		client := NewOllamaClient(cfg)
		res, err := client.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("expected successful investigation, got %v", err)
		}
		if res.Summary != "Analysis complete" {
			t.Errorf("expected summary %q, got %q", "Analysis complete", res.Summary)
		}
	})

	t.Run("Insufficient Evidence Flow", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := `{
				"id": "chat-insufficient",
				"choices": [{
					"message": {
						"role": "assistant",
						"content": "{\"summary\": \"State cannot be fully resolved\", \"confidence\": \"INSUFFICIENT_EVIDENCE\", \"findings\": [{\"explanation\": \"Case observed\", \"evidence_references\": [\"case-test-1\"]}], \"missing_information\": [\"PayPal batch record missing\"], \"recommended_next_steps\": [{\"action\": \"ESCALATE_TO_OPERATOR\", \"reason\": \"Investigate missing records\"}]}"
					},
					"finish_reason": "stop"
				}]
			}`
			_, _ = w.Write([]byte(resp))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:            srv.URL,
			AIModel:              "llama3.2",
			AIRequestTimeout:     5 * time.Second,
			AIMaxOutputTokens:    1024,
		}
		client := NewOllamaClient(cfg)
		res, err := client.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("expected success on insufficient evidence, got %v", err)
		}
		if res.Confidence != ConfidenceInsufficientEvidence {
			t.Errorf("expected INSUFFICIENT_EVIDENCE, got %q", res.Confidence)
		}
		if len(res.MissingInformation) != 1 {
			t.Errorf("expected 1 missing information item, got %d", len(res.MissingInformation))
		}
	})

	t.Run("Malformed JSON Rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := `{
				"id": "chat-err",
				"choices": [{
					"message": {
						"role": "assistant",
						"content": "{malformed json content missing quotes"
					}
				}]
			}`
			_, _ = w.Write([]byte(resp))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:            srv.URL,
			AIModel:              "llama3.2",
			AIRequestTimeout:     5 * time.Second,
			AIMaxOutputTokens:    1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("expected ErrInvalidSchema for malformed JSON, got %v", err)
		}
	})

	t.Run("Rate Limited 429", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:            srv.URL,
			AIModel:              "llama3.2",
			AIRequestTimeout:     5 * time.Second,
			AIMaxOutputTokens:    1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrAIRateLimited) {
			t.Fatalf("expected ErrAIRateLimited on 429, got %v", err)
		}
	})

	t.Run("Server Error 503", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:            srv.URL,
			AIModel:              "llama3.2",
			AIRequestTimeout:     5 * time.Second,
			AIMaxOutputTokens:    1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrAIUnavailable) {
			t.Fatalf("expected ErrAIUnavailable on 503, got %v", err)
		}
	})

	t.Run("Oversized Response Rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Send > 1 MB payload
			huge := strings.Repeat("A", maxResponseBytes+100)
			fmt.Fprintf(w, `{"id":"huge","choices":[{"message":{"content":"%s"}}]}`, huge)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:            srv.URL,
			AIModel:              "llama3.2",
			AIRequestTimeout:     5 * time.Second,
			AIMaxOutputTokens:    1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("expected ErrResponseTooLarge, got %v", err)
		}
	})
}
