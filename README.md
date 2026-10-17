# ClaimBridge

> **Autonomous AI Claims Triage with Deterministic Rust Safety Policy Enforcement and PayPal Payouts Settlement**

ClaimBridge is a security-conscious claims processing and instant payout orchestration platform built for the **PayPal AI Hackathon**.

ClaimBridge decouples non-deterministic AI claim triage (damage scoring, document summarization, anomaly detection) from **deterministic, fail-closed financial safety policy enforcement** implemented in Rust, backed by a high-concurrency Go API gateway and Next.js frontend.

---

## Architecture Overview

```
claim-bridge/
├── apps/
│   └── web/                 # Next.js 14, TypeScript, Tailwind CSS, shadcn/ui
├── services/
│   ├── api/                 # Go 1.22+ HTTP API (auth, orchestration, PayPal REST client)
│   │   ├── cmd/
│   │   │   ├── server/      # Go HTTP server entrypoint
│   │   │   └── paypal-smoke/# Explicitly invoked PayPal sandbox smoke-test tool
│   │   └── internal/
│   │       ├── config/      # Validated configuration & sandbox assertion
│   │       ├── logger/      # Structured JSON logging with stream redactions
│   │       ├── paypal/      # Server-side PayPal sandbox REST client
│   │       ├── policy/      # Go bridge to policy evaluation
│   │       └── server/      # HTTP router, middleware, and handlers
│   └── policy/              # Rust deterministic safety policy engine
│       ├── src/             # Pure mathematical rules & lifecycle state machine
│       └── tests/           # Deterministic unit & boundary test suite
├── docs/                    # PRD, Architecture, Threat Model, Acceptance Criteria, ADRs
│   ├── requirements.md
│   ├── architecture.md
│   ├── threat-model.md
│   ├── operational-instructions.md
│   └── adr/
├── infra/                   # Local infrastructure (PostgreSQL 16 docker-compose)
├── .github/workflows/       # CI pipelines for Go, Rust, Web, and security audits
└── .env.example             # Configuration template with placeholders only
```

---

## Core Security Invariants

1. **AI Proposes, Policy Decides**: AI models provide risk advice, but have ZERO execution authority over payouts. Only the deterministic Rust policy engine can grant approval.
2. **Fail-Closed by Default**: If any parameter is missing, out-of-bounds, unverified, or ambiguous, disbursement is strictly blocked.
3. **Absolute Idempotency & Zero Blind Retries**: A single claim is bound to a single deterministic `sender_batch_id`. Network timeouts never generate new IDs.
4. **Defense in Depth**: Secrets are strictly server-side; logs are redacted at the stream level; sandboxes are enforced with build/config-time assertions.
5. **Human-in-the-Loop Thresholds**: Dual-custody approval is mandatory for claims exceeding defined risk (> 20.0) or monetary limits (> $250.00).
6. **Zero Browser Token Exposure**: Sessions use cryptographically opaque 32-byte tokens with SHA-256 hashes stored in PostgreSQL. Raw tokens and secrets are never returned in JSON or placed in `localStorage`.
7. **Argon2id Password Cryptography**: Password hashing enforces memory=64MB, iterations=3, parallelism=4, and constant-time dummy verification for non-existent users to eliminate timing side-channels.
8. **Multi-Tenant Data Isolation**: Every account is transactionally provisioned with an isolated organization workspace. Database queries enforce tenant ID boundaries with foreign key integrity.

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
Ensure `PAYPAL_ENV=sandbox`. Production environments and live payout credentials are strictly blocked in code.

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

Automatic initialization is performed by PostgreSQL via `infra/postgres-init/init.sql` on container creation.

### 5. Run Mission 002 Acceptance Verification Suite
Execute the end-to-end automated runner that verifies all 12 acceptance scenarios required by Mission 002:
```bash
cd services/api
go run ./cmd/acceptance
```
This tests:
1. Registration & email normalization
2. Mailpit email capture & token consumption
3. Single-use token enforcement
4. HttpOnly cookie session establishment
5. Authenticated route access (`/api/v1/auth/me`)
6. Session persistence across requests
7. Logout and subsequent HTTP 401 rejection
8. Password reset dispatch & single-use consumption
9. Revocation of old credentials and login with new credentials
10. Rejection of unauthenticated direct API requests
11. Multi-tenant cross-organization isolation
12. Application restart data persistence

### 6. Verify Policy Engine (Rust)
```bash
cd services/policy
cargo test
```

### 7. Run API Gateway (Go)
```bash
cd services/api
go test -v ./...
go run ./cmd/server
```
The server listens on `http://localhost:8080`.

### 8. Run Frontend Web Application (Next.js)
```bash
cd apps/web
npm install
npm run dev
```
Open [http://localhost:3000](http://localhost:3000) to access the application.

---

## Authentication Routes & API Contracts

### Frontend Authentication Pages
| Route | Description |
|---|---|
| `/register` | User signup with name, email, password, and automatic workspace creation |
| `/login` | Secure login with HttpOnly session cookie and unverified-email alerts |
| `/verify-email` | Consumes single-use verification token from email link |
| `/forgot-password` | Enumeration-safe reset link request |
| `/reset-password` | New password selection and invalidation of all prior sessions |
| `/app` | Authenticated tenant dashboard and policy safety overview |
| `/settings/account` | User profile, active organization details, and session security |

### Go API Contracts
| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Register user and create personal organization |
| `POST` | `/api/v1/auth/verify-email` | Validate and consume email verification token |
| `POST` | `/api/v1/auth/login` | Authenticate credentials and issue HttpOnly session cookie |
| `POST` | `/api/v1/auth/logout` | Invalidate session in database and expire cookie |
| `GET` | `/api/v1/auth/me` | Return authenticated user and tenant organization profile |
| `POST` | `/api/v1/auth/forgot-password` | Issue single-use reset token and dispatch email |
| `POST` | `/api/v1/auth/reset-password` | Reset password via token and revoke active sessions |

---

## Testing & CI

Run all component validations:
```bash
# Go unit, security & acceptance integration tests
cd services/api && go test -v -race ./...

# Rust deterministic unit tests & clippy
cd services/policy && cargo test && cargo clippy

# Web bundle build and TypeScript type-check
cd apps/web && npm run build
```
