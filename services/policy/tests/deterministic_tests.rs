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

#[test]
fn test_payout_op_self_approval_is_denied() {
    let engine = default_engine();
    let req = claimbridge_policy::PayoutOperationEvaluationRequest {
        operation_id: "op-101".to_string(),
        case_id: "case-001".to_string(),
        amount_cents: 35_000, // $350.00
        currency: "USD".to_string(),
        initiator_user_id: "user-initiator".to_string(),
        actor_user_id: "user-initiator".to_string(), // Self-approval attempt!
        current_state: claimbridge_policy::PayoutOperationState::AwaitingApproval,
        requested_action: claimbridge_policy::PayoutAction::Approve,
        provider_item_status: None,
        requires_dual_approval: true,
    };

    let res = engine.evaluate_payout_operation(&req);
    assert!(!res.allowed);
    assert_eq!(res.reason_code, "ERR_SELF_APPROVAL_FORBIDDEN");
    assert_eq!(res.target_state, None);
}

#[test]
fn test_payout_op_dual_approval_by_distinct_user_is_permitted() {
    let engine = default_engine();
    let req = claimbridge_policy::PayoutOperationEvaluationRequest {
        operation_id: "op-101".to_string(),
        case_id: "case-001".to_string(),
        amount_cents: 35_000,
        currency: "USD".to_string(),
        initiator_user_id: "user-initiator".to_string(),
        actor_user_id: "user-distinct-auditor".to_string(), // Distinct auditor
        current_state: claimbridge_policy::PayoutOperationState::AwaitingApproval,
        requested_action: claimbridge_policy::PayoutAction::Approve,
        provider_item_status: None,
        requires_dual_approval: true,
    };

    let res = engine.evaluate_payout_operation(&req);
    assert!(res.allowed);
    assert_eq!(res.reason_code, "RULE_APPROVAL_GRANTED");
    assert_eq!(
        res.target_state,
        Some(claimbridge_policy::PayoutOperationState::Approved)
    );
}

#[test]
fn test_payout_op_submit_without_required_approval_is_rejected() {
    let engine = default_engine();
    let req = claimbridge_policy::PayoutOperationEvaluationRequest {
        operation_id: "op-102".to_string(),
        case_id: "case-002".to_string(),
        amount_cents: 30_000,
        currency: "USD".to_string(),
        initiator_user_id: "user-1".to_string(),
        actor_user_id: "user-1".to_string(),
        current_state: claimbridge_policy::PayoutOperationState::AwaitingApproval,
        requested_action: claimbridge_policy::PayoutAction::Submit,
        provider_item_status: None,
        requires_dual_approval: true,
    };

    let res = engine.evaluate_payout_operation(&req);
    assert!(!res.allowed);
    assert_eq!(res.reason_code, "ERR_MISSING_REQUIRED_APPROVAL");
}

#[test]
fn test_payout_op_cancellation_eligibility() {
    let engine = default_engine();

    // 1. UNCLAIMED provider item is eligible for cancellation
    let req_unclaimed = claimbridge_policy::PayoutOperationEvaluationRequest {
        operation_id: "op-103".to_string(),
        case_id: "case-003".to_string(),
        amount_cents: 10_000,
        currency: "USD".to_string(),
        initiator_user_id: "user-1".to_string(),
        actor_user_id: "user-1".to_string(),
        current_state: claimbridge_policy::PayoutOperationState::Submitted,
        requested_action: claimbridge_policy::PayoutAction::Cancel,
        provider_item_status: Some("UNCLAIMED".to_string()),
        requires_dual_approval: false,
    };
    let res_unclaimed = engine.evaluate_payout_operation(&req_unclaimed);
    assert!(res_unclaimed.allowed);
    assert!(res_unclaimed.is_cancellation_eligible);
    assert_eq!(res_unclaimed.reason_code, "RULE_CANCELLATION_PERMITTED");
    assert_eq!(
        res_unclaimed.target_state,
        Some(claimbridge_policy::PayoutOperationState::Cancelled)
    );

    // 2. SUCCESS provider item CANNOT be cancelled
    let req_success = claimbridge_policy::PayoutOperationEvaluationRequest {
        operation_id: "op-103".to_string(),
        case_id: "case-003".to_string(),
        amount_cents: 10_000,
        currency: "USD".to_string(),
        initiator_user_id: "user-1".to_string(),
        actor_user_id: "user-1".to_string(),
        current_state: claimbridge_policy::PayoutOperationState::Submitted,
        requested_action: claimbridge_policy::PayoutAction::Cancel,
        provider_item_status: Some("SUCCESS".to_string()),
        requires_dual_approval: false,
    };
    let res_success = engine.evaluate_payout_operation(&req_success);
    assert!(!res_success.allowed);
    assert!(!res_success.is_cancellation_eligible);
    assert_eq!(res_success.reason_code, "ERR_NOT_ELIGIBLE_FOR_CANCELLATION");
}

#[test]
fn test_payout_op_terminal_state_mutation_rejected() {
    let engine = default_engine();
    let req = claimbridge_policy::PayoutOperationEvaluationRequest {
        operation_id: "op-104".to_string(),
        case_id: "case-004".to_string(),
        amount_cents: 5_000,
        currency: "USD".to_string(),
        initiator_user_id: "user-1".to_string(),
        actor_user_id: "user-1".to_string(),
        current_state: claimbridge_policy::PayoutOperationState::Succeeded,
        requested_action: claimbridge_policy::PayoutAction::Submit,
        provider_item_status: Some("SUCCESS".to_string()),
        requires_dual_approval: false,
    };

    let res = engine.evaluate_payout_operation(&req);
    assert!(!res.allowed);
    assert!(res.permitted_actions.is_empty());
    assert!(res.is_investigation_permitted);
}
