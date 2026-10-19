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

## Step 5: Troubleshooting Common Sandbox Issues

| Issue | Root Cause | Fix |
|---|---|---|
| `invalid_client` HTTP 401 | Incorrect Client ID or Secret, or Live credentials used in Sandbox URL. | Double check developer dashboard; ensure Sandbox toggle was selected. |
| `FEATURE_NOT_AVAILABLE` / HTTP 403 | Payouts permission not enabled on the Sandbox Business account. | Go to PayPal Developer Dashboard -> Sandbox Accounts -> View/Edit Account -> Enable Payouts. |
| `INSUFFICIENT_FUNDS` | Sandbox Business account has $0 balance. | Go to Sandbox Accounts -> Account Details -> Funding -> Add balance to PayPal wallet. |
| `Ambiguous Timeout` | Network interruption during payout dispatch. | Do NOT rerun with new batch ID. Query status using `-batch-id <id>`. |

---

## Step 6: Local Ollama Model Setup & Verification

ClaimBridge features genuine local model inference for payout investigations, requiring no paid API keys.

### 6.1 Install & Start Ollama
1. Download and install Ollama from [ollama.ai](https://ollama.ai).
2. Start the local daemon:
   ```bash
   ollama serve
   ```
3. Pull the configured local model (`llama3.2`):
   ```bash
   ollama pull llama3.2
   ```

### 6.2 Verify Model Readiness & System Gates
Run the pre-demo readiness diagnostic tool to verify that Ollama is reachable, the model is installed, and genuine non-mutating inference succeeds:

```bash
cd services/api
go run ./cmd/readiness -profile=submission
```

---

## Step 7: System Readiness Diagnostic Profiles & Gate Semantics

The readiness diagnostic tool (`services/api/cmd/readiness`) eliminates false-green reporting by evaluating discrete checks against explicit operational profiles.

### 7.1 Diagnostic Profiles

| Profile | Command | Intended Use | Required Checks | Optional / Informational Checks |
|---|---|---|---|---|
| **`core`** (default) | `go run ./cmd/readiness -profile=core` | Local backend & frontend development. | 1. PostgreSQL Persistence<br>2. Go API Health (`/healthz`)<br>3. Go API Readiness (`/readyz`)<br>4. Rust Deterministic Policy Engine<br>5. Next.js Web Frontend | 6. PayPal Sandbox OAuth<br>7. PayPal Read-Only Lookup<br>8. AI Reachability & Model<br>9. AI Model Inference Probe |
| **`submission`** (or `demo`) | `go run ./cmd/readiness -profile=submission` | Demo recording & hackathon evaluation. | 1. PostgreSQL Persistence<br>2. Go API Health (`/healthz`)<br>3. Go API Readiness (`/readyz`)<br>4. Rust Deterministic Policy Engine<br>5. Next.js Web Frontend<br>6. Genuine PayPal Sandbox OAuth<br>8. AI Reachability & Model<br>9. Genuine AI Model Inference Probe | 7. PayPal Read-Only Lookup (evaluated if `-batch-id` or `-item-id` supplied) |

### 7.2 Check Status Taxonomy

Every check outputs one of five explicit statuses:
- **`PASS`**: The check completed successfully and met all verification criteria.
- **`FAIL`**: The check executed and failed (e.g. non-200 HTTP status, timeout, malformed payload, or rejection).
- **`BLOCKED`**: The check could not execute due to a missing external/local prerequisite (e.g. unconfigured credentials or offline local daemon).
- **`WARN`**: Informational warning (e.g. mock provider configured in `core` profile).
- **`SKIPPED`**: Check was intentionally unrequested (e.g. no payout batch/item ID provided for lookup).

### 7.3 Truth in Testing Guarantees
1. **Mock Tests Do Not Verify Genuine Integration:** Mock providers and simulated endpoints in unit tests verify code syntax and error handling, but can never satisfy the `submission` profile.
2. **Model Availability Is Not Inference Verification:** Probing `/api/tags` or `/v1/models` verifies only that the daemon is reachable and the model tag exists. The `submission` profile requires executing a non-mutating synthetic inference probe and passing evidence grounding validation.
3. **OAuth Success Does Not Establish Payout Lookup:** Successful OAuth authentication verifies credentials with PayPal Sandbox, but does not prove payout creation or lookup. Payout lookups are evaluated independently.
4. **Current Environment Status:** In an environment without local `.env` sandbox keys or without a running Ollama daemon, running `-profile=submission` accurately reports `[STATUS: BLOCKED]` with Exit Code 1.

### 7.4 Strict API Storage Readiness (`/readyz`) Contract
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

### 7.5 Mock vs. Live Test Double Isolation
- **Controlled Testing Mechanism:** Tests using simulated HTTP servers (`httptest.NewServer`) inject test doubles via `paypal.NewTestClient` or `WithSimulatedBaseURL`.
- **Production Guardrail:** If `APP_ENV=production`, `WithSimulatedBaseURL` returns `ErrSimulatedEndpointForbidden` fail-closed.
- **Core Profile Isolation:** When `AI_PROVIDER=mock` is configured, running the `core` profile reports Check 9 as `WARN` (informational) without failing core readiness. In contrast, running the `submission` profile strictly reports `FAIL` and terminates with exit code 1.


### 6.3 Run an End-to-End Investigation in the Web UI
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

