export interface PayoutCase {
  id: string;
  organization_id: string;
  created_by_user_id: string;
  reference: string;
  recipient_email: string;
  recipient_name: string;
  title: string;
  description?: string;
  currency: string;
  amount_cents: number;
  status: "DRAFT" | "SUBMITTED" | "COMPLETED" | "CANCELLED" | "DENIED" | "FAILED";
  risk_score: number;
  dual_approval_required: boolean;
  created_at: string;
  updated_at: string;
}

export interface PayoutOperation {
  id: string;
  payout_case_id: string;
  organization_id: string;
  initiated_by_user_id: string;
  approved_by_user_id?: string;
  sender_batch_id: string;
  sender_item_id: string;
  paypal_payout_batch_id?: string;
  paypal_payout_item_id?: string;
  amount_cents: number;
  currency: string;
  status:
    | "PREPARED"
    | "AWAITING_APPROVAL"
    | "APPROVED"
    | "SUBMITTING"
    | "SUBMITTED"
    | "SUCCEEDED"
    | "FAILED"
    | "CANCELLED"
    | "RECONCILIATION_REQUIRED";
  requires_dual_approval: boolean;
  paypal_batch_status?: string;
  paypal_item_status?: string;
  error_message?: string;
  raw_provider_response?: Record<string, any>;
  submitted_at?: string;
  completed_at?: string;
  cancelled_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ConnectionStatus {
  connected: boolean;
  environment: string;
  base_url: string;
  app_id?: string;
  token_expires_in?: number;
  checked_at: string;
  error?: string;
}

export interface InvestigationFinding {
  explanation: string;
  evidence_references: string[];
}

export interface InvestigationRecommendation {
  action:
    | "REQUEST_RECIPIENT_INFORMATION"
    | "CHECK_PROVIDER_STATUS"
    | "REVIEW_RECIPIENT_DETAILS"
    | "REVIEW_CANCELLATION_ELIGIBILITY"
    | "ESCALATE_TO_OPERATOR"
    | "NO_ACTION_UNTIL_RECONCILED";
  reason: string;
  requires_human_review: boolean;
  deterministic_policy_permitted: boolean;
  policy_rule_id?: string;
}

export interface PayoutInvestigation {
  id: string;
  payout_case_id: string;
  organization_id: string;
  requested_by_user_id: string;
  provider: string;
  model: string;
  prompt_version: string;
  status: "COMPLETED" | "FAILED" | "INVALID_EVIDENCE_REFERENCE";
  summary: string;
  confidence: "HIGH" | "MEDIUM" | "LOW" | "INSUFFICIENT_EVIDENCE";
  requires_human_review: boolean;
  observed_facts: string[];
  findings: InvestigationFinding[];
  recommended_next_steps: InvestigationRecommendation[];
  missing_information: string[];
  risk_flags: string[];
  limitations: string[];
  raw_response?: Record<string, any>;
  error_message?: string;
  created_at: string;
}

/**
 * Format minor unit cents as standard USD currency string ($XX.XX).
 * Floating-point arithmetic is never used; string padding guarantees integer precision.
 */
export function formatCentsToUSD(cents: number): string {
  const isNegative = cents < 0;
  const absCents = Math.abs(cents);
  const dollars = Math.floor(absCents / 100);
  const remainder = absCents % 100;
  const remainderStr = remainder < 10 ? `0${remainder}` : `${remainder}`;
  return `${isNegative ? "-" : ""}$${dollars}.${remainderStr}`;
}

/**
 * Formats ISO timestamps into human-readable UTC representation.
 */
export function formatDate(isoString?: string): string {
  if (!isoString) return "—";
  try {
    const d = new Date(isoString);
    return d.toLocaleString("en-US", {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
  } catch {
    return isoString;
  }
}
