use claimbridge_policy::{
    ClaimAction, ClaimState, PolicyConfig, PolicyDecision, PolicyEngine, PolicyEvaluationRequest,
    PolicyViolation,
};

fn default_engine() -> PolicyEngine {
    PolicyEngine::new(PolicyConfig::default())
}

fn valid_low_risk_request() -> PolicyEvaluationRequest {
    PolicyEvaluationRequest {
        claim_id: "claim-1001".to_string(),
        claimant_email: "claimant@example.com".to_string(),
        amount_cents: 15_000, // $150.00
        currency: "USD".to_string(),
        risk_score: 5.0,
        current_state: ClaimState::AwaitingPolicyCheck,
        requested_action: Some(ClaimAction::ApproveLowRisk),
    }
}

#[test]
fn test_valid_low_risk_claim_is_approved() {
    let engine = default_engine();
    let req = valid_low_risk_request();

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Approved);
    assert!(!res.requires_dual_approval);
    assert!(res.violations.is_empty());
    assert_eq!(res.target_state, Some(ClaimState::Approved));
}

#[test]
fn test_amount_exceeding_ceiling_is_denied() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.amount_cents = 60_000; // $600.00 (> $500 max)

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Denied);
    assert!(res
        .violations
        .contains(&PolicyViolation::AmountExceedsCeiling));
    assert_eq!(res.target_state, Some(ClaimState::PayoutBlocked));
}

#[test]
fn test_zero_amount_is_denied() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.amount_cents = 0;

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Denied);
    assert!(res
        .violations
        .contains(&PolicyViolation::AmountZeroOrNegative));
}

#[test]
fn test_unsupported_currency_is_denied() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.currency = "EUR".to_string();

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Denied);
    assert!(res
        .violations
        .contains(&PolicyViolation::UnsupportedCurrency));
}

#[test]
fn test_invalid_email_is_denied() {
    let engine = default_engine();
    let invalid_emails = vec![
        "notanemail",
        "user@",
        "@domain.com",
        "user @domain.com",
        "user@domain",
    ];

    for email in invalid_emails {
        let mut req = valid_low_risk_request();
        req.claimant_email = email.to_string();
        let res = engine.evaluate(&req);

        assert_eq!(
            res.decision,
            PolicyDecision::Denied,
            "Email '{}' should be denied",
            email
        );
        assert!(res
            .violations
            .contains(&PolicyViolation::InvalidRecipientEmail));
    }
}

#[test]
fn test_moderate_amount_requires_dual_approval() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.amount_cents = 35_000; // $350.00 (> $250 threshold)

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::EscalatedForDualApproval);
    assert!(res.requires_dual_approval);
    assert!(res.violations.is_empty());
    assert_eq!(res.target_state, Some(ClaimState::EscalatedForDualApproval));
}

#[test]
fn test_moderate_risk_requires_dual_approval() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.risk_score = 45.0; // > 20.0 auto threshold

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::EscalatedForDualApproval);
    assert!(res.requires_dual_approval);
    assert_eq!(res.target_state, Some(ClaimState::EscalatedForDualApproval));
}

#[test]
fn test_extreme_risk_is_hard_blocked() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.risk_score = 85.0; // > 80.0 hard block limit

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Denied);
    assert!(res
        .violations
        .contains(&PolicyViolation::RiskScoreUnacceptable));
}

#[test]
fn test_nan_or_negative_risk_score_is_denied() {
    let engine = default_engine();
    let mut req = valid_low_risk_request();
    req.risk_score = -5.0;

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Denied);
    assert!(res
        .violations
        .contains(&PolicyViolation::RiskScoreOutOfBounds));
}

#[test]
fn test_unapproved_claim_cannot_dispatch_payout() {
    let engine = default_engine();
    let req = PolicyEvaluationRequest {
        claim_id: "claim-unapproved".to_string(),
        claimant_email: "claimant@example.com".to_string(),
        amount_cents: 10_000,
        currency: "USD".to_string(),
        risk_score: 5.0,
        current_state: ClaimState::PendingReview,
        requested_action: Some(ClaimAction::DispatchPayout),
    };

    let res = engine.evaluate(&req);

    assert_eq!(res.decision, PolicyDecision::Denied);
    assert!(res
        .violations
        .contains(&PolicyViolation::InvalidStateTransition));
}

#[test]
fn test_legal_lifecycle_transitions() {
    // Draft -> PendingReview
    assert!(PolicyEngine::can_transition(
        ClaimState::Draft,
        ClaimState::PendingReview,
        ClaimAction::SubmitForTriage
    ));

    // PendingReview -> AwaitingPolicyCheck
    assert!(PolicyEngine::can_transition(
        ClaimState::PendingReview,
        ClaimState::AwaitingPolicyCheck,
        ClaimAction::RunPolicyCheck
    ));

    // Approved -> PayoutScheduled
    assert!(PolicyEngine::can_transition(
        ClaimState::Approved,
        ClaimState::PayoutScheduled,
        ClaimAction::SchedulePayout
    ));

    // PayoutScheduled -> PayoutSubmitted
    assert!(PolicyEngine::can_transition(
        ClaimState::PayoutScheduled,
        ClaimState::PayoutSubmitted,
        ClaimAction::DispatchPayout
    ));

    // PayoutSubmitted -> PayoutSettled
    assert!(PolicyEngine::can_transition(
        ClaimState::PayoutSubmitted,
        ClaimState::PayoutSettled,
        ClaimAction::ConfirmSettlement
    ));
}

#[test]
fn test_illegal_state_jumps_rejected() {
    // Cannot jump from Draft directly to PayoutSettled
    assert!(!PolicyEngine::can_transition(
        ClaimState::Draft,
        ClaimState::PayoutSettled,
        ClaimAction::ConfirmSettlement
    ));

    // Cannot jump from PayoutFailed directly to Approved
    assert!(!PolicyEngine::can_transition(
        ClaimState::PayoutFailed,
        ClaimState::Approved,
        ClaimAction::ApproveLowRisk
    ));
}
