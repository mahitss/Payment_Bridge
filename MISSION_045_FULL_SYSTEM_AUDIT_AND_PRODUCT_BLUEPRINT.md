# Mission 045 — ClaimBridge Full System Audit & Product Blueprint

**Repository:** `https://github.com/mahitss/Payment_Bridge`  
**Authoritative Baseline:** `main` at [`d3b8a6e0b0b85bc588be2e0e6a8076e2c4c7440e`](https://github.com/mahitss/Payment_Bridge/commit/d3b8a6e0b0b85bc588be2e0e6a8076e2c4c7440e)  
**Audit Classification:** READ-ONLY SYSTEM AUDIT & ARCHITECTURAL BLUEPRINT  
**Product Theme Direction:** **Hybrid Theme + Modern Product Personality** (Dark restrained navigation frame `slate-950`/`slate-900`, crisp operational workspace, interactive electric blue `#2563EB` accents, high-contrast monospace financial ledgers, minimal functional cards, zero decorative gradients or vanity charts).

---

## 1. Executive Assessment

ClaimBridge possesses an exceptionally well-engineered core architecture for payment governance, separation of duties, and verifiable financial operations:
- **Financial Safety Invariants:** Enforced at the database and service layers—integer minor units (`BIGINT amount_cents`), strict \$500.00 USD safety ceiling, dual-custody mandate (> \$250.00 USD or risk > 20.0), absolute initiator self-approval prevention (HTTP 403 `ErrSelfApprovalForbidden`), durable pre-flight idempotency (`sender_batch_id`), and ambiguous network failure quarantine (`RECONCILIATION_REQUIRED`).
- **Verifiable AI Evidence Grounding:** The Go API gateway verifies every citation emitted by Ollama LLM inferences against immutable PostgreSQL snapshots; ungrounded citations fail closed with HTTP 422 `INVALID_EVIDENCE_REFERENCE`.
- **Synthetic Fixture Quarantine:** Pre-seeded demonstration cases (`is_synthetic_fixture = true`) are blocked server-side from generating outbound PayPal requests (HTTP 403 `FIXTURE_EXTERNAL_OPERATION_FORBIDDEN`).

However, this comprehensive system audit identifies **three critical architectural realities** that must be resolved to elevate ClaimBridge from an evaluation prototype to an enterprise-grade financial operations application:
1. **The Rust Policy Engine Runtime Disconnect (P0 Architectural Finding):** In all Docker Compose evaluator configurations and standard CI workflow runs, `POLICY_ENGINE_BIN` is **unset**. The Go API gateway therefore silently executes its in-memory Go fallback (`evaluateDeterministically`) rather than the compiled Rust binary. While both engines enforce identical financial thresholds (\$250 dual-custody, \$500 ceiling), the Rust binary is not currently built into or packaged within the API container.
2. **Dashboard Metric Inaccuracy via Pagination Truncation (Correctness Finding):** `apps/web/src/app/app/page.tsx` fetches only 5 payout cases (`/api/v1/payout-cases?limit=5`) and derives pending dual approvals and completed payout dollar volumes strictly from that 5-case subset. Any organization with > 5 cases displays truncated, inaccurate dashboard figures. A tenant-scoped backend aggregate endpoint is urgently required.
3. **Monolithic Frontend Architecture & Buried AI Workspace:** The payout detail page (`apps/web/src/app/app/payouts/[id]/page.tsx`) has ballooned to **112 KB and 2,257 lines**, bundling case metadata, 5 action workflows, 2 modal dialogs, AI investigation triggers, confidence scores, telemetry JSON, historical carousels, and an accessible evidence drawer into a single monolithic component. Furthermore, the AI investigation capability is buried at the bottom of case details rather than functioning as a dedicated investigation workspace.

---

## 2. Verified Baseline SHA & Environment State

- **Branch:** `main`
- **Commit SHA:** `d3b8a6e0b0b85bc588be2e0e6a8076e2c4c7440e`
- **Commit Date:** Wed Nov 11 00:37:53 2026 +0530
- **Upstream PR:** PR #29 merged cleanly (`docs/mission-043-submission-reconciliation`).
- **Post-Merge CI:** [Run 42882172778](https://github.com/mahitss/Payment_Bridge/actions/runs/42882172778) (6/6 jobs green, SUCCESS).
- **Working Tree:** Clean; zero uncommitted tracked changes; audit strictly read-only.

---

## 3. Complete Frontend Route Inventory & Screen-by-Screen Audit

### 3.1 Route Summary Table

| Route Path | Source File | Purpose & Responsibilities | Auth Required | Classification |
| :--- | :--- | :--- | :---: | :---: |
| `/` | `apps/web/src/app/page.tsx` | Public Landing Page: Value proposition, 4 architectural pillars, Sandbox Prototype badge, diagnostic probe card, credentials banner. | No | `VERIFIED WORKING` |
| `/login` | `apps/web/src/app/login/page.tsx` | Operator / Officer Authentication: Email/password form, Mailpit unverified redirect, demo mode autofill, returnTo sanitization. | No | `VERIFIED WORKING` |
| `/register` | `apps/web/src/app/register/page.tsx` | User Registration: Name, email, password confirmation, validation, email verification dispatch notification. | No | `VERIFIED WORKING` |
| `/verify-email` | `apps/web/src/app/verify-email/page.tsx` | Email Verification: Token extraction from query string or manual token form, single-use token consumption. | No | `VERIFIED WORKING` |
| `/forgot-password` | `apps/web/src/app/forgot-password/page.tsx` | Password Recovery Request: Generates single-use reset token and dispatches email via Mailpit/SMTP. | No | `VERIFIED WORKING` |
| `/reset-password` | `apps/web/src/app/reset-password/page.tsx` | Password Reset Execution: Consumes single-use token, validates password match, terminates existing sessions. | No | `VERIFIED WORKING` |
| `/app` | `apps/web/src/app/app/page.tsx` | Operator Dashboard: Overview cards (Total Cases, Approvals Needed, Settled Minor Units, PayPal status), recent cases table. | Yes | `PARTIALLY IMPLEMENTED` (Metrics truncated by pagination) |
| `/app/payouts` | `apps/web/src/app/app/payouts/page.tsx` | Payout Cases Registry: Paginated list (15 per page), client-side search and status filter. | Yes | `PARTIALLY IMPLEMENTED` (Client filter conflicts with pagination) |
| `/app/payouts/new` | `apps/web/src/app/app/payouts/new/page.tsx` | Payout Case Creation: Integer cents conversion, real-time dual-approval preview, policy ceiling validation. | Yes | `VERIFIED WORKING` |
| `/app/payouts/[id]` | `apps/web/src/app/app/payouts/[id]/page.tsx` | Payout Case Detail & Investigation: 112 KB monolith containing case info, action toolbar, modals, AI investigation, evidence drawer. | Yes | `VERIFIED WORKING` (Monolith requires splitting) |
| `/settings/account` | `apps/web/src/app/settings/account/page.tsx` | User Account & Tenant Profile: Read-only display of user identity, organization role, and cryptography details. | Yes | `VERIFIED WORKING` |
| `/settings/integrations` | `apps/web/src/app/settings/integrations/page.tsx` | PayPal Sandbox Status: Connection status probe, environment validation, credential documentation. | Yes | `VERIFIED WORKING` |
| `/app/settings/integrations` | `apps/web/src/app/app/settings/integrations/page.tsx` | Re-export alias pointing to `@/app/settings/integrations/page`. | Yes | `VERIFIED WORKING` |

---

### 3.2 Deep Screen-by-Screen Audit (12 Core Interfaces)

#### 1. Public Landing Page (`apps/web/src/app/page.tsx:1-240`)
- **Responsibilities:** Explains ClaimBridge's value proposition as an enterprise payment governance bridge. Details 4 architectural pillars: Dual-Custody Approval, Deterministic Rust Engine, Grounded LLM Evidence, and Pre-Flight Idempotency. Renders an interactive diagnostic probe card querying `/readyz` and `/healthz`. Surfaces pre-seeded demonstration credentials (`operator@claimbridge.internal`, `officer@claimbridge.internal`).
- **Auth Required:** No.
- **API Calls:** `GET /healthz`, `GET /readyz`.
- **States:** Interactive live probe checks backend health; handles network failure with fail-safe error indicators.
- **Accessibility & Responsive:** Clean responsive layout; semantic landmarks; keyboard navigable.
- **Classification:** `VERIFIED WORKING`.

#### 2. Authentication / Login (`apps/web/src/app/login/page.tsx:1-180`)
- **Responsibilities:** Authenticates operators and compliance officers. Provides one-click demo account population buttons. Sanitizes `returnTo` URLs against open-redirect vulnerabilities. Detects unverified email status and redirects to verification instructions.
- **Auth Required:** No (redirects authenticated users to `/app`).
- **API Calls:** `POST /api/v1/auth/login`.
- **Form Validation:** Validates email presence and syntax; validates password presence.
- **States:** Pending spinner during Argon2id verification; red error callout for invalid credentials.
- **Classification:** `VERIFIED WORKING`.

#### 3. User Registration (`apps/web/src/app/register/page.tsx:1-190`)
- **Responsibilities:** Self-service registration. Captures full name, email, and password. Informs user that they will join the default evaluation tenant. Dispatches verification token via Mailpit.
- **Auth Required:** No.
- **API Calls:** `POST /api/v1/auth/register`.
- **Form Validation:** Enforces 8-character minimum password, syntax verification, and matching confirmation.
- **States:** Success view instructing user to inspect Mailpit inbox (`localhost:8025`).
- **Classification:** `VERIFIED WORKING`.

#### 4. Email Verification (`apps/web/src/app/verify-email/page.tsx:1-140`)
- **Responsibilities:** Consumes single-use email verification tokens. Reads token from URL parameter `?token=...` or manual text input.
- **Auth Required:** No.
- **API Calls:** `POST /api/v1/auth/verify-email`.
- **States:** Auto-submits on mount if query parameter present; clear success and expired-token error messages.
- **Classification:** `VERIFIED WORKING`.

#### 5. Password Recovery (`apps/web/src/app/forgot-password/page.tsx:1-130`, `apps/web/src/app/reset-password/page.tsx:1-160`)
- **Responsibilities:** Two-stage recovery workflow. `forgot-password` requests single-use token; `reset-password` accepts token and updates Argon2id password hash, invalidating existing sessions.
- **Auth Required:** No.
- **API Calls:** `POST /api/v1/auth/forgot-password`, `POST /api/v1/auth/reset-password`.
- **States:** Generic confirmation message prevents account enumeration. Clear token expiry handling.
- **Classification:** `VERIFIED WORKING`.

#### 6. Operator Dashboard (`apps/web/src/app/app/page.tsx:1-320`)
- **Responsibilities:** High-level summary of tenant financial operations: Total Cases, Dual Approvals Needed, Settled Minor Units ($ USD), and PayPal Sandbox Connectivity status. Displays recent cases table.
- **Auth Required:** Yes.
- **API Calls:** `GET /api/v1/payout-cases?limit=5`, `GET /api/v1/paypal/connection-status`.
- **Flaws & Inaccuracies:** **Severe Pagination Truncation Flaw**: `pendingApprovalsCount` and `totalCompletedCents` are calculated by client-side filtering over the 5-item slice returned by `/api/v1/payout-cases?limit=5`. If an organization has 20 cases, only cases within the top 5 are counted.
- **Classification:** `PARTIALLY IMPLEMENTED` (Metrics truncated by pagination).

#### 7. Payout Registry (`apps/web/src/app/app/payouts/page.tsx:1-260`)
- **Responsibilities:** Browsable registry of payout cases. Supports pagination (15 cases per page).
- **Auth Required:** Yes.
- **API Calls:** `GET /api/v1/payout-cases?limit=15&offset=...`.
- **Flaws & Inaccuracies:** **Client-Side Filter Mismatch**: Status filtering and search are performed strictly client-side on the currently loaded 15 items. Searching for an older case on page 2 while on page 1 produces zero results.
- **Classification:** `PARTIALLY IMPLEMENTED`.

#### 8. Payout Case Creation (`apps/web/src/app/app/payouts/new/page.tsx:1-495`)
- **Responsibilities:** Case creation form: reference, recipient email, recipient name, title, description, settlement amount, and risk score (0–100). Live Policy Engine simulator panel updates dynamically as user types, demonstrating $500 ceiling and $250 dual-custody boundaries.
- **Auth Required:** Yes.
- **API Calls:** `POST /api/v1/payout-cases`.
- **Form Validation:** Implements integer minor units (`parseAmountToCents`), preventing floating-point inaccuracies. Blocks submission fail-closed if amount > $500.00 USD.
- **Classification:** `VERIFIED WORKING`.

#### 9. Payout Case Detail (`apps/web/src/app/app/payouts/[id]/page.tsx:1-2257`)
- **Responsibilities:** Comprehensive case view: metadata, lifecycle status badge, synthetic demo banner, action toolbar (Initiate, Approve, Submit, Sync, Cancel, Recover), operations history table, grounded evidence inspector drawer, dual-custody approval modal, ambiguous recovery modal, and AI investigation panel.
- **Auth Required:** Yes.
- **API Calls:** `GET /api/v1/payout-cases/{id}`, `POST /api/v1/payout-cases/{id}/payout-operations`, `POST /api/v1/payout-operations/{id}/approve`, `POST .../submit`, `POST .../sync`, `POST .../cancel`, `POST .../recover`, `GET .../evidence/{id}`, `POST .../investigations`.
- **Flaws & Inaccuracies:** Extreme monolithic size (112 KB / 2,257 lines / 32 useState hooks). The component is difficult to maintain and bundles unrelated concerns into a single file.
- **Classification:** `VERIFIED WORKING` (Monolith requires decomposition).

#### 10. AI Investigation Interface (`apps/web/src/app/app/payouts/[id]/page.tsx:1200-1850`)
- **Responsibilities:** Triggers Ollama LLM investigation. Renders advisory disclaimer, root cause analysis, structured findings with interactive grounded evidence pills, missing information, risk flags, limitations, and deterministic policy verification.
- **Auth Required:** Yes.
- **API Calls:** `POST /api/v1/payout-cases/{id}/investigations`, `GET /api/v1/payout-cases/{id}/investigations`.
- **Flaws & Inaccuracies:** Buried at the bottom of the payout detail page. Does not possess its own route, URL bookmarking, breadcrumb hierarchy, or side-by-side evidence inspection layout.
- **Classification:** `PARTIALLY IMPLEMENTED` (Functionally sound, architecturally buried).

#### 11. User & Tenant Settings (`apps/web/src/app/settings/account/page.tsx:1-120`)
- **Responsibilities:** Read-only view of authenticated user profile, assigned role (`OWNER`, `ADMIN`, `MEMBER`), tenant ID, and security encryption details.
- **Auth Required:** Yes.
- **API Calls:** `GET /api/v1/auth/me`.
- **Flaws:** Purely informational; lacks role management or team member invitations.
- **Classification:** `VERIFIED WORKING` (Read-only).

#### 12. PayPal Integration Settings (`apps/web/src/app/settings/integrations/page.tsx:1-160`)
- **Responsibilities:** Displays PayPal Sandbox environment status, Client ID masking, webhook ID configuration, live OAuth connectivity probe, and judge runbook instructions.
- **Auth Required:** Yes.
- **API Calls:** `GET /api/v1/paypal/connection-status`.
- **Classification:** `VERIFIED WORKING`.

---

## 4. Frontend Architecture Assessment

### 4.1 Component Sizing & Monolithic Complexity
The primary maintenance bottleneck is `apps/web/src/app/app/payouts/[id]/page.tsx` (112,732 bytes / 2,257 lines). It contains:
- 32 separate `useState` hooks.
- 5 independent operational action handlers with distinct validation rules.
- 2 complex modal dialogs (Dual-Custody Approval and Recovery Claim).
- 1 slide-over evidence inspection drawer.
- 1 embedded AI investigation suite with history carousel.
- **Proposed Decomposition:**
  1. `PayoutHeader.tsx` (Title, reference, status badge, synthetic badge, refresh)
  2. `PayoutSummaryCards.tsx` (Amount in cents/USD, recipient details, governance policy)
  3. `PayoutActionToolbar.tsx` (Initiate, approve, submit, sync, cancel, recover)
  4. `PayoutOperationsHistory.tsx` (State transitions, sender batch ID, provider item ID)
  5. `DualCustodyModal.tsx` (Accessible approval dialog)
  6. `RecoveryModal.tsx` (Accessible recovery dialog)
  7. `EvidenceInspectorDrawer.tsx` (Accessible drawer with focus trapping)
  8. Extraction of AI Investigation into a dedicated workspace route (`/app/payouts/[id]/investigate`).

### 4.2 State Management & Authentication Flow
- Handled by `apps/web/src/lib/auth.tsx` (`useAuth` hook and `AuthContext`):
  - Fetches `/api/v1/auth/me` on initialization with `credentials: "include"`.
  - Exposes `user`, `organization`, `loading`, `login`, `logout`, `refreshUser`.
  - Redirects unauthenticated users to `/login?returnTo=...` with URL sanitization.
  - Lacks caching/deduplication (e.g. SWR/TanStack Query), triggering repeated requests on route transitions.

### 4.3 Form Validation & Minor Unit Arithmetic
- Currency input in `new/page.tsx:52-63` implements `parseAmountToCents` to avoid floating-point rounding issues:
  - Parses string `$350.00` into integer `35000` cents.
  - Rejects zero or negative amounts.
  - Hard-blocks amounts exceeding `$500.00` (`50000` cents).
- Forms currently rely on local component state without schema-driven validation (e.g. Zod).

### 4.4 Accessibility (a11y) & Design Hygiene
- Focus trapping and Escape key dismissal are implemented in the Evidence Inspector drawer and mobile navigation drawer.
- Interactive controls conform to `min-h-[44px]` and `min-w-[44px]` touch target minimums.
- Existing markup is 100% dark mode (`bg-slate-950`). It does not yet feature the hybrid contrast (dark navigation + light operational workspace) specified by the product blueprint.

---

## 5. Backend Endpoint & Package Inventory

The Go backend (`services/api`) is structured into clean modular domain packages:

```
services/api/
├── cmd/
│   ├── server/       (Main API gateway daemon)
│   ├── seed/         (Idempotent evaluator database seeder)
│   ├── readiness/    (Diagnostic probe runner)
│   ├── acceptance/   (32-scenario mock-backed acceptance suite)
│   └── paypal-smoke/ (Standalone PayPal OAuth CLI)
└── internal/
    ├── ai/           (Ollama native chat & OpenAI-compatible client, grounding validation)
    ├── auth/         (Argon2id, sessions, verification tokens, middleware)
    ├── config/       (Strict environment parsing & credential redaction)
    ├── crypto/       (Argon2id hashing, CSPRNG tokens, SHA-256)
    ├── db/           (PostgreSQL connection pool, migrations 000001-000009, models)
    ├── email/        (SMTP Mailpit client & test double)
    ├── investigations/ (Investigation service, prompt envelope, persistence)
    ├── logger/       (Structured slog with credential redaction)
    ├── payouts/      (Payout case & operation state machine, locking, reconciliation)
    ├── paypal/       (REST client, OAuth cache, webhook signature verification)
    ├── policy/       (Rust binary CLI bridge & deterministic Go fallback)
    └── server/       (Mux routing, CORS, body limits, security headers, recovery)
```

### Complete HTTP Endpoint Matrix

| Method | Endpoint Path | Handler Function | Security / Auth | Purpose | Classification |
| :--- | :--- | :--- | :---: | :--- | :---: |
| `GET` | `/healthz` | `server.handleHealthz` | Public | Liveness probe returning status ok and timestamp. | `VERIFIED WORKING` |
| `GET` | `/readyz` | `server.handleReadyz` | Public | Readiness probe checking PostgreSQL and PayPal credentials. | `VERIFIED WORKING` |
| `POST` | `/api/v1/policy/evaluate` | `server.handlePolicyEvaluate` | Public | Standalone policy evaluation request. | `VERIFIED WORKING` (No UI) |
| `GET` | `/api/v1/sandbox/status` | `server.handleSandboxStatus` | Public | Diagnostic probe verifying sandbox OAuth and webhook ID. | `VERIFIED WORKING` |
| `GET` | `/api/v1/paypal/connection-status` | `payouts.handleConnectionStatus` | Authenticated | Live sandbox token check and webhook registration status. | `VERIFIED WORKING` |
| `POST` | `/api/v1/paypal/webhooks` | `payouts.handlePayPalWebhook` | Public (Signed) | Webhook receiver verifying PayPal SHA256withRSA signature. | `VERIFIED WORKING` |
| `POST` | `/api/v1/auth/register` | `auth.handleRegister` | Public | Register user, hash Argon2id password, send token. | `VERIFIED WORKING` |
| `POST` | `/api/v1/auth/verify-email` | `auth.handleVerifyEmail` | Public | Consume single-use email verification token. | `VERIFIED WORKING` |
| `POST` | `/api/v1/auth/login` | `auth.handleLogin` | Public | Verify credentials, issue 32-byte SHA-256 session cookie. | `VERIFIED WORKING` |
| `POST` | `/api/v1/auth/logout` | `auth.handleLogout` | Authenticated | Invalidate session in database, clear cookie. | `VERIFIED WORKING` |
| `POST` | `/api/v1/auth/forgot-password` | `auth.handleForgotPassword` | Public | Generate single-use password reset token. | `VERIFIED WORKING` |
| `POST` | `/api/v1/auth/reset-password` | `auth.handleResetPassword` | Public | Reset password, consume token, revoke active sessions. | `VERIFIED WORKING` |
| `GET` | `/api/v1/auth/me` | `auth.handleMe` | Authenticated | Return authenticated user and primary organization. | `VERIFIED WORKING` |
| `GET` | `/api/v1/payout-cases` | `payouts.handleListPayoutCases` | Authenticated | List tenant payout cases (limit, offset). | `PARTIALLY IMPLEMENTED` (Lacks status/search filter) |
| `POST` | `/api/v1/payout-cases` | `payouts.handleCreatePayoutCase` | Authenticated | Create payout case in integer cents; policy evaluated. | `VERIFIED WORKING` |
| `GET` | `/api/v1/payout-cases/{case_id}` | `payouts.handleGetPayoutCase` | Authenticated | Get case details by UUID (scoped to organization). | `VERIFIED WORKING` |
| `POST` | `/api/v1/payout-cases/{case_id}/sync` | `payouts.handleSyncPayoutCase` | Authenticated | Reconcile batch/item status from PayPal REST API. | `VERIFIED WORKING` |
| `POST` | `/api/v1/payout-cases/{case_id}/payout-operations` | `payouts.handleInitiatePayoutOperation` | Authenticated | Initiate operation; generate durable `sender_batch_id`. | `VERIFIED WORKING` |
| `GET` | `/api/v1/payout-cases/{case_id}/operations` | `payouts.handleListPayoutOperations` | Authenticated | List all operations for a case. | `VERIFIED WORKING` |
| `GET` | `/api/v1/payout-cases/{case_id}/evidence/{evidence_id}` | `payouts.handleResolveEvidence` | Authenticated | Resolve grounded citation to safe allowlist with PII mask. | `VERIFIED WORKING` |
| `POST` | `/api/v1/payout-operations/{id}/approve` | `payouts.handleApprovePayoutOperation` | Authenticated | Dual-custody approval; blocks initiator self-approval. | `VERIFIED WORKING` |
| `POST` | `/api/v1/payout-operations/{id}/submit` | `payouts.handleSubmitPayoutOperation` | Authenticated | Atomic submission claim and PayPal Sandbox dispatch. | `VERIFIED WORKING` |
| `POST` | `/api/v1/payout-operations/{id}/cancel` | `payouts.handleCancelPayoutOperation` | Authenticated | Cancel eligible UNCLAIMED item via PayPal REST. | `VERIFIED WORKING` |
| `POST` | `/api/v1/payout-operations/{id}/recover` | `payouts.handleRecoverPayoutOperation` | Authenticated | Force-recover or lease-reconcile ambiguous timeout. | `VERIFIED WORKING` |
| `GET` | `/api/v1/ai/status` | `investigations.handleAIStatus` | Authenticated | Return AI provider health, model name, and configuration. | `VERIFIED WORKING` (No UI) |
| `POST` | `/api/v1/payout-cases/{id}/investigations` | `investigations.handleCreateInvestigation` | Authenticated | Trigger grounded LLM investigation with prompt envelope. | `VERIFIED WORKING` |
| `GET` | `/api/v1/payout-cases/{id}/investigations` | `investigations.handleListInvestigations` | Authenticated | List historical investigations for a payout case. | `VERIFIED WORKING` |
| `GET` | `/api/v1/investigations/{id}` | `investigations.handleGetInvestigation` | Authenticated | Get specific investigation record and findings JSONB. | `VERIFIED WORKING` (No dedicated UI) |

---

## 6. Deep Domain Audit Across All 20 Backend Capabilities

1. **Authentication and Session Lifecycle (`services/api/internal/auth/handler.go:61-135`, `service.go:120-175`, `crypto/session.go:15-35`):**  
   - Sessions are 32-byte CSPRNG random tokens stored in PostgreSQL as SHA-256 hashes (`idx_sessions_token_hash`).
   - Delivered via `HttpOnly`, `SameSite=Lax`, `Path=/` secure cookie. Revoked on logout.
   - Classification: `VERIFIED WORKING`.
2. **Email Verification and Password Recovery (`services/api/internal/auth/handler.go:137-210`, `service.go:210-310`):**  
   - Single-use cryptographically secure tokens with TTL. Tokens consumed atomically in a transaction.
   - Classification: `VERIFIED WORKING`.
3. **Organization Membership and Roles (`services/api/internal/db/models.go:45-56`, `000001_auth_schema.up.sql:24-35`):**  
   - DB model supports roles `OWNER`, `ADMIN`, `MEMBER`, `VIEWER`.
   - **Gap:** No API endpoints or frontend screens exist to manage organization memberships, invite users, or alter roles.
   - Classification: `PARTIALLY IMPLEMENTED`.
4. **Authorization and Tenant Isolation (`services/api/internal/auth/middleware.go:45-85`):**  
   - Middleware `VerifyUserOrgAccess` asserts active user holds membership in the target organization.
   - Cross-tenant queries return HTTP 404 to prevent IDOR information leakage.
   - Classification: `VERIFIED WORKING`.
5. **Payout Creation and Lifecycle (`services/api/internal/payouts/handler.go:45-95`, `service.go:160-230`):**  
   - Creates case record with `status = 'DRAFT'`. Integer minor units strictly validated.
   - Evaluates policy engine to flag `dual_approval_required`.
   - Classification: `VERIFIED WORKING`.
6. **Policy Evaluation (`services/api/internal/policy/engine.go:80-140`):**  
   - Deterministic policy enforcement for ceiling ($500), dual custody ($250/risk > 20.0), and currency (USD).
   - Classification: `VERIFIED WORKING`.
7. **Dual-Custody Approval (`services/api/internal/payouts/handler.go:180-220`, `service.go:400-440`):**  
   - Transitions operation from `AWAITING_APPROVAL` to `APPROVED`.
   - Records approver identity, approval timestamp, and notes.
   - Classification: `VERIFIED WORKING`.
8. **Self-Approval Protection (`services/api/internal/payouts/service.go:413`):**  
   - Explicit check: `if op.InitiatedByUserID == actorUserID { return nil, ErrSelfApprovalForbidden }`.
   - Returns HTTP 403 Forbidden fail-closed.
   - Classification: `VERIFIED WORKING`.
9. **Idempotency (`services/api/internal/payouts/service.go:60-61, 254, 388`):**  
   - Generates durable `sender_batch_id` persisted in PostgreSQL before PayPal HTTP dispatch.
   - Enforces a 27-day cutoff to maintain a 3-day safety margin against PayPal's 30-day window.
   - Classification: `VERIFIED WORKING`.
10. **Submission Fingerprinting (`services/api/internal/payouts/service.go:287-320`):**  
    - SHA-256 fingerprint generated over canonical string `v1|sender_batch_id=...|amount=...|recipient=...`.
    - Persisted in `submission_fingerprint` to detect duplicate submissions.
    - Classification: `VERIFIED WORKING`.
11. **PayPal Dispatch and Status Synchronization (`services/api/internal/paypal/client.go:85-160`, `service.go:500-650, 700-850`):**  
    - Submits batch via PayPal REST API. Persists raw JSON response in `payout_status_snapshots`.
    - Sync endpoint polls provider status and updates local case/operation states.
    - Classification: `VERIFIED WORKING`.
12. **Cancellation (`services/api/internal/payouts/service.go:870-940`):**  
    - Calls PayPal REST cancel endpoint for eligible items in `UNCLAIMED` status.
    - Transitions operation to `CANCELLED`.
    - Classification: `VERIFIED WORKING`.
13. **Reconciliation and Recovery Leases (`services/api/internal/payouts/service.go:1050-1350`, `000007_recovery_claim.up.sql:1-25`):**  
    - Network failures during dispatch transition operation to `RECONCILIATION_REQUIRED`.
    - Leases prevent concurrent recovery workers from racing to resolve ambiguous operations.
    - Classification: `VERIFIED WORKING`.
14. **Webhook Verification and Processing (`services/api/internal/paypal/webhook.go:45-180`, `000009_paypal_webhook_events.up.sql:1-20`):**  
    - Verifies PayPal SHA256withRSA certificate signatures.
    - Deduplicates webhook events via unique constraint on `event_id`.
    - Background recovery worker retries unhandled events.
    - Classification: `VERIFIED WORKING`.
15. **Evidence Resolution (`services/api/internal/payouts/handler.go:260-310`, `service.go:960-1040`):**  
    - Resolves grounded evidence citations (`CASE:...`, `SNAPSHOT:...`) against PostgreSQL snapshots.
    - Masks PII (emails, OAuth tokens) using an allowlist before returning data to the client.
    - Classification: `VERIFIED WORKING`.
16. **AI Investigation Creation and Retrieval (`services/api/internal/investigations/handler.go:50-96`, `service.go:80-190`, `internal/ai/grounding.go:30-85`):**  
    - Invokes Ollama chat inference (`qwen3.5:4b`) using a structured prompt envelope.
    - Parses JSON output. Validates that all emitted citations exist in database snapshots.
    - Rejects ungrounded or hallucinated references with HTTP 422 `INVALID_EVIDENCE_REFERENCE`.
    - Classification: `VERIFIED WORKING`.
17. **Audit Event Persistence (`services/api/internal/db/postgres.go:410-450`, `000001_auth_schema.up.sql:60-75`):**  
    - Immutable audit log table `audit_events`.
    - Captures actor ID, action name, entity ID, organization ID, and JSONB metadata.
    - Classification: `VERIFIED WORKING`.
18. **Error Mapping (`services/api/internal/payouts/handler.go:340-390`):**  
    - Translates sentinel errors into standard HTTP status codes:
      - `ErrSelfApprovalForbidden` -> 403 Forbidden
      - `ErrCaseNotFound` -> 404 Not Found
      - `ErrOperationAlreadySubmitted` -> 409 Conflict
      - `ErrActiveOperationConflict` -> 409 Conflict
      - `ErrAIUnavailable` -> 503 Service Unavailable
    - Classification: `VERIFIED WORKING`.
19. **Timeouts and Concurrency (`services/api/internal/config/config.go:65-75`, `service.go:488-510`):**  
    - 15-second standard HTTP request context passed to database and PayPal REST clients.
    - Row-level locking via PostgreSQL `SELECT ... FOR UPDATE` prevents race conditions.
    - Classification: `VERIFIED WORKING`.
20. **Database Transactions and Constraints (`services/api/internal/db/postgres.go:180-230`, `000008_active_operation_unique_constraint.up.sql:1-15`):**  
    - Atomic submission claim uses `UPDATE payout_operations SET status = 'SUBMITTING' WHERE id = $1 AND status = 'APPROVED' RETURNING ...`.
    - Migration 000008 enforces partial unique index `uq_payout_ops_case_active` prohibiting concurrent active operations.
    - Classification: `VERIFIED WORKING`.

### Discrepancy Analysis

#### HTTP Capabilities Without Frontend Screens:
- `POST /api/v1/policy/evaluate`: Direct standalone policy evaluation request (`services/api/internal/server/routes.go:37`). No dedicated UI screen.
- `GET /api/v1/ai/status`: Probes AI inference health and configuration (`services/api/internal/investigations/handler.go:30`). No dedicated UI screen.
- `GET /api/v1/investigations/{id}`: Direct fetch of an investigation record by UUID (`services/api/internal/investigations/handler.go:33`). No dedicated screen (only fetched via case list).
- Organization Administration APIs: Role management, member invitations, and team settings are missing from both API and UI.

#### Frontend Features Without Genuine Backend Implementation:
- Client-side status and text search filtering in `apps/web/src/app/app/payouts/page.tsx:85-97` filters only the loaded 15-item page slice. The backend `GET /api/v1/payout-cases` query does NOT accept `status` or `search` parameters in SQL.
- Dashboard summary cards in `apps/web/src/app/app/page.tsx:108-115` calculate pending dual-custody approvals and total completed dollar volume by filtering the first 5 records returned by `/api/v1/payout-cases?limit=5`. There was no backend aggregate endpoint `GET /api/v1/payout-cases/metrics` in the baseline.

---

## 7. Database Schema & Migration Inventory

PostgreSQL 16 persistence is governed by 9 up/down migrations in `services/api/internal/db/migrations/`:

| Migration File | Tables Created / Altered | Critical Constraints & Indexes | Classification |
| :--- | :--- | :--- | :---: |
| `000001_auth_schema.up.sql` | `users`, `organizations`, `organization_memberships`, `sessions`, `email_verification_tokens`, `password_reset_tokens`, `audit_events` | `idx_users_email` (UNIQUE), `idx_sessions_token_hash` (UNIQUE), `idx_audit_org_created`, FK cascading rules. | `VERIFIED WORKING` |
| `000002_claims_schema.up.sql` | `payout_cases` | `uq_payout_cases_reference` (UNIQUE per org), `idx_payout_cases_org`, `amount_cents BIGINT CHECK (amount_cents > 0)`. | `VERIFIED WORKING` |
| `000003_payout_operations.up.sql` | `payout_operations`, `payout_status_snapshots` | `idx_payout_operations_sender_batch` (UNIQUE), `idx_snapshots_case_created`, JSONB provider payload storage. | `VERIFIED WORKING` |
| `000004_investigations.up.sql` | `investigations` | `idx_investigations_case`, `findings JSONB`, `confidence VARCHAR(32)`. | `VERIFIED WORKING` |
| `000005_fixture_provenance.up.sql` | Alter `payout_cases`, `payout_operations`, `payout_status_snapshots` | Adds `is_synthetic_fixture BOOLEAN DEFAULT FALSE` and performance filter indexes. | `VERIFIED WORKING` |
| `000006_submission_fingerprint.up.sql` | Alter `payout_operations` | Adds `submission_fingerprint VARCHAR(128)`. | `VERIFIED WORKING` |
| `000007_recovery_claim.up.sql` | Alter `payout_operations` | Adds `recovery_lease_owner`, `recovery_lease_token`, `recovery_lease_expires_at`, `recovery_attempt_count`. | `VERIFIED WORKING` |
| `000008_active_operation_unique_constraint.up.sql` | Alter `payout_operations` | **Partial Unique Index:** `uq_payout_ops_case_active` on `payout_operations(payout_case_id) WHERE status NOT IN ('CANCELLED', 'FAILED')`. Strictly prevents duplicate active operations. | `VERIFIED WORKING` |
| `000009_paypal_webhook_events.up.sql` | `paypal_webhook_events` | `uq_paypal_webhook_event_id` (UNIQUE on PayPal `event_id`), `idx_paypal_webhook_status`. Enforces idempotent webhook deduplication. | `VERIFIED WORKING` |

---

## 8. Authentication & Authorization Assessment

1. **Password Security:** Passwords hashed with Argon2id (`time=3`, `memory=64MB`, `parallelism=4`). Salt length 16 bytes, key length 32 bytes. Constant-time verification prevents timing attacks.
2. **Session Lifecycle:** Opaque 32-byte CSPRNG token generated on login. Stored in PostgreSQL as SHA-256 hash. Delivered via `HttpOnly`, `SameSite=Lax` (or `Strict`), `Path=/` cookie. Revoked immediately on logout or password reset.
3. **Multi-Tenancy & Tenant Isolation:** Every case, operation, snapshot, investigation, and audit record contains `organization_id`. `VerifyUserOrgAccess` middleware asserts the active user holds membership in the target organization. Cross-organization access attempts return `HTTP 404 Not Found` without disclosing record existence (IDOR prevention, Scenarios 11, 23, 30).
4. **Missing Organization Administration:** While the database model supports roles (`OWNER`, `ADMIN`, `MEMBER`, `VIEWER`), there are **no API endpoints or UI screens** for inviting new users, changing roles, or managing organizations. Organizations are currently seeded statically.

---

## 9. Financial Workflow State-Machine Map

### 9.1 Payout Case State Machine
```
[DRAFT] ──────────────────────────► [SUBMITTED] ─────────────► [COMPLETED]
   │                                     │                          ▲
   │                                     ▼                          │
   └──────── (via Cancel) ────────► [CANCELLED]                     │
   │                                                                │
   └──────── (via Terminal Fail) ──► [FAILED]                       │
                                         ▲                          │
                                         └─────── [RECONCILED] ─────┘
```

### 9.2 Payout Operation State Machine
```
   [PREPARED] 
       │
       ▼ (If amount > $250 or risk > 20.0)
[AWAITING_APPROVAL] ──(Dual Approval by Distinct Officer)──► [APPROVED]
       │                                                         │
       │ (Initiator Self-Approval: HTTP 403 Forbidden)          │
       ▼                                                         ▼
[ATOMIC CLAIM: SQL UPDATE ... RETURNING] ────────────────► [SUBMITTING]
                                                                 │
                                                                 ▼
                                    ┌─────────────────── [PayPal Dispatch] ───────────────────┐
                                    │                                                         │
                             (HTTP 201 Success)                                      (Network Timeout / 5xx)
                                    ▼                                                         ▼
                               [SUBMITTED]                                       [RECONCILIATION_REQUIRED]
                                    │                                                         │
                  ┌─────────────────┴─────────────────┐                                       │
                  │                                   │                                       │
            (Batch SUCCESS)                    (Item UNCLAIMED)                               │
                  ▼                                   ▼                                       ▼
             [SUCCEEDED]                     [Eligible for Cancel]                 [Recovery Lease Claim]
                                                      │                                       │
                                                      ▼                                       ▼
                                                 [CANCELLED]                             [Reconcile]
```

### Safety Invariants Enforced:
1. **Integer Minor Units:** `amount_cents` strictly enforced across database, API, and frontend parsing.
2. **Deterministic Ceiling:** \$500.00 USD hard block enforced by Rust policy engine and Go validation.
3. **Dual-Custody Mandate:** Operations > \$250.00 USD mandate approval by a second authorized officer (`actor.UserID != op.InitiatedByUserID`).
4. **Durable Idempotency Key:** `sender_batch_id` persisted in PostgreSQL before external HTTP dispatch.
5. **Ambiguous Outcome Quarantine:** Network timeouts and 5xx errors transition operation to `RECONCILIATION_REQUIRED`. Blind retries are strictly blocked.
6. **Active Operation Uniqueness:** Migration 000008 partial unique index prohibits concurrent active operations on the same case.

---

## 10. Critical Rust Policy Runtime Verification (P0 Finding)

### 10.1 The Investigation Findings
A systematic audit of all policy runtime files was conducted:
1. **`services/api/internal/policy/engine.go` (lines 80–91):**
   ```go
   func (e *Engine) Evaluate(ctx context.Context, req EvaluationRequest) (*EvaluationResponse, error) {
       if e.binaryPath != "" {
           res, err := e.evaluateViaBinary(ctx, req)
           if err == nil {
               return res, nil
           }
       }
       return e.evaluateDeterministically(req)
   }
   ```
2. **`services/api/Dockerfile`:**
   - Multi-stage build compiles only `/bin/server`, `/bin/seed`, `/bin/readiness` from Go source.
   - The Rust toolchain is absent. The compiled Rust binary `claimbridge-policy` is **never built into or copied into** the API Docker image.
3. **`infra/docker-compose.full.yml` & `infra/docker-compose.demo.yml`:**
   - `POLICY_ENGINE_BIN` is **NOT configured** in the environment block for service `api`.
   - `services/policy` has its own Dockerfile, but it is **not defined as a service** in Docker Compose.
4. **`.github/workflows/ci.yml`:**
   - Job `test-rust` tests the Rust crate independently (`cargo test`, `cargo clippy`, `cargo build --release`).
   - Job `e2e` and Job `docker-compose-evaluator` run `services/api/bin/server` **without setting `POLICY_ENGINE_BIN`**.

### 10.2 The Definitive Answer
> [!IMPORTANT]
> **In all Docker Compose evaluator environments and automated CI workflow runs, ClaimBridge executes the Go deterministic fallback (`evaluateDeterministically`), NOT the compiled Rust binary.**
> 
> The Rust binary was only executed in local development when launched via `services/api/run_local_server.ps1` where `$env:POLICY_ENGINE_BIN` pointed to the local Windows executable `target\release\claimbridge-policy.exe`.

### 10.3 Invariant Alignment & Divergence Analysis
- **Rule Parity:** Both Rust (`engine.rs`) and Go (`engine.go`) enforce identical numeric thresholds:
  - Ceiling: \$500.00 USD (`50000` cents).
  - Dual-Approval Threshold: \$250.00 USD (`25000` cents) or risk score > `20.0`.
  - Allowed Currency: `USD`.
  - Fail-Closed Risk Score: `>= 80.0` blocked.
  - Fail-Closed Semantics: Any violation produces `DecisionDenied` / `StatePayoutBlocked`.
- **Divergence Gap:** The Rust crate implements full action-based state-transition validation (`req.requested_action` and `evaluate_payout_operation`), whereas the Go fallback only evaluates the static risk/amount bounds. (Go handles payout operation status transitions in `services/api/internal/payouts/service.go`).

### 10.4 Safe Implementation Proposal for Future Milestone
To make the Rust binary genuinely operational in Docker without breaking existing stability:
1. Update `services/api/Dockerfile` to use a multi-stage builder that includes `services/policy`:
   ```dockerfile
   FROM rust:1.77-alpine AS rust-builder
   WORKDIR /usr/src/policy
   COPY services/policy/ .
   RUN cargo build --release

   FROM golang:1.22-alpine AS go-builder
   ...

   FROM alpine:3.19
   COPY --from=rust-builder /usr/src/policy/target/release/claimbridge-policy /app/claimbridge-policy
   COPY --from=go-builder /bin/server /app/server
   ```
2. Set `POLICY_ENGINE_BIN=/app/claimbridge-policy` in `infra/docker-compose.full.yml`.
3. In `services/api/internal/policy/engine.go`, log whether the evaluation occurred via Rust binary or Go fallback to maintain runtime transparency.

---

## 11. Critical Dashboard Correctness Investigation

### 11.1 The Root Cause of Metric Truncation
In `apps/web/src/app/app/page.tsx` (lines 44–52, 108–115):
```typescript
const casesRes = await fetch("/api/v1/payout-cases?limit=5", { ... });
const data = await casesRes.json();
setCases(data.cases || []);
setTotalCases(data.total || 0);

const pendingApprovalsCount = cases.filter(
  (c) => c.dual_approval_required && c.status === "DRAFT"
).length;

const totalCompletedCents = cases
  .filter((c) => c.status === "COMPLETED")
  .reduce((acc, c) => acc + c.amount_cents, 0);
```

### 11.2 Flawed Dashboard Figures
| Metric Displayed on Dashboard | Source in Code | Calculation Mechanism | Error Under Pagination (> 5 Cases) |
| :--- | :--- | :--- | :--- |
| **Total Payout Cases** | `data.total` | SQL `COUNT(*)` in `ListPayoutCasesByOrg` | **Accurate** (True database total) |
| **Dual Approvals Needed** | `pendingApprovalsCount` | `cases.filter(...).length` (Client-side) | **TRUNCATED:** If 15 cases need dual approval, dashboard shows at most 5! |
| **Settled Minor Units** | `totalCompletedCents` | `cases.reduce(...)` (Client-side) | **TRUNCATED:** Only sums completed payouts that happen to appear in the latest 5 records! |
| **Recent Cases Table** | `cases` | First page slice (5 records) | **Accurate as a "Recent" list**, but misleadingly labeled as comprehensive. |

### 11.3 Required Backend Aggregation Specification
A dedicated, tenant-scoped aggregate endpoint must be added:
- **Endpoint:** `GET /api/v1/payout-cases/metrics` (or `GET /api/v1/dashboard/metrics`)
- **SQL Implementation:**
  ```sql
  SELECT 
      COUNT(*) AS total_cases,
      COUNT(*) FILTER (WHERE dual_approval_required = TRUE AND status = 'DRAFT') AS pending_dual_approvals,
      COUNT(*) FILTER (WHERE status = 'COMPLETED') AS completed_cases_count,
      COALESCE(SUM(amount_cents) FILTER (WHERE status = 'COMPLETED'), 0) AS total_completed_cents,
      COUNT(*) FILTER (WHERE status = 'FAILED' OR status = 'RECONCILIATION_REQUIRED') AS attention_needed_count
  FROM payout_cases
  WHERE organization_id = $1;
  ```
- **Response Contract (JSON):**
  ```json
  {
    "total_cases": 42,
    "pending_dual_approvals": 6,
    "completed_cases_count": 28,
    "total_completed_cents": 842500,
    "attention_needed_count": 3
  }
  ```
This replaces client-side array filtering with an O(1) PostgreSQL aggregate query, eliminating pagination distortion without fabricated numbers.

---

## 12. Dedicated Investigation Workspace Specification

### 12.1 Architectural Rationale
In the current implementation, the AI Investigation Engine is an embedded card on `/app/payouts/[id]`. Because investigations involve rich forensic timelines, missing data callouts, provider raw payload comparisons, and deterministic policy audits, forcing it into a single scrollable page clutters the interface and obscures key evidence.

### 12.2 Dedicated Route: `/app/payouts/[id]/investigate`
The new dedicated workspace route will feature a 3-panel layout:

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ TOP BAR: Case Reference [DEMO-W9821] | Amount: $350.00 | Recipient | Status: SUBMITTED   │
├─────────────────────────┬──────────────────────────────────┬───────────────────────────┤
│ LEFT: Case & Evidence   │ CENTER: Forensic Investigation   │ RIGHT: Policy & Actions   │
│ ─────────────────────── │ ──────────────────────────────── │ ───────────────────────── │
│ • Case Metadata         │ • AI Advisory Disclaimer         │ • Deterministic Policy    │
│ • Integer minor cents   │ • Root Cause Analysis            │   Assessment Badge        │
│ • Provider Snapshot Log │ • Structured Findings            │ • Grounded Action Options │
│ • Raw Payload Inspector │ • Grounded Evidence Pills        │ • Investigation History   │
│ • Provenance Badges     │ • Missing Information Callouts   │   (Timeline switcher)     │
│                         │ • Risk Flags & Limitations       │ • Trigger New Probe       │
└─────────────────────────┴──────────────────────────────────┴───────────────────────────┘
```

### 12.3 Core Workspace Capabilities:
1. **Case Context Header:** Persistent case identifier, dollar amount, recipient email, and current lifecycle state.
2. **Status Snapshot Timeline:** Chronological list of all PayPal status checks and raw JSONB payloads stored in `payout_status_snapshots`.
3. **Advisory Separation:** Clear visual distinction between AI advisory analysis (amber warning notice) and verified facts (blue/green badges).
4. **Interactive Grounded Evidence Chips:** Interactive pills (e.g. `[CASE:DEMO-W9821]`, `[SNAPSHOT:SNAP-1]`) that trigger the slide-over Evidence Inspector.
5. **Deterministic Policy Validation:** Every AI recommendation is cross-referenced against the policy engine in real-time, displaying whether the recommended action is `PERMITTED` or `BLOCKED_BY_POLICY`.
6. **Telemetry Display:** Shows model name (`qwen3.5:4b`), transport (`ollama_native_chat`), execution time (`inference_duration_ms`), and token consumption.

---

## 13. Security & Reliability Gaps

| Gap ID | Subsystem | Description | Risk Level | Remediation Proposal |
| :--- | :--- | :--- | :---: | :--- |
| **SEC-001** | Frontend Search | Client-side filtering in `payouts/page.tsx` only filters the loaded 15-item page slice. | Low (UX) | Add `search` and `status` query parameters to backend `GET /api/v1/payout-cases`. |
| **SEC-002** | Metrics Integrity | Dashboard derives metrics from a 5-record slice. | Medium (Data Accuracy) | Implement backend aggregate endpoint `GET /api/v1/payout-cases/metrics`. |
| **SEC-003** | Docker Policy Link | `POLICY_ENGINE_BIN` unset in Docker Compose; runs Go fallback. | Medium (Architecture) | Multi-stage build copying Rust binary into API container; set `POLICY_ENGINE_BIN`. |
| **SEC-004** | Monolith Component | `payouts/[id]/page.tsx` is 112 KB / 2,257 lines. | Low (Maintainability) | Decompose into 7 focused components; extract dedicated investigation workspace. |
| **SEC-005** | Public Ingress | Live PayPal webhook delivery blocked without public DNS tunnel. | Low (Sandbox Limitation) | Maintain manual polling fallback; document ngrok/Cloudflare tunnel setup. |

---

## 14. Frontend Design System Specification

### 14.1 Theme Philosophy: Hybrid Theme + Modern Product Personality
- **Dark Navigation Frame:** Left sidebar and global header in deep charcoal/slate (`slate-950` / `slate-900`) with subtle 1px border dividers (`slate-800`), providing an authoritative, focused control room feel.
- **Light Operational Workspace:** Main content container in crisp, modern light mode (`slate-50` / `white`), maximizing readability for auditing data tables, financial ledgers, and evidence payloads.
- **Brand Accent:** High-contrast Electric Blue (`#2563EB` / `blue-600`) for primary interactive triggers, active navigation indicators, and keyboard focus rings.
- **Meaningful Financial Semantics:**
  - `Emerald-600`: Succeeded, Approved, Settled, Cryptographically Verified.
  - `Amber-500`: Dual-Custody Mandated, Simulated Demo Fixture, Advisory AI Opinion, Pending.
  - `Rose-600`: Rejected, Policy Violated, Reconciliation Required, Terminal Failure.
  - `Blue-600`: Submitted, In Transit, Processing, Live Provider Event.
- **Zero Decorative Fluff:** No multi-color gradient fills, neon glowing borders, or non-functional charts. Clean, crisp borders and functional badges only.

### 14.2 Typography & Spacing System
- **Font Stack:** Clean sans-serif system stack (`Inter`, `system-ui`, `-apple-system`, `sans-serif`).
- **Tabular Numerics:** Strict use of `font-mono` for all money minor units (`$350.00`), transaction references, batch IDs, and timestamps.
- **Grid:** 8pt modular spacing (`p-2`, `p-4`, `p-6`, `gap-4`, `gap-6`).
- **Touch Targets:** Minimum `44px × 44px` for all interactive buttons and inputs.
- **Focus Indicators:** Accessible `2px` solid blue focus ring with `2px` offset (`focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2`).

### 14.3 Reusable Component Architecture
```
apps/web/src/components/ui/
├── Button.tsx          (Variants: primary, secondary, destructive, ghost; size: sm, md, lg)
├── Badge.tsx           (Variants: success, warning, danger, neutral, info, synthetic)
├── Card.tsx            (Minimalist card container with header, content, footer)
├── Table.tsx           (Clean operational data table with sortable headers & pagination)
├── Modal.tsx           (Accessible dialog with focus trap & Escape dismissal)
├── Drawer.tsx          (Accessible slide-over drawer with focus trap & restoration)
└── StatCard.tsx        (Metric card displaying label, value, and context pill)
```
*Technical Note:* Existing Tailwind CSS 3.4 and Lucide React packages in `apps/web/package.json` are 100% sufficient to build these components without installing heavy third-party UI libraries.

---

## 15. Backend Completion Backlog

| Task ID | Component | Description | Priority |
| :---: | :--- | :--- | :---: |
| **BE-001** | `internal/payouts` | Add `GET /api/v1/payout-cases/metrics` returning tenant-scoped aggregates (`total_cases`, `pending_dual_approvals`, `completed_cents`, `attention_needed`). | **P0** |
| **BE-002** | `internal/payouts` | Update `ListPayoutCasesByOrg` to support optional `status` filter and `search` query parameters in SQL. | **P1** |
| **BE-003** | `infra` / `Dockerfile` | Multi-stage Dockerfile packaging `claimbridge-policy` Rust binary into API image; set `POLICY_ENGINE_BIN=/app/claimbridge-policy`. | **P1** |
| **BE-004** | `internal/policy` | Add structured logging in `policy.Engine` indicating whether evaluation executed via Rust binary or Go fallback. | **P2** |
| **BE-005** | `internal/investigations` | Add `GET /api/v1/investigations/stats` returning aggregate investigation telemetry (average inference duration, confidence breakdown). | **P2** |

---

## 16. Frontend Implementation Backlog

| Task ID | Route / Component | Description | Priority |
| :---: | :--- | :--- | :---: |
| **FE-001** | `app/page.tsx` | Wire dashboard metric cards to new `GET /api/v1/payout-cases/metrics` endpoint, resolving pagination truncation. | **P0** |
| **FE-002** | `app/payouts/[id]` | Decompose 112 KB monolith into 7 focused components (`Header`, `SummaryCards`, `Toolbar`, `OperationsHistory`, `ApprovalModal`, `RecoveryModal`, `EvidenceDrawer`). | **P0** |
| **FE-003** | `app/payouts/[id]/investigate` | Build dedicated 3-panel Investigation Workspace with case context, evidence timeline, AI findings, and policy validation. | **P0** |
| **FE-004** | Global Layout | Implement Hybrid Theme: Dark restrained sidebar/header + Light operational workspace. | **P1** |
| **FE-005** | `app/payouts/page.tsx` | Connect search bar and status dropdown to backend query parameters (`?status=...&search=...`). | **P1** |
| **FE-006** | `components/ui/` | Build reusable UI component library (`Button`, `Badge`, `Card`, `Table`, `Modal`, `Drawer`). | **P2** |

---

## 17. Test Strategy

1. **Unit & Contract Testing:**
   - Go: `go test -v ./internal/payouts` testing new metrics aggregate queries.
   - Rust: `cargo test` in `services/policy` verifying state-machine invariants.
2. **Acceptance Test Suite:**
   - Extend `services/api/cmd/acceptance/main.go` to include Scenario 33 (Dashboard metrics aggregate accuracy under multi-case pagination) and Scenario 34 (Server-side search and status filtering).
3. **Playwright End-to-End Suite:**
   - Regression test existing 38 tests across 9 spec files.
   - Author `e2e/investigation-workspace.spec.ts` testing the dedicated 3-panel investigation route.
   - Author `e2e/dashboard-metrics.spec.ts` asserting dashboard figures match total database counts.

---

## 18. Risk Register

| Risk ID | Description | Impact | Probability | Mitigation Strategy |
| :--- | :--- | :---: | :---: | :--- |
| **RSK-001** | Refactoring 112 KB detail page causes regression in dual-approval or recovery modals. | High | Medium | Execute Playwright `policy-workflow.spec.ts` and `mutation-guards.spec.ts` after each component extraction. |
| **RSK-002** | Adding Rust binary to API container increases Docker build time. | Low | High | Utilize Docker multi-stage caching and pre-built alpine binary target. |
| **RSK-003** | Backend metrics query causes performance degradation on large tables. | Medium | Low | `organization_id` index already exists; add compound index `idx_payout_cases_org_status` if needed. |
| **RSK-004** | Theme shift creates visual contrast issues or breaks accessibility. | Medium | Low | Enforce WCAG AA contrast ratios (minimum 4.5:1 for text) and run axe-core accessibility audits. |

---

## 19. Acceptance Criteria

1. **Dashboard Correctness:** Dashboard metrics (`Total Cases`, `Dual Approvals Needed`, `Settled Minor Units`) display 100% accurate database totals regardless of page limit.
2. **Component Modularity:** No single frontend file exceeds **300 lines** or **15 KB**.
3. **Dedicated Investigation Workspace:** `/app/payouts/[id]/investigate` provides a complete 3-panel layout with evidence timeline, AI findings, and real-time policy checks.
4. **Rust Runtime Verification:** The API container builds and executes the compiled Rust policy binary when `POLICY_ENGINE_BIN` is configured.
5. **Zero Financial Regression:** Dual-custody, $500 ceiling, idempotency keys, and synthetic fixture mutation blocks remain 100% enforced and verified.

---

## 20. Four-Phase Execution Plan & Recommended First Implementation Task for Mission 046

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ PHASE 1: BACKEND METRICS & FILTERING API (Mission 046)                                 │
│ • Implement GET /api/v1/payout-cases/metrics tenant-scoped SQL aggregate.              │
│ • Add status & search query params to ListPayoutCasesByOrg.                            │
│ • Add Acceptance Scenario 33 verifying aggregate correctness.                          │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ PHASE 2: FRONTEND MONOLITH DECOMPOSITION (Mission 047)                                 │
│ • Decompose 112 KB payout detail page into 7 modular subcomponents.                   │
│ • Extract EvidenceInspectorDrawer into standalone accessible component.                │
│ • Verify zero regression across existing 38 Playwright E2E tests.                       │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ PHASE 3: DEDICATED INVESTIGATION WORKSPACE (Mission 048)                               │
│ • Build dedicated 3-panel route /app/payouts/[id]/investigate.                         │
│ • Wire case context, provider snapshots, grounded AI findings, and policy badge.      │
│ • Author e2e/investigation-workspace.spec.ts Playwright suite.                         │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ PHASE 4: HYBRID THEME & RUST DOCKER INTEGRATION (Mission 049)                          │
│ • Apply Hybrid Theme (dark restrained navigation + light operational workspace).       │
│ • Multi-stage Dockerfile packaging Rust claimbridge-policy binary into API image.      │
│ • Full regression verification in CI and Docker Compose.                               │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### Recommended First Implementation Task for Mission 046

**Recommended First Task:**  
Implement **`GET /api/v1/payout-cases/metrics`** in the Go backend (`services/api/internal/payouts`) and update `apps/web/src/app/app/page.tsx` to consume this endpoint.

**Why this is the optimal first step:**
1. It immediately resolves the critical **Dashboard Metric Inaccuracy** without altering financial rules or database schemas.
2. It is a clean, isolated backend addition with zero regression risk to existing payout lifecycles.
3. It lays the accurate data foundation needed before beginning frontend visual transformations.
