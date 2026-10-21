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
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 1024,
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
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 1024,
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
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 1024,
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
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 1024,
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
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 1024,
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
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("expected ErrResponseTooLarge, got %v", err)
		}
	})
}

func TestOllamaClient_CheckHealth(t *testing.T) {
	ctx := context.Background()

	t.Run("Daemon and Model Available via /api/tags", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:latest"},{"name":"mistral:latest"}]}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:        srv.URL,
			AIModel:          "llama3.2",
			AIRequestTimeout: 3 * time.Second,
		}
		client := NewOllamaClient(cfg)
		health := client.CheckHealth(ctx)

		if !health.Reachable {
			t.Errorf("expected reachable true, got false")
		}
		if !health.ModelAvailable {
			t.Errorf("expected model_available true, got false with message: %s", health.ErrorMessage)
		}
		if !client.IsAvailable(ctx) {
			t.Errorf("expected IsAvailable true")
		}
	})

	t.Run("Daemon Available but Model Missing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:latest"},{"name":"phi3:latest"}]}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:        srv.URL,
			AIModel:          "llama3.2",
			AIRequestTimeout: 3 * time.Second,
		}
		client := NewOllamaClient(cfg)
		health := client.CheckHealth(ctx)

		if !health.Reachable {
			t.Errorf("expected reachable true, got false")
		}
		if health.ModelAvailable {
			t.Errorf("expected model_available false for missing model")
		}
		if health.ErrorCategory != "MODEL_NOT_FOUND" {
			t.Errorf("expected error_category MODEL_NOT_FOUND, got: %s", health.ErrorCategory)
		}
		if client.IsAvailable(ctx) {
			t.Errorf("expected IsAvailable false when model is missing")
		}
	})

	t.Run("Daemon Unreachable", func(t *testing.T) {
		cfg := &config.Config{
			AIBaseURL:        "http://127.0.0.1:54321", // non-listening port
			AIModel:          "llama3.2",
			AIRequestTimeout: 1 * time.Second,
		}
		client := NewOllamaClient(cfg)
		health := client.CheckHealth(ctx)

		if health.Reachable {
			t.Errorf("expected reachable false for unreachable daemon")
		}
		if health.ModelAvailable {
			t.Errorf("expected model_available false")
		}
		if health.ErrorCategory != "OLLAMA_UNREACHABLE" {
			t.Errorf("expected error_category OLLAMA_UNREACHABLE, got: %s", health.ErrorCategory)
		}
		if client.IsAvailable(ctx) {
			t.Errorf("expected IsAvailable false")
		}
	})
}

func TestOllamaClient_SensitiveLoggingSanitization(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-sensitive-1",
		ValidEvidenceIDs: []string{"case-sensitive-1"},
	}

	t.Run("HTTP Error Body Not Leaked in Logs", func(t *testing.T) {
		var logBuf bytes.Buffer
		testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		oldLogger := slog.Default()
		slog.SetDefault(testLogger)
		defer slog.SetDefault(oldLogger)

		secretPayload := "SUPER_CONFIDENTIAL_RECIPIENT_SECRET_12345"

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"bad_request","detail":"%s"}`, secretPayload)))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  3 * time.Second,
			AIMaxOutputTokens: 1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if err == nil {
			t.Fatalf("expected error from 400 response, got nil")
		}

		logs := logBuf.String()
		if strings.Contains(logs, secretPayload) {
			t.Fatalf("SECURITY VIOLATION: provider error body was leaked into slog output:\n%s", logs)
		}
		if !strings.Contains(logs, "provider_http_error") {
			t.Errorf("expected log to contain classified error_class=provider_http_error, got:\n%s", logs)
		}
		if !strings.Contains(logs, "case_id=case-sensitive-1") {
			t.Errorf("expected log to include case_id correlation, got:\n%s", logs)
		}
	})

	t.Run("Malformed Model Output Not Leaked in Logs", func(t *testing.T) {
		var logBuf bytes.Buffer
		testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		oldLogger := slog.Default()
		slog.SetDefault(testLogger)
		defer slog.SetDefault(oldLogger)

		secretModelOutput := "SENSITIVE_INVESTIGATION_PII_SSN_999-00-1111_DO_NOT_LOG"

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := fmt.Sprintf(`{
				"id": "chat-err",
				"choices": [{
					"message": {
						"role": "assistant",
						"content": "{malformed json unquoted %s"
					}
				}]
			}`, secretModelOutput)
			_, _ = w.Write([]byte(resp))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:         srv.URL,
			AIModel:           "llama3.2",
			AIRequestTimeout:  3 * time.Second,
			AIMaxOutputTokens: 1024,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if err == nil {
			t.Fatalf("expected error from malformed json, got nil")
		}

		logs := logBuf.String()
		if strings.Contains(logs, secretModelOutput) {
			t.Fatalf("SECURITY VIOLATION: raw model output was leaked into slog output:\n%s", logs)
		}
		if !strings.Contains(logs, "malformed_model_json") {
			t.Errorf("expected log to contain classified error_class=malformed_model_json, got:\n%s", logs)
		}
		if !strings.Contains(logs, "raw_length=") {
			t.Errorf("expected log to contain size metadata raw_length=, got:\n%s", logs)
		}
	})
}

func TestOllamaClient_ReasoningEffort(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-reasoning-test-1",
		ValidEvidenceIDs: []string{"case-reasoning-test-1"},
	}

	validInvestigationResp := `{
		"id": "chat-qwen-test",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "{\"summary\": \"Investigation complete for Qwen\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Case observed\", \"evidence_references\": [\"case-reasoning-test-1\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"Verify with provider\"}]}"
			},
			"finish_reason": "stop"
		}]
	}`

	t.Run("AI_PROVIDER=ollama includes reasoning_effort none", func(t *testing.T) {
		var receivedPayload map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("failed to read request body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := json.Unmarshal(bodyBytes, &receivedPayload); err != nil {
				t.Errorf("failed to unmarshal request body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(validInvestigationResp))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 500,
		}

		client := NewOllamaClient(cfg)
		res, err := client.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("expected successful investigation, got %v", err)
		}
		if res.Summary != "Investigation complete for Qwen" {
			t.Errorf("expected summary %q, got %q", "Investigation complete for Qwen", res.Summary)
		}

		val, ok := receivedPayload["reasoning_effort"]
		if !ok {
			t.Fatalf("expected 'reasoning_effort' field to be present in Ollama request body, but was omitted")
		}
		if val != "none" {
			t.Errorf("expected reasoning_effort='none', got %v", val)
		}
		if receivedPayload["model"] != "qwen3.5:4b" {
			t.Errorf("expected model='qwen3.5:4b', got %v", receivedPayload["model"])
		}
		if maxTok, ok := receivedPayload["max_tokens"].(float64); !ok || int(maxTok) != 500 {
			t.Errorf("expected max_tokens=500, got %v", receivedPayload["max_tokens"])
		}
	})

	t.Run("Other compatible providers omit reasoning_effort", func(t *testing.T) {
		testCases := []struct {
			providerName string
			expectedOmit bool
		}{
			{"openai-compatible", true},
			{"openai", true},
			{"generic-openai", true},
			{"", true},
		}

		for _, tc := range testCases {
			t.Run("provider="+tc.providerName, func(t *testing.T) {
				var receivedPayload map[string]interface{}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					bodyBytes, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("failed to read request body: %v", err)
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					if err := json.Unmarshal(bodyBytes, &receivedPayload); err != nil {
						t.Errorf("failed to unmarshal request body: %v", err)
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(validInvestigationResp))
				}))
				defer srv.Close()

				cfg := &config.Config{
					AIProvider:        tc.providerName,
					AIBaseURL:         srv.URL,
					AIModel:           "gpt-4o-mini",
					AIRequestTimeout:  5 * time.Second,
					AIMaxOutputTokens: 500,
				}

				client := NewOllamaClient(cfg)
				_, err := client.Investigate(ctx, snap)
				if err != nil {
					t.Fatalf("expected successful investigation, got %v", err)
				}

				if val, exists := receivedPayload["reasoning_effort"]; exists {
					t.Errorf("expected 'reasoning_effort' to be omitted for provider %q, but got %v", tc.providerName, val)
				}
			})
		}
	})

	t.Run("NewProvider factory honors provider reasoning_effort scoping", func(t *testing.T) {
		var lastPayload map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(bodyBytes, &lastPayload)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(validInvestigationResp))
		}))
		defer srv.Close()

		// 1. Factory with ollama
		cfgOllama := &config.Config{
			AIEnabled:         true,
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 500,
		}
		provOllama, err := NewProvider(cfgOllama)
		if err != nil {
			t.Fatalf("failed to create provider: %v", err)
		}
		_, err = provOllama.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("investigate failed: %v", err)
		}
		if lastPayload["reasoning_effort"] != "none" {
			t.Errorf("expected reasoning_effort='none' from NewProvider(ollama), got %v", lastPayload["reasoning_effort"])
		}

		// 2. Factory with openai-compatible
		lastPayload = nil
		cfgOpenAI := &config.Config{
			AIEnabled:         true,
			AIProvider:        "openai-compatible",
			AIBaseURL:         srv.URL,
			AIModel:           "gpt-4o-mini",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 500,
		}
		provOpenAI, err := NewProvider(cfgOpenAI)
		if err != nil {
			t.Fatalf("failed to create provider: %v", err)
		}
		_, err = provOpenAI.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("investigate failed: %v", err)
		}
		if _, exists := lastPayload["reasoning_effort"]; exists {
			t.Errorf("expected reasoning_effort to be omitted from NewProvider(openai-compatible), got %v", lastPayload["reasoning_effort"])
		}
	})
}
