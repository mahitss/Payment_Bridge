# ClaimBridge — Phase 1 Acceptance Criteria

## 1. Objective

Validate the core foundation, security invariants, deterministic policy engine, and PayPal sandbox integration feasibility before proceeding to full feature development.

---

## 2. Milestone 001 Criteria

### Criterion 1: Monorepo Architecture & Hygiene
- [x] Repository initialized with clear component separation:
  - `apps/web`: Next.js web interface.
  - `services/api`: Go HTTP backend and integration orchestrator.
  - `services/policy`: Rust deterministic safety policy engine.
  - `infra`: Local database and infrastructure definitions.
  - `docs`: Architecture, Threat Model, Operational Instructions, ADRs.
- [x] Git ignore strictly blocks `.env*` (except `.env.example`), private keys, tokens, and build artifacts.
- [x] Template `.env.example` provides explicit placeholders and documents all parameters.

### Criterion 2: Rust Policy Engine (`services/policy`)
- [x] Compiles without warnings under standard Rust toolchain.
- [x] Implements explicit state machine:
  - Allowed states: `Draft`, `PendingReview`, `AwaitingPolicyCheck`, `Approved`, `FlaggedHighRisk`, `PayoutScheduled`, `PayoutSubmitted`, `PayoutSettled`, `PayoutFailed`, `PayoutBlocked`.
- [x] Implements fail-closed invariant validation:
  - Amount bounds (positive, <= ceiling).
  - Currency validation (e.g. USD).
  - Recipient email RFC-format check.
  - Risk score thresholds (> 20 triggers human dual-approval; > 80 triggers automatic block).
- [x] Comprehensive unit tests verify both valid paths and deliberate boundary violations.

### Criterion 3: Go API Orchestrator (`services/api`)
- [x] Compiles with standard Go toolchain (`go build ./...`).
- [x] Configuration loading validates:
  - Rejecting `production` environment in Phase 1 (sandbox enforcement).
  - Validating required environment variables.
  - Redacting secrets when config is serialized/logged.
- [x] Health check endpoints (`/healthz`, `/readyz`) return JSON with proper status codes.
- [x] Structured logger redacts Bearer tokens, passwords, and sensitive keys.
- [x] Bounded HTTP request timeouts enforced on all inbound and outbound requests.

### Criterion 4: PayPal Sandbox Integration Feasibility
- [x] Implements OAuth2 client credentials grant against PayPal Sandbox endpoint.
- [x] Implements Payout Batch status retrieval (`GET /v1/payments/payouts/{payout_batch_id}`).
- [x] Redacts Bearer tokens and sensitive response fields.
- [x] Zero-blind-retry policy: ambiguous network failures do not reissue requests with new IDs.
- [x] Explicit smoke test command (`cmd/paypal-smoke`):
  - Does NOT issue payouts automatically.
  - Verifies OAuth credentials and Sandbox endpoint connectivity.
  - When sandbox credentials are valid, successfully retrieves an auth token and optionally queries a batch.
  - When credentials are blank or invalid, gracefully reports the exact blocker with clear guidance.

### Criterion 5: Developer Ergonomics & CI
- [x] CI workflows in `.github/workflows/ci.yml` run formatting, linting, and automated tests for Go, Rust, and Web.
- [x] Step-by-step instructions in `docs/operational-instructions.md` guide developers to configure PayPal developer accounts.
