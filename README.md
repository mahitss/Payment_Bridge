# ClaimBridge

> **AI-Augmented Payout Investigation Engine, Evidence-Grounded Recommendations, Deterministic Rust Safety Policy Enforcement, and PayPal Payouts Settlement**

ClaimBridge is a security-conscious claims processing, instant payout orchestration, and forensic AI investigation platform built for the **PayPal AI Hackathon**.

ClaimBridge decouples non-deterministic AI claim triage and forensic investigations from **deterministic, fail-closed financial safety policy enforcement** implemented in Rust, backed by a high-concurrency Go API gateway, persistent PostgreSQL storage, and a modern Next.js App Router frontend.

---

## Architecture Overview

```
                                  +------------------------------------+
                                  |         Next.js Frontend           |
                                  |     (TypeScript, Tailwind CSS)     |
                                  +-----------------+------------------+
                                                    |  HTTP / Cookies
                                                    v
+---------------------------------------------------------------------------------------------------+
|                                      Go API Orchestration Gateway                                  |
|                                                                                                   |
|   +-----------------------+     +-----------------------+     +-------------------------------+   |
|   |  Argon2id Auth &      |     |  Payout Operations    |     |  AI Investigation Engine      |   |
|   |  Session Management   |     |  & Case Lifecycle     |     |  (Replaceable Free-First)     |   |
|   +-----------+-----------+     +-----------+-----------+     +---------------+---------------+   |
|               |                             |                                 |                   |
+---------------|-----------------------------|---------------------------------|-------------------+
                |                             |                                 |
                v                             v                                 v
+-------------------------------+  +--------------------------+  +----------------------------------+
|      PostgreSQL Database      |  |  Deterministic Rust     |  |   Server-Side AI Inference       |
|  (ACID, Multi-Tenant Scoped,  |  |  Safety Policy Engine   |  |   Local Ollama (Llama 3.2)       |
|  Integer Minor Units Cents)   |  |  (State Machine & Caps)  |  |   OpenAI-Compatible Endpoint     |
+-------------------------------+  +--------------------------+  +----------------------------------+
                                              |
                                              v
                               +------------------------------+
                               |   PayPal Payouts REST API    |
                               |  (Sandbox-Only Enforcement)  |
                               +------------------------------+
```

```text
claim-bridge/
├── apps/
│   └── web/                 # Next.js 14 (App Router), TypeScript, Tailwind CSS
│       ├── Dockerfile       # Standalone multi-stage production runner
│       └── src/
│           ├── app/
│           │   ├── app/     # Dashboard, Payouts Registry, Payout Detail, New Case
│           │   └── settings/# Account profile & PayPal Sandbox Integrations
│           ├── components/  # Cybernetic shared navigation header
│           └── lib/         # Auth provider & Payout domain models (minor units in cents)
├── services/
│   ├── api/                 # Go 1.22+ HTTP API (auth, orchestration, PayPal client, AI engine)
│   │   ├── Dockerfile       # Multi-stage Go production runner
│   │   ├── cmd/
│   │   │   ├── server/      # Production HTTP server entrypoint
│   │   │   ├── acceptance/  # 32-scenario end-to-end acceptance verification suite
│   │   │   └── paypal-smoke/# Explicitly invoked PayPal sandbox smoke-test tool
│   │   └── internal/
│   │       ├── ai/          # Replaceable AI provider abstraction & grounding validator
│   │       ├── auth/        # Argon2id, session lifecycle, CSRF/Origin check
│   │       ├── config/      # Validated configuration & sandbox assertion
│   │       ├── crypto/      # CSPRNG tokens, SHA-256 hashes, email normalization
│   │       ├── db/          # PostgreSQL migrations & thread-safe mock repository
│   │       │   └── migrations/ # 000001_auth, 000002_claims, 000003_payouts, 000004_investigations, 000005_fixture_provenance
│   │       ├── email/       # Mailpit SMTP & thread-safe mock mailer
│   │       ├── investigations/ # AI snapshot aggregator, grounding check & audit persistence
│   │       ├── logger/      # Structured JSON logging with stream redactions
│   │       ├── paypal/      # Server-side PayPal sandbox REST client
│   │       ├── payouts/     # Payout cases, operations, approval, sync, cancellation
│   │       ├── policy/      # Go bridge to policy evaluation
│   │       └── server/      # HTTP router, middleware (body limit, security headers)
│   └── policy/              # Rust deterministic safety policy engine
│       ├── Dockerfile       # Standalone multi-stage production binary
│       ├── src/             # Mathematical rules, payout transitions, dual-approval engine
│       └── tests/           # Deterministic unit & boundary test suite
├── docs/                    # PRD, Architecture, Threat Model, Acceptance Criteria, ADRs
│   ├── release/             # Traceability, Acceptance Plan, Security Review, Runbooks
│   ├── architecture/        # Deep-dive architecture specifications
│   └── adr/                 # Architecture Decision Records (ADR-001 through ADR-006)
├── infra/                   # Local infrastructure definitions
│   ├── docker-compose.yml   # Lightweight Postgres 16 & Mailpit
│   ├── docker-compose.full.yml # Full-stack containerized deployment
│   └── postgres-init/       # Database bootstrap schema
├── .github/workflows/       # CI pipelines for Go, Rust, Web, and 32-scenario acceptance suite
├── LICENSE                  # MIT License
└── .env.example             # Configuration template with placeholders only
```

---

## Core Security Invariants

1. **AI Proposes, Policy Decides**: AI inference provides explanations and suggestions, but has **ZERO execution authority** over money movement. Only the deterministic Rust policy engine can grant approval.
2. **Fail-Closed by Default**: If any parameter is missing, out-of-bounds, unverified, or ambiguous, disbursement is strictly blocked. A hard ceiling of $500.00 USD is permanently enforced in code and policy.
3. **Evidence Grounding Verification**: The Go backend inspects every evidence identifier cited by the AI model. If an AI finding references an unknown or hallucinated ID, the investigation is rejected with HTTP 422 `INVALID_EVIDENCE_REFERENCE`.
4. **Closed Action Whitelist**: AI recommendations are constrained to 6 pre-approved types (`REQUEST_RECIPIENT_INFORMATION`, `CHECK_PROVIDER_STATUS`, `REVIEW_RECIPIENT_DETAILS`, `REVIEW_CANCELLATION_ELIGIBILITY`, `ESCALATE_TO_OPERATOR`, `NO_ACTION_UNTIL_RECONCILED`).
5. **Absolute Idempotency & Zero Blind Retries**: A single claim is bound to a single deterministic `sender_batch_id` committed to PostgreSQL *before* calling PayPal. Network timeouts never generate replacement identifiers.
6. **Separation of Duties (Dual Custody)**: Payouts exceeding defined risk (> 20.0) or monetary limits (> $250.00 USD) require dual custody. The initiating officer is strictly forbidden from approving their own payout (`ERR_SELF_APPROVAL_FORBIDDEN`).
7. **Reconciliation-Required Safety**: If a PayPal submission times out or returns an ambiguous response, the operation transitions to `RECONCILIATION_REQUIRED`. Automatic duplicate payouts are strictly prohibited.
8. **Prompt Injection Defense**: Untrusted user text is bounded in length, sanitized, and isolated within `<UNTRUSTED_EVIDENCE>` tags. Adversarial instructions are ignored.
9. **Zero Browser Secret Exposure**: Client secrets, OAuth tokens, and server-side credentials are never transmitted to the browser or placed in public Next.js bundles.
10. **Strict Sandbox Assertion**: The Go API gateway immediately terminates at startup if `api-m.paypal.com` or any production host is configured.
11. **Integer Minor Unit Currency**: Money is calculated, compared, and persisted exclusively in integer minor units (cents) to avoid floating-point rounding hazards.
12. **Multi-Tenant Data Isolation**: Every case, operation, investigation, and audit event is hard-isolated by organization ID across database indexes, foreign keys, and service boundaries.
13. **Synthetic Fixture Provenance & Mutation Guard**: All demo fixtures are isolated via explicit `is_synthetic_fixture` markers, unmistakable identifiers (`SIMULATED-DEMO-...`), and server-side guards blocking external PayPal API dispatch. Safe reset purges only synthetic records without deleting non-demo data.

---

## Quick Start

### 1. Option A: Single-Command Full-Stack Docker Launch
```bash
# Clone repository
git clone https://github.com/mahitss/Payment_Bridge.git
cd Payment_Bridge

# Configure environment
cp .env.example .env

# Launch complete stack (Postgres, Mailpit, API, Web)
docker compose -f infra/docker-compose.full.yml up --build -d
```
- Web Application: [http://localhost:3000](http://localhost:3000)
- API Health Probe: [http://localhost:8080/healthz](http://localhost:8080/healthz)
- API Readiness Probe: [http://localhost:8080/readyz](http://localhost:8080/readyz)
- Mailpit Web Inbox: [http://localhost:8025](http://localhost:8025)

---

### 2. Option B: Local Developer Launch

#### Step 1: Start Backing Services
```bash
docker compose -f infra/docker-compose.yml up -d
```

#### Step 2: Configure Environment
```bash
cp .env.example .env
```
Ensure `PAYPAL_ENV=sandbox`. Configure your PayPal Sandbox credentials:
```env
PAYPAL_CLIENT_ID=your_sandbox_client_id
PAYPAL_CLIENT_SECRET=your_sandbox_client_secret
PAYPAL_SANDBOX_BASE_URL=https://api-m.sandbox.paypal.com
```

#### Step 3: Run Policy Engine (Rust)
```bash
cd services/policy
cargo test
cargo clippy -- -D warnings
```

#### Step 4: Run API Gateway (Go)
```bash
cd services/api
go test ./...
# Seed idempotent demo fixtures (safe repeated execution)
go run ./cmd/seed
go run ./cmd/server
```

#### Step 5: Run Web Application (Next.js)
```bash
cd apps/web
npm install
npm run build
# Set NEXT_PUBLIC_DEMO_MODE=true if you want the evaluator quick-start banner visible on the landing page
NEXT_PUBLIC_DEMO_MODE=true npm run dev
```
Open [http://localhost:3000](http://localhost:3000) in your browser.

---

## Local Free-Tier AI Inference (Ollama)

To run the AI investigation engine locally with zero API fees:

1. Install Ollama: [https://ollama.ai](https://ollama.ai)
2. Pull and start Llama 3.2:
   ```bash
   ollama pull llama3.2
   ollama serve
   ```
3. Set environment variables in `.env`:
   ```env
   AI_ENABLED=true
   AI_PROVIDER=ollama
   AI_BASE_URL=http://localhost:11434
   AI_MODEL=llama3.2
   ```
4. Restart the Go API server.

---

## Automated Acceptance Test Suite (32 Scenarios)

Execute the end-to-end automated runner that verifies all 32 acceptance scenarios across Missions 001 through 005 in a deterministic simulated test harness (using test doubles that make zero external network requests):

```bash
cd services/api
go run ./cmd/acceptance
```

### Verified Acceptance Scenarios Summary:
- **Scenarios 01–12 (Auth & Multi-Tenancy):** Registration, single-use email verification, HttpOnly session cookies, protected routes, session persistence, logout revocation, password reset with replay rejection, unauthenticated route blocking, tenant isolation, reboot persistence.
- **Scenarios 13–24 (Payouts & PayPal Integration):** Live sandbox OAuth probe, single-officer case creation, dual-custody case creation, fail-closed $500 ceiling rejection, durable operation initiation, self-approval prevention (HTTP 403), dual approval by distinct officer, PayPal REST submission, live status synchronization, unclaimed cancellation, cross-tenant case blocking, ambiguous network timeout quarantine (`RECONCILIATION_REQUIRED`).
- **Scenarios 25–32 (AI Investigations & Security):** Grounded investigation with evidence linking, insufficient evidence handling, fabricated evidence ID rejection (HTTP 422), policy precedence over AI recommendations, AI downtime graceful degradation (HTTP 503), cross-tenant investigation blocking, investigation persistence across reboots, prompt injection text neutralization.

---

## Hackathon Demo Script (Under 3 Minutes)

ClaimBridge features a reproducible, zero-budget **Offline Simulated Walkthrough (Track 1)** that requires neither live PayPal credentials nor local Ollama daemons, alongside an **Optional Live Sandbox Track (Track 2)** when verified:

### [00:00 - 00:25] Introduction & Truthful System Probing
- **Visual:** Open ClaimBridge landing page (`/`). Point out the **Sandbox Prototype** badge.
- **Action:** Click **Probe Gateway**. Show discrete verification cards: API Gateway online, PayPal Sandbox status distinctly partitioned without leaking secrets.
- **Narrative:** "Automated payout systems face critical risks: LLMs hallucinating fund transfers, duplicate disbursements during flaky network retries, and leaked credentials. ClaimBridge solves this by decoupling non-deterministic AI investigation from deterministic, fail-closed Rust policy enforcement."

### [00:25 - 01:25] Dual-Custody Payout & Separation of Duties
- **Visual:** Log in as demo operator (`operator@claimbridge.internal` / `OperatorPass123!`). Navigate to **New Payout** (`/app/payouts/new`).
- **Action:** Enter a payout for $350.00 USD (exceeds $250.00 threshold). Point out the real-time Rust policy indicator: `Dual-Approval Required`.
- **Action:** Submit case. Show the active operation in `AWAITING_APPROVAL`.
- **Action:** Demonstrate self-approval blocked fail-closed for the creator.
- **Action:** Switch to distinct compliance officer (`officer@claimbridge.internal`). Approve the payout.
- **Result:** Operation transitions to `APPROVED`. Ordinary cases are not automatically submitted as a side effect.

### [01:25 - 01:50] Synthetic Fixture Provenance & Mutation Guards
- **Visual:** Navigate to Payout Cases Registry (`/app/payouts`) and open pre-seeded fixture `DEMO-W9821`.
- **Action:** Point out the prominent banner: **“SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION”**.
- **Action:** Show disabled external buttons (`Submit to PayPal Sandbox`, `Sync`, `Cancel`) with tooltip: `Disabled: Synthetic Demo Fixture`.
- **Action:** Explain backend API mutation guards: fixture requests are rejected with HTTP 403 `FIXTURE_EXTERNAL_OPERATION_FORBIDDEN` with zero outbound PayPal requests.

### [01:50 - 02:20] AI Forensic Investigation & Grounded Evidence Boundary
- **Visual:** Scroll to the **AI Payout Investigation Engine** card on `DEMO-W9821`.
- **Action:** Inspect the pre-seeded synthetic investigation report (`SIMULATED DEMO INVESTIGATION`).
- **Highlight:**
  1. **Persisted Offline Inspection:** Explains how ClaimBridge presents verified pre-seeded findings without faking live model responses during offline evaluation.
  2. **Honest Offline Handling:** If "Run Investigation" is clicked without a running Ollama daemon, the system honestly reports `AI_UNAVAILABLE` while financial operations remain operational.
  3. **Evidence Citations:** Verified references linking to persisted case and status snapshot records.
  4. **Grounding Safety:** Fabricated IDs are rejected fail-closed with HTTP 422. AI recommendations are strictly advisory and cannot override Rust safety policies.

### [02:20 - 02:35] Automation, Regression Suite & Release Gates
- **Visual:** Terminal split screen showing:
  - `cargo test` passing 17/17 deterministic policy tests.
  - `go run ./cmd/acceptance` running all 32 end-to-end acceptance scenarios.
  - `npm run test:e2e` running Playwright browser regression tests.
  - Next.js production build with 15/15 standalone routes.
- **Narrative:** "ClaimBridge enforces integer minor-unit math, multi-tenant isolation, 1MB request limits, and prompt injection defense. AI proposes, policy decides, and PayPal settles securely."

---

## License

This project is licensed under the [MIT License](LICENSE).
