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
- [ ] **Next.js Web Frontend Running (with Demo Mode for Evaluators):**
  ```bash
  # In apps/web: set NEXT_PUBLIC_DEMO_MODE=true if you want the evaluator quick-start banner visible
  NEXT_PUBLIC_DEMO_MODE=true npm run dev
  # Check http://localhost:3000 loads without console errors
  ```
- [ ] **Seed / Reset Demo Fixtures:**
  ```bash
  cd services/api
  # Seed idempotent demo fixtures and verify memberships
  go run ./cmd/seed
  # (Optional: To cleanly reset only synthetic fixtures without touching user accounts: go run ./cmd/seed -reset)
  ```
- [ ] **Decision on Recording Track:**
  - **Track 1 (Recommended / Deterministic Fallback):**
    - Record entirely with seeded synthetic fixtures (`DEMO-M4421`, `DEMO-W9821`, `DEMO-V1082`).
    - No external credentials or live Ollama daemon required.
    - Demonstrates: dual-custody policy, prompt injection defense, grounding validation, and server-side mutation protection on fixtures.
  - **Track 2 (Optional Live PayPal Sandbox & Ollama):**
    - Only record if `go run ./cmd/readiness -profile=submission` passes with status `[STATUS: READY]`.
    - Creates genuine non-fixture cases and submits to PayPal Sandbox REST API.

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
| **Primary Operator** | `operator@claimbridge.internal` | `OperatorPass123!` | Create $350 payout; attempt self-approval (blocked fail-closed). |
| **Compliance Officer** | `officer@claimbridge.internal` | `OfficerPass123!` | Review and approve $350 payout (dual custody passed). Both users belong to `demoOrg`. |

---

## 4. Fallback Strategies During Recording

- **When presenting offline without Ollama:** Open and inspect the pre-seeded synthetic investigation on `DEMO-W9821` (`SIMULATED DEMO INVESTIGATION`). Explain that ClaimBridge presents persisted forensic findings rather than faking live inference during offline evaluation. If "Run Investigation" is clicked while Ollama is offline, the system honestly reports that the investigation assistant is offline (`AI_UNAVAILABLE`) while core financial and dual-custody workflows remain fully functional.
- **Synthetic Fixture Provenance:** When displaying seeded cases, point out the **“SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION”** banner. Explain that server-side guards block external PayPal dispatch on synthetic records (HTTP 403), protecting external payment networks.
- **If PayPal Sandbox API experiences latency during Track 2:** The UI preserves local operation states and displays a durable progress state. You can narrate: *"ClaimBridge maintains our durable sender batch ID in PostgreSQL, ensuring that regardless of network latency, no duplicate payout can be created."*
