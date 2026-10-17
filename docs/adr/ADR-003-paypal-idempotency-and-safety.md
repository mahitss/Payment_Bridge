# ADR-003: PayPal REST Client, Idempotency, and Zero-Blind-Retries

## Status
Accepted

## Context
The PayPal Payouts REST API (`POST /v1/payments/payouts`) executes irrevocable fund disbursements. Network latency, socket drops, or HTTP 504 Gateway Timeouts frequently leave the client unsure whether PayPal received and processed the disbursement request. Retrying the call with a newly generated idempotency identifier (`sender_batch_id`) is a catastrophic failure mode that causes double payouts.

Furthermore, API access requires OAuth2 Client Credentials grant (`POST /v1/oauth2/token`). Accidental leakage of access tokens or client secrets into log files, bug trackers, or client responses presents an unacceptable security exposure.

## Decision
1. **Zero Blind Retries on Mutation**:
   - The Go HTTP client is strictly configured with `MaxRetries = 0` for payment creation mutations.
   - Every claim is bound to a single deterministic `sender_batch_id`:
     `sender_batch_id = fmt.Sprintf("claimbridge-%s", claimID)`
   - When a network timeout or error occurs during `POST /v1/payments/payouts`, the claim is transitioned to `PayoutReconciliationRequired`.
   - The orchestrator polls `GET /v1/payments/payouts/{payout_batch_id}` (or by sender batch ID) to verify true upstream status before taking any corrective action.
2. **Strict Redacted Transport**:
   - An HTTP round-tripper and logging middleware intercept all request/response streams and sanitize `Authorization` headers, `access_token` fields, and client secrets.
3. **Sandbox Guardrails**:
   - The configuration layer enforces `PAYPAL_ENV == "sandbox"`. If set to `production` during Phase 1, the service refuses to boot.
   - Payout creation is disabled by default in CLI smoke test tools unless the developer supplies `-confirm-sandbox-only`.

## Consequences
### Positive
- Double payouts are prevented even in unstable network environments.
- Credentials and access tokens are scrubbed from APM, console logs, and error traces.
- Protects developers against accidental live account charges.

### Negative
- Requires a polling or reconciliation worker to resolve ambiguous network states rather than instant retry.
