# ClaimBridge

> **Autonomous AI Claims Triage with Deterministic Rust Safety Policy Enforcement and PayPal Payouts Settlement**

ClaimBridge is a security-conscious claims processing and instant payout orchestration platform built for the **PayPal AI Hackathon**.

ClaimBridge decouples non-deterministic AI claim triage (damage scoring, document summarization, anomaly detection) from **deterministic, fail-closed financial safety policy enforcement** implemented in Rust, backed by a high-concurrency Go API gateway, persistent PostgreSQL storage, and Next.js frontend.

---

## Architecture Overview

```
claim-bridge/
├── apps/
│   └── web/                 # Next.js 14 (App Router), TypeScript, Tailwind CSS
│       └── src/
│           ├── app/
│           │   ├── app/     # Dashboard, Payouts List, Payout Detail, New Case
│           │   └── settings/# Account profile & PayPal Sandbox Integrations
│           ├── components/  # Cybernetic shared navigation header
│           └── lib/         # Auth provider & Payout domain models (minor units in cents)
├── services/
│   ├── api/                 # Go 1.22+ HTTP API (auth, orchestration, PayPal REST client)
│   │   ├── cmd/
│   │   │   ├── server/      # Production HTTP server entrypoint
│   │   │   ├── acceptance/  # 24-scenario end-to-end acceptance verification suite
│   │   │   └── paypal-smoke/# Explicitly invoked PayPal sandbox smoke-test tool
│   │   └── internal/
│   │       ├── auth/        # Argon2id, session lifecycle, CSRF/Origin check
│   │       ├── config/      # Validated configuration & sandbox assertion
│   │       ├── crypto/      # CSPRNG tokens, SHA-256 hashes, email normalization
│   │       ├── db/          # PostgreSQL migrations & thread-safe mock repository
│   │       │   └── migrations/ # 000001_auth, 000002_claims, 000003_payout_operations
│   │       ├── email/       # Mailpit SMTP & thread-safe mock mailer
│   │       ├── logger/      # Structured JSON logging with stream redactions
│   │       ├── paypal/      # Server-side PayPal sandbox REST client
│   │       ├── payouts/     # Payout cases, operations, approval, sync, cancellation
│   │       ├── policy/      # Go bridge to policy evaluation
│   │       └── server/      # HTTP router, middleware, and handlers
│   └── policy/              # Rust deterministic safety policy engine
│       ├── src/             # Mathematical rules, payout transitions, dual-approval engine
│       └── tests/           # Deterministic unit & boundary test suite
├── docs/                    # PRD, Architecture, Threat Model, Acceptance Criteria, ADRs
│   ├── architecture/        # Architecture specifications & Mission 003 designs
│   ├── adr/                 # Architecture Decision Records (ADR-001 through ADR-005)
│   ├── requirements.md
│   ├── architecture.md
│   ├── threat-model.md
│   └── operational-instructions.md
├── infra/                   # Local infrastructure (PostgreSQL 16 & Mailpit docker-compose)
│   └── postgres-init/       # Database bootstrap schema
├── .github/workflows/       # CI pipelines for Go, Rust, Web, and security audits
└── .env.example             # Configuration template with placeholders only
```

---

## Core Security Invariants

1. **AI Proposes, Policy Decides**: AI models provide risk advice, but have ZERO execution authority over payouts. Only the deterministic Rust policy engine can grant approval.
2. **Fail-Closed by Default**: If any parameter is missing, out-of-bounds, unverified, or ambiguous, disbursement is strictly blocked. Hard ceiling of $500.00 USD is permanently enforced in code and policy.
3. **Absolute Idempotency & Zero Blind Retries**: A single claim is bound to a single deterministic `sender_batch_id` and `sender_item_id` committed to PostgreSQL *before* calling PayPal. Network timeouts never generate replacement identifiers.
4. **Separation of Duties (Dual-Custody)**: Payouts exceeding defined risk (> 20.0) or monetary limits (> $250.00 USD) require dual custody. The initiating officer is strictly forbidden from approving their own payout (`ERR_SELF_APPROVAL_FORBIDDEN`).
5. **Reconciliation-Required Safety**: If a PayPal submission times out or returns an ambiguous response, the operation transitions to `RECONCILIATION_REQUIRED`. Automatic duplicate payouts are strictly prohibited.
6. **Zero Browser Secret Exposure**: Client secrets, OAuth tokens, and server-side credentials are never transmitted to the browser or placed in public Next.js bundles.
7. **Strict Sandbox Assertion**: The Go API gateway immediately terminates at startup if `api-m.paypal.com` or any production host is configured.
8. **Integer Minor Unit Currency**: Money is calculated, compared, and persisted exclusively in integer minor units (cents) to avoid floating-point rounding hazards.
9. **Multi-Tenant Data Isolation**: Every case, operation, and audit event is hard-isolated by organization ID across database indexes, foreign keys, and service boundaries.

---

## Quick Start

### 1. Prerequisites
- **Go 1.22+** (`go version`)
- **Rust 1.75+** (`cargo --version`)
- **Node.js 20+** (`node -v`)
- **Docker Compose** (for PostgreSQL 16 & Mailpit local email testing)

### 2. Configure Environment
```bash
cp .env.example .env
```
Ensure `PAYPAL_ENV=sandbox`. Configure your PayPal Sandbox credentials if live sandbox probing is desired:
```env
PAYPAL_CLIENT_ID=your_sandbox_client_id
PAYPAL_CLIENT_SECRET=your_sandbox_client_secret
PAYPAL_SANDBOX_BASE_URL=https://api-m.sandbox.paypal.com
```

### 3. Launch Local Infrastructure (PostgreSQL & Mailpit)
```bash
docker compose -f infra/docker-compose.yml up -d
```
- **PostgreSQL**: `localhost:5432` (`claimbridge` / `claimbridge_dev_secret`)
- **Mailpit SMTP**: `localhost:1025`
- **Mailpit Web Inbox**: [http://localhost:8025](http://localhost:8025)

*Note: If Docker is unavailable, the Go API server automatically initializes an in-memory repository with thread-safe mock mailer for seamless offline development and testing.*

### 4. Database Migrations
Migrations are located in `services/api/internal/db/migrations/`:
- `000001_auth_schema.up.sql`: Users, Organizations, Memberships, Sessions, Verification Tokens, Reset Tokens, Audit Events.
- `000002_claims_schema.up.sql`: Multi-tenant claims tied to organizations.
- `000003_payout_operations.up.sql`: `payout_cases`, `payout_operations`, `payout_status_snapshots`.

### 5. Run the 24-Scenario Acceptance Verification Suite
Execute the end-to-end automated runner that verifies all 24 acceptance scenarios across Mission 001, 002, and 003:
```bash
cd services/api
go run ./cmd/acceptance
```

This verifies:
- **Scenarios 01-12 (Auth & Tenant Foundation)**: Account registration, email verification, single-use token replay rejection, HttpOnly session cookie, protected route access, session persistence, logout revocation, password reset dispatch & consumption, credential rotation, unauthenticated API blocking, cross-org isolation, server restart persistence.
- **Scenarios 13-24 (Payout Operations & PayPal Integration)**:
  - 13: Live PayPal Sandbox OAuth token probe (`GET /api/v1/paypal/connection-status`).
  - 14: Single-officer payout case creation (`<= $250.00 USD`).
  - 15: Dual-custody payout case creation (`> $250.00 USD`).
  - 16: Fail-closed ceiling rejection (`> $500.00 USD`).
  - 17: Initiate durable payout operation with pre-persisted `sender_batch_id`.
  - 18: Dual-custody separation of duties: initiator self-approval strictly rejected (HTTP 403).
  - 19: Dual-custody approval by distinct authorized officer succeeds (HTTP 200).
  - 20: Explicit submission of approved operation to PayPal Sandbox REST API.
  - 21: Status synchronization pulling latest provider batch and item statuses.
  - 22: Cancellation of eligible `UNCLAIMED` payout with PayPal.
  - 23: Cross-organization tenant isolation on payout cases.
  - 24: Ambiguous network timeout transition to `RECONCILIATION_REQUIRED` without duplicate dispatch.

### 6. Smoke-Test PayPal Sandbox
```bash
cd services/api
# Check credentials and probe live sandbox OAuth2:
go run ./cmd/paypal-smoke

# Query existing batch details:
go run ./cmd/paypal-smoke -batch-id <payout-batch-id>

# Query item status:
go run ./cmd/paypal-smoke -item-id <payout-item-id>

# Cancel unclaimed item:
go run ./cmd/paypal-smoke -cancel-item <payout-item-id>

# Explicitly create single sandbox test payout (requires confirmation):
go run ./cmd/paypal-smoke -create-test-payout -recipient test@business.example.com -confirm-sandbox-only
```

### 7. Run Policy Engine (Rust)
```bash
cd services/policy
cargo test
cargo clippy -- -D warnings
```

### 8. Run API Gateway (Go)
```bash
cd services/api
go test -v ./...
go run ./cmd/server
```
The server listens on `http://localhost:8080`.

### 9. Run Frontend Web Application (Next.js)
```bash
cd apps/web
npm install
npm run build
npm run dev
```
Open [http://localhost:3000](http://localhost:3000) to access the application.

---

## Frontend Screens & Navigation

| Route | Description |
|---|---|
| `/register` | User signup with name, email, password, and automatic workspace creation |
| `/login` | Secure login with HttpOnly session cookie and unverified-email alerts |
| `/verify-email` | Consumes single-use verification token from email link |
| `/forgot-password` | Enumeration-safe reset link request |
| `/reset-password` | New password selection and invalidation of all prior sessions |
| `/app` | Authenticated tenant dashboard with real case counts, settled amounts, and live PayPal connection status |
| `/app/payouts` | Searchable, paginated registry of organization payout cases |
| `/app/payouts/new` | Payout intake form with real-time Rust policy evaluation preview and minor unit calculation |
| `/app/payouts/[id]` | Case investigation view, provider status snapshot, dual-custody approval, PayPal sandbox submission, sync, and cancellation |
| `/app/settings/integrations` | PayPal Sandbox connection inspector with live OAuth verification and developer setup instructions |
| `/settings/account` | User profile, active organization details, and session security |

---

## API Contracts & Permission Requirements

All endpoints enforce strict server-side authentication, origin/CSRF protection, and organization scoping:

| Method | Endpoint | Permission / Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/paypal/connection-status` | Authenticated User | Probes live PayPal Sandbox OAuth status |
| `GET` | `/api/v1/payout-cases` | Authenticated Org Member | List paginated payout cases for organization |
| `POST` | `/api/v1/payout-cases` | Authenticated Org Member | Create new payout case with server-side validation |
| `GET` | `/api/v1/payout-cases/{case_id}` | Authenticated Org Member | Retrieve case details and operation history |
| `POST` | `/api/v1/payout-cases/{case_id}/sync` | Authenticated Org Member | Synchronize status with PayPal Sandbox REST API |
| `POST` | `/api/v1/payout-cases/{case_id}/payout-operations` | Authenticated Org Member | Initiate durable operation and generate `sender_batch_id` |
| `GET` | `/api/v1/payout-cases/{case_id}/operations` | Authenticated Org Member | List operations for a case |
| `POST` | `/api/v1/payout-operations/{op_id}/approve` | Distinct Authorized Officer | Authorize payout (Self-approval strictly forbidden) |
| `POST` | `/api/v1/payout-operations/{op_id}/submit` | Authenticated Org Member | Execute payout submission to PayPal Sandbox |
| `POST` | `/api/v1/payout-operations/{op_id}/cancel` | Authenticated Org Member | Cancel eligible unclaimed operation |
| `POST` | `/api/v1/auth/register` | Public | Account registration |
| `POST` | `/api/v1/auth/verify-email` | Public | Single-use email verification |
| `POST` | `/api/v1/auth/login` | Public | Authenticate user and issue HttpOnly cookie |
| `POST` | `/api/v1/auth/logout` | Authenticated User | Revoke server-side session |
| `GET` | `/api/v1/auth/me` | Authenticated User | Return session user and organization |

---

## Testing Verification Matrix

| Test Suite | Scope | Result |
|---|---|---|
| `cargo test` (`services/policy`) | 17 deterministic policy tests | **PASS** (17/17) |
| `cargo clippy` (`services/policy`) | Zero compiler or lint warnings | **PASS** (0 warnings) |
| `go test ./...` (`services/api`) | 10 Go packages with mock/unit tests | **PASS** (10/10 packages) |
| `go run ./cmd/acceptance` | Full 24-scenario end-to-end integration | **PASS** (24/24 scenarios) |
| `npm run build` (`apps/web`) | Production bundle, TypeScript check, 15 static/dynamic routes | **PASS** (0 errors) |
| `go run ./cmd/paypal-smoke` | Live PayPal credentials assertion | **GUARDED** (Explicit block reported when credentials unset) |
