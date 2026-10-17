package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrPolicyViolated = errors.New("claim policy violated")
	ErrEngineFailed   = errors.New("policy engine execution failed")
)

type ClaimState string

const (
	StateDraft                    ClaimState = "Draft"
	StatePendingReview            ClaimState = "PendingReview"
	StateAwaitingPolicyCheck      ClaimState = "AwaitingPolicyCheck"
	StateApproved                 ClaimState = "Approved"
	StateFlaggedHighRisk          ClaimState = "FlaggedHighRisk"
	StateEscalatedForDualApproval ClaimState = "EscalatedForDualApproval"
	StatePayoutScheduled          ClaimState = "PayoutScheduled"
	StatePayoutSubmitted          ClaimState = "PayoutSubmitted"
	StatePayoutSettled            ClaimState = "PayoutSettled"
	StatePayoutFailed             ClaimState = "PayoutFailed"
	StatePayoutBlocked            ClaimState = "PayoutBlocked"
)

type PolicyDecision string

const (
	DecisionApproved                 PolicyDecision = "Approved"
	DecisionEscalatedForDualApproval PolicyDecision = "EscalatedForDualApproval"
	DecisionFlaggedHighRisk          PolicyDecision = "FlaggedHighRisk"
	DecisionDenied                   PolicyDecision = "Denied"
)

// EvaluationRequest matches the Rust crate PolicyEvaluationRequest schema.
type EvaluationRequest struct {
	ClaimID         string     `json:"claim_id"`
	ClaimantEmail   string     `json:"claimant_email"`
	AmountCents     uint64     `json:"amount_cents"`
	Currency        string     `json:"currency"`
	RiskScore       float64    `json:"risk_score"`
	CurrentState    ClaimState `json:"current_state"`
	RequestedAction *string    `json:"requested_action,omitempty"`
}

// EvaluationResponse matches the Rust crate PolicyEvaluationResponse schema.
type EvaluationResponse struct {
	ClaimID              string         `json:"claim_id"`
	Decision             PolicyDecision `json:"decision"`
	RequiresDualApproval bool           `json:"requires_dual_approval"`
	Violations           []string       `json:"violations"`
	CurrentState         ClaimState     `json:"current_state"`
	TargetState          *ClaimState    `json:"target_state"`
	TimestampUTC         string         `json:"timestamp_utc"`
}

// PolicyEngine provides an interface for deterministic safety evaluation.
type PolicyEngine interface {
	Evaluate(ctx context.Context, req EvaluationRequest) (*EvaluationResponse, error)
}

// Engine implements PolicyEngine using the compiled Rust binary with fallback to in-memory evaluation.
type Engine struct {
	binaryPath string
}

func NewEngine(binaryPath string) *Engine {
	return &Engine{binaryPath: binaryPath}
}

func (e *Engine) Evaluate(ctx context.Context, req EvaluationRequest) (*EvaluationResponse, error) {
	// If binaryPath is set and exists, run via Rust binary
	if e.binaryPath != "" {
		res, err := e.evaluateViaBinary(ctx, req)
		if err == nil {
			return res, nil
		}
		// If binary fails, we fail-closed or fall back to native deterministic checks
	}

	return e.evaluateDeterministically(req)
}

func (e *Engine) evaluateViaBinary(ctx context.Context, req EvaluationRequest) (*EvaluationResponse, error) {
	inputJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal policy request: %w", err)
	}

	cmd := exec.CommandContext(ctx, e.binaryPath, "--stdin")
	cmd.Stdin = bytes.NewReader(inputJSON)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if outBuf.Len() > 0 {
		var res EvaluationResponse
		if err := json.Unmarshal(outBuf.Bytes(), &res); err == nil {
			return &res, nil
		}
	}

	return nil, fmt.Errorf("%w: %v, stderr: %s", ErrEngineFailed, runErr, errBuf.String())
}

// evaluateDeterministically provides native fail-closed policy checks identical to the Rust crate.
func (e *Engine) evaluateDeterministically(req EvaluationRequest) (*EvaluationResponse, error) {
	var violations []string

	if strings.TrimSpace(req.ClaimID) == "" {
		violations = append(violations, "INVALID_CLAIM_ID")
	}

	if req.AmountCents == 0 {
		violations = append(violations, "AMOUNT_ZERO_OR_NEGATIVE")
	} else if req.AmountCents > 50000 { // Max $500.00
		violations = append(violations, "AMOUNT_EXCEEDS_CEILING")
	}

	if !strings.EqualFold(req.Currency, "USD") {
		violations = append(violations, "UNSUPPORTED_CURRENCY")
	}

	if !isValidEmail(req.ClaimantEmail) {
		violations = append(violations, "INVALID_RECIPIENT_EMAIL")
	}

	if req.RiskScore < 0.0 || req.RiskScore > 100.0 {
		violations = append(violations, "RISK_SCORE_OUT_OF_BOUNDS")
	} else if req.RiskScore >= 80.0 {
		violations = append(violations, "RISK_SCORE_UNACCEPTABLE")
	}

	decision := DecisionApproved
	requiresDual := false
	targetState := StateApproved

	if len(violations) > 0 {
		decision = DecisionDenied
		targetState = StatePayoutBlocked
	} else if req.AmountCents > 25000 || req.RiskScore > 20.0 {
		decision = DecisionEscalatedForDualApproval
		requiresDual = true
		targetState = StateEscalatedForDualApproval
	}

	return &EvaluationResponse{
		ClaimID:              req.ClaimID,
		Decision:             decision,
		RequiresDualApproval: requiresDual,
		Violations:           violations,
		CurrentState:         req.CurrentState,
		TargetState:          &targetState,
		TimestampUTC:         time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if len(email) < 5 || len(email) > 254 {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "" || parts[1] == "" {
		return false
	}
	if !strings.Contains(parts[1], ".") {
		return false
	}
	return !strings.ContainsAny(email, " \t\r\n;:,")
}
