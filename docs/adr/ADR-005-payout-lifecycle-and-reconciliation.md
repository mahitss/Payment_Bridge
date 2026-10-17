# ADR-005: Payout Lifecycle State Machine, Dual-Custody Approval, and Ambiguous Reconciliation

## Status
Accepted

## Context
Mission 003 requires implementing the core money-moving product workflow: creating payout cases, issuing explicitly authorized sandbox payouts to PayPal, tracking batch and item statuses, and providing safe cancellation.
Because network boundaries between ClaimBridge and PayPal can fail or time out while a transaction is in flight, ClaimBridge must never assume an unreturned or timed-out request failed. Automatically creating a replacement batch with a new sender identifier could lead to duplicate money disbursement.
Furthermore, security compliance mandates separation of duties (dual custody) for transactions exceeding threshold limits, and strict tenancy isolation so one organization cannot view, approve, or cancel another organization's payouts.

## Decisions

1. **Integer Minor Units**:
   - Amounts are strictly stored as 64-bit integers in minor units (e.g., cents for USD: $250.00 = 25000 cents).
   - Floating point arithmetic is prohibited across the backend, database, and policy engine.

2. **Durable Pre-Creation of Sender Identifiers**:
   - Before an HTTP call to PayPal's REST API is made, ClaimBridge inserts a `payout_operations` record in PostgreSQL with state `SUBMITTING`, storing deterministic unique keys: `sender_batch_id` and `sender_item_id`.
   - If the database write fails, no request is sent to PayPal.

3. **Ambiguous State & Reconciliation Protocol**:
   - If an HTTP request to PayPal times out, encounters a network reset, or receives an HTTP 500/503 response, the operation state is updated to `RECONCILIATION_REQUIRED`.
   - The system NEVER generates a new batch ID or retries blindly.
   - An explicit synchronization endpoint (`POST /api/v1/payout-cases/{id}/sync`) queries PayPal using the persisted `sender_batch_id` to determine whether the batch was actually received and queued, or if it was never created.

4. **Dual Custody (No Self-Approval)**:
   - Claims with `amount_cents > 25000` ($250.00 USD) or `risk_score > 20.0` require dual approval.
   - The user who created the case or initiated the payout operation cannot approve it.
   - Self-approval attempts are blocked with HTTP 403 `SELF_APPROVAL_FORBIDDEN`.

5. **PayPal Item Cancellation Eligibility**:
   - PayPal Payouts API permits cancellation of an item only when its status is `UNCLAIMED`.
   - Attempting to cancel an item that is `SUCCESS`, `PENDING`, `ONHOLD`, or already `CANCELLED` is rejected before calling the provider, with a fallback error handler for race conditions.

6. **Multi-Tenant Scoping**:
   - All payout cases, operations, and audit records are bound to `organization_id`.
   - Access to cases belonging to another organization is denied by database foreign keys and Go service authorization guards.

## Consequences
- **Positive**: Complete defense against duplicate payouts, race conditions, and unauthorized self-disbursements.
- **Negative**: Operator intervention or explicit sync is required when network dropouts occur, but this trade-off is required for financial safety.
