# ADR-006: AI Investigation Engine, Evidence-Grounded Recommendations, and Human-Approved Workflows

## Status
Accepted

## Context
Mission 004 requires building an AI-powered payout investigation capability integrated into ClaimBridge.
Payout cases can enter complex or delayed states (e.g., `AWAITING_APPROVAL`, `SUBMITTED`, `UNCLAIMED`, `FAILED`, `RECONCILIATION_REQUIRED`). Operators require clear explanations grounded in verifiable evidence, identification of evidentiary gaps, and actionable recommendations for the safest next steps.

However, deploying AI into a financial workflow introduces severe security and operational risks:
1. **Hallucination & Fabrication**: Large Language Models may invent transaction identifiers, timestamps, or provider error codes.
2. **Prompt Injection**: Untrusted user-supplied data (such as recipient names, payment descriptions, notes, or PayPal provider error messages) could contain adversarial instructions attempting to alter agent behavior.
3. **Execution Hazards**: An AI model must never have authority to disburse funds, alter approval requirements, bypass tenant isolation, or override deterministic policy rules.
4. **Cost & Reliability Constraints**: ClaimBridge operates under a zero-budget, free-first mandate. The system cannot rely on paid-only hosted APIs and must support local inference (such as Ollama) with graceful fail-closed behavior when AI is offline.

## Decisions

1. **Replaceable Server-Side Provider Abstraction**:
   - AI inference is decoupled behind a Go interface `AIProvider`:
     ```go
     type AIProvider interface {
         Investigate(ctx context.Context, snapshot *InvestigationSnapshot) (*InvestigationResult, error)
         Name() string
         Model() string
         IsAvailable(ctx context.Context) bool
     }
     ```
   - Browser code never communicates with an LLM provider directly. All credentials and provider endpoints are maintained exclusively in server-side environment variables.

2. **Free-First Local Inference with Ollama Support**:
   - The primary supported provider is local Ollama via its OpenAI-compatible endpoint (`POST /v1/chat/completions`), defaulting to standard local port `11434`.
   - External hosted OpenAI-compatible inference endpoints are optionally configurable via `AI_BASE_URL` and `AI_API_KEY`.
   - If AI is disabled (`AI_ENABLED=false`) or the provider is unavailable/offline, the API responds with HTTP 503 and a clear status code (`AI_UNAVAILABLE`), leaving the underlying payout case and all financial workflows untouched.

3. **Deterministic Evidence Grounding & Reference Validation**:
   - Before model invocation, the Go backend compiles a normalized `InvestigationSnapshot` containing authoritative facts: case details, durable operations, recorded PayPal status snapshots, and deterministic policy findings.
   - The model is constrained to output structured JSON conforming to a strict schema.
   - Every factual finding in `findings` must cite explicit `evidence_references`.
   - **Backend Grounding Validator**: The Go service extracts all cited evidence identifiers and cross-checks them against the snapshot's valid identifier set (`case_id`, `operation_id`, `sender_batch_id`, `paypal_payout_batch_id`, `paypal_payout_item_id`, `snapshot_id`). If an unknown or fabricated identifier is found, the investigation is flagged as invalid (`INVALID_EVIDENCE_REFERENCE`).

4. **Closed Whitelist of Recommendation Types**:
   - Recommendations are restricted to a strictly implemented enumeration:
     - `REQUEST_RECIPIENT_INFORMATION`
     - `CHECK_PROVIDER_STATUS`
     - `REVIEW_RECIPIENT_DETAILS`
     - `REVIEW_CANCELLATION_ELIGIBILITY`
     - `ESCALATE_TO_OPERATOR`
     - `NO_ACTION_UNTIL_RECONCILED`
   - Unrecognized recommendation types trigger validation errors.

5. **Deterministic Policy Precedence**:
   - AI recommendations are strictly advisory. They cannot execute financial actions.
   - For every recommendation, the Go backend evaluates current case state and invokes the Rust deterministic policy engine to verify whether the action is permitted under current policy.
   - If the AI proposes an action that contradicts policy (e.g., recommending cancellation when an item is in terminal `SUCCESS` or not in `UNCLAIMED`), the recommendation is flagged as `disallowed_by_policy: true`, and the discrepancy is permanently audited.

6. **Prompt Injection Defenses & Data Minimization**:
   - Untrusted strings (recipient names, case descriptions, external provider error strings) are sanitized, length-limited, and wrapped in delimited data envelopes (`<UNTRUSTED_EVIDENCE>`).
   - The versioned system prompt explicitly instructs the model to treat enclosed records as passive data and ignore any embedded instructions.
   - Sensitive credentials (passwords, session tokens, PayPal client secrets, OAuth tokens) are never included in the snapshot.

7. **Multi-Tenant Auditing & Persistence**:
   - Investigations are stored in `payout_investigations`, strictly scoped by `organization_id` and foreign-keyed to `payout_cases(id)`.
   - Cross-tenant investigation queries are rejected with HTTP 404.

## Consequences
- **Positive**: Complete defense against hallucinated evidence, zero risk of autonomous financial operations, deterministic verification of AI advice, and 100% free local development.
- **Negative**: Local inference speed depends on host hardware; when local LLMs are not running, AI investigations return 503 while all core payout management continues to function normally.
