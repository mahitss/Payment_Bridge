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

	t.Run("Fallback to /v1/models When /api/tags Fails With Separate Context", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" {
				// Simulate failure on native tags endpoint (e.g. 500 or 404)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			if r.URL.Path == "/v1/models" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"qwen3.5:4b"}]}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:        srv.URL,
			AIModel:          "qwen3.5:4b",
			AIRequestTimeout: 3 * time.Second,
		}
		client := NewOllamaClient(cfg)
		health := client.CheckHealth(ctx)

		if !health.Reachable {
			t.Errorf("expected reachable true")
		}
		if !health.ModelAvailable {
			t.Errorf("expected model_available true via /v1/models fallback, got false")
		}
		if !client.IsAvailable(ctx) {
			t.Errorf("expected IsAvailable true")
		}
	})

	t.Run("Model Name Normalization and Matching", func(t *testing.T) {
		testCases := []struct {
			name            string
			availableModels []string
			configuredModel string
			expectMatch     bool
		}{
			{
				name:            "Library prefix matches target",
				availableModels: []string{"library/qwen3.5:4b"},
				configuredModel: "qwen3.5:4b",
				expectMatch:     true,
			},
			{
				name:            "Target with latest matches available without tag",
				availableModels: []string{"qwen3.5"},
				configuredModel: "qwen3.5:latest",
				expectMatch:     true,
			},
			{
				name:            "Target without tag matches available with latest",
				availableModels: []string{"qwen3.5:latest"},
				configuredModel: "qwen3.5",
				expectMatch:     true,
			},
			{
				name:            "Target prefix matches available with variant tag",
				availableModels: []string{"qwen3.5:4b"},
				configuredModel: "qwen3.5",
				expectMatch:     true,
			},
			{
				name:            "Different model does not match",
				availableModels: []string{"llama3.2:latest"},
				configuredModel: "qwen3.5:4b",
				expectMatch:     false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					var models []map[string]string
					for _, m := range tc.availableModels {
						models = append(models, map[string]string{"name": m})
					}
					respData, _ := json.Marshal(map[string]interface{}{"models": models})
					_, _ = w.Write(respData)
				}))
				defer srv.Close()

				cfg := &config.Config{
					AIBaseURL:        srv.URL,
					AIModel:          tc.configuredModel,
					AIRequestTimeout: 3 * time.Second,
				}
				client := NewOllamaClient(cfg)
				health := client.CheckHealth(ctx)

				if health.ModelAvailable != tc.expectMatch {
					t.Errorf("matchModel(%v, %q) = %v; want %v", tc.availableModels, tc.configuredModel, health.ModelAvailable, tc.expectMatch)
				}
			})
		}
	})

	t.Run("Provider Returns HTTP Error Status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"service overloaded"}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIBaseURL:        srv.URL,
			AIModel:          "qwen3.5:4b",
			AIRequestTimeout: 3 * time.Second,
		}
		client := NewOllamaClient(cfg)
		health := client.CheckHealth(ctx)

		if !health.Reachable {
			t.Errorf("expected reachable true when provider responds with HTTP 503")
		}
		if health.ModelAvailable {
			t.Errorf("expected model_available false on 503")
		}
		if health.ErrorCategory != "HTTP_STATUS_503" {
			t.Errorf("expected error_category HTTP_STATUS_503, got: %s", health.ErrorCategory)
		}
	})

	t.Run("CheckHealth Context Canceled Categorization", func(t *testing.T) {
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel() // immediately canceled

		cfg := &config.Config{
			AIBaseURL:        "http://127.0.0.1:11434",
			AIModel:          "qwen3.5:4b",
			AIRequestTimeout: 1 * time.Second,
		}
		client := NewOllamaClient(cfg)
		health := client.CheckHealth(canceledCtx)

		if health.ModelAvailable {
			t.Errorf("expected model_available false on canceled context")
		}
		if health.ErrorCategory != "CONTEXT_CANCELED" {
			t.Errorf("expected error_category CONTEXT_CANCELED, got: %s", health.ErrorCategory)
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

func TestOllamaClient_NativeInference_SuccessAndPayload(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-native-1",
		ValidEvidenceIDs: []string{"case-native-1"},
	}

	var receivedPath string
	var receivedPayload map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(bodyBytes, &receivedPayload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"model": "qwen3.5:4b",
			"created_at": "2026-10-23T12:00:00Z",
			"message": {
				"role": "assistant",
				"content": "{\"summary\": \"Native Ollama investigation complete\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Case observed\", \"evidence_references\": [\"case-native-1\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"Verify with provider\"}]}"
			},
			"done_reason": "stop",
			"done": true
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	cfg := &config.Config{
		AIProvider:        "ollama",
		AIBaseURL:         srv.URL,
		AIModel:           "qwen3.5:4b",
		AIRequestTimeout:  5 * time.Second,
		AIMaxOutputTokens: 300,
		AIContextSize:     4096,
	}

	client := NewOllamaClient(cfg)
	res, err := client.Investigate(ctx, snap)
	if err != nil {
		t.Fatalf("expected successful investigation, got %v", err)
	}

	// 1. Ollama provider requests /api/chat, not /v1/chat/completions
	if receivedPath != "/api/chat" {
		t.Errorf("expected request path '/api/chat', got %q", receivedPath)
	}

	// 2. Payload contains think: false, format: 'json', stream: false, num_ctx, and num_predict
	if receivedPayload["model"] != "qwen3.5:4b" {
		t.Errorf("expected model='qwen3.5:4b', got %v", receivedPayload["model"])
	}
	if streamVal, ok := receivedPayload["stream"].(bool); !ok || streamVal {
		t.Errorf("expected stream=false, got %v", receivedPayload["stream"])
	}
	if thinkVal, ok := receivedPayload["think"].(bool); !ok || thinkVal {
		t.Errorf("expected think=false, got %v", receivedPayload["think"])
	}
	if receivedPayload["format"] != "json" {
		t.Errorf("expected format='json', got %v", receivedPayload["format"])
	}
	if receivedPayload["keep_alive"] != "10m" {
		t.Errorf("expected keep_alive='10m', got %v", receivedPayload["keep_alive"])
	}

	opts, ok := receivedPayload["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'options' object in payload, got %v", receivedPayload["options"])
	}
	if numCtx, ok := opts["num_ctx"].(float64); !ok || int(numCtx) != 4096 {
		t.Errorf("expected options.num_ctx=4096, got %v", opts["num_ctx"])
	}
	if numPredict, ok := opts["num_predict"].(float64); !ok || int(numPredict) != 300 {
		t.Errorf("expected options.num_predict=300, got %v", opts["num_predict"])
	}
	if temp, ok := opts["temperature"].(float64); !ok || temp != 0.1 {
		t.Errorf("expected options.temperature=0.1, got %v", opts["temperature"])
	}

	// 3. Valid native Ollama JSON response is decoded and passes grounding
	if res.Summary != "Native Ollama investigation complete" {
		t.Errorf("expected summary %q, got %q", "Native Ollama investigation complete", res.Summary)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("expected confidence HIGH, got %q", res.Confidence)
	}
	if len(res.Findings) != 1 || res.Findings[0].EvidenceReferences[0] != "case-native-1" {
		t.Errorf("unexpected findings: %+v", res.Findings)
	}
	if res.RawResponse["transport"] != "ollama_native_chat" {
		t.Errorf("expected transport 'ollama_native_chat', got %v", res.RawResponse["transport"])
	}
}

func TestOllamaClient_NativeInference_Failures(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-fail-1",
		ValidEvidenceIDs: []string{"case-fail-1"},
	}

	t.Run("Empty Message Content Fails Safely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"qwen3.5:4b","message":{"role":"assistant","content":""},"done":true}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("expected ErrInvalidSchema for empty message content, got %v", err)
		}
	})

	t.Run("Malformed JSON Fails Safely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"qwen3.5:4b","message":{"role":"assistant","content":"{malformed json"},"done":true}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("expected ErrInvalidSchema for malformed JSON, got %v", err)
		}
	})

	t.Run("Ungrounded Evidence Fails Closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := `{
				"model": "qwen3.5:4b",
				"message": {
					"role": "assistant",
					"content": "{\"summary\": \"Ungrounded\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Hallucinated\", \"evidence_references\": [\"hallucinated-evidence-999\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"test\"}]}"
				},
				"done": true
			}`
			_, _ = w.Write([]byte(resp))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrInvalidEvidenceRef) {
			t.Fatalf("expected ErrInvalidEvidenceRef for ungrounded evidence, got %v", err)
		}
	})

	t.Run("Provider Envelope Error Fails Safely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"model 'qwen3.5:4b' not found"}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrAIUnavailable) {
			t.Fatalf("expected ErrAIUnavailable for provider error, got %v", err)
		}
	})

	t.Run("HTTP 500 Server Error Fails Safely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal crash"}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrAIUnavailable) {
			t.Fatalf("expected ErrAIUnavailable for 500, got %v", err)
		}
	})

	t.Run("HTTP 429 Rate Limit Fails Safely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrAIRateLimited) {
			t.Fatalf("expected ErrAIRateLimited for 429, got %v", err)
		}
	})

	t.Run("Oversized Response Rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			huge := strings.Repeat("X", maxResponseBytes+100)
			fmt.Fprintf(w, `{"model":"qwen3.5:4b","message":{"role":"assistant","content":"%s"},"done":true}`, huge)
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("expected ErrResponseTooLarge, got %v", err)
		}
	})

	t.Run("Timeout Fails Safely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"qwen3.5:4b","message":{"role":"assistant","content":"{}"},"done":true}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  20 * time.Millisecond,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}
		client := NewOllamaClient(cfg)
		_, err := client.Investigate(ctx, snap)
		if !errors.Is(err, ErrAITimeout) {
			t.Fatalf("expected ErrAITimeout, got %v", err)
		}
	})
}

func TestOllamaClient_OpenAICompatibleTransport(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-compat-1",
		ValidEvidenceIDs: []string{"case-compat-1"},
	}

	validOpenAIResp := `{
		"id": "chat-compat-1",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "{\"summary\": \"Compatible provider result\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Case observed\", \"evidence_references\": [\"case-compat-1\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"Verify with provider\"}]}"
			},
			"finish_reason": "stop"
		}]
	}`

	providers := []struct {
		name         string
		providerType string
	}{
		{"openai-compatible", "openai-compatible"},
		{"openai", "openai"},
		{"generic-openai", "generic-openai"},
		{"empty-provider", ""},
	}

	for _, tc := range providers {
		t.Run("Provider="+tc.name, func(t *testing.T) {
			var receivedPath string
			var receivedPayload map[string]interface{}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				bodyBytes, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(bodyBytes, &receivedPayload)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(validOpenAIResp))
			}))
			defer srv.Close()

			cfg := &config.Config{
				AIProvider:        tc.providerType,
				AIBaseURL:         srv.URL,
				AIModel:           "gpt-4o-mini",
				AIRequestTimeout:  5 * time.Second,
				AIMaxOutputTokens: 500,
				AIContextSize:     4096,
			}

			client := NewOllamaClient(cfg)
			res, err := client.Investigate(ctx, snap)
			if err != nil {
				t.Fatalf("expected successful investigation, got %v", err)
			}

			// Verify it uses /v1/chat/completions, NOT /api/chat
			if receivedPath != "/v1/chat/completions" {
				t.Errorf("expected path '/v1/chat/completions', got %q", receivedPath)
			}

			// Verify native Ollama parameters are NOT sent
			if _, exists := receivedPayload["think"]; exists {
				t.Errorf("expected 'think' to be omitted for OpenAI-compatible, got %v", receivedPayload["think"])
			}
			if _, exists := receivedPayload["options"]; exists {
				t.Errorf("expected 'options' to be omitted for OpenAI-compatible, got %v", receivedPayload["options"])
			}
			if _, exists := receivedPayload["keep_alive"]; exists {
				t.Errorf("expected 'keep_alive' to be omitted for OpenAI-compatible, got %v", receivedPayload["keep_alive"])
			}
			if _, exists := receivedPayload["format"]; exists {
				t.Errorf("expected 'format' to be omitted for OpenAI-compatible, got %v", receivedPayload["format"])
			}

			// Verify OpenAI parameters
			if rf, ok := receivedPayload["response_format"].(map[string]interface{}); !ok || rf["type"] != "json_object" {
				t.Errorf("expected response_format.type='json_object', got %v", receivedPayload["response_format"])
			}

			if res.Summary != "Compatible provider result" {
				t.Errorf("expected summary 'Compatible provider result', got %q", res.Summary)
			}
			if res.RawResponse["transport"] != "openai_compatible_chat" {
				t.Errorf("expected transport 'openai_compatible_chat', got %v", res.RawResponse["transport"])
			}
		})
	}
}

func TestNewProvider_ProviderRouting(t *testing.T) {
	ctx := context.Background()
	snap := &InvestigationSnapshot{
		CaseID:           "case-route-1",
		ValidEvidenceIDs: []string{"case-route-1"},
	}

	// 1. Ollama provider routes to /api/chat
	t.Run("AIProvider=ollama routes to /api/chat", func(t *testing.T) {
		var receivedPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"model": "qwen3.5:4b",
				"message": {
					"role": "assistant",
					"content": "{\"summary\": \"Ollama routed\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Case observed\", \"evidence_references\": [\"case-route-1\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"Verify with provider\"}]}"
				},
				"done": true
			}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIEnabled:         true,
			AIProvider:        "ollama",
			AIBaseURL:         srv.URL,
			AIModel:           "qwen3.5:4b",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}

		prov, err := NewProvider(cfg)
		if err != nil {
			t.Fatalf("NewProvider failed: %v", err)
		}
		res, err := prov.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("Investigate failed: %v", err)
		}
		if receivedPath != "/api/chat" {
			t.Errorf("expected route /api/chat, got %q", receivedPath)
		}
		if res.Summary != "Ollama routed" {
			t.Errorf("expected summary 'Ollama routed', got %q", res.Summary)
		}
	})

	// 2. OpenAI-compatible routes to /v1/chat/completions
	t.Run("AIProvider=openai-compatible routes to /v1/chat/completions", func(t *testing.T) {
		var receivedPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "chat-route-1",
				"choices": [{
					"message": {
						"role": "assistant",
						"content": "{\"summary\": \"OpenAI routed\", \"confidence\": \"HIGH\", \"findings\": [{\"explanation\": \"Case observed\", \"evidence_references\": [\"case-route-1\"]}], \"recommended_next_steps\": [{\"action\": \"CHECK_PROVIDER_STATUS\", \"reason\": \"Verify with provider\"}]}"
					},
					"finish_reason": "stop"
				}]
			}`))
		}))
		defer srv.Close()

		cfg := &config.Config{
			AIEnabled:         true,
			AIProvider:        "openai-compatible",
			AIBaseURL:         srv.URL,
			AIModel:           "gpt-4o-mini",
			AIRequestTimeout:  5 * time.Second,
			AIMaxOutputTokens: 300,
			AIContextSize:     4096,
		}

		prov, err := NewProvider(cfg)
		if err != nil {
			t.Fatalf("NewProvider failed: %v", err)
		}
		res, err := prov.Investigate(ctx, snap)
		if err != nil {
			t.Fatalf("Investigate failed: %v", err)
		}
		if receivedPath != "/v1/chat/completions" {
			t.Errorf("expected route /v1/chat/completions, got %q", receivedPath)
		}
		if res.Summary != "OpenAI routed" {
			t.Errorf("expected summary 'OpenAI routed', got %q", res.Summary)
		}
	})
}
