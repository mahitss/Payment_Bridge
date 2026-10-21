# ClaimBridge — Hackathon Video Demonstration Script

**Format:** Screen recording with voiceover narration  
**Presenter:** Single speaker (measured, technical pace)  
**Resolution:** 1080p (1920x1080), browser at 100% zoom  
**Time Limit:** Under 3 minutes (180 seconds maximum)

---

## Two Demonstration Tracks

ClaimBridge supports two distinct, truthful demonstration tracks:

1. **Track 1: Deterministic Simulated Workflow (Recommended / Fallback)**  
   - Runs locally without requiring live PayPal Sandbox credentials or local Ollama daemons.  
   - Uses pre-seeded synthetic fixtures (`DEMO-M4421`, `DEMO-W9821`, `DEMO-V1082`).  
   - Demonstrates dual-custody approval, prompt injection defense, evidence grounding, and server-side mutation protection.  
   - All fixture data is prominently labeled: **“SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION”**.
   - External provider mutations on fixtures are blocked server-side (HTTP 403) with zero outbound PayPal requests.

2. **Track 2: Optional Live PayPal Sandbox & Ollama Verification**  
   - Performed *only* after `go run ./cmd/readiness -profile=submission` reports `[STATUS: READY]`.  
   - Requires valid PayPal Sandbox OAuth credentials (`PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET`) and an active Ollama instance (`llama3.2`).  
   - Operates exclusively on genuine non-fixture cases.  
   - Live submission, live polling, and item cancellation execute real REST calls against `https://api-m.sandbox.paypal.com`.

---

## Track 1: Deterministic Simulated Workflow Walkthrough

**Raw Automated Recording:** 145–160 seconds (measured at 152.84s in Docker Compose and 153.28s in standard browser CI runs; strictly verified by CI media gate: 120.0s – 175.0s, silent WebM).  
**Final Edited Voiceover Export Target:** ~2 minutes 30 seconds (150 seconds, under the 180s hackathon maximum).  

### Scene A — Problem & Architecture [00:00 – 00:25] (25 seconds)

**Visual:**
- Open browser on ClaimBridge Landing Page (`http://localhost:3000`).
- Ensure `NEXT_PUBLIC_DEMO_MODE=true` is set if using the evaluator credential banner, or demonstrate standard login.
- Scroll past the four architectural pillars (Go Gateway, Rust Policy Engine, AI Investigation, PayPal REST Boundary).

**Spoken Narration:**
> "In multi-party disbursement systems—such as warranty claims, vendor reimbursements, or marketplace payouts—disbursements frequently stall in unclaimed, rejected, or ambiguous provider states.
> 
> Operators today face a dangerous dilemma: manual dashboard swiveling with cryptic errors, or naive AI chatbots that risk fund depletion through prompt injection and hallucinated payments.
> 
> Welcome to ClaimBridge. ClaimBridge solves this by decoupling non-deterministic AI investigation from deterministic, fail-closed Rust safety policies and direct PayPal Payouts settlement."

---

### Scene B — Authentication & Payout Policy Enforcement [00:25 – 00:55] (30 seconds)

**Visual:**
- Sign in with evaluator account `operator@claimbridge.internal` / `OperatorPass123!`.
- Land on the Overview Dashboard (`/app`).
- Navigate to **New Payout** (`/app/payouts/new`), heading: `Create Payout Settlement Case`.
- Enter Case Reference (unique), Title, Recipient: `claimant-demo@sandbox.example.com`, Amount: `$350.00` USD, Risk Score: `25.0`.
- Highlight the real-time UI policy preview badge: `DUAL-CUSTODY MANDATED (Amount > $250.00)`.

**Spoken Narration:**
> "Logging in as our primary operator, we enter our authenticated workspace.
> 
> When creating a payout case, all calculations occur strictly in integer minor units—cents—to eliminate floating-point rounding hazards.
> 
> As we enter $350.00, the interface displays our dual-custody threshold preview. The Go backend and Rust policy engine authoritatively enforce this upon submission, mandating a distinct second approver."

---

### Scene C — Dual-Custody Approval & Separation of Duties [00:55 – 01:25] (30 seconds)

**Visual:**
- Submit the case. Land on Case Details (`/app/payouts/[id]`).
- Click **Initiate Sandbox Payout Operation**. Operation shows status `AWAITING_APPROVAL`.
- Highlight that self-approval is blocked fail-closed for the initiator: `Self-approval blocked by policy`.
- Click the header sign-out button (`Sign Out of ClaimBridge`), and sign in as distinct compliance officer `officer@claimbridge.internal` / `OfficerPass123!`.
- Navigate to `/app/payouts`, locate the case, open it, and click **Approve Operation (Distinct Officer)**.
- Confirm in modal (`Authorize Dual-Custody Approval`).
- Verify status transitions to `Approved by officer` and remains in internal `DRAFT` status without automatically submitting to PayPal.

**Spoken Narration:**
> "Under strict separation-of-duties rules, the initiating operator is strictly forbidden from self-approving. The system rejects the attempt fail-closed with HTTP 403.
> 
> Logging in as our distinct compliance officer (`officer@claimbridge.internal`), who shares membership in the organization, we review the case and approve the operation. The payout operation is now in the approved internal state—crucially, approval does not automatically dispatch funds or trigger an external PayPal transfer."

---

### Scene D — Synthetic Fixture Provenance & Mutation Guard [01:25 – 01:50] (25 seconds)

**Visual:**
- Navigate back to the Payout Cases Registry (`/app/payouts`).
- Point out the list: distinguish the newly created case (`APPROVED`, local non-fixture) from the pre-seeded fixture cases tagged with the amber `SIMULATED` badge.
- Open the pre-seeded synthetic demo case `DEMO-W9821` (`Warranty Replacement Claim #9821`).
- Point out the prominent alert banner:  
  **“SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION”**.
- Point out the synthetic identifiers (`SIMULATED-DEMO-BATCH-W9821`, `SIMULATED-DEMO-ITEM-W9821`) and provider status panels explicitly marked as simulated.
- Show that external mutating buttons (Submit to PayPal Sandbox, Sync, Cancel) are disabled with safe indicators (`Disabled: Synthetic Demo Fixture`).
- Explain that backend API mutation guards enforce this server-side: fixture records are permanently blocked from generating outbound PayPal requests with HTTP 403.

**Spoken Narration:**
> "Returning to our payout registry, we distinguish our newly created case from our pre-seeded evaluation fixtures.
> 
> Opening our warranty fixture, DEMO-W9821, ClaimBridge enforces strict provenance boundaries.
> 
> Notice the prominent banner: this is simulated demo data, not a live PayPal transaction. All provider IDs are unmistakably synthetic.
> 
> Crucially, our UI disables external action buttons, and our Go backend guards reject any fixture mutation attempt fail-closed with HTTP 403, guaranteeing zero outbound requests to PayPal and preventing test IDs from polluting live networks."

---

### Scene E — Forensic Investigation & Grounded Evidence Boundary [01:50 – 02:20] (30 seconds)

**Visual:**
- Scroll down to the **AI Payout Investigation Engine** card on `DEMO-W9821`.
- Point out the `Simulated Evaluation Fixture` badge on the panel.
- Inspect the existing pre-seeded synthetic investigation report (`SIMULATED DEMO INVESTIGATION`).
- Highlight:
  1. Advisory notice: AI suggestions have zero direct financial execution authority.
  2. Grounded Evidence References: verified citations linking to persisted case and status snapshot records (`[CASE:DEMO-W9821]`, `[SNAPSHOT:SNAP-W9821-1]`).
  3. Recommendation: `REVIEW_CANCELLATION_ELIGIBILITY` validated against Rust policy rules.
- Clarify honest offline behavior: if "Run Investigation" is clicked when Ollama is offline, ClaimBridge displays an honest `AI_UNAVAILABLE` notice without corrupting case state or faking model outputs.

**Spoken Narration:**
> "Scrolling to our investigation panel, we inspect the pre-seeded synthetic investigation report.
> 
> In this offline demonstration, ClaimBridge presents persisted forensic findings rather than faking live model responses. If an operator requests fresh analysis without a running Ollama daemon, ClaimBridge honestly reports the assistant is offline—never fabricating artificial outputs.
> 
> Every recommendation is strictly advisory. Every cited identifier is mathematically validated against database snapshots—hallucinated IDs are rejected fail-closed with HTTP 422.
> 
> The recommendation identifies that the item is UNCLAIMED, validated by our Rust policy engine, while financial controls remain firmly in human hands."

---

### Scene F — Verified Automation & Closing [02:20 – 02:35] (15 seconds)

**Visual:**
- Switch to terminal showing:
  - `go run ./cmd/acceptance` with all 32 scenarios green in the simulated test environment.
  - `go test -v ./internal/seeder ./internal/payouts` confirming idempotent seeding, PostgreSQL reset safety, and fixture guards.
  - Browser Playwright regression suite passing 27/27 tests across 7 spec files including deterministic walkthrough recording.

**Spoken Narration:**
> "ClaimBridge bridges the flexibility of AI with the rigorous determinism required in financial infrastructure.
> 
> With automated browser regression tests, 32 simulated acceptance scenarios, verified dual custody, and fail-closed PayPal safety boundaries, ClaimBridge is dependable. Thank you!"

---

## Track 2: Optional Live PayPal Sandbox & Ollama Execution

*(Only record this track when `go run ./cmd/readiness -profile=submission` succeeds with real PayPal OAuth and local Ollama)*

1. **Pre-flight Verification:** Show terminal executing `go run ./cmd/readiness -profile=submission` reporting `[STATUS: READY]`.
2. **Intake & Approval:** Create a fresh, non-fixture payout case for `$260.00` USD, initiate operation, and execute distinct officer dual-approval.
3. **Live Sandbox Submission:** Click **Submit to PayPal Sandbox**. The browser displays genuine PayPal-assigned batch and item IDs from `api-m.sandbox.paypal.com`.
4. **Live Polling:** Click **Sync Latest Status** to retrieve live provider status updates directly from PayPal Sandbox REST API v1.
5. **Live Ollama Inference:** Run investigation with model-backed inference from local `llama3.2`.
6. **Controlled Cancellation:** If and only if the item is verified in `UNCLAIMED` status, click **Cancel Payout** to execute a real PayPal Sandbox cancellation.
