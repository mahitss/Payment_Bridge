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

## 2. Option A: Single-Command Full-Stack Docker Deployment (Recommended)

To launch the complete application stack (PostgreSQL, Mailpit, Go API, and Next.js Web) in containerized isolation:

```bash
# 1. Clone repository
git clone https://github.com/mahitss/Payment_Bridge.git
cd Payment_Bridge

# 2. Copy and customize environment variables
cp .env.example .env

# Edit .env with your PayPal Sandbox credentials:
# PAYPAL_CLIENT_ID=your_sandbox_client_id
# PAYPAL_CLIENT_SECRET=your_sandbox_client_secret

# 3. Launch full stack
docker compose -f infra/docker-compose.full.yml up --build -d
```

### Verification Endpoints:
- **Web Application:** `http://localhost:3000`
- **Go API Gateway:** `http://localhost:8080/healthz`
- **Readiness Probe:** `http://localhost:8080/readyz`
- **Mailpit Web UI (Inspect emails):** `http://localhost:8025`

---

## 3. Option B: Native Host Process Development Launch

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

### 3.4 Start the Go API Server
```bash
cd services/api
go run ./cmd/server
```
Expected output:
```text
INFO Database connection established max_conns=25 min_conns=2
INFO Applied database migrations count=4
INFO Initialized AI inference provider provider=mock model=llama3.2 enabled=false
INFO HTTP Server listening addr=:8080
```

### 3.5 Start the Next.js Frontend
```bash
cd apps/web
npm ci
npm run dev
```
Open `http://localhost:3000` in your browser.

---

## 4. Option C: Local Free-Tier AI Inference (Ollama)

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

## 5. Health, Readiness, and Liveness Probes

| Endpoint | Method | Expected HTTP Status | Output Semantics |
| :--- | :--- | :--- | :--- |
| `/healthz` | GET | `200 OK` | Confirms the HTTP server is running and accepting TCP connections. |
| `/readyz` | GET | `200 OK` or `503 Service Unavailable` | Probes PostgreSQL connectivity and reports PayPal credential configuration. |
| `/api/v1/sandbox/status` | GET | `200 OK` or `503 Service Unavailable` | Negotiates live OAuth token with PayPal Sandbox REST API. |

---

## 6. Disaster Recovery & Troubleshooting

### Scenario: Database Connection Failure
- **Symptom:** `/readyz` returns HTTP 503 with `"database": "unavailable"`.
- **Remediation:** Verify PostgreSQL container health: `docker ps`. Inspect logs: `docker logs claimbridge-postgres`.

### Scenario: Payout Operation in `RECONCILIATION_REQUIRED`
- **Symptom:** Ambiguous network drop occurred during PayPal submission.
- **Remediation:** The durable `sender_batch_id` prevents duplicate payouts. Click "Synchronize Status" in the Payout Details view. The system calls PayPal REST `GET /v1/payments/payouts/{payout_batch_id}` to reconcile the genuine state.
