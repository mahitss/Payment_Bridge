# ClaimBridge — Hackathon Judging Criteria Evidence Map

**Evaluation Target:** PayPal AI Hackathon  
**Submission Version:** v1.0.0-hackathon  
**Repository:** [https://github.com/mahitss/Payment_Bridge.git](https://github.com/mahitss/Payment_Bridge.git)  

---

## 1. Technological Implementation

| Evaluation Dimension | Implemented Architecture & Source Files | Concrete Test & Verification Evidence |
| :--- | :--- | :--- |
| **Backend Orchestration (Go)** | `services/api/internal/server`, `services/api/internal/payouts`, `services/api/internal/auth` | 11/11 Go packages pass unit and integration tests with race detection (`go test -v ./...`). Enforces 1MB request limits and stream log redactions. |
| **Deterministic Policy Engine (Rust)** | `services/policy/src/engine.rs`, `services/policy/src/main.rs` | 17/17 deterministic tests pass (`cargo test`). Zero warnings on `cargo clippy -- -D warnings`. Mathematical invariant checks ($500 ceiling, dual custody). |
| **PayPal Payouts REST Integration** | `services/api/internal/paypal/client.go` | Sandbox OAuth2 token negotiation, batch creation (`POST /v1/payments/payouts`), status sync (`GET /v1/payments/payouts/{batch_id}`), item inspection, and cancellation of `UNCLAIMED` items. Verified 100% against mock HTTP contracts; live sandbox execution is gated by operator credentials and reported honestly as `BLOCKED` when unset. |
| **AI Investigation & Evidence Grounding** | `services/api/internal/ai/schema.go`, `services/api/internal/ai/prompt.go`, `services/api/internal/investigations/service.go` | Set-membership verification algorithm rejects hallucinated evidence IDs (Scenario 27 PASS with HTTP 422). Prompt injection neutralized (Scenario 32 PASS). |
| **Data Integrity & Multi-Tenancy** | `services/api/internal/db/postgres.go`, `services/api/internal/db/migrations/` | PostgreSQL 16 schema with 5 atomic SQL migrations (auth, claims, payouts, investigations, fixture provenance). All queries strictly scoped by `organization_id`. Money stored in integer minor cents (BIGINT). |
| **Acceptance Suite** | `services/api/cmd/acceptance/main.go` | 32/32 automated end-to-end scenarios pass in sequence in simulated deterministic test harness (`go run ./cmd/acceptance`). |
| **Browser End-to-End Suite** | `apps/web/playwright.config.ts`, `apps/web/e2e/` | 27/27 Playwright browser tests pass across 7 spec files (`health`, `auth`, `fixtures`, `policy-workflow`, `mutation-guards`, `responsive-nav`, `demo-recording`) in CI and locally, capturing a verified 1080p demo walkthrough. |

---

## 2. Design & User Experience

| Evaluation Dimension | Implemented UX & Source Files | Concrete Evidence |
| :--- | :--- | :--- |
| **Information Architecture** | `apps/web/src/app/` (Next.js 14 App Router) | Logical, authenticated workflow: Public Landing -> Auth -> Overview Dashboard -> Payouts Registry -> Case Investigation -> Settings. |
| **Visual Separation of AI vs Fact** | `apps/web/src/app/app/payouts/[id]/page.tsx` | UI uses distinct amber alert badges and notices indicating AI suggestions are advisory opinions, while PayPal status data is labeled as verified backend evidence. |
| **Evidence Reference Chips** | `apps/web/src/app/app/payouts/[id]/page.tsx` | Findings display interactive evidence reference pills linking to persistent case UUIDs, operation IDs, and PayPal batch numbers. |
| **Accessibility & Keyboard Navigation** | All components in `apps/web/src/components/` and `apps/web/src/app/` | Tested focus states, semantic HTML tags, accessible form labels, and zero horizontal overflow on mobile viewports. |
| **Production Build Cleanliness** | `apps/web/package.json`, `apps/web/next.config.mjs` | Standalone bundle builds with 0 ESLint warnings (`✔ No ESLint warnings or errors`) across 15 static and dynamic routes. |
| **Browser Regression Stability** | `apps/web/e2e/` | Playwright test suite verifies UI state transitions, disabled fixture mutation buttons, and dual-custody approval modals across real browser sessions. |

---

## 3. Potential Impact

| Evaluation Dimension | Real-World Problem & ClaimBridge Solution | Evidence & Constraints |
| :--- | :--- | :--- |
| **Unclaimed & Stalled Disbursements** | In automated multi-party disbursements, payouts frequently stall in ambiguous or uncollected states due to unconfirmed recipient emails, closed accounts, or compliance holds. | ClaimBridge provides an evidence-grounded investigation panel that explains why the transfer stalled and presents safe recovery options (e.g. requesting email confirmation or cancelling unclaimed funds). |
| **Elimination of Double Payouts** | Support agents frequently issue duplicate payouts during network timeouts. | Pre-persisted `sender_batch_id` locked in PostgreSQL before PayPal dispatch; timeouts quarantine operations in `RECONCILIATION_REQUIRED`, blocking duplicates. |
| **Zero-Budget Accessibility** | Small businesses cannot afford expensive hosted vector databases or paid GPU clusters. | Supports 100% local model inference via Ollama (`llama3.2`) with zero external API fees. |

---

## 4. Innovation & Product Differentiation

| Evaluation Dimension | Differentiating Innovation | Comparison with Existing Approaches |
| :--- | :--- | :--- |
| **Decoupled Financial Boundary** | "AI Proposes, Policy Decides" | Unlike chatbot wrappers that give LLMs direct execution tools, ClaimBridge treats AI output as untrusted advice. The deterministic Rust engine retains final authority. |
| **Strict Evidence Grounding** | Hard set-membership assertion | LLMs cannot invent transaction IDs or fake success states to justify actions; fabricated references are rejected fail-closed (HTTP 422). |
| **Separation-of-Duties Enforcement** | Built-in dual-custody authorization | Initiator self-approval is rejected fail-closed (HTTP 403), enforcing compliance for high-value claims. |
| **Synthetic Provenance & Mutation Guards** | Server-side fixture boundary | Evaluator demo fixtures are cryptographically and server-side isolated (`is_synthetic_fixture = true`); mutating operations return HTTP 403, preventing sandbox pollution. |

---

## 5. Presentation & Demonstration Mapping

| Demo Scene | Implemented Feature Highlight | Observed Result & Status |
| :--- | :--- | :--- |
| **Scene A: Problem Overview** | Landing page with value proposition | Clear framing of the unclaimed payout dilemma; Sandbox Prototype badge verified. |
| **Scene B: Authentication & Intake** | Argon2id auth & Payout intake form | Creation of case with real-time UI threshold preview and authoritative Go backend / Rust policy evaluation ($350 requires dual approval). |
| **Scene C: Dual-Custody Approval** | Separation of duties enforcement | Self-approval blocked with HTTP 403; approval granted by second officer account (`officer@claimbridge.internal`). |
| **Scene D: Provider Execution & Fixture Guards** | Track 1: Server-side blocked fixture mutations<br>Track 2: Optional live PayPal Sandbox submission | Track 1 (Recommended): External mutation buttons disabled on `DEMO-W9821`; API returns HTTP 403 with 0 outbound requests.<br>Track 2: Requires real credentials; returns `BLOCKED` when unset. |
| **Scene E: AI Forensic Investigation** | Grounded investigation panel | Explanation of `UNCLAIMED` item, evidence links, missing information callout; honest `AI_UNAVAILABLE` notice when Ollama offline. |
| **Scene F: Safe Recovery Boundaries & Closing** | Controlled recovery workflow & test summary | Track 1 (Walkthrough): Proves external mutation buttons remain safely disabled on synthetic fixture; cancellation eligibility is advisory; cancellation contract verified in acceptance suite (Scenario 22).<br>Track 2 (Optional Live): Native REST cancellation of genuine UNCLAIMED item when credentials configured. |
| **Demo Video Upload** | 2-minute video presentation | **VIDEO ARTIFACT GENERATED; PUBLIC VIDEO PENDING**: Deterministic 1080p walkthrough captured in CI (`claimbridge-offline-demo-recording` artifact, paced at 145–160s, measured at 152.84s – 153.6s, strictly verified by automated media quality gate via `ffprobe`); public hosting upload pending team presentation session. |
