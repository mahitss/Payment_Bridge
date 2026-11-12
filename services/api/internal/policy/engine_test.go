package policy

import (
	"context"
	"testing"
)

func TestEngine_Evaluate_Approved(t *testing.T) {
	eng := NewEngine("")
	req := EvaluationRequest{
		ClaimID:       "claim-test-1",
		ClaimantEmail: "user@example.com",
		AmountCents:   12000,
		Currency:      "USD",
		RiskScore:     10.0,
		CurrentState:  StateAwaitingPolicyCheck,
	}

	res, err := eng.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionApproved {
		t.Errorf("expected DecisionApproved, got: %s", res.Decision)
	}
	if res.RequiresDualApproval {
		t.Errorf("expected RequiresDualApproval false, got true")
	}
	if len(res.Violations) != 0 {
		t.Errorf("expected 0 violations, got: %v", res.Violations)
	}
}

func TestEngine_Evaluate_Denied_CeilingExceeded(t *testing.T) {
	eng := NewEngine("")
	req := EvaluationRequest{
		ClaimID:       "claim-test-2",
		ClaimantEmail: "user@example.com",
		AmountCents:   75000, // > $500 max
		Currency:      "USD",
		RiskScore:     5.0,
		CurrentState:  StateAwaitingPolicyCheck,
	}

	res, err := eng.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionDenied {
		t.Errorf("expected DecisionDenied, got: %s", res.Decision)
	}
	if len(res.Violations) == 0 {
		t.Errorf("expected violations, got none")
	}
}

func TestEngine_Evaluate_EscalatedForDualApproval(t *testing.T) {
	eng := NewEngine("")
	req := EvaluationRequest{
		ClaimID:       "claim-test-3",
		ClaimantEmail: "user@example.com",
		AmountCents:   30000, // > $250 threshold
		Currency:      "USD",
		RiskScore:     10.0,
		CurrentState:  StateAwaitingPolicyCheck,
	}

	res, err := eng.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionEscalatedForDualApproval {
		t.Errorf("expected DecisionEscalatedForDualApproval, got: %s", res.Decision)
	}
	if !res.RequiresDualApproval {
		t.Errorf("expected RequiresDualApproval true, got false")
	}
}

func TestEngine_AuthorityMode(t *testing.T) {
	fallbackEng := NewEngine("")
	if fallbackEng.AuthorityMode() != "go_deterministic_fallback" {
		t.Errorf("expected go_deterministic_fallback, got %s", fallbackEng.AuthorityMode())
	}
	if fallbackEng.BinaryPath() != "" {
		t.Errorf("expected empty binary path, got %s", fallbackEng.BinaryPath())
	}

	binaryEng := NewEngine("/usr/local/bin/claimbridge-policy")
	if binaryEng.AuthorityMode() != "rust_binary" {
		t.Errorf("expected rust_binary, got %s", binaryEng.AuthorityMode())
	}
	if binaryEng.BinaryPath() != "/usr/local/bin/claimbridge-policy" {
		t.Errorf("expected /usr/local/bin/claimbridge-policy, got %s", binaryEng.BinaryPath())
	}
}
