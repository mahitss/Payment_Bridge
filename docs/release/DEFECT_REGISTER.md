# ClaimBridge — Defect Register & Remediation Log
**Document Version:** 2.0 (Mission 033.8.1A.4 Release Evidence Reconciliation)  
**Date of Audit & Remediation:** November 2, 2026  
**Auditor & Remediation Lead:** Senior Technical Documentation Auditor & Application Security Engineer  
**Repository:** `https://github.com/mahitss/Payment_Bridge` (Public, Branch: `main`)  
**Standard:** OWASP ASVS v4.0.3 L2 / Fail-Closed Deterministic Enforcement  

---

## 1. Remediation Executive Summary

During Missions 008, 009, 021–024, and 033.8.1A through 033.8.1A.4, the ClaimBridge codebase underwent comprehensive evidence-driven remediation and full-system integration verification targeting financial state machine integrity, duplicate disbursement prevention, server context timeout harmonization, database partial unique constraints, ambiguous provider outcome quarantine, and dedicated PostgreSQL test isolation.

All two critical P0 defects (`DEF-007`, `DEF-010`) and four high-priority P1 defects (`DEF-006`, `DEF-009`, `DEF-011`, `DEF-012`) have been remediated, verified fixed, and proven resolved via dedicated automated regression test suites, PostgreSQL concurrency integration tests, and the 32-scenario end-to-end acceptance suite. The external sandbox dependency (`DEF-001`) remains blocked on developer sandbox keys, demo video publication (`DEF-003`) is verified by automated CI artifact generation with public hosting link pending, and local Ollama inference (`DEF-002`) has been resolved and verified in local runtime on a single CPU development environment with native bounded `/api/chat` transport.

---

## 2. Defect Register

Allowed Final Status Taxonomy:
- `VERIFIED_FIXED`: Code change applied and formally proven resolved by executed automated regression test.
- `RESOLVED_VERIFIED_LOCAL_RUNTIME`: Code implemented, unit-tested, and verified in live local execution (with hardware scope documented).
- `BLOCKED`: Remediation or verification blocked on unavailable external credentials or third-party service.
- `CI_ARTIFACT_VERIFIED; PUBLIC_VIDEO_PENDING`: Automated CI artifact generation and quality gate verified; final public presentation upload pending.
- `ACCEPTED_NONCRITICAL`: Documented architectural tradeoff or zero-budget constraint.
- `UNRESOLVED`: Known pending task.

---

### DEF-001: Live PayPal Sandbox Verification Pending External Credentials
- **Severity:** P2
- **Initial Status:** `IMPLEMENTED_NOT_VERIFIED`
- **Observed Behavior:** Unit tests run against internal RFC-compliant mock server (`fakePayPalServer`). Live HTTP calls against `https://api-m.sandbox.paypal.com` cannot execute because `.env` contains placeholder credentials.
- **Affected Files:** `services/api/internal/paypal/client.go`, `services/api/cmd/paypal-smoke/main.go`
- **Expected Behavior:** `go run ./cmd/paypal-smoke` executes live OAuth authentication and payout polling against PayPal sandbox servers.
- **Root Cause:** Zero-budget local testing environment does not bundle real PayPal developer secrets in version control (enforcing security rule: no credentials in git).
- **Resolution:** Client code is fully implemented and passes mock unit test suites. Standalone diagnostic tool `cmd/paypal-smoke` is compiled and verified ready with exit code semantics and token redaction.
- **Regression Test Evidence:** `client_test.go` and Acceptance Scenario 13 pass.
- **Remaining Risk:** Low. Genuine sandbox handshake requires evaluator or developer to insert valid sandbox keys in `.env`.
- **Final Status:** `BLOCKED`

---

### DEF-002: Live Ollama Model Inference Execution & Bounded Options
- **Severity:** P2
- **Initial Status:** `IMPLEMENTED_NOT_VERIFIED`
- **Observed Behavior:** Automated acceptance tests and CI run against deterministic mock providers. Under earlier unconstrained completions, local Ollama execution could encounter inference timeout stalls on CPU hardware.
- **Affected Files:** `services/api/internal/ai/ollama.go`, `services/api/internal/config/config.go`
- **Expected Behavior:** Local Ollama daemon serves bounded model inference (e.g. `qwen3.5:4b` or `llama3.2`) with bounded context and token budgeting without hanging.
- **Root Cause:** Unbounded context size and default token lengths caused local CPU execution to exceed HTTP timeouts.
- **Resolution:** PR #4 merged native bounded `/api/chat` transport into `main` with `think: false`, `format: "json"`, `keep_alive: "10m"`, explicit context bounds (`AI_CONTEXT_SIZE=4096`), token budgeting (`AI_MAX_OUTPUT_TOKENS=300`), and configurable client timeout (`AI_REQUEST_TIMEOUT_SECONDS=120`). Verified on a local CPU development environment using Ollama with Qwen3.5 4B (`qwen3.5:4b`) on synthetic case `DEMO-W9821` (HTTP 201 Created, ~60.6s).
- **Regression Test Evidence:** Scenario 25–32 pass; grounding validation verified; local runtime verification on case `DEMO-W9821` with grounded evidence output in web UI.
- **Remaining Risk:** Low. Performance depends on host CPU specifications (not a performance guarantee for arbitrary CPU machines). Automated CI runs deliberately against mock test doubles.
- **Final Status:** `RESOLVED_VERIFIED_LOCAL_RUNTIME (SINGLE CPU MACHINE)`

---

### DEF-003: Hackathon Demo Video Recording File Pending Public URL
- **Severity:** P2
- **Initial Status:** `NOT_IMPLEMENTED`
- **Observed Behavior:** Repository contains complete demo script, shot list, and recording checklist. CI automatically generates and validates a 1080p WebM walkthrough artifact (`claimbridge-offline-demo-recording` / `claimbridge-demo-walkthrough.webm`, latest Main-Head Validation Run [40119075389](https://github.com/mahitss/Payment_Bridge/actions/runs/40119075389) Artifact ID `12569654956`, expiring November 7, 2026), but public video link is not yet posted to a video hosting platform.
- **Affected Files:** `docs/demo/DEMO_SCRIPT.md`, `docs/demo/SHOT_LIST.md`, `docs/demo/VIDEO_ARTIFACT_GUIDE.md`
- **Expected Behavior:** 2-minute video recording uploaded to video hosting service and linked in hackathon portal.
- **Root Cause:** Recording voiceover and public upload is an external manual presentation task pending final team video recording session.
- **Resolution:** Script, shot list, and automated CI recording artifact complete; public recording scheduled prior to final submission.
- **Regression Test Evidence:** Automated media-integrity check in CI (120–175s duration, 1920×1080 resolution, silent WebM).
- **Remaining Risk:** Submission incomplete until video link is provided in Devpost/hackathon portal.
- **Final Status:** `CI_ARTIFACT_VERIFIED; PUBLIC_VIDEO_PENDING`

---

### DEF-004: Currency Settlement Constrained to USD
- **Severity:** P3
- **Initial Status:** `ACCEPTED_NONCRITICAL`
- **Observed Behavior:** Rust policy engine and Go API enforce `currency == "USD"`; non-USD currencies are rejected fail-closed.
- **Affected Files:** `services/policy/src/engine.rs`, `services/api/internal/payouts/service.go`
- **Expected Behavior:** Multi-currency payout settlement.
- **Root Cause:** Intentional scope boundary for initial PayPal AI Hackathon prototype.
- **Resolution:** Documented in `KNOWN_ISSUES.md`. Rust policy engine rejects non-USD requests with `ErrUnsupportedCurrency`.
- **Regression Test Evidence:** `cargo test` (`test_unsupported_currency_is_denied`) PASS.
- **Remaining Risk:** None. Intentional safety invariant.
- **Final Status:** `ACCEPTED_NONCRITICAL`

---

### DEF-005: Local Ollama CPU Inference Latency Notice
- **Severity:** P3
- **Initial Status:** `ACCEPTED_NONCRITICAL`
- **Observed Behavior:** Local LLM inference on CPU without GPU acceleration takes approximately 60.6 seconds with `qwen3.5:4b` under a 4096 context window.
- **Affected Files:** `apps/web/src/app/app/payouts/[id]/page.tsx`, `services/api/internal/ai/ollama.go`
- **Expected Behavior:** Instantaneous inference responses.
- **Root Cause:** Zero-budget constraint avoids paid hosted APIs; open-weights local execution on consumer hardware.
- **Resolution:** Web frontend implements animated responsive status indicators ("Analyzing transaction evidence with Ollama...") and route timeout was increased to 120s. Core payout operations are completely decoupled from AI inference latency.
- **Regression Test Evidence:** Web UI inspection, empirical verification (~60.6s HTTP 201), and Scenario 29 pass.
- **Remaining Risk:** Low. Mitigated by asynchronous UI states and bounded timeout.
- **Final Status:** `ACCEPTED_NONCRITICAL`

---

### DEF-006: Unenforced Terminal & Non-Submittable Status Checks in `SubmitPayoutOperation`
- **Severity:** P1
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** When `RequiresDualApproval` was false, `SubmitPayoutOperation` only checked `op.Status == "SUBMITTING" || op.Status == "SUBMITTED" || op.Status == "SUCCEEDED"`. If `op.Status` was `CANCELLED`, `FAILED`, or `RECONCILIATION_REQUIRED`, or if parent `pCase.Status` was `CANCELLED` or `COMPLETED`, the operation was not rejected and could be dispatched to PayPal.
- **Affected Files:**
  - `services/api/internal/payouts/service.go`
  - `services/api/internal/payouts/handler.go`
  - `services/api/internal/payouts/service_test.go`
  - `services/api/internal/payouts/handler_test.go`
- **Expected Behavior:** Payout submission must strictly enforce permitted submittable states immediately before dispatch:
  - If `RequiresDualApproval == true`, status must strictly be `APPROVED`.
  - If `RequiresDualApproval == false`, status must strictly be `PREPARED`.
  - If parent `pCase.Status` is `CANCELLED` or `COMPLETED`, submission must be rejected fail-closed.
  - Submitting from any other state must return HTTP 409 Conflict.
- **Root Cause:** Incomplete state machine check in `SubmitPayoutOperation` which omitted validation of non-dual-approval operations and parent case terminal status.
- **Resolution:** Defined `ErrOperationCannotBeSubmitted` and `ErrCaseInTerminalState`. Added strict guard checks in `SubmitPayoutOperation` and mapped errors to HTTP 409 Conflict.
- **Regression Test Evidence:**
  - `TestPayoutService_RejectSubmitFromInvalidStates`: PASS
  - `TestPayoutHandler_RejectSubmitCancelledOperation`: PASS
- **Remaining Risk:** None. All state transitions strictly verified.
- **Final Status:** `VERIFIED_FIXED`

---

### DEF-007: Missing Active-Operation Guard on Payout Case Initiation & Submission
- **Severity:** P0
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** `InitiatePayoutOperation` did not inspect existing operations for a case. An operator or sequential requests could create multiple distinct operations on the same payout case with separate `sender_batch_id` values, leading to duplicate financial disbursements for a single claim.
- **Affected Files:**
  - `services/api/internal/payouts/service.go`
  - `services/api/internal/payouts/handler.go`
  - `services/api/internal/payouts/service_test.go`
  - `services/api/internal/payouts/handler_test.go`
- **Expected Behavior:** A payout case must have at most ONE active operation at any time. If an operation is already in progress (status not `CANCELLED` and not `FAILED`), initiating a new operation must be rejected with `ErrActiveOperationExists` (HTTP 409 Conflict). In `SubmitPayoutOperation`, verify that no other operation on the case was already submitted or succeeded.
- **Root Cause:** Missing active-operation query check in `InitiatePayoutOperation` and cross-operation check in `SubmitPayoutOperation`.
- **Resolution:** Defined `ErrActiveOperationExists`. Added active-operation guard to `InitiatePayoutOperation` and duplicate submission check in `SubmitPayoutOperation`. Mapped to HTTP 409 Conflict.
- **Regression Test Evidence:**
  - `TestPayoutService_PreventDuplicateActiveOperations`: PASS
  - `TestPayoutHandler_RejectDuplicateActiveOperation`: PASS
- **Remaining Risk:** None. Double-disbursement risk permanently eliminated in application layer.
- **Final Status:** `VERIFIED_FIXED`

---

### DEF-008: Global Server HTTP Context Timeout Truncates Bounded AI Inference Requests
- **Severity:** P2
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** `timeoutMiddleware` in `server.go` wrapped all HTTP requests with `s.cfg.PayPalRequestTimeout` (15 seconds). Meanwhile, `s.cfg.AIRequestTimeout` was configured to 30 seconds. Because `context.WithTimeout` on a context already deadline-constrained to 15s expires at 15s, any local AI inference taking between 15s and 30s was killed prematurely by the outer server transport wrapper.
- **Affected Files:**
  - `services/api/internal/server/server.go`
- **Expected Behavior:** Outer HTTP server middleware should apply route-aware deadlines: 30s+ for `/investigations` endpoints and 15s for PayPal/standard operations, allowing each subsystem deadline to govern its respective operations.
- **Root Cause:** Homogeneous global timeout middleware configured with PayPal request timeout.
- **Resolution:** Refactored `timeoutMiddleware` to apply route-aware timeouts (30s+ for AI investigations, 15s for standard operations).
- **Regression Test Evidence:**
  - All 32 acceptance scenarios pass (including Scenario 24 timeout test and Scenarios 25–32 AI tests).
  - Go server package tests pass (`server_test.go`).
- **Remaining Risk:** None.
- **Final Status:** `VERIFIED_FIXED`

---

### DEF-009: Ambiguous Provider Outcome Handling & Blind Retry Risk
- **Severity:** P1
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** Network timeouts, HTTP 5xx errors, connection resets, response body read failures, or malformed HTTP 200/201 responses (missing/empty batch ID or status) could lead to ambiguous provider outcomes where operation state was left unsettled or risked blind retries with new sender batch IDs.
- **Affected Files:**
  - `services/api/internal/payouts/service.go`
  - `services/api/internal/paypal/client.go`
  - `services/api/internal/paypal/client_test.go`
  - `services/api/internal/payouts/service_test.go`
- **Expected Behavior:** Ambiguous provider responses and malformed HTTP 200/201 bodies must safely transition the operation to `RECONCILIATION_REQUIRED`. The original `sender_batch_id` must be preserved; blind retries and generating replacement batch IDs are strictly prohibited.
- **Root Cause:** Absence of explicit classification for malformed successful responses (e.g. HTTP 200/201 with missing `payout_batch_id`) and insufficient quarantine guards for network disconnects and body read errors.
- **Resolution:** Hardened `SubmitPayoutOperation` and `paypal.Client` to classify 5xx, timeouts, connection resets, response body read failures, and missing batch IDs as ambiguous, transitioning to `RECONCILIATION_REQUIRED`. Prohibited blind retries; original sender ID retained.
- **Regression Test Evidence:**
  - `TestPayoutService_AmbiguousProviderOutcomes`: PASS
  - `TestPayPal_CreatePayout_MalformedResponses`: PASS
- **Remaining Risk:** None. Ambiguous outcomes quarantined safely without capital risk.
- **Final Status:** `VERIFIED_FIXED`

---

### DEF-010: Concurrent Multi-Active Operation Race & Database Constraint Gap
- **Severity:** P0
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** Application-level active-operation checks (DEF-007) prevented sequential multi-operation creation, but concurrent requests executing in parallel transactions could pass the check before either committed, allowing multiple active operations per payout case.
- **Affected Files:**
  - `services/api/internal/db/migrations/000008_active_operation_unique_constraint.up.sql`
  - `services/api/internal/db/migrations/000008_active_operation_unique_constraint.down.sql`
  - `services/api/internal/db/postgres.go`
  - `services/api/internal/payouts/service.go`
  - `services/api/internal/db/postgres_integration_test.go`
- **Expected Behavior:** Single active operation per payout case must be enforced unconditionally by the database engine via a partial unique index, rejecting concurrent inserts at the transaction commit level. Atomic submission claim (`ClaimPayoutOperationForSubmission`) must ensure exactly one winning worker dispatches.
- **Root Cause:** Absence of a database-level partial unique index on active payout operations.
- **Resolution:** Added Migration 000008 creating partial unique index `uq_payout_ops_case_active` on `payout_operations(payout_case_id) WHERE status NOT IN ('CANCELLED', 'FAILED')`. Added `ClaimPayoutOperationForSubmission` row-level locking atomic claim.
- **Regression Test Evidence:**
  - `TestPostgres_Mission033_8_1A_Concurrency_Integration` (10 concurrent goroutines against dedicated PostgreSQL test DB, exactly 1 winner, 9 conflicts) PASS.
- **Remaining Risk:** None. Enforced at the storage engine level.
- **Final Status:** `VERIFIED_FIXED`

---

### DEF-011: Recovery Lease Integration Test Multi-Active Operation Fixture Collision
- **Severity:** P1
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** In `TestPostgres_RecoveryLeaseAndFinalization_Integration`, a secondary operation `op2` in `RECONCILIATION_REQUIRED` was created for the same payout case after the first operation was already `SUBMITTED`. When Migration 000008 was introduced, this violated `uq_payout_ops_case_active`, causing CI failure.
- **Affected Files:**
  - `services/api/internal/db/postgres_integration_test.go`
- **Expected Behavior:** Recovery integration tests must maintain distinct payout case fixtures for distinct active operations, proving recovery semantics while respecting the single-active-operation database constraint.
- **Root Cause:** Test fixture reused the same payout case for two active operations, violating real production lifecycle constraints.
- **Resolution:** Created a distinct payout case `pCase2` for operation `op2` in `TestPostgres_RecoveryLeaseAndFinalization_Integration`, preserving all lease recovery assertions, late responses, manual reconciliation, and adding explicit assertion `op.PayoutCaseID != op2.PayoutCaseID`.
- **Regression Test Evidence:**
  - `TestPostgres_RecoveryLeaseAndFinalization_Integration` PASS.
- **Remaining Risk:** None. Test fixture aligns with database schema invariants.
- **Final Status:** `VERIFIED_FIXED`

---

### DEF-012: Implicit `DATABASE_URL` Fallback in Database Integration Tests
- **Severity:** P1
- **Initial Status:** `UNRESOLVED`
- **Observed Behavior:** Several PostgreSQL integration tests (`TestPostgres_Integration`, `TestPostgres_MigrationRollbackSafeWithV2Fingerprints_Integration`, `TestPostgres_CountAuditEvents`) fell back to `DATABASE_URL` when `TEST_DATABASE_URL` was unset, risking accidental schema mutations or data wiping on development/production databases.
- **Affected Files:**
  - `services/api/internal/db/postgres_integration_test.go`
  - `services/api/internal/seeder/seeder_postgres_test.go`
- **Expected Behavior:** All database integration tests must require `TEST_DATABASE_URL` exclusively. When unset, tests must skip cleanly in 0.00s with zero connection attempts. If configured but unreachable, tests must fail closed without leaking credentials in logs.
- **Root Cause:** Legacy tests contained fallback logic `url := os.Getenv("TEST_DATABASE_URL"); if url == "" { url = os.Getenv("DATABASE_URL") }`.
- **Resolution:** Removed all `DATABASE_URL` fallbacks across all 6 integration tests. Added explicit sanitized error reporting (zero credential leakage). Added sentinel regression test confirming invalid `DATABASE_URL` is ignored.
- **Regression Test Evidence:**
  - All 6 integration tests skip cleanly in 0.00s when `TEST_DATABASE_URL` is unset; all pass when set; invalid `DATABASE_URL` sentinel confirmed inert.
- **Remaining Risk:** None. Test isolation completely enforced.
- **Final Status:** `VERIFIED_FIXED`

---

## 3. Summary of Remediation Status

| Defect ID | Severity | Area | Final Status |
| :--- | :--- | :--- | :--- |
| **DEF-001** | P2 | PayPal Sandbox Verification | `BLOCKED` |
| **DEF-002** | P2 | AI Inference Service | `RESOLVED_VERIFIED_LOCAL_RUNTIME (SINGLE CPU MACHINE)` |
| **DEF-003** | P2 | Demo Video Recording | `CI_ARTIFACT_VERIFIED; PUBLIC_VIDEO_PENDING` |
| **DEF-004** | P3 | Multi-Currency Scope | `ACCEPTED_NONCRITICAL` |
| **DEF-005** | P3 | CPU Inference Latency | `ACCEPTED_NONCRITICAL` |
| **DEF-006** | P1 | Financial State Machine Integrity | `VERIFIED_FIXED` |
| **DEF-007** | P0 | Duplicate Disbursement Prevention (Service Layer) | `VERIFIED_FIXED` |
| **DEF-008** | P2 | Server Context Timeout Harmonization | `VERIFIED_FIXED` |
| **DEF-009** | P1 | Ambiguous Provider Outcome Quarantine | `VERIFIED_FIXED` |
| **DEF-010** | P0 | Concurrent Multi-Active Race & DB Constraint | `VERIFIED_FIXED` |
| **DEF-011** | P1 | Recovery Lease Integration Fixture Separation | `VERIFIED_FIXED` |
| **DEF-012** | P1 | PostgreSQL Integration Test Isolation | `VERIFIED_FIXED` |

- **Confirmed P0 Defects:** 0 remaining (2 remediated and verified fixed: DEF-007, DEF-010).
- **Confirmed P1 Defects:** 0 remaining (4 remediated and verified fixed: DEF-006, DEF-009, DEF-011, DEF-012).
- **Confirmed P2 Defects:** 0 active code defects (DEF-008 verified fixed; DEF-001 blocked on external credentials; DEF-002 resolved in local CPU runtime; DEF-003 CI artifact verified with public video upload pending).
- **All Core Acceptance Tests (32/32):** PASSING.
- **All PostgreSQL Dedicated Integration Tests (6/6):** PASSING.
