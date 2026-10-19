# ClaimBridge — Hackathon Project Description

## 1. Short Project Description (Hackathon Listing)

**ClaimBridge** is an AI-augmented payout investigation and instant settlement platform built for the PayPal AI Hackathon. When multi-party disbursements fail or stall in `UNCLAIMED` status, payout operators struggle with cryptic error codes, disjointed dashboards, and dangerous double-payout retry loops. ClaimBridge solves this by decoupling non-deterministic AI claim investigations from a **deterministic, fail-closed Rust safety policy engine** and direct **PayPal Payouts REST API** settlement.

ClaimBridge leverages local and server-side LLMs (such as Ollama Llama 3.2) to synthesize evidence-grounded root cause explanations. Crucially, the Go backend enforces strict grounding verification: if an AI model references an unknown or hallucinated transaction ID, the response is rejected fail-closed (HTTP 422). Financial actions remain strictly gated by dual-custody authorization, durable idempotency keys (`sender_batch_id`), and PayPal sandbox status verification.

---

## 2. Technical Submission Overview

### Problem Statement
In automated claims, warranty reimbursements, and marketplace disbursements, payouts frequently stall in ambiguous states (such as unclaimed funds, closed recipient accounts, or network drops during batch submission). Traditional dashboards fail to explain root causes, while naive AI agents risk fund depletion through prompt injection and hallucinated disbursements.

### Solution & Core Workflow
1. **Intake & Evaluation:** Operator creates a payout case in integer minor units (cents). The Rust policy engine verifies invariants ($500 ceiling, risk limits) and flags whether dual-custody approval is mandatory.
2. **Dual-Custody Authorization:** High-value payouts (> $250.00 USD) require approval by a distinct authorized officer. Initiator self-approval is blocked fail-closed (HTTP 403).
3. **Durable Sandbox Execution:** A pre-persisted `sender_batch_id` is dispatched to the PayPal Payouts REST API. Network drops quarantine the transaction in `RECONCILIATION_REQUIRED`, blocking duplicate disbursements.
4. **Grounded AI Investigation:** When a payout stalls, the operator triggers an AI investigation. The model synthesizes facts and recommends next steps (e.g., verifying recipient email or cancelling unclaimed funds).
5. **Deterministic Boundary:** AI output is validated against the real snapshot identifiers. If any ID is fabricated, or if an action violates policy (e.g. attempting to cancel an already-completed payout), it is flagged and blocked.
6. **Controlled Recovery:** Operators cancel eligible `UNCLAIMED` items natively through PayPal REST endpoints.

### PayPal Technologies Used
- **PayPal Payouts REST API (v1):**
  - `POST /v1/payments/payouts` (Batch payout submission with durable client-generated idempotency keys).
  - `GET /v1/payments/payouts/{payout_batch_id}` (Batch status retrieval and item reconciliation).
  - `GET /v1/payments/payouts-item/{payout_item_id}` (Granular transaction item status inspection).
  - `POST /v1/payments/payouts-item/{payout_item_id}/cancel` (Native cancellation of unclaimed funds).
- **OAuth2 Client Credentials Grant:** Server-side token negotiation with in-memory caching and 60-second refresh buffer.
- **Strict Sandbox Enforcement:** Automated startup panic if production payment hosts are configured.

### AI Technologies & Models Used
- **Replaceable Server-Side AI Provider Abstraction:** Pure Go interface (`services/api/internal/ai/provider.go`).
- **Free-First Local Inference:** Native integration with local **Ollama** running **Llama 3.2** via OpenAI-compatible endpoints (`/v1/chat/completions`). Zero paid API keys required.
- **Prompt Injection Envelope:** Adversarial instructions neutralized using bounded fields and `<UNTRUSTED_EVIDENCE>` boundary tags.
- **Strict Evidence Grounding:** Go backend asserts that every cited evidence identifier exists in the current case snapshot.

### Architecture & Tech Stack
- **Web:** Next.js 14 App Router, TypeScript, Tailwind CSS, standalone Docker build.
- **Backend API:** Go 1.22+, Argon2id password hashing, SHA-256 session tokens, 1MB body limit, structured log redaction.
- **Policy Engine:** Rust 1.77+ deterministic state machine, Clippy audited, zero runtime panics.
- **Database:** PostgreSQL 16 with multi-tenant foreign keys, decimal integer cents, and atomic migrations.

### Open-Source License
ClaimBridge is licensed under the **MIT License**. Full source code is available at:  
[https://github.com/mahitss/Payment_Bridge.git](https://github.com/mahitss/Payment_Bridge.git)
