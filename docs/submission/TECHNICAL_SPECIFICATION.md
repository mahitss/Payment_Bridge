# ClaimBridge — Technical Specification & Architecture Manual

**Target Audience:** Technical Judges, System Architects, Application Security Reviewers  
**Revision:** 1.0 (Hackathon Release)  

---

## 1. System Architecture

```text
+---------------------------------------------------------------------------------------+
|                                    PRESENTATION LAYER                                 |
|                                Next.js 14 App Router (Node.js)                        |
|                     TypeScript • Tailwind CSS • Standalone Output                     |
+-------------------------------------------+-------------------------------------------+
                                            |
                         HTTPS / Secure HttpOnly Session Cookies
                                            v
+---------------------------------------------------------------------------------------+
|                                  ORCHESTRATION GATEWAY                                |
|                                       Go 1.22+ API                                    |
|                                                                                       |
|  - Middleware: Recovery, Security Headers (CSP, Nosniff), 1MB Body Limit, Timeout, CORS|
|  - Auth Service: Argon2id password hashing, SHA-256 session tokens, Org Scoping       |
|  - Payouts Service: Payout cases, state transitions, dual approval, sync, cancel      |
|  - Investigations Service: Normalized snapshot aggregator & audit logger             |
|  - AI Grounding Engine: JSON schema validation & snapshot ID set-membership assertion  |
+-------------------+-------------------------------+-------------------+---------------+
                    |                               |                   |
                    v                               v                   v
+-----------------------+       +-----------------------+   +-----------------------+
|   PERSISTENCE LAYER   |       | DETERMINISTIC ENGINE  |   |    AI INFERENCE       |
|     PostgreSQL 16     |       |    Rust 1.77+ Engine  |   |    Local Ollama       |
|                       |       |                       |   |    (Llama 3.2)        |
| - Foreign keys & org  |       | - Pure functions      |   | - Server-side only    |
|   scoping             |       | - Fail-closed checks  |   | - 30s timeout cap     |
| - Integer minor cents |       | - Mathematical bounds |   | - Output token limits |
| - Atomic transactions |       | - $500 hard ceiling   |   | - Fail-closed 503     |
+-----------------------+       +-----------+-----------+   +-----------------------+
                                            |
                                            v
                                +-----------------------+
                                |  PAYPAL PAYOUTS REST  |
                                |      Sandbox v1       |
                                |                       |
                                | - OAuth2 client grant |
                                | - Batch & item sync   |
                                | - Unclaimed cancel    |
                                +-----------------------+
```

---

## 2. Evidence Grounding Algorithm (`ValidateGrounding`)

To eliminate LLM hallucinations in financial audits, ClaimBridge implements a set-membership assertion algorithm in Go (`services/api/internal/ai/schema.go`):

```go
func ValidateGrounding(output *InvestigationOutput, validIDs map[string]struct{}) error {
    for i, finding := range output.Findings {
        for _, ref := range finding.EvidenceReferences {
            cleanRef := strings.TrimSpace(ref)
            if _, exists := validIDs[cleanRef]; !exists {
                return fmt.Errorf("finding[%d] references unknown or fabricated identifier %q", i, ref)
            }
        }
    }
    return nil
}
```

### Identifier Set Aggregation
Before calling the model, the backend builds an immutable map of valid IDs:
1. `case_id`: Stored payout case UUID.
2. `operation_id`: Active or historical operation UUIDs.
3. `sender_batch_id`: Pre-persisted client-side idempotency key (`CB-BATCH-CASE-...`).
4. `paypal_payout_batch_id`: Provider batch ID returned by PayPal.
5. `paypal_payout_item_id`: Provider item ID returned by PayPal.
6. `snapshot_id`: Recorded status snapshot UUIDs.

If the model outputs an invented reference (e.g., `fabricated-batch-id-9999`), the entire investigation is marked invalid, logged, and rejected with HTTP 422 `INVALID_EVIDENCE_REFERENCE`.

---

## 3. Deterministic Safety Boundaries (Rust Policy Engine)

The policy engine (`services/policy/src/engine.rs`) executes invariant checks in pure, deterministic Rust:
1. **Monetary Ceiling Rule (`RULE_MAX_AMOUNT_CEILING`):** Payouts > $500.00 USD are rejected fail-closed.
2. **Dual-Custody Mandate (`RULE_DUAL_APPROVAL_MANDATORY`):** Payouts > $250.00 USD or risk score > 20.0 require approval by a second authorized officer.
3. **Currency Invariant (`RULE_CURRENCY_USD_ONLY`):** Currencies other than USD are rejected.
4. **Cancellation Eligibility (`RULE_PAYPAL_CANCEL_UNCLAIMED_ONLY`):** Only transactions in `UNCLAIMED` status may be cancelled. If the AI model recommends cancelling a `SUCCESS` or `FAILED` item, Go cross-references the policy rule and flags `deterministic_policy_permitted = false`.

---

## 4. Cryptographic & Session Architecture

- **Password Hashing:** Argon2id with 64MB memory, 3 iterations, 4 parallelism threads, 32-byte cryptographically secure salt, and 32-byte key.
- **Session Tokens:** 32 bytes generated via `crypto/rand`. Only the SHA-256 hash is persisted in the `sessions` table.
- **Cookie Attributes:** `HttpOnly`, `SameSite=Lax`, and `Secure` flags prevent client-side JavaScript access.
- **Single-Use Tokens:** Email verification and password reset tokens enforce atomic single-use consumption in SQL (`UPDATE ... WHERE consumed_at IS NULL`).

---

## 5. PayPal REST API Integration

- **Client:** `services/api/internal/paypal/client.go`
- **Endpoints Utilized:**
  - `POST /v1/oauth2/token`
  - `POST /v1/payments/payouts`
  - `GET /v1/payments/payouts/{payout_batch_id}`
  - `GET /v1/payments/payouts-item/{payout_item_id}`
  - `POST /v1/payments/payouts-item/{payout_item_id}/cancel`
- **Safety Guarantees:**
  - Token caching with 60-second safety refresh buffer prevents redundant authorization calls.
  - Automatic stream regex redaction masks `Bearer [A-Za-z0-9._-]+` tokens in all logs.
  - `PAYPAL_ENV=sandbox` assertion causes immediate startup panic if production URLs are supplied.
