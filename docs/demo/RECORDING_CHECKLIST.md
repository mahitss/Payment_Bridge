# ClaimBridge — Hackathon Demo Recording Checklist

**Pre-Flight Verification Checklist for Recording the Submission Video**

---

## 1. System & Service Readiness

- [ ] **Docker Containers Running:**
  ```bash
  docker compose -f infra/docker-compose.yml ps
  # Verify claimbridge-postgres (port 5432) and claimbridge-mailpit (port 8025) are running
  ```
- [ ] **Go API Gateway Running:**
  ```bash
  cd services/api && go run ./cmd/server
  # Check http://localhost:8080/healthz returns 200 OK
  ```
- [ ] **Next.js Web Frontend Running:**
  ```bash
  cd apps/web && npm run dev
  # Check http://localhost:3000 loads without console errors
  ```
- [ ] **AI Model Provider (Ollama):**
  ```bash
  ollama list
  # Confirm llama3.2 is installed
  ollama serve
  # Verify http://localhost:11434 responds
  ```
- [ ] **Run Pre-Demo Readiness Diagnostic (Submission Profile):**
  ```bash
  cd services/api && go run ./cmd/readiness -profile=submission
  # Must verify all 8 required gates (5 Core + PayPal Sandbox OAuth + Ollama Health + Genuine Non-Mutating Inference Probe)
  # Ensure exit code is 0 and output reports: [STATUS: READY] FULL SUBMISSION & DEMO READINESS VERIFIED
  # (For local core development only, use: go run ./cmd/readiness -profile=core)
  ```
- [ ] **Seed Test Fixtures:**
  ```bash
  cd services/api && go run ./cmd/seed
  # Ensures operator@claimbridge.internal and officer@claimbridge.internal accounts exist
  ```

---

## 2. Screen & Recording Environment

- [ ] **Display Resolution:** Set main display to 1920x1080 (1080p).
- [ ] **Browser Window:** Maximize or set to full screen (1920x1080), 100% zoom level (`Ctrl + 0`).
- [ ] **Browser Extensions:** Disable ad-blockers, password autofill overlays, and personal notifications.
- [ ] **Operating System:** Turn on "Do Not Disturb" / Focus Assist (mute notifications, Slack, Discord).
- [ ] **Microphone Level:** Set input gain to peak between -12dB and -6dB. Ensure no background fan noise or echo.

---

## 3. Account Credentials Quick Reference

| Account Role | Email Address | Password | Intended Action in Demo |
| :--- | :--- | :--- | :--- |
| **Primary Operator** | `operator@claimbridge.internal` | `OperatorPass123!` | Create $350 payout; attempt self-approval (blocked). |
| **Compliance Officer** | `officer@claimbridge.internal` | `OfficerPass123!` | Review and approve $350 payout (dual custody passed). |

---

## 4. Fallback Strategies During Recording

- **If Ollama inference takes longer than 10 seconds:** The frontend displays an active glowing brain spinner. You can narrate: *"While Ollama runs locally on CPU, the Go backend enforces a strict 30-second bounded timeout, keeping our payment thread non-blocking."*
- **If PayPal Sandbox API is experiencing latency:** The UI shows an explicit spinner and preserves all local operation states. You can narrate: *"ClaimBridge maintains our durable sender batch ID in PostgreSQL, ensuring that regardless of network latency, no duplicate payout can be created."*
