# MISSION 040.8.1 — CI Audit & Reproducible Build Verification Report

**Project:** ClaimBridge — `Payment_Bridge`  
**Repository:** `https://github.com/mahitss/Payment_Bridge`  
**Branch:** `feat/paypal-sandbox-preflight-and-config`  
**Initial HEAD SHA:** `f2aaf070532021b5851005ec81b334813e10f91e`  
**Audited & Fixed HEAD SHA:** `964c2a39a8e0f6ca423dbe024225d3fa969eb21d`  
**Audit Timestamp:** 2026-11-09T11:02:00Z  
**Author:** Staff Financial Systems Engineer, Application Security Engineer and Reliability Lead  

---

## 1. Executive Summary

This mission audited GitHub Actions CI configuration, investigated historical CI failures on `main` and feature branches, established complete local reproducibility across all monorepo test suites, resolved the verified root cause behind the failing CI acceptance step, and preserved all financial and environment safety boundaries.

### Key Audit Findings
1. **Why Commit `f2aaf07` (Mission 040.7) Had No Reported Check Runs:**  
   GitHub Actions workflow trigger in `.github/workflows/ci.yml` strictly restricts `push` events to `branches: [main, master]` and `pull_request` events to `branches: [main, master]`. Since PR #26 was merged as commit `b4b24a02`, there was no open pull request targeting `main`, and branch `feat/paypal-sandbox-preflight-and-config` is not named `main` or `master`. GitHub Actions intentionally does not trigger workflows on un-targeted feature branch pushes under this configuration.
2. **Root Cause of Failed CI Run on `main` (Run ID `42414493714`):**  
   In both Run ID `42414493714` (commit `b4b24a02` on `main`) and Run ID `42414115544` (commit `e7306adb` on PR #26), 5 of 6 jobs passed (`Next.js Web Frontend`, `Rust Deterministic Policy Engine`, `Security & Secret Audit`, `Docker Compose Offline Evaluator Verification`, `End-to-End Browser & Integration Tests`). The only failing job was `Go API (Gate & Orchestrator)` at step `Run Full Acceptance Suite (32 Scenarios)` (`go run ./cmd/acceptance`).  
   **Root Cause:** In Mission 033/034, `EnsurePayoutsAuthorization` was introduced in `services/api/internal/paypal/client.go` to enforce that the OAuth access token explicitly includes the mandatory payouts scope (`https://uri.paypal.com/payments/payouts`). The test double server (`fakePayPalServer` and `timeoutPayPalServer` in `services/api/cmd/acceptance/main.go`) returned mock token responses missing the `"scope"` key. Consequently, Scenarios 20, 21, and 24 failed with `HTTP 403 Forbidden` (`PAYPAL_SCOPE_MISSING`).
3. **Verified Minimal Safe Fix:**  
   Added `"scope": "https://uri.paypal.com/payments/payouts"` to both mock OAuth token handlers in [services/api/cmd/acceptance/main.go](file:///C:/Users/pc/OneDrive/Desktop/claim%20bridge/services/api/cmd/acceptance/main.go). Re-running `go run ./cmd/acceptance` resulted in **all 32 scenarios passing with exit code 0**.
4. **Local Reproducibility & Database Isolation:**  
   All unit, integration, lint, policy, and frontend build suites were executed locally. Disposable test database `claimbridge_test` was used for PostgreSQL integration tests, ensuring zero mutation of persistent baseline data in `claimbridge_dev` (exactly 6 payout operations remain intact).

---

## 2. GitHub Actions Investigation (Phase 2)

### Trigger Configuration Audit (`.github/workflows/ci.yml`)
```yaml
name: ClaimBridge CI

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]
```
- **Finding:** Workflows are triggered exclusively on `push` to `main`/`master` or `pull_request` targeting `main`/`master`.
- **Reason for Missing Check Runs on `f2aaf07`:** Commit `f2aaf07` was pushed to `feat/paypal-sandbox-preflight-and-config`. PR #26 from this branch was already closed/merged. Without an open PR targeting `main`, GitHub Actions does not evaluate pushes to this branch.

### Historical CI Runs Audit (Recent 5 Runs via GitHub Public API)

| Run ID | Event | Branch | Commit SHA | Job Status | Conclusion | Failing Job / Step |
| :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| **42414493714** | `push` | `main` | `b4b24a02` | Completed | **Failure** | `Go API` / `Run Full Acceptance Suite (32 Scenarios)` |
| **42414115544** | `pull_request` (#26) | `feat/paypal-sandbox...` | `e7306adb` | Completed | **Failure** | `Go API` / `Run Full Acceptance Suite (32 Scenarios)` |
| **42405369668** | `push` | `main` | `6e351fc1` | Completed | **Failure** | `Go API` / `Run Full Acceptance Suite (32 Scenarios)` |
| **42404442661** | `pull_request` (#25) | `feat/paypal-sandbox...` | `c66adc10` | Completed | **Failure** | `Go API` / `Run Full Acceptance Suite (32 Scenarios)` |
| **42233460795** | `push` | `main` | `03020db7` | Completed | **Failure** | `Go API` / `Run Full Acceptance Suite (32 Scenarios)` |

### Detailed Job Breakdown of Failed Run 42414493714

1. **Job 1: End-to-End Browser & Integration Tests:** `SUCCESS` (Playwright E2E passed)
2. **Job 2: Rust Deterministic Policy Engine:** `SUCCESS` (`cargo check`, `cargo test` passed)
3. **Job 3: Security & Secret Audit:** `SUCCESS` (Secret scanning, forbidden patterns passed)
4. **Job 4: Docker Compose Offline Evaluator Verification:** `SUCCESS` (Offline compose passed)
5. **Job 5: Next.js Web Frontend:** `SUCCESS` (`next lint`, `next build` passed)
6. **Job 6: Go API (Gate & Orchestrator):** `FAILURE`
   - Step: `Set up Go` -> `SUCCESS`
   - Step: `Go Vet` -> `SUCCESS`
   - Step: `Run Go Unit Tests` (`go test -v -race ./...`) -> `SUCCESS`
   - Step: `Run PayPal Webhook, Payouts & Database Race Detector` -> `SUCCESS`
   - Step: `Run PostgreSQL Seeder Integration Test` -> `SUCCESS`
   - Step: `Compile Binaries` -> `SUCCESS`
   - Step: `Run Full Acceptance Suite (32 Scenarios)` -> **`FAILURE (Exit Code 1)`**

---

## 3. Local Reproducibility & Test Execution Matrix (Phase 3)

All documented CI commands were executed locally against the relevant toolchains:

| CI Stage / Command | Working Directory | Command Executed | Tests Passed | Exit Code | Result |
| :--- | :--- | :--- | :---: | :---: | :---: |
| **Go Static Analysis** | `services/api` | `go vet ./...` | 0 warnings | 0 | **PASS** |
| **Go Full Unit Suite** | `services/api` | `go test ./...` | 16 packages ok | 0 | **PASS** |
| **PayPal Verifier Tests** | `services/api` | `go test -v ./internal/paypal -run TestWebhookVerifier` | 27 / 27 | 0 | **PASS** |
| **Mission 040.7 Webhook Matrix** | `services/api` | `go test -v ./internal/payouts -run TestWebhook_Mission040_7` | 1 / 1 (multi-assert) | 0 | **PASS** |
| **Seeder Postgres Integration** | `services/api` | `go test -v -run TestSeeder_PostgresIntegration ./internal/seeder` | 1 / 1 | 0 | **PASS** |
| **Database Integration Suite** | `services/api` | `go test -v ./internal/db/...` (with `TEST_DATABASE_URL`) | 12 / 12 (5 pg + 7 mock) | 0 | **PASS** |
| **Acceptance Suite (32 Scenarios)** | `services/api` | `go run ./cmd/acceptance` | 32 / 32 | 0 | **PASS** |
| **Binary Compilation (7 Binaries)** | `services/api` | `go build -v -o bin/... ./cmd/...` (7 targets) | 7 / 7 built | 0 | **PASS** |
| **Rust Policy Engine Check** | `services/policy` | `cargo check --verbose` | 0 errors | 0 | **PASS** |
| **Rust Policy Deterministic Tests**| `services/policy` | `cargo test --verbose` | 17 / 17 | 0 | **PASS** |
| **Frontend ESLint Check** | `apps/web` | `npm run lint` | 0 errors / 0 warnings | 0 | **PASS** |
| **Frontend Production Build** | `apps/web` | `npm run build` | 15 / 15 routes | 0 | **PASS** |
| **Go Race Detector** | `services/api` | `go test -race ./...` | N/A (MinGW CGO missing `cc1` on Windows host) | 1 | **NOT RUN (OS LIMITATION)** |
| **Live Playwright E2E** | `apps/web` | `playwright test` | N/A (Port 3000 web server not running) | N/A | **NOT RUN** |

---

## 4. Root Cause Analysis & Verified Fix (Phase 4)

### Failure Trace in Acceptance Scenarios
```text
[FAIL] Scenario 20: Submit payout operation to PayPal sandbox
      Detail: Submission failed: 403 (map[code:PAYPAL_SCOPE_MISSING error:PayPal OAuth token missing required scope https://uri.paypal.com/payments/payouts operation:<nil>])
[FAIL] Scenario 21: Synchronize status with PayPal Sandbox
      Detail: Sync failed: 409 (map[code:NO_PAYPAL_BATCH_ID error:cannot sync payout case: no paypal payout batch id known...])
[FAIL] Scenario 24: Ambiguous network timeout transitions to RECONCILIATION_REQUIRED
      Detail: Expected RECONCILIATION_REQUIRED, got op status 'PREPARED' (HTTP 403, map[code:PAYPAL_SCOPE_MISSING...])
```

### Explanation
When `services/api/cmd/acceptance/main.go` sets up `fakePayPalServer` and `timeoutPayPalServer`, it mocks the PayPal OAuth2 token endpoint `/v1/oauth2/token`. The service client parses the token's `scope` string. When `"scope"` was omitted, `c.cachedTokenScope` was empty. The preflight check `c.EnsurePayoutsAuthorization()` returned `ErrMissingPayoutsScope`, halting submissions at the boundary before reaching the payout dispatch mock.

### Exact Code Correction
File: `services/api/cmd/acceptance/main.go`
```diff
@@ -58,6 +58,7 @@ func main() {
 				"access_token": "fake-sandbox-token-acceptance",
 				"token_type":   "Bearer",
 				"app_id":       "APP-SANDBOX-ACCEPTANCE",
+				"scope":        "https://uri.paypal.com/payments/payouts",
 				"expires_in":   3600,
 			})
@@ -946,6 +947,7 @@ func main() {
 				"access_token": "fake-sandbox-token-acceptance",
 				"token_type":   "Bearer",
 				"app_id":       "APP-SANDBOX-ACCEPTANCE",
+				"scope":        "https://uri.paypal.com/payments/payouts",
 				"expires_in":   3600,
 			})
```

### Verification
Following the change:
- `go run ./cmd/acceptance` executed all 32 scenarios.
- All 32 scenarios reported `[PASS]`.
- Exit code: `0`.
- Committed as SHA `964c2a3`.

---

## 5. Financial Baseline & Safety Verification

- Database `claimbridge_dev` operation count: **Exactly 6 operations**.
- All 6 historical payout operations from Mission 040.6 remain strictly untouched.
- Disposable database `claimbridge_test` was used for `TEST_DATABASE_URL` integration tests.
- Zero live payouts, refunds, cancellations, retries, or credential changes occurred.

---

## 6. Changed Files & Commit Metadata

- **Modified Files:** [services/api/cmd/acceptance/main.go](file:///C:/Users/pc/OneDrive/Desktop/claim%20bridge/services/api/cmd/acceptance/main.go) (+2 lines)
- **Commit SHA:** `964c2a39a8e0f6ca423dbe024225d3fa969eb21d`
- **Commit Message:** `fix(ci): add required payouts scope to acceptance mock OAuth token responses`
- **Untracked Preserved Files:** `MISSION_040_7_RECOVERY_REPORT.md`, `apps/web/run_local_web.ps1`, `apps/web/tsconfig.tsbuildinfo`, `scratch/`, `services/api/run_local_server.ps1`

---

## 7. Final Status Matrix

```text
REPOSITORY INTEGRITY:           PASS
GITHUB ACTIONS TRIGGER AUDIT:   PASS (Explained why feature branch pushes have no runs)
HISTORICAL CI FAILURE AUDIT:    PASS (Pinpointed Run 42414493714 to acceptance suite)
ROOT CAUSE IDENTIFIED:          PASS (PAYPAL_SCOPE_MISSING in acceptance OAuth mock)
MINIMAL SAFE FIX IMPLEMENTED:   PASS (Added payouts scope to acceptance mocks)
GO COMPILATION (7 BINARIES):    PASS (Exit code 0)
GO VET:                         PASS (Exit code 0)
GO UNIT TESTS (16 PACKAGES):    PASS (Exit code 0)
PAYPAL & MISSION 040.7 PROOFS:  PASS (Exit code 0)
SEATION INTEGRATION (DISPOSABLE):PASS (Exit code 0)
DB INTEGRATION (DISPOSABLE):    PASS (Exit code 0)
ACCEPTANCE SUITE (32/32):       PASS (Exit code 0)
RUST POLICY ENGINE:             PASS (Exit code 0)
FRONTEND ESLINT:                PASS (Exit code 0)
FRONTEND PRODUCTION BUILD:      PASS (Exit code 0)
FINANCIAL BASELINE INTEGRITY:   UNCHANGED (6/6 operations intact)
REMOTE CHECK RUN REPORTING:     NOT RUN ON GITHUB (No open PR targeting main)
```

