# ClaimBridge — Security Threat Model

## 1. Scope & Methodology

This threat model analyzes the ClaimBridge architecture under the **STRIDE** framework with particular focus on the automated payment boundary (PayPal Payouts API) and AI integration risks.

---

## 2. Threat Analysis Matrix

| Threat (STRIDE) | Attack Vector | Consequence | Mitigation in ClaimBridge |
|---|---|---|---|
| **Spoofing** | Attacker impersonates claimant or submits forged webhook from PayPal. | Unauthorized disbursement or status tampering. | 1. OAuth2 Bearer token authentication.<br>2. Verification of PayPal webhook signatures with PayPal root certificates.<br>3. Strict IP & domain verification. |
| **Tampering** | Claimant modifies claim amount or recipient email after policy approval. | Inflated payout or payout to attacker's account. | 1. Hash pinning: claim state is cryptographically signed upon policy approval.<br>2. Rust policy engine validates the immutable batch payload immediately prior to dispatch.<br>3. Relational DB state transitions are strictly ACID-enforced. |
| **Repudiation** | Adjuster denies approving a high-risk claim or system denies sending a payout. | Inability to audit unauthorized transfers. | 1. Append-only `audit_logs` table in PostgreSQL.<br>2. Audit records capture actor identity, timestamps, inputs, rule execution results, and PayPal batch IDs. |
| **Information Disclosure** | PayPal client secret or access tokens leak into frontend logs, GitHub, or error traces. | Attacker drains sandbox/production treasury balance. | 1. Strict `.gitignore` on all `.env` and credential files.<br>2. Zero client secrets in frontend codebase or Next.js build bundle.<br>3. Structured log sanitizer regex-redacts tokens (`Bearer [A-Za-z0-9._-]+`) and secret keys.<br>4. Secrets passed only via environment variables. |
| **Denial of Service** | Upstream PayPal API rate limits or slow responses block API workers. | Exhaustion of Go goroutines or database connection pool. | 1. Bounded HTTP timeouts (15s max) on all external calls via Go `context.WithTimeout`.<br>2. Circuit breaker patterns for upstream service degradation.<br>3. Rate limiting on public-facing intake endpoints. |
| **Elevation of Privilege** | Prompt injection in LLM claim evidence tricks system into auto-approving claim. | AI grants malicious approval beyond normal risk limits. | 1. **Complete separation of concerns**: LLM outputs are treated as UNTRUSTED advice.<br>2. Rust Policy Engine is purely deterministic code and executes invariant checks regardless of what the LLM recommends. |

---

## 3. High-Risk Financial Attack Vectors

### 3.1 Payout Replay & Ambiguous Retries
- **Scenario**: Go API sends `POST /v1/payments/payouts` to PayPal. The network drops before receiving the response HTTP 201.
- **Vulnerability**: A naive retry loop generates a new batch ID and submits again, resulting in a **double payout**.
- **Defense**:
  - The `sender_batch_id` is derived deterministically from the persistent `claim_id`.
  - The record in PostgreSQL is set to `PENDING_DISPATCH` before the call is made.
  - If a timeout occurs, automatic retry with a new ID is **forbidden**. The system reconciles status using `GET /v1/payments/payouts/{batch_id}`.

### 3.2 Live Environment Accidental Exposure
- **Scenario**: Developer accidentally tests with production credentials or against `api-m.paypal.com`.
- **Vulnerability**: Real funds transferred during automated tests.
- **Defense**:
  - `PAYPAL_ENV=sandbox` is strictly asserted in configuration parsing.
  - If `PAYPAL_ENV` is set to `production` during Phase 1, Go API execution panics immediately at startup.
  - Test scripts run strictly against `https://api-m.sandbox.paypal.com`.

### 3.3 Token Leakage via APM and Console Logs
- **Scenario**: `curl` commands or HTTP response logs print `access_token` into stdout or APM tooling.
- **Defense**:
  - Custom `RedactedTransport` and `RedactedLogger` automatically replace token values with `[REDACTED_ACCESS_TOKEN]`.
