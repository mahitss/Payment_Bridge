# ClaimBridge — Video Demo Shot List & Visual Cue Sheet

**Demonstration Track:** Track 1 — Deterministic Offline / Simulated Workflow (Reproducible & Zero-Budget)  
**Total Duration:** 2 minutes 30 seconds  
**Target Video Resolution:** 1920x1080 @ 60fps  
**Audio:** Voiceover (Mono/Stereo 48kHz, -14 LUFS)  

---

## Shot-by-Shot Execution Plan

| Shot # | Timecode | Screen / View | Visual Focus & On-Screen Action | Mouse / Audio Cues |
| :--- | :--- | :--- | :--- | :--- |
| **01** | `00:00 - 00:12` | Landing Page (`/`) | Full view of ClaimBridge header, title badge: "PayPal AI Hackathon 2026 • Sandbox Prototype". | Smooth mouse scroll over value proposition. Narration opens with problem statement. |
| **02** | `00:12 - 00:25` | Landing Page (`/`) | Click "Probe Gateway" button to showcase diagnostic card: API Gateway Online & Reachable, PayPal Sandbox authentication status distinctly partitioned. | Point out honest status differentiation without leaking secrets. |
| **03** | `00:25 - 00:35` | Landing Page Quick-Start Banner | Click "Sign In to Demo Workspace". Redirect to `/login`. Sign in as `operator@claimbridge.internal` / `OperatorPass123!`. | Click "Sign In". Immediate entry into `/app`. |
| **04** | `00:35 - 00:45` | Dashboard (`/app`) | Overview dashboard showing tenant badge (`PayPal AI Hackathon Adjudications`) and quick metrics. | Click "New Payout" in header navigation. |
| **05** | `00:45 - 00:55` | Intake Form (`/app/payouts/new`) | Type recipient email and amount: `$350.00`. | Highlight Rust policy preview badge: "Dual-Approval Mandatory (Amount > $250.00)". Click Create. |
| **06** | `00:55 - 01:10` | Case Details (`/app/payouts/[id]`) | Click "Initiate Sandbox Payout Operation". Status changes to `AWAITING_APPROVAL`. Point out "Self-approval blocked by policy" notice. | Verify self-approval button is disabled fail-closed. |
| **07** | `01:10 - 01:25` | Login Screen & Case Details | Sign out. Sign in as distinct compliance officer `officer@claimbridge.internal` / `OfficerPass123!`. Return to case details. | Click "Approve Operation (Distinct Officer)". Status badge turns to `Internal: APPROVED`. Note that ordinary cases are not auto-submitted. |
| **08** | `01:25 - 01:38` | Payout Cases Registry (`/app/payouts`) | Open seeded fixture case `DEMO-W9821` (`Warranty Replacement Claim #9821`). | Point out amber `SIMULATED` badge on registry row. |
| **09** | `01:38 - 01:50` | Case Details (Synthetic Fixture) | Prominent banner: "SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION". Synthetic IDs (`SIMULATED-DEMO-...`). | Hover over disabled Submit, Sync, and Cancel buttons showing tooltip: "Disabled: Synthetic Demo Fixture". Server guards enforce HTTP 403. |
| **10** | `01:50 - 02:05` | Case Details (AI Panel) | Scroll down to AI Investigation card showing "Simulated Evaluation Fixture" indicator. | Inspect persisted synthetic investigation report (`SIMULATED DEMO INVESTIGATION`). |
| **11** | `02:05 - 02:20` | Case Details (Grounded Citations) | Point out grounded citation chips (`[CASE:DEMO-W9821]`, `[SNAPSHOT:SNAP-W9821-1]`), advisory notice, and policy validation. | Explain honest offline behavior: if "Run Investigation" is clicked without Ollama, returns honest `AI_UNAVAILABLE` rather than faking outputs. |
| **12** | `02:20 - 02:35` | Terminal Verification | Terminal running `go run ./cmd/acceptance` (32 scenarios passed), seeder PostgreSQL test, and Playwright E2E browser tests. | Conclude on determinism, safety boundaries, and release readiness. |
