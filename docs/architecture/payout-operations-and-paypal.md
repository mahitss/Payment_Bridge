# ClaimBridge Architecture: Payout Operations & PayPal Sandbox Integration

## Mission 003 Foundation & Baseline Gate

### 1. Pre-Flight Baseline Results
- **Rust Deterministic Policy Engine**: 12/12 unit tests passing (`cargo test`).
- **Go API Gateway & Auth Layer**: All 8 packages passing unit tests with race detection (`go test -v ./...`).
- **Mission 002 Acceptance Scenarios**: 12/12 end-to-end acceptance scenarios passing (`services/api/cmd/acceptance`).
- **Web Build**: Next.js 14 App Router compiling with 0 type errors.

---

## 2. Core Architectural Model

```
+-----------------------------------------------------------------------------------+
|                                 Next.js Web (apps/web)                            |
|  - /app: Case counts, connection status, overview metrics                         |
|  - /app/payouts: Filterable, paginated payout cases list                          |
|  - /app/payouts/new: Validated case intake & payout staging                       |
|  - /app/payouts/[id]: Status timeline, dual-approval, PayPal batch/item details   |
|  - /app/settings/integrations: Live PayPal sandbox authentication verification    |
+-----------------------------------------------------------------------------------+
                                          |
                         (Authenticated HttpOnly Cookie)
                                          v
+-----------------------------------------------------------------------------------+
|                             Go API Gateway (services/api)                         |
|  - Authenticated session enforcement & multi-tenant organization boundary checks  |
|  - Durable operation lifecycle coordinator                                        |
|  - Separation of duties (creator cannot self-approve if dual approval is required) |
|  - Persistent idempotency keys: sender_batch_id & sender_item_id                  |
|  - Ambient timeout & ambiguous response reconciliation engine                     |
+-----------------------------------------------------------------------------------+
         |                                                 |
         v                                                 v
+-----------------------------+           +-----------------------------------------+
| Rust Policy (services/policy)|           |  PayPal Payouts REST (Sandbox v1)       |
| - Pure state transitions    |           |  - POST /v1/payments/payouts            |
| - Dual-approval threshold   |           |  - GET  /v1/payments/payouts/{id}       |
| - Maximum ceiling ($500.00) |           |  - GET  /v1/payments/payouts-item/{id}  |
| - Cancellation eligibility  |           |  - POST /v1/payments/payouts-item/...   |
| - Read-only investigation   |           |         .../cancel                      |
+-----------------------------+           +-----------------------------------------+
                                          |
                                          v
+-----------------------------------------------------------------------------------+
|                            PostgreSQL 16 Multi-Tenant Data Store                  |
|  - payout_cases (organization_id, reference, recipient, amount_cents, status)     |
|  - payout_operations (sender_batch_id, sender_item_id, status, provider_ids)      |
|  - payout_status_snapshots (normalized raw provider snapshots)                   |
|  - audit_events (reusable immutable audit trail)                                  |
+-----------------------------------------------------------------------------------+
```

---

## 3. Payout Operation Lifecycle & State Machine

```
              [ DRAFT / CASE CREATED ]
                         |
                         v
                [ PREPARED OPERATION ]
                         |
           (Dual Approval Required?)
             /                     \
           YES                      NO
           /                         \
          v                           v
 [ AWAITING_APPROVAL ]           [ APPROVED ]
          |                           |
    (Approved by                      |
    different user)                   |
          |                           |
          +------------+--------------+
                       |
                       v
                 [ SUBMITTING ]  <-- Transactionally persisted with sender_batch_id
                       |
        +--------------+---------------+
        |                              |
 (PayPal 201 Created)           (Timeout / 5xx)
        |                              |
        v                              v
   [ SUBMITTED ]            [ RECONCILIATION_REQUIRED ]
        |                              |
 (Poll / Webhook / Sync)          (Explicit Sync / Probe)
        |                              |
        +--------------+---------------+
                       |
         +-------------+-------------+
         |                           |
   (Item SUCCESS)             (Item FAILED)
         |                           |
         v                           v
   [ SUCCEEDED ]                 [ FAILED ]

   (Item UNCLAIMED -> Cancel Action)
         |
         v
   [ CANCELLED ]
```

---

## 4. Safety Invariants & Boundary Guards

1. **Deterministic Minor Units**: All money amounts are stored and calculated as 64-bit signed integers in cents (minor units). Floating point arithmetic is strictly forbidden.
2. **Pre-Flight Operation Persistence**: The `sender_batch_id` and `sender_item_id` MUST be committed to PostgreSQL before the HTTP request to PayPal is initiated.
3. **Ambiguous Response Safety**: Network timeouts, socket disconnections, and 5xx errors from PayPal MUST transition the operation to `RECONCILIATION_REQUIRED`. The system never creates a new batch ID or automatically retries with new identifiers.
4. **Separation of Duties (Dual Custody)**: If a payout requires dual approval (`amount > $250.00` or `risk_score > 20.0`), the user who created or initiated the case cannot be the user who approves it. Self-approval is rejected by both the Go authorization layer and the Rust policy engine.
5. **Cancellation Eligibility**: Only payout items currently reported by PayPal in `UNCLAIMED` status may be cancelled. Once `SUCCESS`, `FAILED`, or `CANCELLED`, cancellation attempts are rejected.
6. **Multi-Tenant Isolation**: Every database query and mutation is strictly filtered by `organization_id`. Cross-tenant ID access returns HTTP 404 or HTTP 403.
