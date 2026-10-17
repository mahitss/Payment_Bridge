# ADR-002: Deterministic Policy Engine Architecture and Fail-Closed Invariants

## Status
Accepted

## Context
Automated claims adjudication systems that rely solely on LLMs or dynamic scripting languages risk prompt injection, non-deterministic outputs, and race conditions where payouts are disbursed beyond authorized thresholds. Financial policies (maximum single payout, cumulative daily limits, required dual-approval tiers, permitted recipient currency, and state transition graphs) must be completely invariant to model hallucinations or network fluctuations.

## Decision
We implement the policy validation engine in Rust (`services/policy`), designed under the following architectural tenets:
1. **Purity and Determinism**: The policy engine executes with zero direct network I/O and zero non-deterministic system clock dependencies during evaluation. Inputs are passed as immutable structs.
2. **Fail-Closed Semantics**: If any field is missing, out-of-bounds, unparseable, or fails invariant checks, the engine MUST return a `Denied` or `Escalated` decision with an explicit violation code. It can never fall through to `Approved`.
3. **Explicit State Machine**: Transitions between claim states (e.g., `Draft` -> `PendingReview` -> `AwaitingPolicyCheck` -> `Approved` -> `PayoutScheduled`) are mathematically validated. Any illegal jump is rejected.
4. **Autonomous Dual-Custody Escalation**: Payouts exceeding configurable thresholds (e.g., $250.00 USD) or risk scores exceeding 20.0 automatically trigger an `Escalated` state requiring multi-party human authorization.

## Consequences
### Positive
- Formal verification of safety invariants at compile time and unit test time.
- Prevents malicious prompt injection attacks from manipulating payout execution.
- High performance and deterministic evaluation times (< 1 millisecond).

### Negative
- Policy changes require compiling and deploying the Rust binary rather than modifying dynamic database scripts.
