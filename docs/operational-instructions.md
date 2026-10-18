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
3. Verify that `PAYPAL_ENV` is strictly `sandbox`. Any value set to `production` will be refused by the runtime.

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

### 4.3 Querying an Existing Sandbox Payout Batch (Read-Only)
If you have an existing batch ID from sandbox activity:

```bash
go run ./cmd/paypal-smoke -batch-id <YOUR_PAYOUT_BATCH_ID>
```

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

### 6.2 Verify Model Readiness
Run the pre-demo diagnostic tool to verify that Ollama is reachable and the model is loaded:

```bash
cd services/api
go run ./cmd/readiness
```

**Expected AI Readiness Output:**
```
------------------------------------------------------------------
 AI INVESTIGATION PROVIDER CONFIGURATION:
   Enabled:     true
   Provider:    ollama
   Model:       llama3.2
   Endpoint:    http://127.0.0.1:11434
   Host Reachable  : true
   Model Available : true
   Outcome         : PASS (Genuine local llama3.2 inference ready)
==================================================================
```

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

