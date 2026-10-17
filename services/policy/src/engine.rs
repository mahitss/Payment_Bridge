use crate::models::{
    ClaimAction, ClaimState, PolicyConfig, PolicyDecision, PolicyEvaluationRequest,
    PolicyEvaluationResponse, PolicyViolation,
};

pub struct PolicyEngine {
    config: PolicyConfig,
}

impl PolicyEngine {
    pub fn new(config: PolicyConfig) -> Self {
        Self { config }
    }

    /// Evaluates a claim against deterministic safety policies.
    /// Operates purely deterministically with fail-closed semantics.
    pub fn evaluate(&self, req: &PolicyEvaluationRequest) -> PolicyEvaluationResponse {
        let mut violations = Vec::new();

        // 1. Basic Invariant Validation (Fail-closed on any malformed input)
        if req.claim_id.trim().is_empty() {
            violations.push(PolicyViolation::InvalidClaimId);
        }

        if req.amount_cents == 0 {
            violations.push(PolicyViolation::AmountZeroOrNegative);
        } else if req.amount_cents > self.config.max_single_payout_cents {
            violations.push(PolicyViolation::AmountExceedsCeiling);
        }

        if !self
            .config
            .allowed_currencies
            .iter()
            .any(|c| c.eq_ignore_ascii_case(&req.currency))
        {
            violations.push(PolicyViolation::UnsupportedCurrency);
        }

        if !is_valid_email(&req.claimant_email) {
            violations.push(PolicyViolation::InvalidRecipientEmail);
        }

        if req.risk_score < 0.0 || req.risk_score > 100.0 || req.risk_score.is_nan() {
            violations.push(PolicyViolation::RiskScoreOutOfBounds);
        } else if req.risk_score >= self.config.hard_block_risk_score {
            violations.push(PolicyViolation::RiskScoreUnacceptable);
        }

        // 2. Action and State Transition Invariants
        let mut target_state = None;
        if let Some(action) = req.requested_action {
            match Self::next_state(req.current_state, action) {
                Some(next) => {
                    // Payout specific safety assertion:
                    // Cannot schedule or dispatch payout unless the claim has been fully APPROVED.
                    if (action == ClaimAction::SchedulePayout
                        || action == ClaimAction::DispatchPayout)
                        && req.current_state != ClaimState::Approved
                        && req.current_state != ClaimState::PayoutScheduled
                    {
                        violations.push(PolicyViolation::UnapprovedPayoutAttempt);
                    }
                    target_state = Some(next);
                }
                None => {
                    violations.push(PolicyViolation::InvalidStateTransition);
                }
            }
        }

        // 3. Fail-Closed Decision Resolution
        let (decision, requires_dual_approval) = if !violations.is_empty() {
            // Any safety violation triggers immediate DENIED
            (PolicyDecision::Denied, false)
        } else if req.amount_cents > self.config.dual_approval_threshold_cents
            || req.risk_score > self.config.max_auto_approval_risk_score
        {
            // Requires human dual approval
            (PolicyDecision::EscalatedForDualApproval, true)
        } else {
            (PolicyDecision::Approved, false)
        };

        // If escalated, override target state to EscalatedForDualApproval
        let resolved_target = if decision == PolicyDecision::EscalatedForDualApproval {
            Some(ClaimState::EscalatedForDualApproval)
        } else if decision == PolicyDecision::Denied {
            Some(ClaimState::PayoutBlocked)
        } else {
            target_state
        };

        PolicyEvaluationResponse {
            claim_id: req.claim_id.clone(),
            decision,
            requires_dual_approval,
            violations,
            current_state: req.current_state,
            target_state: resolved_target,
            timestamp_utc: "deterministic-eval".to_string(),
        }
    }

    /// Evaluates if a state transition is legal according to the lifecycle state graph
    pub fn can_transition(from: ClaimState, to: ClaimState, action: ClaimAction) -> bool {
        match Self::next_state(from, action) {
            Some(expected) => expected == to,
            None => false,
        }
    }

    /// Deterministic state machine transition rules
    pub fn next_state(from: ClaimState, action: ClaimAction) -> Option<ClaimState> {
        match (from, action) {
            (ClaimState::Draft, ClaimAction::SubmitForTriage) => Some(ClaimState::PendingReview),
            (ClaimState::PendingReview, ClaimAction::RunPolicyCheck) => {
                Some(ClaimState::AwaitingPolicyCheck)
            }
            (ClaimState::AwaitingPolicyCheck, ClaimAction::ApproveLowRisk) => {
                Some(ClaimState::Approved)
            }
            (ClaimState::AwaitingPolicyCheck, ClaimAction::EscalateForReview) => {
                Some(ClaimState::EscalatedForDualApproval)
            }
            (ClaimState::AwaitingPolicyCheck, ClaimAction::FlagHighRisk) => {
                Some(ClaimState::FlaggedHighRisk)
            }
            (ClaimState::EscalatedForDualApproval, ClaimAction::GrantDualApproval) => {
                Some(ClaimState::Approved)
            }
            (ClaimState::PendingReview, ClaimAction::RejectClaim)
            | (ClaimState::AwaitingPolicyCheck, ClaimAction::RejectClaim)
            | (ClaimState::EscalatedForDualApproval, ClaimAction::RejectClaim)
            | (ClaimState::FlaggedHighRisk, ClaimAction::RejectClaim) => {
                Some(ClaimState::PayoutBlocked)
            }
            (ClaimState::Approved, ClaimAction::SchedulePayout) => {
                Some(ClaimState::PayoutScheduled)
            }
            (ClaimState::PayoutScheduled, ClaimAction::DispatchPayout) => {
                Some(ClaimState::PayoutSubmitted)
            }
            (ClaimState::PayoutSubmitted, ClaimAction::ConfirmSettlement) => {
                Some(ClaimState::PayoutSettled)
            }
            (ClaimState::PayoutSubmitted, ClaimAction::MarkPayoutFailed) => {
                Some(ClaimState::PayoutFailed)
            }
            // Emergency lockout from any non-terminal state
            (s, ClaimAction::BlockPayout)
                if s != ClaimState::PayoutSettled && s != ClaimState::PayoutBlocked =>
            {
                Some(ClaimState::PayoutBlocked)
            }
            _ => None,
        }
    }
}

/// Strict RFC-compliant basic email sanity check
fn is_valid_email(email: &str) -> bool {
    let email = email.trim();
    if email.len() < 5 || email.len() > 254 {
        return false;
    }
    let parts: Vec<&str> = email.split('@').collect();
    if parts.len() != 2 {
        return false;
    }
    let (local, domain) = (parts[0], parts[1]);
    if local.is_empty() || domain.is_empty() {
        return false;
    }
    if !domain.contains('.') {
        return false;
    }
    if domain.starts_with('.') || domain.ends_with('.') {
        return false;
    }
    !email
        .chars()
        .any(|c| c.is_whitespace() || c == ':' || c == ';' || c == ',')
}
