# ClaimBridge — Hackathon Demo Recording Checklist

**Pre-Flight Verification Checklist for Recording the Submission Video**

---

## 1. System & Service Readiness

- [ ] **Method 1: Single-Command Docker Compose Evaluator Launch (Recommended):**
  ```bash
  # Launches PostgreSQL, Mailpit, API (with auto-migrations), Seeder, and Next.js Web (with evaluator banner)
  docker compose -f infra/docker-compose.full.yml -f infra/docker-compose.demo.yml up --build -d
  # Verify web is at http://localhost:3000 and API readiness probe returns 200:
  curl -s http://localhost:8080/readyz
  ```

- [ ] **Method 2: Native Host Development Launch:**
  ```bash
  # 1. Start backing services
  docker compose -f infra/docker-compose.yml up -d
  # 2. Seed idempotent demo fixtures
  cd services/api && go run ./cmd/seed
  # 3. Start API gateway
  go run ./cmd/server
  # 4. In a separate terminal, start Next.js frontend with demo banner
  cd apps/web && NEXT_PUBLIC_DEMO_MODE=true npm run dev
  ```

- [ ] **Method 3: Automated Playwright Walkthrough Recording (Deterministic 1080p CI Artifact):**
  ```bash
  # Run the dedicated demo recording spec against a running instance
  cd apps/web
  E2E_BASE_URL=http://localhost:3000 npx playwright test e2e/demo-recording.spec.ts
  # Output artifact generated at:
  # apps/web/test-results/demo-recording/claimbridge-demo-walkthrough.webm
  # Verify media integrity and duration (120s - 175s, 1920x1080):
  node scripts/verify-recording-artifact.mjs
  ```
  *(See [`docs/demo/VIDEO_ARTIFACT_GUIDE.md`](./VIDEO_ARTIFACT_GUIDE.md) for full guide on downloading the CI-generated artifact, adding voiceover, and publishing to the public video platform.)*

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
