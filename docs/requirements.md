# ClaimBridge — Product Requirements Document (PRD)

## 1. Executive Summary

**ClaimBridge** is an AI-augmented, security-hardened claims processing and instant payout orchestration platform. Built for the PayPal AI Hackathon, it bridges insurance/warranty/medical claim adjudication with real-time settlement using the PayPal Payouts API.

ClaimBridge decouples non-deterministic AI claim triage (fraud detection, document summarization, damage assessment) from **deterministic, fail-closed financial safety policy enforcement** implemented in Rust, backed by a robust orchestration API in Go.

---

## 2. Problem Statement

Automated payout systems frequently suffer from three critical vulnerabilities:
1. **Model Hallucination / Prompt Injection Risk**: When an LLM directly controls payout disbursement APIs, malicious claimants or stochastic model errors can trigger unauthorized capital transfers.
2. **Ambiguous API Retry Disasters**: Flaky networks lead automated retry loops to issue duplicate payouts with fresh idempotency keys or unverified state reconciliation.
3. **Auditability & Credential Bleed**: Accidental leak of client secrets or sensitive PII into browser bundles, prompt logs, or unredacted APM logs.

---

## 3. Product Principles

1. **AI Proposes, Policy Decides**: AI inference can score risk and summarize evidence, but it has ZERO execution authority. Only the deterministic policy engine can grant approval.
2. **Fail-Closed by Default**: If any parameter is missing, out-of-bounds, unverified, or ambiguous, disbursement is strictly blocked.
3. **Absolute Idempotency & Zero Blind Retries**: A single claim is bound to a single deterministic `sender_batch_id`. Network timeouts NEVER generate new IDs.
4. **Defense in Depth**: Secrets are strictly server-side; logs are redacted at the stream level; sandboxes are enforced with build/config-time assertions.
5. **Human-in-the-Loop Thresholds**: Dual-custody approval is mandatory for claims exceeding defined risk or monetary limits (e.g., > $250.00).

---

## 4. System Boundaries & Core Entities

### 4.1 Entities
- **Claim**: A request for reimbursement containing claim items, claimant identity, policy/warranty reference, and supporting documents.
- **Triage Result**: AI-generated assessment including damage classification, anomaly score, and suggested payout amount.
- **Policy Evaluation**: The deterministic verification output produced by the Rust policy engine.
- **Payout Record**: The PayPal Payout batch and item representation tracking external payout status (`PENDING`, `SUCCESS`, `UNCLAIMED`, `FAILED`, `BLOCKED`).
- **Audit Log**: Immutable append-only ledger of every policy evaluation, status change, and disbursement event.

---

## 5. Phase 1 vs Future Milestones

| Capability | Phase 1 (Foundation & Feasibility) | Phase 2 (Core Orchestration) | Phase 3 (Full Hackathon Demo) |
|---|---|---|---|
| **Architecture** | Monorepo, Go API, Rust Engine, Next.js scaffold | Full DB migrations, Redis queue | Polished reactive dashboard |
| **PayPal API** | Direct REST sandbox client, OAuth credentials, Batch details, Smoke test CLI | Payout creation with DB state lock, Webhook verification | Multi-item batched payouts, dispute handling |
| **Policy Engine** | Deterministic Rust engine with unit test suite & CLI/interface | Shared C-FFI / gRPC / IPC boundary with Go API | Dynamic rule configuration & compliance flags |
| **AI Inference** | Replaceable interface definition (mockable) | Server-side LLM claim triage service | Live multi-modal document extraction |
| **Security** | Redacted logs, sandbox guardrails, zero-blind-retries | JWT role-based access control, mTLS | Full audit trail export & anomaly alarms |
