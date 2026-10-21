# ClaimBridge — Requirements Traceability Matrix (RTM)

**Status:** AUDITED & EVIDENCE-VERIFIED RELEASE BASELINE  
**Revision:** 2.5 (Mission 021 Audit Freeze & Mission 022 Evidence Sync)  
**Security Baseline:** OWASP ASVS v4.0.3 Level 2 / OWASP Top 10  
**CI Baseline:** Audited Source Baseline [Workflow Run 39600293507](https://github.com/mahitss/Payment_Bridge/actions/runs/39600293507) (Commit `c6a261a`) | Main-Head Validation [Workflow Run 39633059170](https://github.com/mahitss/Payment_Bridge/actions/runs/39633059170) (Commit `9b0b6c7`)  
**Enforcement Boundary:** Fail-Closed Deterministic Rust Policy Engine + Go Orchestration Gateway  

---

## 1. Traceability Matrix

The status values below strictly adhere to the project's zero-trust evaluation taxonomy:
- `VERIFIED`: Formally proven through executed automated tests or compiled source artifacts in this environment.
- `IMPLEMENTED_NOT_VERIFIED`: Code is complete, syntactically correct, and unit-tested against test doubles, but live external runtime verification is pending credentials or external service availability.
- `BLOCKED`: Verification cannot proceed due to missing external credentials or service prerequisites.
- `PENDING`: Planned final deliverable pending manual team presentation action.

| Req ID | User-Visible Expected Behavior | Responsible Component | Relevant Source Files | Test / Verification Method | Most Recent Evidence | Status |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **REQ-001** | Multi-language monorepo structure with Go API, Rust policy engine, Next.js web app, and PostgreSQL persistence | Workspace Root | `services/api`, `services/policy`, `apps/web`, `infra/` | Build & unit checks across all 3 stacks | `cargo test` (17 pass), `go test ./...` (11 pass), `npm run build` (15 static/dynamic routes), clean `npm ci` | **VERIFIED** |
| **REQ-002** | User registration with input validation, Argon2id password hashing, and email verification token issuance | Go Auth Service | `services/api/internal/auth/service.go`, `services/api/internal/crypto/password.go` | Scenario 01 & 02; Argon2id memory-cost unit tests | Account created, single-use token consumed, replay rejected (HTTP 400) | **VERIFIED** |
| **REQ-003** | Opaque 32-byte session token with SHA-256 hash in DB, HttpOnly/SameSite/Secure cookies, and server-side invalidation | Go Auth Handler | `services/api/internal/auth/handler.go`, `services/api/internal/auth/service.go` | Scenarios 03, 04, 05, 06; Playwright `auth.spec.ts` | Session established, persistent across requests, cleared on logout with subsequent 401 | **VERIFIED** |
| **REQ-004** | Password reset with cryptographically secure single-use token, replay prevention, and session invalidation | Go Auth Service | `services/api/internal/auth/service.go` | Scenarios 07, 08, 09 | Reset token consumed, replay rejected, old password rejected, new password verified | **VERIFIED** |
| **REQ-005** | Multi-tenant organization isolation: queries and mutations strictly scoped to authenticated user's organization | Go API Middleware & Repositories | `services/api/internal/auth/middleware.go`, `services/api/internal/db/postgres.go` | Scenarios 11, 23, 30 | Cross-organization access attempts return HTTP 404 Not Found (zero IDOR exposure) | **VERIFIED** |
| **REQ-006** | Deterministic Rust policy engine with fail-closed invariant validation ($500 ceiling, risk score limits, RFC email formats) | Rust Policy Engine | `services/policy/src/engine.rs` | `cargo test --verbose`, `cargo clippy -- -D warnings` | 17/17 tests passing; zero warnings on `cargo clippy -- -D warnings` | **VERIFIED** |
| **REQ-007** | Dual-custody approval policy mandate: payouts exceeding $250.00 USD or risk score > 20 require a distinct officer approval | Rust Policy Engine & Go Payouts Service | `services/policy/src/engine.rs`, `services/api/internal/payouts/service.go` | Unit & Acceptance tests (Scenarios 15, 17, 18, 19); Playwright `policy-workflow.spec.ts` | Self-approval rejected with HTTP 403; approval granted by distinct officer (HTTP 200) | **VERIFIED** |
| **REQ-008** | Payout case lifecycle management: creation, minor-unit (cents) integer representation, and listing with pagination | Go Payouts Service & Next.js Web UI | `services/api/internal/payouts/service.go`, `apps/web/src/app/app/payouts/page.tsx` | Scenarios 14, 15; Web UI build | Cases persisted, listed, and filtered by status and search terms without floating-point errors | **VERIFIED** |
| **REQ-009** | Payout operation state machine with durable sender_batch_id (`CB-BATCH-CASE-...`), zero blind retries, and PayPal dispatch | Go Payouts Service & PayPal Client | `services/api/internal/payouts/service.go`, `services/api/internal/paypal/client.go` | Scenarios 17, 20 | Operation transitioned from DRAFT -> AWAITING_APPROVAL -> APPROVED -> SUBMITTED | **VERIFIED** |
| **REQ-010** | PayPal REST API client: OAuth client credentials, token caching (60s buffer), batch creation, status polling, and strict Sandbox destination enforcement | Go API (`paypal.Client`) | `services/api/internal/paypal/client.go`, `services/api/internal/config/config.go`, `services/api/cmd/paypal-smoke/main.go` | `client_test.go` (100% pass), `cmd/paypal-smoke` unit suite (8/8 pass), Live CLI execution | Endpoint locked to `https://api-m.sandbox.paypal.com`; fails closed on HTTP, production hosts, or arbitrary destinations. Live CLI execution returned Exit Code 3 (`BLOCKED: PAYPAL_CLIENT_ID / PAYPAL_CLIENT_SECRET unset in .env`). Unit/mock contract 100% verified. | **BLOCKED** |
| **REQ-011** | PayPal status synchronization and snapshot auditing for item status updates (`SUCCESS`, `UNCLAIMED`, `FAILED`, `BLOCKED`) | Go Payouts Service | `services/api/internal/payouts/service.go` | Scenario 21 | Status snapshots persisted with timestamp and raw provider JSONB audit log | **VERIFIED** |
| **REQ-012** | Cancellation of eligible UNCLAIMED payouts via PayPal REST API with deterministic policy validation | Go Payouts Service & Rust Engine | `services/api/internal/payouts/service.go`, `services/policy/src/engine.rs` | Scenario 22 & 28 | Deterministic cancellation policy & state transition verified against mock test double in harness; synthetic fixtures protected by mutation guards; live PayPal Sandbox cancellation externally unverified pending real credentials & non-fixture transaction | **IMPLEMENTED_NOT_VERIFIED** |
| **REQ-013** | Ambiguous network timeout quarantine: operation placed in `RECONCILIATION_REQUIRED`; duplicate submissions strictly prevented | Go Payouts Service | `services/api/internal/payouts/service.go` | Scenario 24 | Ambiguous timeout safely quarantined; duplicate submission blocked without capital loss | **VERIFIED** |
| **REQ-014** | Server-side AI provider abstraction with free-first local Ollama support (`ollama-openai-compatible`) and mock test providers | Go AI Provider | `services/api/internal/ai/provider.go`, `services/api/internal/ai/ollama.go` | `ai_test.go`, MockProvider unit suite | Provider interface handles 30s timeouts, bounded tokens, and error classifications (429, 503) | **VERIFIED** |
| **REQ-015** | Live AI model inference using local Ollama (`llama3.2`) or OpenAI-compatible inference endpoint | Go AI Service & Readiness Runner | `services/api/internal/ai/ollama.go`, `cmd/readiness` | `ai_test.go` (100% pass), `TestOllamaClient_CheckHealth`, Live diagnostic probe | Live probe via `cmd/readiness` returned `BLOCKED: Daemon unreachable at http://127.0.0.1:11434`. Unit & sanitization tests 100% verified. | **BLOCKED** |
| **REQ-016** | Strict evidence grounding validation: model output validated against normalized snapshot identifiers; fabricated IDs rejected | Go AI Grounding Engine | `services/api/internal/ai/schema.go` | `ai_test.go`, Scenario 25 & 27 | Grounded investigation passes; hallucinated identifier rejected with HTTP 422 | **VERIFIED** |
| **REQ-017** | Recommendation action whitelist: strictly limited to 6 pre-approved types; non-whitelisted actions rejected | Go AI Schema | `services/api/internal/ai/schema.go` | `ai_test.go` | Invalid action `AUTO_DISBURSE_FUNDS` rejected fail-closed with HTTP 422 | **VERIFIED** |
| **REQ-018** | Insufficient evidence acknowledgment: when information is incomplete, model explicitly flags uncertainty and missing items | Go AI Service & Prompt | `services/api/internal/ai/prompt.go`, `services/api/internal/investigations/service.go` | Scenario 26 | Confidence set to `INSUFFICIENT_EVIDENCE` with explicit missing information callouts | **VERIFIED** |
| **REQ-019** | Fail-closed AI downtime handling: if Ollama or hosted model is unreachable, HTTP 503 is returned without mutating payout state | Go Investigations Service | `services/api/internal/investigations/service.go` | Scenario 29 | HTTP 503 returned gracefully; payout case remains 100% operational | **VERIFIED** |
| **REQ-020** | Prompt injection neutralization: untrusted inputs wrapped in `<UNTRUSTED_EVIDENCE>` tags; adversarial instructions ignored | Go AI Prompt Engine | `services/api/internal/ai/prompt.go` | Scenario 32 | Malicious prompt instructions contained; investigation completes safely without side effects | **VERIFIED** |
| **REQ-021** | UI/UX Forensic Investigation Panel: distinct visual boundaries between immutable provider facts and AI advisory opinions | Next.js Frontend | `apps/web/src/app/app/payouts/[id]/page.tsx` | Next.js build & Route audit; Playwright `fixtures.spec.ts` | Rendered with amber advisory badges, evidence reference selector chips, and policy checks | **VERIFIED** |
| **REQ-022** | System hardening: HTTP request body size limits (1MB), comprehensive security headers, and DB readiness probes | Go HTTP Server | `services/api/internal/server/server.go` | `server_test.go`, Playwright `health.spec.ts` | 1MB payload rejected; nosniff, DENY, CSP, Permissions-Policy verified; `/readyz` checks DB | **VERIFIED** |
| **REQ-023** | Zero-budget deployment artifacts: multi-stage Dockerfiles and full-stack compose definition with isolated networking | Docker & Compose | `services/api/Dockerfile`, `apps/web/Dockerfile`, `infra/docker-compose.full.yml` | Container specification audit | Reproducible single-command local and server launch | **VERIFIED** |
| **REQ-024** | Automated end-to-end acceptance runner verifying 32 full lifecycle scenarios in deterministic test harness | Test Engineering | `services/api/cmd/acceptance/main.go` | `go run ./cmd/acceptance` | 32/32 scenarios PASSED across auth, payouts, and AI safety | **VERIFIED** |
| **REQ-025** | Evaluation tooling: synthetic evaluator fixture seeder and multi-profile diagnostic tool | Operations & Release | `services/api/cmd/seed/main.go`, `services/api/cmd/readiness/main.go`, `internal/readiness/` | `seeder_test.go` (5 pass), `readiness_test.go` (29 pass), CLI execution | Seeder provides idempotent setup and safe reset; readiness runner validates discrete profile gates | **VERIFIED** |
| **REQ-026** | Hackathon demo artifacts: completed demo script, 2-minute shot list, and recording checklist | Technical Documentation | `docs/demo/DEMO_SCRIPT.md`, `docs/demo/SHOT_LIST.md`, `docs/demo/RECORDING_CHECKLIST.md` | Document review & visual cue mapping | Complete two-track walkthrough script with exact voiceover, screen actions, and truthful fallback instructions | **VERIFIED** |
| **REQ-027** | Hackathon final video recording upload | Team Presentation | N/A | Inspection of repository files and remote | No video file (`.mp4`, `.webm`) or external video link exists in the repository; pending team presentation action | **PENDING** |
| **REQ-028** | Synthetic fixture isolation & external mutation protection: server-side rejection of PayPal operations on fixtures | Go Payouts Service & Handler | `services/api/internal/payouts/service.go`, `services/api/internal/payouts/handler.go`, `services/api/internal/db/migrations/000005_fixture_provenance.up.sql` | `service_fixture_guard_test.go` (100% pass), Playwright `mutation-guards.spec.ts` | Submit, sync, and cancel rejected with HTTP 403 `FIXTURE_EXTERNAL_OPERATION_FORBIDDEN`; zero outbound requests to PayPal | **VERIFIED** |

---

## 2. Verification Summary

- **Total Tracked Requirements:** 28
- **Verified:** 24 (85.7%)
- **Implemented, Not Externally Verified:** 1 (3.6%) — REQ-012 (PayPal UNCLAIMED cancellation contract verified in harness; live Sandbox unverified)
- **Blocked (External Dependencies Unconfigured):** 2 (7.1%) — REQ-010 (PayPal credentials unset in audit env), REQ-015 (Local Ollama daemon offline)
- **Pending (Final Team Presentation Action):** 1 (3.6%) — REQ-027 (Video recording upload)
- **Failed:** 0 (0%)
- **Not Implemented:** 0 (0%)

---

## 3. Playwright Browser Test Suite Verification (27/27 Tests)

The complete end-to-end browser suite executed against the integrated Next.js frontend, Go API, and PostgreSQL database:

| Spec File | Test Cases Verified | Result |
| :--- | :--- | :--- |
| `health.spec.ts` | Prototype badge presence, missing release badge assertion, discrete API/PayPal status partitioning | **3/3 PASS** |
| `auth.spec.ts` | Protected route redirect to login, invalid password rejection, operator login, officer login, demo banner | **5/5 PASS** |
| `fixtures.spec.ts` | DEMO-W9821 registry presence, prominent simulated banner, offline investigation view, disabled action buttons | **4/4 PASS** |
| `policy-workflow.spec.ts` | $350 case creation, dual-custody policy detection, self-approval blockage, distinct officer approval, no auto-dispatch | **3/3 PASS** |
| `mutation-guards.spec.ts` | Server-side rejection of synthetic fixture submit (HTTP 403), sync (HTTP 403), cancel (HTTP 403) | **3/3 PASS** |
| `responsive-nav.spec.ts` | Desktop nav, mobile drawer, drawer auto-close, skip link, zero horizontal overflow, auth & payout form accessibility, 320px touch targets | **8/8 PASS** |
| `demo-recording.spec.ts` | Deterministic end-to-end walkthrough recording capturing 1080p verified walkthrough artifact | **1/1 PASS** |
| **Total** | Full browser workflow, security boundaries, and responsive accessibility verified in headless Chromium | **27/27 PASS** |
