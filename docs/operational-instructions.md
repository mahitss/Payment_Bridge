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
This verifies that your Client ID and Secret can obtain an OAuth2 Bearer token from PayPal's sandbox servers:

```bash
cd services/api
go run ./cmd/paypal-smoke
```

**Expected Output:**
```
[INFO] ClaimBridge PayPal Sandbox Smoke-Test Tool
[INFO] Loading and validating configuration...
[INFO] Environment: sandbox | Base URL: https://api-m.sandbox.paypal.com
[INFO] Testing OAuth2 client credentials authentication...
[SUCCESS] OAuth2 Authentication Succeeded!
   - Token Type: Bearer
   - Expires In: 32399 seconds
   - App ID: APP-80W284485P519543T
   - Raw Token: [REDACTED_ACCESS_TOKEN]
[SUCCESS] PayPal Sandbox API connectivity verified safely.
```

### 4.2 Querying an Existing Sandbox Payout Batch
If you have an existing batch ID from previous testing:

```bash
go run ./cmd/paypal-smoke -batch-id <YOUR_PAYOUT_BATCH_ID>
```

### 4.3 Safe Test Payout Creation (Explicit Flag Required)
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

If `-confirm-sandbox-only` is omitted or `PAYPAL_ENV != sandbox`, execution aborts immediately.

---

## Step 5: Troubleshooting Common Sandbox Issues

| Issue | Root Cause | Fix |
|---|---|---|
| `invalid_client` HTTP 401 | Incorrect Client ID or Secret, or Live credentials used in Sandbox URL. | Double check developer dashboard; ensure Sandbox toggle was selected. |
| `FEATURE_NOT_AVAILABLE` | Payouts permission not enabled on the Sandbox Business account. | Go to PayPal Developer Dashboard -> Sandbox Accounts -> View/Edit Account -> Enable Payouts. |
| `INSUFFICIENT_FUNDS` | Sandbox Business account has $0 balance. | Go to Sandbox Accounts -> Account Details -> Funding -> Add balance to PayPal wallet. |
| `Ambiguous Timeout` | Network interruption during payout dispatch. | Do NOT rerun with new batch ID. Query status using `-batch-id <id>`. |
