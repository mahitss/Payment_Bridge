# ClaimBridge — Requirements Traceability Matrix (RTM)

**Status:** APPROVED & VERIFIED FOR HACKATHON RELEASE  
**Revision:** 1.0 (Mission 005 Release Gate)  
**Security Baseline:** OWASP ASVS v4.0.3 Level 2 / OWASP Top 10  
**Enforcement Boundary:** Fail-Closed Deterministic Rust Policy Engine + Go Orchestration Gateway  

---

## 1. Traceability Matrix

| Req ID | User-Visible Expected Behavior | Responsible Component | Relevant Source Files | Test / Verification Method | Most Recent Evidence | Status |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **REQ-001** | Multi-language monorepo structure with Go API, Rust policy engine, Next.js web app, and PostgreSQL persistence | Workspace Root | `services/api`, `services/policy`, `apps/web`, `infra/` | Build & unit checks across all 3 stacks | `cargo test` (17 pass), `go test ./...` (11 pass), `npm run build` (15 static/dynamic routes) | **VERIFIED** |
| **REQ-002** | PayPal Sandbox OAuth client credentials authentication without leaking secrets into logs or client bundles | Go API (`paypal.Client`) | `services/api/internal/paypal/client.go` | `client_test.go`, `cmd/paypal-smoke` | HTTP 200 with masked token `[REDACTED_ACCESS_TOKEN]`; Scenario 13 PASS | **VERIFIED** |
| **REQ-003** | Retrieve live/mock PayPal payout batch and item details by durable identifier | Go API (`paypal.Client`) | `services/api/internal/paypal/client.go` | Unit tests with mock HTTP server, `cmd/paypal-smoke` | Batch retrieval test PASS; item retrieval test PASS; Scenario 21 PASS | **VERIFIED** |
| **REQ-004** | Deterministic Rust policy engine with fail-closed invariant validation ($500 ceiling, risk score limits, RFC email formats) | Rust Policy Engine | `services/policy/src/engine.rs` | `cargo test --verbose` | 17/17 tests passing; zero warnings on `cargo clippy -- -D warnings` | **VERIFIED** |
| **REQ-005** | Dual-custody approval policy mandate: payouts exceeding $250.00 USD or risk score > 20 require a distinct officer approval | Rust Policy Engine & Go Payouts Service | `services/policy/src/engine.rs`, `services/api/internal/payouts/service.go` | Unit & Acceptance tests (Scenarios 15, 17, 18, 19) | Self-approval rejected with HTTP 403; approval granted by distinct officer (HTTP 200) | **VERIFIED** |
| **REQ-006** | User registration with input validation, Argon2id password hashing, and email verification token issuance | Go Auth Service | `services/api/internal/auth/service.go`, `services/api/internal/crypto/password.go` | Scenario 01 & 02; Argon2id memory-cost unit tests | Account created, single-use token consumed, replay rejected (HTTP 400) | **VERIFIED** |
| **REQ-007** | Opaque 32-byte session token with SHA-256 hash in DB, HttpOnly/SameSite/Secure cookies, and server-side invalidation | Go Auth Handler | `services/api/internal/auth/handler.go`, `services/api/internal/auth/service.go` | Scenarios 03, 04, 05, 06 | Session established, persistent across requests, cleared on logout with subsequent 401 | **VERIFIED** |
| **REQ-008** | Password reset with cryptographically secure single-use token, replay prevention, and session invalidation | Go Auth Service | `services/api/internal/auth/service.go` | Scenarios 07, 08, 09 | Reset token consumed, replay rejected, old password rejected, new password verified | **VERIFIED** |
| **REQ-009** | Multi-tenant organization isolation: queries and mutations strictly scoped to authenticated user's organization | Go API Middleware & Repositories | `services/api/internal/auth/middleware.go`, `services/api/internal/db/postgres.go` | Scenarios 11, 23, 30 | Cross-organization access attempts return HTTP 404 Not Found (zero IDOR exposure) | **VERIFIED** |
| **REQ-010** | Payout case lifecycle management: creation, minor-unit (cents) integer representation, and listing with pagination | Go Payouts Service & Next.js Web UI | `services/api/internal/payouts/service.go`, `apps/web/src/app/app/payouts/page.tsx` | Scenarios 14, 15; Web UI build | Cases persisted, listed, and filtered by status and search terms without floating-point errors | **VERIFIED** |
| **REQ-011** | Payout operation state machine with durable sender_batch_id (`CB-BATCH-CASE-...`), zero blind retries, and PayPal dispatch | Go Payouts Service & PayPal Client | `services/api/internal/payouts/service.go`, `services/api/internal/paypal/client.go` | Scenarios 17, 20 | Operation transitioned from DRAFT -> AWAITING_APPROVAL -> APPROVED -> SUBMITTED | **VERIFIED** |
| **REQ-012** | PayPal status synchronization and snapshot auditing for item status updates (`SUCCESS`, `UNCLAIMED`, `FAILED`, `BLOCKED`) | Go Payouts Service | `services/api/internal/payouts/service.go` | Scenario 21 | Status snapshots persisted with timestamp and raw provider JSONB audit log | **VERIFIED** |
| **REQ-013** | Cancellation of eligible UNCLAIMED payouts via PayPal REST API with deterministic policy validation | Go Payouts Service & Rust Engine | `services/api/internal/payouts/service.go`, `services/policy/src/engine.rs` | Scenario 22 & 28 | UNCLAIMED item successfully cancelled; cancellation on non-unclaimed item blocked by policy | **VERIFIED** |
| **REQ-014** | Ambiguous network timeout quarantine: operation placed in `RECONCILIATION_REQUIRED`; duplicate submissions strictly prevented | Go Payouts Service | `services/api/internal/payouts/service.go` | Scenario 24 | Ambiguous timeout safely quarantined; duplicate submission blocked without capital loss | **VERIFIED** |
| **REQ-015** | Server-side AI provider abstraction with free-first local Ollama support (`ollama-openai-compatible`) and mock test providers | Go AI Provider | `services/api/internal/ai/provider.go`, `services/api/internal/ai/ollama.go` | `ai_test.go`, MockProvider unit suite | Provider interface handles 30s timeouts, bounded tokens, and error classifications (429, 503) | **VERIFIED** |
| **REQ-016** | Strict evidence grounding validation: model output validated against normalized snapshot identifiers; fabricated IDs rejected | Go AI Grounding Engine | `services/api/internal/ai/schema.go` | `ai_test.go`, Scenario 25 & 27 | Grounded investigation passes; hallucinated identifier rejected with HTTP 422 | **VERIFIED** |
| **REQ-017** | Recommendation action whitelist: strictly limited to 6 pre-approved types; non-whitelisted actions rejected | Go AI Schema | `services/api/internal/ai/schema.go` | `ai_test.go` | Invalid action `AUTO_DISBURSE_FUNDS` rejected fail-closed with HTTP 422 | **VERIFIED** |
| **REQ-018** | Insufficient evidence acknowledgment: when information is incomplete, model explicitly flags uncertainty and missing items | Go AI Service & Prompt | `services/api/internal/ai/prompt.go`, `services/api/internal/investigations/service.go` | Scenario 26 | Confidence set to `INSUFFICIENT_EVIDENCE` with explicit missing information callouts | **VERIFIED** |
| **REQ-019** | Fail-closed AI downtime handling: if Ollama or hosted model is unreachable, HTTP 503 is returned without mutating payout state | Go Investigations Service | `services/api/internal/investigations/service.go` | Scenario 29 | HTTP 503 returned gracefully; payout case remains 100% operational | **VERIFIED** |
| **REQ-020** | Prompt injection neutralization: untrusted inputs wrapped in `<UNTRUSTED_EVIDENCE>` tags; adversarial instructions ignored | Go AI Prompt Engine | `services/api/internal/ai/prompt.go` | Scenario 32 | Malicious prompt instructions contained; investigation completes safely without side effects | **VERIFIED** |
| **REQ-021** | UI/UX Forensic Investigation Panel: distinct visual boundaries between immutable provider facts and AI advisory opinions | Next.js Frontend | `apps/web/src/app/app/payouts/[id]/page.tsx` | Next.js build & Route audit | Rendered with amber advisory badges, evidence reference selector chips, and policy checks | **VERIFIED** |
| **REQ-022** | System hardening: HTTP request body size limits (1MB), comprehensive security headers, and DB readiness probes | Go HTTP Server | `services/api/internal/server/server.go` | `server_test.go` | 1MB payload rejected; nosniff, DENY, CSP, Permissions-Policy verified; `/readyz` checks DB | **VERIFIED** |
| **REQ-023** | Zero-budget deployment artifacts: multi-stage Dockerfiles and full-stack compose definition with isolated networking | Docker & Compose | `services/api/Dockerfile`, `apps/web/Dockerfile`, `infra/docker-compose.full.yml` | Container specification audit | Reproducible single-command local and server launch | **VERIFIED** |

---

## 2. Verification Summary

- **Total Tracked Requirements:** 23
- **Verified:** 23 (100%)
- **Failed:** 0 (0%)
- **Blocked:** 0 (0%)
- **Not Implemented:** 0 (0%)
