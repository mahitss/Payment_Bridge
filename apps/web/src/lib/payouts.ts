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
  is_synthetic_fixture?: boolean;
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
    | "RECONCILIATION_REQUIRED"
    | "RECOVERING"
    | "MANUAL_RECONCILIATION_REQUIRED";
  submission_fingerprint?: string;
  requires_dual_approval: boolean;
  paypal_batch_status?: string;
  paypal_item_status?: string;
  error_message?: string;
  raw_provider_response?: Record<string, any>;
  submitted_at?: string;
  completed_at?: string;
  cancelled_at?: string;
  is_synthetic_fixture?: boolean;
  recovery_attempt_id?: string;
  recovery_claimed_at?: string;
  recovery_lease_expires_at?: string;
  created_at: string;
  updated_at: string;
}

export interface PayoutStatusSnapshot {
  id: string;
  payout_operation_id: string;
  organization_id: string;
  provider: string;
  batch_status: string;
  item_status?: string;
  raw_response: Record<string, any>;
  is_synthetic_fixture?: boolean;
  recorded_at: string;
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

export interface EvidenceField {
  name: string;
  value: string;
}

export interface EvidenceResolution {
  reference_id: string;
  source_type: "PAYOUT_CASE" | "PAYOUT_OPERATION" | "STATUS_SNAPSHOT";
  display_label: string;
  fields: EvidenceField[];
  recorded_at?: string;
  is_synthetic_fixture: boolean;
  resolution_status: "RESOLVED" | "UNAVAILABLE";
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

export interface InvestigationTransportInfo {
  type: "native_ollama" | "openai_compatible" | "historical";
  label: string;
  badgeLabel: string;
}

/**
 * Distinguishes truthfully between native Ollama chat, OpenAI-compatible chat,
 * and historical records that lack a transport field.
 * Native transport is never inferred solely from model name.
 */
export function getInvestigationTransportInfo(inv?: PayoutInvestigation | null): InvestigationTransportInfo {
  if (!inv) {
    return {
      type: "historical",
      label: "Historical / Untracked Transport",
      badgeLabel: "Historical Transport",
    };
  }

  const transport = inv.raw_response?.transport;
  if (transport === "ollama_native_chat") {
    return {
      type: "native_ollama",
      label: "Native Ollama Chat",
      badgeLabel: "Ollama Native Chat",
    };
  }
  if (transport === "openai_compatible_chat") {
    return {
      type: "openai_compatible",
      label: "OpenAI-Compatible Chat",
      badgeLabel: "OpenAI Compatible",
    };
  }

  return {
    type: "historical",
    label: "Standard Provider (Historical / Untracked Transport)",
    badgeLabel: "Historical / Untracked",
  };
}

/**
 * Formats non-negative inference duration measured in milliseconds.
 * Returns null for historical records without duration metadata so no invented values appear.
 */
export function formatInferenceDuration(rawResponse?: Record<string, any> | null): string | null {
  if (!rawResponse) return null;
  const durationMs = rawResponse.inference_duration_ms;
  if (typeof durationMs !== "number" || isNaN(durationMs) || durationMs < 0) {
    return null;
  }
  if (durationMs < 1000) {
    return `${Math.round(durationMs)}ms`;
  }
  const seconds = (durationMs / 1000).toFixed(1);
  return `${seconds}s (${durationMs.toLocaleString()}ms)`;
}

/**
 * Maps internal case references to enterprise financial operations identifiers.
 * E.g., DEMO-W9821 -> CB-2026-09821
 */
export function formatCaseReference(reference?: string | null): string {
  if (!reference) return "CB-2026-00000";
  if (reference === "DEMO-W9821") return "CB-2026-09821";
  if (reference === "DEMO-M4421") return "CB-2026-04421";
  if (reference === "DEMO-V1082") return "CB-2026-01082";
  if (reference.startsWith("DEMO-")) {
    return reference.replace(/^DEMO-/, "CB-2026-");
  }
  return reference;
}

/**
 * Maps internal evidence references to enterprise audit identifiers.
 * E.g., SIMULATED-DEMO-BATCH-W9821 -> EVD-PAYPAL-BATCH-09821
 */
export function formatEvidenceReference(refId?: string | null): string {
  if (!refId) return "";
  if (refId === "SIMULATED-DEMO-BATCH-W9821") return "EVD-PAYPAL-BATCH-09821";
  if (refId === "SIMULATED-DEMO-ITEM-W9821") return "EVD-PAYPAL-ITEM-09821";
  let formatted = refId
    .replace(/^SIMULATED-DEMO-BATCH-/, "EVD-PAYPAL-BATCH-")
    .replace(/^SIMULATED-DEMO-ITEM-/, "EVD-PAYPAL-ITEM-")
    .replace(/^SIMULATED-DEMO-/, "EVD-")
    .replace(/DEMO-W9821/g, "09821")
    .replace(/DEMO-M4421/g, "04421")
    .replace(/DEMO-V1082/g, "01082")
    .replace(/W9821/g, "09821")
    .replace(/M4421/g, "04421")
    .replace(/V1082/g, "01082")
    .replace(/^DEMO-/, "CB-2026-");
  return formatted;
}

/**
 * Strips prototype and simulation phrasing from displayed narrative text in customer-facing UI.
 */
export function sanitizeNarrativeText(text?: string | null): string {
  if (!text) return "";
  return text
    .replace(/\[SIMULATED DEMO DATA\]/gi, "[Sample Case Telemetry]")
    .replace(/\[DEMO TEST FIXTURE\]/gi, "[Sample Case Record]")
    .replace(/SIMULATED-DEMO-BATCH-W9821/gi, "EVD-PAYPAL-BATCH-09821")
    .replace(/SIMULATED-DEMO-/gi, "EVD-")
    .replace(/DEMO-W9821/gi, "CB-2026-09821")
    .replace(/DEMO-M4421/gi, "CB-2026-04421")
    .replace(/DEMO-V1082/gi, "CB-2026-01082")
    .replace(/Simulated scenario:/gi, "Observed case facts:")
    .replace(/In this synthetic scenario,/gi, "In this case record,")
    .replace(/simulated UNCLAIMED status/gi, "recorded UNCLAIMED status")
    .replace(/simulated UNCLAIMED/gi, "recorded UNCLAIMED")
    .replace(/simulated as processed/gi, "recorded as processed")
    .replace(/is in simulated UNCLAIMED/gi, "is in recorded UNCLAIMED");
}

/**
 * Formats elapsed seconds counter for active in-progress pending states.
 */
export function formatElapsedTimer(seconds: number): string {
  if (seconds < 0) seconds = 0;
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (mins === 0) {
    return `${secs}s`;
  }
  return `${mins}m ${secs < 10 ? "0" : ""}${secs}s`;
}

export type RecoveryOutcome =
  | "ORIGINAL_BATCH_RECOVERED"
  | "REQUEST_NEWLY_ACCEPTED"
  | "UNRESOLVED_OUTCOME"
  | "MANUAL_RECONCILIATION_REQUIRED";

export interface RecoverPayoutOperationResponse {
  case: PayoutCase;
  operation: PayoutOperation;
  outcome: RecoveryOutcome;
  message: string;
}

export interface PayoutCasesMetrics {
  total_cases: number;
  pending_dual_approvals: number;
  completed_cases_count: number;
  total_completed_cents: number;
  attention_needed_count: number;
  reconciliation_required: number;
}

export async function fetchPayoutCasesMetrics(): Promise<PayoutCasesMetrics> {
  const res = await fetch("/api/v1/payout-cases/metrics", {
    method: "GET",
    credentials: "include",
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `HTTP error ${res.status}`);
  }
  return res.json();
}

export async function recoverPayoutOperation(
  operationId: string,
  caseId?: string
): Promise<RecoverPayoutOperationResponse> {
  const url = caseId
    ? `/api/v1/payout-cases/${encodeURIComponent(caseId)}/operations/${encodeURIComponent(operationId)}/recover`
    : `/api/v1/payout-operations/${encodeURIComponent(operationId)}/recover`;

  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    credentials: "include",
  });

  const data = await res.json();
  if (!res.ok) {
    const errorMsg = data.error || `HTTP error ${res.status}`;
    const err: any = new Error(errorMsg);
    err.status = res.status;
    err.code = data.code;
    err.outcome = data.outcome;
    err.operation = data.operation;
    err.case = data.case;
    throw err;
  }

  return data;
}

