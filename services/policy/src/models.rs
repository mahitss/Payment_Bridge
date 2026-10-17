use serde::{Deserialize, Serialize};

/// Explicit payout and claim states
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub enum ClaimState {
    Draft,
    PendingReview,
    AwaitingPolicyCheck,
    Approved,
    FlaggedHighRisk,
    EscalatedForDualApproval,
    PayoutScheduled,
    PayoutSubmitted,
    PayoutSettled,
    PayoutFailed,
    PayoutBlocked,
}

/// Permitted lifecycle actions
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub enum ClaimAction {
    SubmitForTriage,
    RunPolicyCheck,
    ApproveLowRisk,
    EscalateForReview,
    FlagHighRisk,
    GrantDualApproval,
    RejectClaim,
    SchedulePayout,
    DispatchPayout,
    ConfirmSettlement,
    MarkPayoutFailed,
    BlockPayout,
}

/// Explicit policy decision outcomes
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub enum PolicyDecision {
    Approved,
    EscalatedForDualApproval,
    FlaggedHighRisk,
    Denied,
}

/// Policy evaluation input payload
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PolicyEvaluationRequest {
    pub claim_id: String,
    pub claimant_email: String,
    pub amount_cents: u64,
    pub currency: String,
    pub risk_score: f64,
    pub current_state: ClaimState,
    pub requested_action: Option<ClaimAction>,
}

/// Specific invariant violation codes
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PolicyViolation {
    InvalidClaimId,
    AmountZeroOrNegative,
    AmountExceedsCeiling,
    UnsupportedCurrency,
    InvalidRecipientEmail,
    RiskScoreOutOfBounds,
    RiskScoreUnacceptable,
    InvalidStateTransition,
    UnapprovedPayoutAttempt,
}

/// Policy evaluation result
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PolicyEvaluationResponse {
    pub claim_id: String,
    pub decision: PolicyDecision,
    pub requires_dual_approval: bool,
    pub violations: Vec<PolicyViolation>,
    pub current_state: ClaimState,
    pub target_state: Option<ClaimState>,
    pub timestamp_utc: String,
}

/// Policy engine configuration limits
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PolicyConfig {
    pub max_single_payout_cents: u64,
    pub dual_approval_threshold_cents: u64,
    pub max_auto_approval_risk_score: f64,
    pub hard_block_risk_score: f64,
    pub allowed_currencies: Vec<String>,
}

impl Default for PolicyConfig {
    fn default() -> Self {
        Self {
            max_single_payout_cents: 50_000,       // $500.00 USD
            dual_approval_threshold_cents: 25_000, // $250.00 USD
            max_auto_approval_risk_score: 20.0,    // Scores > 20 require human dual-approval
            hard_block_risk_score: 80.0,           // Scores > 80 are auto-denied
            allowed_currencies: vec!["USD".to_string()],
        }
    }
}

/// Explicit lifecycle states for Payout Operations
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PayoutOperationState {
    Prepared,
    AwaitingApproval,
    Approved,
    Submitting,
    Submitted,
    Succeeded,
    Failed,
    ReconciliationRequired,
    Cancelled,
}

/// Permitted actions on a Payout Operation
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PayoutAction {
    Prepare,
    RequestApproval,
    Approve,
    Submit,
    RecordSuccess,
    RecordFailure,
    FlagReconciliation,
    Cancel,
}

/// Payout operation policy evaluation request
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PayoutOperationEvaluationRequest {
    pub operation_id: String,
    pub case_id: String,
    pub amount_cents: u64,
    pub currency: String,
    pub initiator_user_id: String,
    pub actor_user_id: String,
    pub current_state: PayoutOperationState,
    pub requested_action: PayoutAction,
    pub provider_item_status: Option<String>,
    pub requires_dual_approval: bool,
}

/// Payout operation policy evaluation decision
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PayoutOperationEvaluationResponse {
    pub operation_id: String,
    pub allowed: bool,
    pub reason_code: String,
    pub target_state: Option<PayoutOperationState>,
    pub permitted_actions: Vec<PayoutAction>,
    pub is_investigation_permitted: bool,
    pub is_cancellation_eligible: bool,
}
