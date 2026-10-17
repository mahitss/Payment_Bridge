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

---

## Quick Start

### 1. Prerequisites
- **Go 1.22+** (`go version`)
- **Rust 1.75+** (`cargo --version`)
- **Node.js 18+** (`node -v`)
- **Docker** (optional, for local PostgreSQL)

### 2. Configure Environment
```bash
cp .env.example .env
```
Ensure `PAYPAL_ENV=sandbox`. Phase 1 strictly forbids setting `PAYPAL_ENV=production`.

### 3. Verify Policy Engine (Rust)
```bash
cd services/policy
cargo test
```

### 4. Verify API Gateway (Go)
```bash
cd services/api
go test -v ./...
go run ./cmd/server
```

### 5. Verify PayPal Sandbox Connectivity
To verify PayPal credentials and API connectivity without issuing any payouts:
```bash
cd services/api
go run ./cmd/paypal-smoke
```
*(See `docs/operational-instructions.md` for steps to configure sandbox credentials).*

### 6. Run the Frontend (Next.js)
```bash
cd apps/web
npm install
npm run dev
```
Open [http://localhost:3000](http://localhost:3000) to view the Mission 001 Foundation & Feasibility console.

---

## Testing & CI

Run all component validations:
```bash
# Go unit & integration tests
cd services/api && go test -v -race ./...

# Rust deterministic unit tests & clippy
cd services/policy && cargo test && cargo clippy

# Web bundle build
cd apps/web && npm run build
```
