# ClaimBridge — Operational Instructions: PayPal Sandbox Setup & Verification

This guide provides the exact steps required for an engineer or auditor to configure PayPal Sandbox credentials and verify API access using ClaimBridge's safety-hardened tooling.

---

## Prerequisites

1. PayPal Developer Account ([developer.paypal.com](https://developer.paypal.com)).
2. Installed Toolchains:
   - Go 1.22+ (`go version`)
   - Rust 1.75+ (`cargo --version`)
   - Node.js 18+ (`node -v`)

---

## Step 1: Create a PayPal Sandbox REST App

1. Log in to the [PayPal Developer Dashboard](https://developer.paypal.com/dashboard/).
2. Navigate to **Apps & Credentials**.
3. Toggle the switch to **Sandbox** (NEVER select Live for development/testing).
4. Click **Create App**:
   - **App Name**: `ClaimBridge-Sandbox-Tester`
   - **App Type**: `Merchant`
   - **Sandbox Business Account**: Select your default sandbox business account (e.g. `sb-facilitator...`).
5. Click **Create App**.

---

## Step 2: Enable Payouts Permission on the Sandbox Account

> [!IMPORTANT]
> The PayPal Payouts API requires explicit permissions enabled in the sandbox profile.

1. Under the newly created app settings, look for the **App Settings** / **Features** checklist.
2. Ensure **Payouts** checkbox is checked. If it is unchecked, toggle it on and save changes.
3. Note your:
   - **Client ID** (alphanumeric string starting with e.g. `A...` or `E...`)
   - **Secret** (click *Show* to reveal)

---

## Step 3: Configure Local Environment

1. Copy `.env.example` to `.env` in the repository root:
   ```bash
   cp .env.example .env
   ```
2. Populate the PayPal Sandbox values:
   ```dotenv
   PAYPAL_ENV=sandbox
   PAYPAL_CLIENT_ID=your_actual_sandbox_client_id
   PAYPAL_CLIENT_SECRET=your_actual_sandbox_client_secret
   PAYPAL_SANDBOX_BASE_URL=https://api-m.sandbox.paypal.com
   ```
3. **Endpoint Security Constraints:**
   - The application strictly permits only the official HTTPS PayPal Sandbox endpoint: `https://api-m.sandbox.paypal.com`.
   - The configuration and client boundaries strictly validate URL scheme (`https`), hostname (`api-m.sandbox.paypal.com`), port (empty or 443), userinfo (forbidden), and paths/queries/fragments (forbidden).
   - Any PayPal production host (e.g. `api-m.paypal.com`), insecure HTTP scheme, or arbitrary host is immediately rejected fail-closed before any HTTP request or credential dispatch occurs.
   - Normal `.env` overrides cannot redirect credentials to an arbitrary destination. Simulated test endpoints are permitted only via explicit test doubles (`WithSimulatedBaseURL` / `NewTestClient`) and are strictly forbidden when `APP_ENV=production`.
4. Verify that `PAYPAL_ENV` is strictly `sandbox`. Any value set to `production` will be refused fail-closed by the runtime.

---

## Step 4: Run the PayPal Sandbox Smoke Test

The smoke-test utility is located in `services/api/cmd/paypal-smoke`.

### 4.1 Dry Run / Auth Verification (No Payouts Created)
This verifies that your Client ID and Secret can obtain an OAuth2 Bearer token from PayPal's sandbox servers.
The CLI automatically resolves `.env` in the repository root (`../../.env`) when running from `services/api`. An explicit path can also be provided with `-env-file <path>`.

```bash
cd services/api
go run ./cmd/paypal-smoke
```

**Expected Output (When configured with valid sandbox credentials):**
```
================================================================================
         CLAIMBRIDGE — PAYPAL SANDBOX INTEGRATION VERIFICATION TOOL            
================================================================================
[INFO] Loaded environment configuration from: ../../.env

[1/3] Environment Security Audit:
  • Application Environment : development
  • PayPal Target Mode      : sandbox [VERIFIED SANDBOX]
  • PayPal Base URL         : https://api-m.sandbox.paypal.com
  • Request Timeout         : 15s

[2/3] Performing OAuth2 Client Credentials Grant with PayPal Sandbox...

  [SUCCESS] OAuth2 Authentication Verified!
  • Token Type : Bearer
  • Token State: Cached & Valid
  • Token Hash : [REDACTED_ACCESS_TOKEN] (never exposed to logs or disk)

[3/3] Sandbox Read-Only Feasibility Mode:
  • Automatic payouts are DISABLED by default for safety.
  • To test payout creation: specify -create-test-payout -recipient <email> -confirm-sandbox-only
  • To inspect a batch    : specify -batch-id <payout-batch-id>

================================================================================
                 FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY                
================================================================================
```

### 4.2 CLI Exit Codes & Status Interpretation

| Exit Code | Status Reported | Meaning | Operator Action |
|---|---|---|---|
| **0** | `SUCCESS` | Requested operation completed safely (OAuth verified, or read-only lookup succeeded). | None. Sandbox integration verified. |
| **1** | `FAILED` | Requested lookup (batch ID / item ID) failed, or cancellation rejected, or safety confirmation missing. | Verify batch/item ID exists in your sandbox account. |
| **2** | `FAILED` | OAuth2 grant failed with HTTP 401 (Invalid Credentials) or 403 (Forbidden). | Check Client ID and Secret in `.env`. Ensure 'Payouts' permission is enabled in PayPal Developer Dashboard. |
| **3** | `BLOCKED` | `PAYPAL_CLIENT_ID` or `PAYPAL_CLIENT_SECRET` unset or empty. | Populate sandbox credentials into `.env`. |

### 4.3 Querying an Existing Sandbox Payout Batch or Item (Read-Only)
If you have an existing batch or item ID from prior sandbox activity:

```bash
# Query by batch ID:
go run ./cmd/paypal-smoke -batch-id <YOUR_PAYOUT_BATCH_ID>

# Or query by item ID:
go run ./cmd/paypal-smoke -item-id <YOUR_PAYOUT_ITEM_ID>
```

> [!IMPORTANT]
> **Read-Only Lookup vs. OAuth Verification Guarantees:**
> - **OAuth Verification (Check 6):** Validates client credentials against the official sandbox endpoint and acquires a cached OAuth2 Bearer token.
> - **Read-Only Lookup (Check 7):** Queries existing sandbox transaction state via HTTP GET requests (`/v1/payments/payouts/{id}` or `/v1/payments/payouts-item/{id}`). Zero payouts or financial mutations are ever created or altered.
> - **OAuth Prerequisite:** A payout lookup will **never** be dispatched if OAuth verification fails or credentials are unset. Check 7 is automatically reported as `BLOCKED` in those cases.
> - **Mutual Exclusion:** Supplying both `-batch-id` and `-item-id` is rejected fail-closed with a non-zero exit code (`ambiguous PayPal lookup invocation`) rather than silently ignoring one.

### 4.4 Safe Test Payout Creation (Explicit Flag Required)
> [!CAUTION]
> Phase 1 defaults to read-only verification. Payout creation is NEVER triggered automatically.
> To run a single test payout in sandbox mode, you must explicitly supply all required safety parameters:

```bash
go run ./cmd/paypal-smoke \
  -create-test-payout \
  -recipient "sb-buyer@business.example.com" \
  -amount "1.00" \
  -currency "USD" \
  -confirm-sandbox-only
```

If `-confirm-sandbox-only` is omitted or `PAYPAL_ENV != sandbox`, execution aborts immediately with a non-zero exit code.

---

## Step 5: Full Application End-to-End Sandbox Verification (`cmd/sandbox-e2e`)

`services/api/cmd/sandbox-e2e` is ClaimBridge's dedicated operator-run integration verification tool. Unlike simulation-only tests or low-level smoke probes, this tool exercises the **actual ClaimBridge application stack**—creating a dedicated real test claim, initiating and submitting an authorized payout operation through the core service, persisting the batch ID and provider response in PostgreSQL, and reconciling its status via PayPal Sandbox.

### 5.1 Account Prerequisites on PayPal Developer Dashboard
1. Log in to [PayPal Developer Dashboard](https://developer.paypal.com/dashboard/).
2. Under **Testing Tools** -> **Sandbox Accounts**:
   - Locate your **Business (Facilitator)** account (from which your Sandbox REST App credentials originate).
   - Locate or create a **Personal (Buyer/Recipient)** account (e.g. `sb-xxxx@personal.example.com`).
   - Copy the email address of the **Personal** sandbox account to use as the recipient.
3. Under **Apps & Credentials** -> Select your Sandbox App:
   - Confirm **Payouts** is enabled in App Settings.
   - Note the **Client ID** and **Client Secret**.

### 5.2 Local Environment Setup (`.env`)
Place sandbox credentials in `.env` in the repository root:
```dotenv
PAYPAL_ENV=sandbox
PAYPAL_CLIENT_ID=your_actual_sandbox_client_id
PAYPAL_CLIENT_SECRET=your_actual_sandbox_client_secret
PAYPAL_SANDBOX_BASE_URL=https://api-m.sandbox.paypal.com
DATABASE_URL=postgres://claimbridge:claimbridge_local_dev_only@localhost:5432/claimbridge_dev?sslmode=disable
```

Before running authorized mutation workflows, ensure PostgreSQL is running and seeded with initial operator fixtures:
```powershell
Set-Location services/api
go run ./cmd/seed
```
> [!IMPORTANT]
> `cmd/sandbox-e2e` strictly validates operator presence and organization membership; it fails closed if the operator context is not initialized in the database (it never uses insecure hard-coded fallback passwords).

### 5.3 Stage A: Run the Read-Only Preflight (Default Mode)
The default mode is strictly read-only and will **never** create a payout. It verifies local environment variables, security constraints, PostgreSQL connectivity/migrations, and performs a live OAuth2 token exchange with PayPal Sandbox.

**Windows PowerShell:**
```powershell
Set-Location services/api
go run ./cmd/sandbox-e2e
```

**Expected Console Output (When configured):**
```
================================================================================
     CLAIMBRIDGE — REAL PAYPAL SANDBOX END-TO-END VERIFICATION TOOL            
================================================================================
[INFO] Loaded environment configuration from: ../../.env

[1/4] Environment Security Audit:
  • Application Environment : development
  • PayPal Target Mode      : sandbox [VERIFIED SANDBOX ONLY]
  • PayPal Sandbox Base URL : https://api-m.sandbox.paypal.com
  • Request Timeout         : 15s
  • Target Commit SHA       : a73ec57

[2/4] Testing Genuine OAuth2 Client Credentials Grant with PayPal Sandbox...
  [SUCCESS] Genuine OAuth2 Authentication Verified!
  • Token Type : Bearer
  • Token State: Cached & Valid
  • Token Hash : [REDACTED_ACCESS_TOKEN] (never exposed to logs or disk)

[3/4] Database & Storage Layer Preflight:
  • PostgreSQL Status       : CONNECTED
  • Schema Migrations       : CURRENT (Verified)

[4/4] End-to-End Application Workflow Execution:
  • Execution Mode          : READ-ONLY PREFLIGHT (Default)
  • Financial Mutations     : DISABLED (Zero payouts attempted)

  [STATUS: PASS] Read-Only Preflight Verification Completed Successfully.
  • All environment, credentials, and OAuth checks passed!
  To execute an explicitly authorized USD 1.00 sandbox test payout:
    go run ./cmd/sandbox-e2e -submit-payout -recipient <sandbox-recipient-email> -confirm-sandbox-only
================================================================================
```

### 5.4 Stage B & C: Run the Authorized USD 1.00 Test Payout
With explicit operator consent, submit exactly one low-value test payout through the real application service and persistence layers.

**Windows PowerShell:**
```powershell
Set-Location services/api
go run ./cmd/sandbox-e2e `
  -submit-payout `
  -recipient "sb-xxxx@personal.example.com" `
  -amount 1.00 `
  -confirm-sandbox-only
```

> [!CAUTION]
> **Strict Mutation Safeguards Enforced:**
> - **Write Flag Required**: `-submit-payout` (or `-create-test-payout`) must be explicitly passed.
> - **Confirmation Required**: `-confirm-sandbox-only` is mandatory; omitting it immediately terminates execution with exit code 1.
> - **Environment Locked**: `PAYPAL_ENV` must strictly be `sandbox`.
> - **Hard Safety Ceiling**: Amounts above **USD 1.00** are refused fail-closed (`amount exceeds hard test ceiling of $1.00 USD`).
> - **No Automatic Retries**: Timeouts never trigger duplicate mutations.

### 5.5 Status Outcomes & Meaning

| Outcome Status | Exit Code | Meaning | Operator Action |
|:---|:---:|:---|:---|
| **`PASS`** | `0` | All preflight checks passed, and (if authorized) payout was submitted, persisted, and reconciled in PostgreSQL. | Payout successfully verified. Inspect report artifacts in `services/api/artifacts/`. |
| **`FAIL`** | `1` | Configuration security failure (e.g. production host, insecure HTTP), missing confirmation flag, amount above $1.00, or correlation mismatch. | Review console error message. Rectify configuration or command-line parameters. |
| **`OAuth Error`** | `2` | OAuth2 handshake failed (HTTP 401 Unauthorized or 403 Forbidden). | Check `PAYPAL_CLIENT_ID` and `PAYPAL_CLIENT_SECRET` in `.env`. Ensure **Payouts** permission is enabled in the PayPal Developer Dashboard. |
| **`BLOCKED`** | `3` | Sandbox credentials are unset or empty in the local environment. | Copy sandbox credentials into `.env` as documented in Step 5.2. |
| **`UNRESOLVED`** | `4` | Network timeout during payout submission. State preserved in `RECONCILIATION_REQUIRED`. | **Do NOT retry or create a new payout.** Follow investigation instructions in Section 5.6. |

### 5.6 Investigating an Unresolved Payout Without Replaying
If an ambiguous network timeout occurs (`EXIT CODE 4: UNRESOLVED`):
1. **Do NOT rerun with a new batch ID or resubmit.** ClaimBridge preserves the operation in `RECONCILIATION_REQUIRED` to prevent duplicate disbursement.
2. Query the batch or item status in read-only mode using `paypal-smoke`:
   ```powershell
   go run ./cmd/paypal-smoke -batch-id <PAYOUT_BATCH_ID>
   ```
3. Or invoke ClaimBridge read-only synchronization endpoint:
   ```powershell
   curl -X POST http://localhost:8080/api/v1/payout-cases/<case_id>/sync
   ```
4. If the payout was not recorded by PayPal, invoke recovery:
   ```powershell
   curl -X POST http://localhost:8080/api/v1/payout-cases/<case_id>/recover
   ```

### 5.7 Inspecting Saved Evidence & Reports
- **Sanitized Report Artifacts**: Written automatically to `services/api/artifacts/sandbox-e2e-report-<timestamp>.json` and `.md`. These artifacts contain zero secrets and masked recipient addresses.
- **Database Status Snapshots**:
  ```sql
  SELECT id, payout_operation_id, batch_status, created_at
  FROM payout_status_snapshots
  ORDER BY created_at DESC LIMIT 5;
  ```
- **Audit Log Events**:
  ```sql
  SELECT event_type, metadata, created_at
  FROM audit_events
  WHERE event_type LIKE 'PAYOUT_%'
  ORDER BY created_at DESC LIMIT 5;
  ```

---

## Step 6: Troubleshooting Common Sandbox Issues

| Issue | Root Cause | Fix |
|---|---|---|
| `invalid_client` HTTP 401 | Incorrect Client ID or Secret, or Live credentials used in Sandbox URL. | Double check developer dashboard; ensure Sandbox toggle was selected. |
| `FEATURE_NOT_AVAILABLE` / HTTP 403 | Payouts permission not enabled on the Sandbox Business account. | Go to PayPal Developer Dashboard -> Sandbox Accounts -> View/Edit Account -> Enable Payouts. |
| `INSUFFICIENT_FUNDS` | Sandbox Business account has $0 balance. | Go to Sandbox Accounts -> Account Details -> Funding -> Add balance to PayPal wallet. |
| `Ambiguous Timeout` | Network interruption during payout dispatch. | Do NOT rerun with new batch ID. Query status using `-batch-id <id>`. |

---

## Step 7: Local Ollama Model Setup & Verification

ClaimBridge features genuine local model inference for payout investigations, requiring no paid API keys.

### 7.1 Install & Start Ollama
1. Download and install Ollama from [ollama.ai](https://ollama.ai).
2. Start the local daemon:
   ```bash
   ollama serve
   ```
3. Pull the configured local model (`llama3.2`):
   ```bash
   ollama pull llama3.2
   ```

### 7.2 Verify Model Readiness & System Gates
Run the pre-demo readiness diagnostic tool to verify that Ollama is reachable, the model is installed, and genuine non-mutating inference succeeds:

```bash
cd services/api
go run ./cmd/readiness -profile=submission
```

### 7.3 Run an End-to-End Investigation in the Web UI
1. Start the services:
   ```bash
   # Terminal 1: Backend
   cd services/api && go run ./cmd/server
   # Terminal 2: Frontend
   cd apps/web && npm run dev
   ```
2. Navigate to `http://localhost:3000/app/payouts`.
3. Open an existing payout case or create a test case.
4. In the **AI Investigation Engine** panel, click **Run Investigation**.
5. The UI will render:
   - Confidence score and advisory disclaimer badge.
   - Grounded findings citing verifiable case and operation IDs.
   - Deterministic policy-checked next steps.
   - Provider name and model metadata (`ollama-openai-compatible (llama3.2)`).
6. Verify that no financial mutation occurred and financial state remains unchanged.

---

## Step 8: System Readiness Diagnostic Profiles & Gate Semantics

The readiness diagnostic tool (`services/api/cmd/readiness`) eliminates false-green reporting by evaluating discrete checks against explicit operational profiles.

### 8.1 Diagnostic Profiles

| Profile | Command | Intended Use | Required Checks | Optional / Informational Checks |
|---|---|---|---|---|
| **`core`** (default) | `go run ./cmd/readiness -profile=core` | Local backend & frontend development. | 1. PostgreSQL Persistence<br>2. Go API Health (`/healthz`)<br>3. Go API Readiness (`/readyz`)<br>4. Rust Deterministic Policy Engine<br>5. Next.js Web Frontend | 6. PayPal Sandbox OAuth<br>7. PayPal Read-Only Lookup<br>8. AI Reachability & Model<br>9. AI Model Inference Probe |
| **`submission`** (or `demo`) | `go run ./cmd/readiness -profile=submission` | Demo recording & hackathon evaluation. | 1. PostgreSQL Persistence<br>2. Go API Health (`/healthz`)<br>3. Go API Readiness (`/readyz`)<br>4. Rust Deterministic Policy Engine<br>5. Next.js Web Frontend<br>6. Genuine PayPal Sandbox OAuth<br>8. AI Reachability & Model<br>9. Genuine AI Model Inference Probe | 7. PayPal Read-Only Lookup (evaluated if `-batch-id` or `-item-id` supplied) |

### 8.2 Check Status Taxonomy

Every check outputs one of five explicit statuses:
- **`PASS`**: The check completed successfully and met all verification criteria.
- **`FAIL`**: The check executed and failed (e.g. non-200 HTTP status, timeout, malformed payload, or rejection).
- **`BLOCKED`**: The check could not execute due to a missing external/local prerequisite (e.g. unconfigured credentials or offline local daemon).
- **`WARN`**: Informational warning (e.g. mock provider configured in `core` profile).
- **`SKIPPED`**: Check was intentionally unrequested (e.g. no payout batch/item ID provided for lookup).

### 8.3 Truth in Testing Guarantees
1. **Mock Tests Do Not Verify Genuine Integration:** Mock providers and simulated endpoints in unit tests verify code syntax and error handling, but can never satisfy the `submission` profile.
2. **Model Availability Is Not Inference Verification:** Probing `/api/tags` or `/v1/models` verifies only that the daemon is reachable and the model tag exists. The `submission` profile requires executing a non-mutating synthetic inference probe and passing evidence grounding validation.
3. **OAuth Success Does Not Establish Payout Lookup:** Successful OAuth authentication verifies credentials with PayPal Sandbox, but does not prove payout creation or lookup. Payout lookups are evaluated independently.
4. **Current Environment Status:** In an environment without local `.env` sandbox keys or without a running Ollama daemon, running `-profile=submission` accurately reports `[STATUS: BLOCKED]` with Exit Code 1.

### 8.4 Strict API Storage Readiness (`/readyz`) Contract
Check 3 (`ProbeAPIReadiness`) enforces strict verification of API operational readiness and database storage health:
- **Bounded Request Timeout:** The HTTP request must complete within a bounded 5-second timeout window.
- **Strict HTTP Status:** The endpoint must return HTTP status code exactly `200 OK`. Any 5xx or 4xx response immediately produces `FAIL`.
- **Valid JSON Body:** The response body must be parseable JSON. Empty bodies or malformed JSON immediately produce `FAIL`.
- **Bounded Memory Ingestion:** Diagnostic responses are read through `io.LimitReader(resp.Body, 65536)` (max 64KB) to prevent denial-of-service from rogue or unbounded remote payloads.
- **Field-Level Strictness:**
  - The response must contain a non-empty string `status` field equal to `"ready"`.
  - The response must contain a non-empty string `database` field equal to `"connected"`.
  - A degraded status or missing database field fails closed and is never defaulted to a healthy state.
- **Sanitized Diagnostics:** Error messages avoid echoing raw HTML or unescaped error strings to console logs.

### 8.5 Mock vs. Live Test Double Isolation
- **Controlled Testing Mechanism:** Tests using simulated HTTP servers (`httptest.NewServer`) inject test doubles via `paypal.NewTestClient` or `WithSimulatedBaseURL`.
- **Production Guardrail:** If `APP_ENV=production`, `WithSimulatedBaseURL` returns `ErrSimulatedEndpointForbidden` fail-closed.
- **Core Profile Isolation:** When `AI_PROVIDER=mock` is configured, running the `core` profile reports Check 9 as `WARN` (informational) without failing core readiness. In contrast, running the `submission` profile strictly reports `FAIL` and terminates with exit code 1.

---

## Step 9: Demo Fixture Management, Idempotent Seeding, and Provenance Safeguards

ClaimBridge provides deterministic, testable evaluation fixtures for local demonstration and review without polluting genuine payment channels or creating ambiguity with live provider records.

### 9.1 Idempotent Seeding
The seeder is executed from `services/api`:
```bash
cd services/api
go run ./cmd/seed
```
- **Atomicity & Fail-Closed Behavior:** All database writes are validated. Any failure immediately terminates the process with a non-zero exit code.
- **Idempotency:** Re-running `go run ./cmd/seed` checks existing references (`DEMO-M4421`, `DEMO-W9821`, `DEMO-V1082`) and creates zero duplicate records.
- **Dual-Custody Membership:** The compliance officer account (`officer@claimbridge.internal`) is automatically verified as an active `ADMIN` member of the demonstration organization (`demoOrg`), ensuring dual-custody approval workflows function seamlessly in the UI.

### 9.2 Safe Scoped Fixture Reset
To reset demo data cleanly:
```bash
cd services/api
go run ./cmd/seed -reset
```
- **Scoped Purge:** The reset command positively identifies records via `is_synthetic_fixture = true`.
- **Zero Cross-Contamination:** Non-demo user accounts, real organizations, and genuine payout cases (`is_synthetic_fixture = false`) are strictly preserved.
- **Dependency-Order Cleanup:** Foreign-key relationships (investigations, status snapshots, and operations) are cleaned up cleanly via foreign key cascade.

### 9.3 Server-Side Mutation Protection
All synthetic fixtures are protected server-side against accidental or malicious dispatch to PayPal:
- **Forbidden Operations:** `SubmitPayoutOperation`, `SyncPayoutCase`, and `CancelPayoutOperation` reject synthetic fixtures with HTTP 403 Forbidden (`code: FIXTURE_EXTERNAL_OPERATION_FORBIDDEN`).
- **Network Safety:** Zero outbound network requests reach PayPal's servers for fixture records.
- **UI Indicators:** The frontend displays prominent banners (`SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION`) and disables submission/cancellation buttons.

### 9.4 Evaluation Demo Mode Configuration
By default, the evaluator quick-start credentials banner on the landing page is hidden to maintain clean production semantics. To enable the banner for local evaluation:
```bash
# In apps/web:
NEXT_PUBLIC_DEMO_MODE=true npm run dev
```

---

## Step 10: Automated End-to-End Browser Testing with Playwright

ClaimBridge includes an automated Playwright browser test suite in `apps/web/e2e/` that verifies the entire system end-to-end against real browser sessions.

### 10.1 Prerequisites
Ensure the Go API server and Next.js frontend are running locally:
```bash
# Terminal 1: Database (PostgreSQL)
docker compose -f infra/docker-compose.yml up -d

# Terminal 2: Seed evaluation fixtures
cd services/api && go run ./cmd/seed -reset

# Terminal 3: Start Go API Gateway
cd services/api && go run ./cmd/server

# Terminal 4: Start Web Frontend with Demo Mode
cd apps/web && NEXT_PUBLIC_DEMO_MODE=true npm run dev
```

### 10.2 Running Browser Tests
From the `apps/web` directory:
```bash
cd apps/web

# Install Chromium browser (first-time only)
npx playwright install chromium

# Run all end-to-end browser tests
npm run test:e2e
```

### 10.3 Scenarios Verified by E2E Suite
1. **Health & Landing Page Truthfulness (`e2e/health.spec.ts`):** Verifies "Sandbox Prototype" branding, lack of false release claims, API Gateway reachability probe, and partitioned PayPal Sandbox OAuth status.
2. **Authentication & Roles (`e2e/auth.spec.ts`):** Tests protected route redirect to `/login`, invalid credential rejection, seeded operator login, compliance officer login, and demo evaluator banner visibility.
3. **Synthetic Fixture Integrity (`e2e/fixtures.spec.ts`):** Asserts `DEMO-W9821` displays the `SIMULATED` badge, prominent provenance alert banner, pre-seeded synthetic investigation report viewable offline without Ollama, and disabled PayPal mutation controls.
4. **Policy Enforcement & Dual-Custody Workflow (`e2e/policy-workflow.spec.ts`):** Verifies operator creating a $350.00 case, self-approval prevention fail-closed, distinct compliance officer approval, that approved operations are not auto-submitted, and honest offline AI error handling.
5. **Server-Side Mutation Guards (`e2e/mutation-guards.spec.ts`):** Direct HTTP API requests attempting to submit, sync, or cancel a fixture operation return HTTP 403 `FIXTURE_EXTERNAL_OPERATION_FORBIDDEN` with zero outbound PayPal calls.

