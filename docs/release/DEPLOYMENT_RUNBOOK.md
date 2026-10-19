# ClaimBridge — Production & Local Deployment Runbook

**Classification:** Hackathon Release Operational Runbook  
**Target Environment:** Zero-Budget Local Containerized Stack / Free-Tier Hosted Environment  
**Revision:** 1.0  

---

## 1. Prerequisites

- **Docker & Docker Compose:** Docker Desktop or Docker Engine v24+
- **Database:** PostgreSQL 16 (local container or free-tier managed instance such as Neon / Supabase)
- **Node.js:** Node.js 20.x LTS & npm 10+
- **Go:** Go 1.22+
- **Rust:** Rust 1.77+ with Cargo
- **PayPal Sandbox Credentials:** Client ID and Client Secret from [PayPal Developer Dashboard](https://developer.paypal.com)

---

## 2. Option A: Single-Command Offline Evaluator Demo Deployment (Recommended for Hackathon Evaluation)

To launch the complete application stack with automatic schema migrations, idempotent demo seeder, and the evaluator credentials banner enabled at build-time:

```bash
# 1. Clone repository
git clone https://github.com/mahitss/Payment_Bridge.git
cd Payment_Bridge

# 2. Launch full evaluation stack
docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml up --build -d
```

### 2.1 Lifecycle & Startup Sequence
1. **PostgreSQL 16 (`claimbridge-postgres`):** Starts and verifies health (`pg_isready -U claimbridge -d claimbridge_dev`). Bound strictly to `127.0.0.1:5432`.
2. **Mailpit (`claimbridge-mailpit`):** Starts mock SMTP server on `127.0.0.1:1025` and web UI on `127.0.0.1:8025`.
3. **Go API Gateway (`claimbridge-api`):** Built from multi-stage Dockerfile containing `/app/server`, `/app/seed`, and `/app/readiness`. Connects to PostgreSQL, executes schema migrations (`DATABASE_AUTOMIGRATE=true`), and exposes health on `127.0.0.1:8080/healthz`.
4. **Idempotent Seeder (`claimbridge-seeder`):** Overlay container runs `/app/seed`. Creates demo organization, operator, compliance officer, and synthetic fixtures (`DEMO-W9821`, `DEMO-M4421`, `DEMO-V1082`). Once complete, exits cleanly with code 0 (`condition: service_completed_successfully`).
5. **Next.js Web Frontend (`claimbridge-web`):** Built with `NEXT_PUBLIC_DEMO_MODE=true` and `API_INTERNAL_URL=http://api:8080`. Waits for both API health and seeder completion. Exposes frontend on `127.0.0.1:3000`.

### 2.2 Verification Endpoints
- **Web Application:** `http://localhost:3000` (Displays evaluator quick-start banner with credentials)
- **API Health Probe:** `http://localhost:8080/healthz`
- **API Readiness Probe:** `http://localhost:8080/readyz`
- **Mailpit Web UI (Inspect emails):** `http://localhost:8025`

### 2.3 Evaluator Demo Accounts
- **Lead Operator:** `operator@claimbridge.internal` / `OperatorPass123!`
- **Compliance Officer:** `officer@claimbridge.internal` / `OfficerPass123!`

### 2.4 Safe Teardown & Data Preservation
```bash
# Stop containers while preserving PostgreSQL data volume (postgres_data)
docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml down

# Reset only synthetic demo fixtures (preserves real accounts and non-demo records)
docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml run --rm seeder -reset
```

---

## 3. Option B: Standard Full-Stack Docker Deployment (Non-Demo Mode)

To launch the full containerized stack without pre-seeded accounts or the evaluator banner:

```bash
docker compose -f infra/docker-compose.full.yml up --build -d
```

---

## 4. Option C: Native Host Process Development Launch

### 3.1 Start Backing Services
```bash
docker compose -f infra/docker-compose.yml up -d
```

### 3.2 Initialize Database Migrations
Migrations run automatically on API startup when `DATABASE_AUTOMIGRATE=true`. Alternatively, apply SQL schemas directly:
```bash
# PostgreSQL initialization script
psql -h localhost -U claimbridge -d claimbridge_dev -f infra/postgres-init/init.sql
```

### 3.3 Start the Rust Policy Engine (Optional Standalone)
```bash
cd services/policy
cargo build --release
./target/release/claimbridge-policy
```

### 4.4 Start the Go API Server
```bash
cd services/api
go run ./cmd/server
```
Expected output:
```text
INFO Database connection established max_conns=25 min_conns=2
INFO Applied database migrations count=5
INFO Initialized AI inference provider provider=mock model=llama3.2 enabled=false
INFO HTTP Server listening addr=:8080
```

### 4.5 Start the Next.js Frontend
```bash
cd apps/web
npm ci
npm run dev
```
Open `http://localhost:3000` in your browser.

---

## 5. Option D: Local Free-Tier AI Inference (Ollama)

To run the AI investigation engine 100% locally with zero external API fees:

1. **Install Ollama:** [https://ollama.ai](https://ollama.ai)
2. **Pull the lightweight Llama 3.2 model:**
   ```bash
   ollama pull llama3.2
   ollama serve
   ```
3. **Configure ClaimBridge `.env`:**
   ```env
   AI_ENABLED=true
   AI_PROVIDER=ollama
   AI_BASE_URL=http://localhost:11434
   AI_MODEL=llama3.2
   ```
4. **Restart Go API server.** Investigations will now be synthesized in real-time by your local model.

---

## 6. Health, Readiness, and Liveness Probes

| Endpoint | Method | Expected HTTP Status | Output Semantics |
| :--- | :--- | :--- | :--- |
| `/healthz` | GET | `200 OK` | Confirms the HTTP server is running and accepting TCP connections. |
| `/readyz` | GET | `200 OK` or `503 Service Unavailable` | Probes PostgreSQL connectivity and reports PayPal credential configuration. |
| `/api/v1/sandbox/status` | GET | `200 OK` or `503 Service Unavailable` | Negotiates live OAuth token with PayPal Sandbox REST API. |

---

## 7. Disaster Recovery & Troubleshooting

### Scenario: Database Connection or Migration Failure
- **Symptom:** `/readyz` returns HTTP 503 with `"database": "unavailable"`, or the `api` container fails to start.
- **Remediation:** Verify PostgreSQL container health:
  ```bash
  docker compose -f infra/docker-compose.full.yml ps
  docker logs claimbridge-postgres
  ```
  Ensure port 5432 is not occupied by an existing local PostgreSQL service. Clean up and restart if needed:
  ```bash
  docker compose -f infra/docker-compose.full.yml down
  docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml up --build -d
  ```

### Scenario: Seeder Container Failure or Restart
- **Symptom:** `claimbridge-seeder` logs an error or fails to complete.
- **Remediation:** Inspect seeder container logs:
  ```bash
  docker logs claimbridge-seeder
  ```
  The seeder is completely idempotent. Re-run manually to inspect output:
  ```bash
  docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml run --rm seeder
  ```

### Scenario: API Reverse-Proxy Routing Misconfiguration
- **Symptom:** Next.js frontend returns HTTP 500 or 404 when calling `/api/v1/...`.
- **Remediation:** Ensure `API_INTERNAL_URL` is set to `http://api:8080` (without trailing paths or wildcards). Next.js automatically appends `/api/:path*` via `next.config.mjs` rewrites. Verify internal bridge networking:
  ```bash
  docker exec claimbridge-web wget -q -O - http://api:8080/healthz
  ```

### Scenario: Payout Operation in `RECONCILIATION_REQUIRED`
- **Symptom:** Ambiguous network drop occurred during PayPal submission.
- **Remediation:** The durable `sender_batch_id` prevents duplicate payouts. Click "Synchronize Status" in the Payout Details view. The system calls PayPal REST `GET /v1/payments/payouts/{payout_batch_id}` to reconcile the genuine state.
