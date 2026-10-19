# ClaimBridge — Product Differentiation & Problem Definition

**Central Proposition:**  
> **"ClaimBridge helps payout operators investigate unresolved payouts using verified PayPal status data, evidence-grounded AI explanations, and controlled recovery workflows."**

---

## 1. Target User & Concrete Problem

### Target Audience
ClaimBridge serves **payout operations officers, claims adjudicators, marketplace treasurers, and insurance settlement analysts** responsible for disbursing multi-party outbound payments using the PayPal Payouts API.

### The Real-World Pain Point: Unresolved & Unclaimed Payouts
In automated disbursement workflows (warranty reimbursements, insurance claims, freelance marketplaces, gig economy platforms), outbound payouts frequently do not immediately settle. They enter ambiguous or delayed states:
- `UNCLAIMED`: The recipient does not yet have an active PayPal account linked to their email, or missed the 30-day PayPal claim window.
- `ONHOLD` / `BLOCKED`: Compliance, risk, or country-specific recipient limitations prevent instant crediting.
- `RECONCILIATION_REQUIRED`: Transient network drops occur during batch submission before an HTTP 201 response is acknowledged.

### The Broken Legacy Workflow
Currently, payout operators struggle with a disjointed, error-prone manual triage process:
1. **Manual Dashboard Swiveling:** Operators swivel between external PayPal Developer dashboards, internal customer CRM tickets, bank account ledgers, and claimant emails.
2. **Missing Forensic Audit Trail:** There is no centralized record linking why a specific payout stalled, what provider status snapshots were recorded, or what communication occurred.
3. **Double-Payout Risk on Blind Retries:** When a disbursement status is unknown, panicked support agents frequently re-issue a second payout with a new reference, leading to permanent treasury loss when the original transaction eventually settles.
4. **Dangerous LLM "Autonomous" Bots:** Emerging AI tools that directly trigger payment APIs risk catastrophic fund depletion through prompt injection, hallucinated recipient IDs, or mathematical rounding errors.

---

## 2. What ClaimBridge Does Differently

Unlike generic payment dashboards or naive chatbot wrappers, ClaimBridge enforces a **fail-closed, evidence-grounded architecture**:

| Feature Area | Typical Payout Dashboard | Chatbot / AI Agent Wrappers | ClaimBridge Approach |
| :--- | :--- | :--- | :--- |
| **Investigation Authority** | Purely tabular; operator must manually decipher cryptic error codes. | AI directly calls tools or payment APIs without deterministic checks. | **AI Proposes, Policy Decides:** AI has zero money-moving authority. Explanations are strictly advisory. |
| **Evidence Grounding** | No AI investigation. | Model hallucinates transaction IDs, amounts, or false "successful" statuses. | **Grounding Engine:** Go backend rejects any AI finding that references an identifier not in the verified snapshot (HTTP 422). |
| **Action Safety** | Manual buttons without safety rails. | Model executes open-ended, unrestricted tool calls. | **Closed Whitelist:** Model recommendations restricted to 6 pre-approved types; validated against Rust policy rules before display. |
| **Idempotency** | Often relies on frontend button debouncing. | Variable idempotency; retry loops generate new keys. | **Pre-Persisted Durable Keys:** `sender_batch_id` locked in PostgreSQL *before* calling PayPal; duplicate submissions blocked. |
| **High-Value Controls** | Single-click execution. | Unbounded tool execution. | **Mandatory Dual Custody:** Payouts > $250.00 USD require approval by a distinct authorized officer; self-approval strictly blocked (HTTP 403). |
| **Currency Handling** | Floating-point numbers. | Floating-point numbers. | **Integer Minor Units:** Money computed and stored strictly in integer cents (e.g. $185.00 = 18500 cents). |

---

## 3. Why AI Contributes Meaningful Investigative Value

AI is **not** used to move money. Instead, ClaimBridge leverages local and server-side LLMs for what they do best: **forensic evidence synthesis, pattern correlation, and root-cause explanations**:
- Correlating cryptic PayPal REST status codes (`UNCLAIMED`, `RECEIVER_UNCONFIRMED`) with claim narrative history and timestamps.
- Explicitly identifying **missing information** (e.g., whether the recipient ever logged into PayPal) rather than inventing conclusions.
- Highlighting relevant operational caveats and recommending the safest next human-approved step.

---

## 4. Why PayPal Is Central to the Architecture

PayPal is the world's most trusted global payout infrastructure. ClaimBridge integrates natively with the **PayPal Payouts REST API (v1)**:
1. **OAuth2 Client Credentials:** Secure token negotiation with automatic 60-second safety refresh buffers.
2. **Durable Batch & Item Tracking:** Full synchronization with PayPal batch resources (`GET /v1/payments/payouts/{payout_batch_id}`).
3. **Controlled Recovery:** Native cancellation of eligible `UNCLAIMED` payouts (`POST /v1/payments/payouts-item/{payout_item_id}/cancel`) with immediate status reconciliation.

---

## 5. Implementation Status: What Is Genuinely Working vs Out of Scope

### Genuinely Implemented & Testable Today
- Full Next.js 14 Web Application with cybernetic UI, dark mode, and responsive layout.
- Go 1.22 API Gateway with Argon2id auth, multi-tenant organization scoping, 1MB request limits, and structured log sanitization.
- Deterministic Rust Safety Policy Engine (`services/policy`) with 17 passing unit tests.
- PayPal Payouts REST Client in sandbox mode with OAuth2 token caching, batch/item sync, and unclaimed cancellation.
- Replaceable server-side AI provider abstraction with local Ollama (`llama3.2`) support and mock fixtures.
- Strict evidence grounding engine rejecting fabricated IDs with HTTP 422.
- 32-scenario end-to-end automated acceptance verification suite (`go run ./cmd/acceptance`).
- Pre-demo readiness diagnostic tool (`go run ./cmd/readiness`) and test fixture seeder (`go run ./cmd/seed`).
- Multi-stage production Dockerfiles and full compose stack.

### Deliberately Out of Scope for Hackathon Submission
- **Live Production Payments:** Enforced sandbox-only; production hosts (`api-m.paypal.com`) are permanently blocked by code assertions.
- **Multi-Currency FX Settlement:** Payouts currently constrained strictly to `USD`.
- **Vector Databases & Autonomous Agent Loops:** Intentionally excluded to guarantee deterministic, zero-budget reproducibility.
- **Native Mobile Apps:** Web interface is fully responsive on mobile; native iOS/Android builds are deferred.
