# ClaimBridge — Hackathon Judge Evaluation Runbook

**Mission 024 Authoritative Evaluator Guide & Release Baseline**  
**Repository:** [https://github.com/mahitss/Payment_Bridge](https://github.com/mahitss/Payment_Bridge)  
**CI Baseline:** Main-Head Validation [Workflow Run 40119075389](https://github.com/mahitss/Payment_Bridge/actions/runs/40119075389) (Commit `b681c87`, 6/6 Jobs Green) | Feature Branch Baseline [Workflow Run 40119057661](https://github.com/mahitss/Payment_Bridge/actions/runs/40119057661) (Commit `7fbd031`, 6/6 Jobs Green) | Audited Source Baseline [Workflow Run 39600293507](https://github.com/mahitss/Payment_Bridge/actions/runs/39600293507) (Commit `c6a261a`, 6/6 Jobs Green) | Historical Validation [Workflow Run 39633059170](https://github.com/mahitss/Payment_Bridge/actions/runs/39633059170) (Commit `9b0b6c7`, 6/6 Jobs Green)  

---

## 1. System Overview & Safety Boundary

**ClaimBridge** is a multi-tier claims adjudication and payout settlement gateway built for the PayPal AI Hackathon. It pairs non-deterministic AI claim investigations with deterministic, fail-closed safety boundaries: a formal Rust state-machine policy engine, strict dual-custody authorization for disbursements exceeding $250.00 USD, persistent idempotency tracking (`sender_batch_id`), and server-side citation grounding against persisted evidence snapshots. The safety boundary strictly enforces that AI agents never directly invoke payment APIs or mutate financial ledgers; all external mutations require explicit human initiation and pass through strict server-side validation. In the offline demo evaluator, synthetic fixtures are immutably guarded to guarantee demo reproducibility without requiring live external network credentials.

---

## 2. Quick-Start: Launch the Offline Evaluator

ClaimBridge provides a reproducible, self-contained Docker Compose stack with PostgreSQL, Mailpit, the Go API Gateway, the idempotent seeder, and the Next.js frontend compiled in Demo Mode.

### Launch Command

```bash
docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml up --build -d
```

### What This Command Does

1. Starts **PostgreSQL 16** (`claimbridge-postgres`) on `127.0.0.1:5432` with auto-migration.
2. Starts **Mailpit** (`claimbridge-mailpit`) on `127.0.0.1:8025` (Web UI) and `1025` (SMTP) for email delivery.
3. Builds and starts **ClaimBridge Go API** (`claimbridge-api`) on `127.0.0.1:8080` (health probe: `http://127.0.0.1:8080/healthz`).
4. Runs the idempotent **Seeder** (`claimbridge-seeder`) to populate demo accounts, organizations, and synthetic fixtures (`DEMO-W9821`, `DEMO-M4421`, `DEMO-V1082`).
5. Builds and launches **Next.js Web Frontend** (`claimbridge-web`) on `http://localhost:3000` with `NEXT_PUBLIC_DEMO_MODE=true`.

> [!NOTE]
> **Environment Validation Status:** In this local evaluation environment, Docker Compose configuration and service definitions were verified by static and container specification inspection. In CI, the full stack is continuously built, seeded, and tested via Playwright end-to-end automation in Job `Docker Compose Offline Evaluator Verification` (`docker-compose-evaluator`) (27/27 tests passed in Run 40119075389, Run 40119057661, Run 39633059170, and Run 39600293507). No external PayPal credentials or Ollama daemons are required to run this offline evaluator.

---

## 3. Seeded Evaluator Accounts (Local Demo Fixtures)

The seeder populates two distinct role accounts. These accounts are local testing fixtures compiled into the demo frontend:

| Role | Email | Password | Organization | Description |
| :--- | :--- | :--- | :--- | :--- |
| **Primary Operator** | `operator@claimbridge.internal` | `OperatorPass123!` | PayPal AI Hackathon Adjudications | Lead adjudicator (`Alex Morgan`). Initiates payouts, cases, and investigations. |
| **Dual-Approval Officer** | `officer@claimbridge.internal` | `OfficerPass123!` | PayPal AI Hackathon Adjudications | Distinct compliance officer. Authorizes high-value payouts (> $250.00 USD). |

*(Mailpit inbox is viewable at `http://localhost:8025` to inspect outbound verification and notification emails.)*

---

## 4. Evaluator Walkthrough: Expected User Flow & Visible Outcomes

Follow this step-by-step workflow to evaluate the core architecture, safety boundaries, and user experience.

### Step 1: System Health & Landing Page Diagnostics

- **Navigate to:** `http://localhost:3000`
- **Actions:**
  1. Observe the **Evaluator Quick-Start Credentials Banner** displayed when demo mode is active.
  2. Click **"Run Live Diagnostic Probe"** in the Diagnostics section.
- **Expected Visible Result:** Probe displays `✓ ALL SYSTEMS OPERATIONAL` (`200 OK`) confirming API gateway, database connectivity, and session subsystem availability.
- **Limits of Result:** Proves internal system readiness; does not prove external PayPal Sandbox or Ollama reachability.

### Step 2: Operator Sign-In

- **Navigate to:** `http://localhost:3000/login`
- **Actions:** Enter `operator@claimbridge.internal` and `OperatorPass123!`. Click **"Sign In"**.
- **Expected Visible Result:** Successful authentication. Browser receives an `HttpOnly`, `SameSite=Lax` session cookie and redirects to `/app` (Claims & Payouts Dashboard).
- **Limits of Result:** Proves internal Argon2id authentication and session issuance; does not involve third-party identity providers.

### Step 3: High-Value Case Creation ($350.00 USD) & UI Preview

- **Navigate to:** `http://localhost:3000/app/payouts/new`
- **Actions:**
  1. Title: `Warranty Hardware Replacement #HW-4402`
  2. Claimant Email: `claimant.hw4402@example.com`
  3. Description: `Approved motherboard replacement reimbursement.`
  4. Amount: Enter `$350.00`.
- **Expected Visible Result:**
  - As `$350.00` is typed, a blue warning badge appears below the amount field:
    `Dual Approval Required: Amount exceeds $250.00 threshold. A distinct authorized officer must approve before dispatch.`
  - Click **"Create Payout Case"**. Case is created with reference `CASE-...` in `DRAFT` status, with an operation in `AWAITING_APPROVAL`.
- **Limits of Result:** The `$250.00` warning badge is a **client-side React UI preview** calculated from input state. Authoritative policy validation occurs on submission and approval via the Go backend and Rust engine.

### Step 4: Initiator Self-Approval Prevention (Dual-Custody Enforcement)

- **Navigate to:** The newly created case page (`http://localhost:3000/app/payouts/[id]`).
- **Actions:** While logged in as `operator@claimbridge.internal`, locate the Pending Approval card and click **"Approve Operation"**.
- **Expected Visible Result:**
  - An error banner appears immediately:
    `Self-approval is prohibited. Dual-custody policy requires a distinct compliance officer to authorize payouts exceeding $250.00 USD.`
  - The backend returns `HTTP 403 Forbidden`. The operation remains safely in `AWAITING_APPROVAL` status.
- **Limits of Result:** Proves internal dual-custody authorization enforcement. No payment transaction was dispatched.

### Step 5: Distinct Officer Approval

- **Actions:**
  1. Click **"Sign Out"** in the top navigation bar.
  2. Navigate to `http://localhost:3000/login`.
  3. Sign in as `officer@claimbridge.internal` with `OfficerPass123!`.
  4. Navigate back to the created case page (`/app/payouts/[id]`).
  5. Click **"Approve Operation (Distinct Officer)"**.
- **Expected Visible Result:**
  - Approval succeeds with a green confirmation toast.
  - Operation status transitions from `AWAITING_APPROVAL` to `APPROVED`.
- **Limits of Result:** Payout enters the internal `APPROVED` draft state. **Approval does NOT transfer money or guarantee PayPal settlement.** Real settlement requires an explicit dispatch call to the PayPal Payouts REST API with valid sandbox credentials.

### Step 6: Synthetic Fixture Provenance & Mutation Blocking

- **Navigate to:** `http://localhost:3000/app/payouts/DEMO-W9821`
- **Actions:** Inspect pre-seeded case `DEMO-W9821` (`[DEMO TEST FIXTURE] Unclaimed Warranty Payout #W-9821`).
- **Expected Visible Result:**
  - An amber simulation banner is prominently displayed:
    `SIMULATED EVALUATOR FIXTURE: This case is pre-seeded for offline evaluation. External PayPal API calls and status mutations are disabled to guarantee demo reproducibility.`
  - The mutation controls (**Submit to PayPal**, **Sync Status**, **Cancel Payout**) are disabled in the UI.
  - Raw HTTP mutation requests (`POST /api/v1/payouts/cases/.../operations/.../cancel`) fail-closed with `HTTP 403 SYNTHETIC_FIXTURE_IMMUTABLE`.
- **Limits of Result:** Proves synthetic fixture immutability and test isolation. The offline demo video does not demonstrate a genuine cancellation; cancellation safety contracts and state transitions are verified in the deterministic acceptance test harness (Scenario 22 test double; Scenario 28 policy). Live PayPal Sandbox cancellation remains externally unverified until genuine Sandbox credentials are used against a genuine non-fixture transaction.

### Step 7: Persisted Investigation Evidence & Grounding Boundary

- **Navigate to:** `http://localhost:3000/app/payouts/DEMO-W9821`
- **Actions:** Scroll to the **AI Root-Cause Investigation** card.
- **Track 1 — Offline Evaluator Track (Default, Zero-Dependency):**
  - Displays pre-seeded forensic investigation cards with verified citation tags:
    - Root Cause Analysis: `UNCLAIMED_FUNDS_STALLED`
    - Recipient Notice: Unclaimed sandbox disbursement; funds held pending receiver onboarding.
    - Grounded Citations: `EVD-PAYPAL-STATUS-4402`, `EVD-CLAIM-SUBMISSION-1092`.
  - When the local Ollama daemon is offline, clicking "Run AI Investigation" returns `HTTP 503 AI_UNAVAILABLE` fail-closed without fabricating data or disrupting payout operations.
- **Track 2 — Optional Live Local Track (Verified on Single Local CPU Environment):**
  - When Ollama is active with Qwen3.5 4B (`qwen3.5:4b`) configured via native bounded `/api/chat` transport (`AI_CONTEXT_SIZE=4096`, `AI_MAX_OUTPUT_TOKENS=300`, `AI_REQUEST_TIMEOUT_SECONDS=120`), clicking "Run AI Investigation" completes genuine local inference in approximately 60.6 seconds (HTTP 201 Created).
  - The UI displays: *"Investigation completed with grounded evidence."* and renders fresh grounded citations.
- **Limits of Result & Hardware Scope:** 
  - Demonstrates verified evidence snapshots, schema conformity, and citation grounding boundaries.
  - **Hardware Scope & Performance Caveat:** Genuine inference was verified on a **single specific local CPU development machine**. This is **not a guarantee of equal performance or identical execution times on other host machines or arbitrary CPUs** (local CPU execution latency depends on host clock speed, core counts, thermal characteristics, and background operating system load).
  - **Factual Boundaries:** Automated CI runs against deterministic mock providers without requiring local Ollama daemons. This genuine local test does **NOT** verify live PayPal OAuth, live payout dispatch, or genuine PayPal cancellation.

---

## 5. System Behavior When External Dependencies Are Unavailable

ClaimBridge is architected to operate safely without live external network dependencies:

| Dependency | Configuration State | Offline Evaluator Behavior |
| :--- | :--- | :--- |
| **PayPal Sandbox REST API** | `PAYPAL_CLIENT_ID` is empty / unset | The application boots normally. Health probe (`/healthz`) returns `200 OK`. The system displays `PAYPAL: BLOCKED (NO_CREDENTIALS)`. Payout submission to PayPal endpoints is rejected fail-closed with an actionable error message. Zero network calls to PayPal are attempted. |
| **Local Ollama Daemon** | `AI_ENABLED=false` or daemon offline | The application functions normally. Triggering a fresh AI investigation returns `HTTP 503 AI_UNAVAILABLE` with an advisory note explaining that AI inference is offline while deterministic policy workflows remain active. When running locally with `qwen3.5:4b` (`AI_CONTEXT_SIZE=4096`, `AI_MAX_OUTPUT_TOKENS=300`, `AI_REQUEST_TIMEOUT_SECONDS=120`), genuine local bounded inference executes successfully (~60.6s on verified local CPU environment). Automated CI runs against deterministic mock providers and requires no local daemon. |

---

## 6. Safe Shutdown & Cleanup

To stop the evaluator and remove all containers:

### Standard Stop (Preserves Database Volume)
```bash
docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml down
```

### Full Clean Reset (Wipes Volumes for Fresh Re-Evaluation)
```bash
docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml down -v
```

---

## 7. Troubleshooting Guide

Derived from real testing scenarios and container dependencies:

### 1. Port 5432 Conflict (`bind: address already in use`)
- **Cause:** A local PostgreSQL instance is already running on the host machine.
- **Resolution:** Stop the local PostgreSQL service (`net stop postgresql-x64-16` or `sudo systemctl stop postgresql`), or change the host port mapping in `infra/docker-compose.full.yml` (e.g., `"127.0.0.1:5433:5432"`).

### 2. Port 3000 or 8080 Conflict
- **Cause:** Another web application or local development server is using port 3000 or 8080.
- **Resolution:** Terminate the conflicting process before running `docker compose up`.

### 3. Web Container Exits on Startup
- **Cause:** The `claimbridge-web` service depends on `claimbridge-seeder` completing successfully (`condition: service_completed_successfully`).
- **Resolution:** Check seeder logs: `docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml logs seeder`. Ensure the database container is healthy and migrations ran.

### 4. Session Cookie Fails to Persist
- **Cause:** Accessing the frontend via an IP address instead of `localhost` while `ALLOWED_ORIGINS` expects `http://localhost:3000`.
- **Resolution:** Access the application strictly at `http://localhost:3000`.

---

## 8. Capability Audit Summary: Verified vs Implemented

| Capability | Status | Evidence / Verification Method |
| :--- | :--- | :--- |
| Docker Compose Full Stack | **VERIFIED** | CI Job `Docker Compose Offline Evaluator Verification` (`docker-compose-evaluator`) (27/27 Playwright tests passed in Run 40119075389, Run 40119057661, Run 39633059170, and Run 39600293507). |
| PayPal UNCLAIMED Cancellation Contract | **CONTRACT_VERIFIED (HARNESS)** | Acceptance Scenarios 22, 28 (test double & policy rule). Live PayPal Sandbox cancellation remains externally unverified; synthetic fixtures are protected from mutation. Zero real money was moved or cancelled. |
| Idempotent Database Seeder | **VERIFIED** | Integration test `seeder_postgres_test.go` + CI compose build. |
| Dual-Custody Approval Policy | **VERIFIED** | Acceptance Scenario 08, 09, 10 + Playwright E2E spec. |
| Initiator Self-Approval Prevention | **VERIFIED** | Acceptance Scenario 09 (`HTTP 403`) + Playwright E2E spec. |
| UI $250 Threshold Preview | **VERIFIED** | Client-side React state in `new/page.tsx` + Playwright E2E spec. |
| Rust Policy Engine Invariants | **VERIFIED** | Cargo test suite (17 passed; 0 failed) + Clippy clean. |
| Synthetic Fixture Immutability Guard | **VERIFIED** | `service_fixture_guard_test.go` (`HTTP 403`) + Playwright E2E spec. |
| AI Evidence Grounding Assertions | **VERIFIED** | Unit test `evidence_test.go` (`HTTP 422` on fabricated IDs). |
| Real PayPal Sandbox OAuth Exchange | **IMPLEMENTED_NOT_VERIFIED** | Implemented with URL whitelist; live token exchange blocked without credentials. |
| Live PayPal Batch Payout Dispatch | **IMPLEMENTED_NOT_VERIFIED** | Code complete; requires valid PayPal Sandbox client credentials. |
| Genuine Ollama Model Inference | **VERIFIED_LOCAL_RUNTIME (SINGLE CPU MACHINE)** | Verified on a local CPU development environment using Ollama with Qwen3.5 4B (`qwen3.5:4b`) on synthetic case `DEMO-W9821` (HTTP 201 Created, ~60.6s, UI displayed *"Investigation completed with grounded evidence."*). **Hardware Caveat:** Verified on one specific local CPU runtime; not a performance guarantee for arbitrary CPU machines. CI continues to run against deterministic mock test doubles without requiring an active Ollama daemon. |
| Public Narrated Video Publication | **PENDING** | Raw WebM artifact generated in CI (`claimbridge-offline-demo-recording` / `claimbridge-demo-walkthrough.webm`, latest Main-Head Validation Run 40119075389 Artifact ID `12569654956`, expiring November 7, 2026); human narration, export, and upload pending. |

---

## 9. Key Documentation Links

- **Architecture Specification:** [docs/architecture.md](../architecture.md)
- **Judging Criteria Mapping:** [docs/submission/JUDGING_CRITERIA_MAP.md](JUDGING_CRITERIA_MAP.md)
- **Verification & Evidence Matrix:** [docs/submission/VERIFICATION_MATRIX.md](VERIFICATION_MATRIX.md)
- **Operational Sandbox Setup Guide:** [docs/operational-instructions.md](../operational-instructions.md)
- **Project Release Status:** [docs/release/PROJECT_STATUS.md](../release/PROJECT_STATUS.md)
- **Verified CI Workflow Runs:** Main-Head Validation [GitHub Actions Run 40119075389](https://github.com/mahitss/Payment_Bridge/actions/runs/40119075389) (Commit `b681c87`) | Feature Branch Baseline [GitHub Actions Run 40119057661](https://github.com/mahitss/Payment_Bridge/actions/runs/40119057661) (Commit `7fbd031`) | Audited Baseline [GitHub Actions Run 39600293507](https://github.com/mahitss/Payment_Bridge/actions/runs/39600293507) (Commit `c6a261a`) | Historical Validation [GitHub Actions Run 39633059170](https://github.com/mahitss/Payment_Bridge/actions/runs/39633059170) (Commit `9b0b6c7`)
