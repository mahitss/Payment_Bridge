# ADR-001: Monorepo Architecture and Polyglot Service Separation

## Status
Accepted

## Context
ClaimBridge is a mission-critical platform designed to bridge AI-driven claims triage with real-world financial settlement via the PayPal Payouts API. Financial payouts require strict security, zero data loss, high auditability, and deterministic safety guarantees. Conversely, web user interfaces require rich reactivity and modern presentation ergonomics.

We evaluated single-language setups (all-Node/TypeScript or all-Python) versus a focused polyglot monorepo leveraging Go, Rust, and Next.js.

## Decision
We adopt a monorepo structure with three distinct responsibilities:
1. `apps/web`: Next.js (TypeScript, Tailwind CSS, shadcn/ui) for presentation. Zero credentials or secret tokens ever reach this layer.
2. `services/api`: Go for high-concurrency API orchestration, database access, PayPal REST client handling, structured logging, and bounded network operations.
3. `services/policy`: Rust for pure, deterministic financial safety rule evaluation with memory safety, fail-closed type invariants, and zero external network side effects.
4. `infra`: PostgreSQL 16 container definitions and infrastructure scripts.

Python is explicitly **excluded** as a mandatory runtime for core services to eliminate interpreter bloat, packaging instability, and non-deterministic concurrency risks. AI triage models are accessed via server-side HTTP interfaces behind a clean abstraction in `services/api`.

## Consequences
### Positive
- Strict separation of concerns between user interface, network orchestration, and policy decision logic.
- Type-safe, compiled binaries for all backend execution with lightning-fast cold starts and minimal container attack surfaces.
- Rust guarantees memory safety and mathematical determinism for financial limits.
- Clear trust boundaries preventing credential leakage to the browser.

### Negative
- Requires multi-language toolchains (Go, Cargo/Rust, Node.js) for local development and CI runners.
