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

**Total Target Duration:** 2 minutes 30 seconds (150 seconds)

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
- Navigate to **New Payout** (`/app/payouts/new`).
- Enter Recipient: `claimant.unclaimed@example.com`, Amount: `$350.00` USD, Risk Score: `25.0`.
- Highlight the live policy preview badge: `Dual-Approval Mandatory (Amount > $250.00)`.

**Spoken Narration:**
> "Logging in as our primary operator, we enter our authenticated workspace.
> 
> When creating a payout case, all calculations occur strictly in integer minor units—cents—to eliminate floating-point rounding hazards.
> 
> As we enter $350.00, our Rust policy engine detects that this exceeds the $250 threshold, enforcing mandatory dual-custody authorization."

---

### Scene C — Dual-Custody Approval & Separation of Duties [00:55 – 01:25] (30 seconds)

**Visual:**
- Submit the case. Land on Case Details (`/app/payouts/[id]`).
- Click **Initiate Sandbox Payout Operation**. Operation shows status `AWAITING_APPROVAL`.
- Highlight that the self-approval button is disabled or, if clicked, returns HTTP 403: `Self-approval forbidden: dual-custody requires a distinct authorized approver`.
- Click the user profile icon, sign out, and sign in as `officer@claimbridge.internal` / `OfficerPass123!`.
- Return to the case and approve the operation. Status transitions to `APPROVED`.

**Spoken Narration:**
> "Under strict separation-of-duties rules, the initiating officer is strictly forbidden from self-approving. The system rejects the attempt fail-closed with HTTP 403.
> 
> Logging in as our compliance officer, Dana Vance, who shares membership in the organization, we review the case and approve the operation. The payout is now unlocked for dispatch."

---

### Scene D — Synthetic Fixture Integrity & Mutation Guard [01:25 – 01:50] (25 seconds)

**Visual:**
- Navigate to the Payout Cases Registry (`/app/payouts`).
- Open the seeded demo case `DEMO-W9821` (`Warranty Replacement Claim #9821`).
- Point out the prominent banner:  
  **“SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION”**.
- Point out the synthetic identifiers (`SIMULATED-DEMO-BATCH-W9821`, `SIMULATED-DEMO-ITEM-W9821`) and provider status marked as simulated.
- Show that external mutating buttons (Submit to PayPal Sandbox, Sync, Cancel) are disabled with safe indicators.
- Mention that backend policy guards enforce this server-side: fixture records are permanently blocked from generating outbound PayPal requests.

**Spoken Narration:**
> "Here on our seeded investigation case, ClaimBridge enforces strict provenance boundaries.
> 
> Notice the clear banner: this is simulated demo data, not a live PayPal transaction. All identifiers are unmistakably synthetic.
> 
> Crucially, our backend server-side guards block any external mutation on synthetic records with HTTP 403, guaranteeing zero outbound requests to PayPal and preventing test IDs from polluting live networks."

---

### Scene E — AI Forensic Investigation with Evidence Grounding [01:50 – 02:20] (30 seconds)

**Visual:**
- Scroll down to the **AI Payout Investigation Engine** card.
- Click **Run Investigation**.
- Show the animated processing state, followed by the generated report.
- Highlight:
  1. Amber advisory notice: AI suggestions have zero direct financial execution authority.
  2. Grounded Evidence References: clickable chips linking to verified case and snapshot records.
  3. Recommendation: `REVIEW_CANCELLATION_ELIGIBILITY` validated against Rust policy rules.

**Spoken Narration:**
> "Now, we trigger our AI Investigation Engine. The model correlates historical status snapshots with case evidence.
> 
> Notice: AI advice is strictly advisory. Every cited identifier is mathematically verified against persisted records—if the model hallucinated an ungrounded ID, our Go backend would reject it fail-closed with HTTP 422.
> 
> The model identifies that the item is UNCLAIMED and recommends reviewing cancellation eligibility, which our Rust policy engine validates."

---

### Scene F — Verified Automation & Closing [02:20 – 02:35] (15 seconds)

**Visual:**
- Switch to terminal showing:
  - `go run ./cmd/acceptance` with all 32 scenarios green in the simulated test environment.
  - `go test ./internal/seeder ./internal/payouts` confirming idempotent seeding and fixture guards.

**Spoken Narration:**
> "ClaimBridge bridges the flexibility of AI with the rigorous determinism required in financial infrastructure.
> 
> With 32 automated simulated acceptance tests passing, verified dual custody, and fail-closed PayPal safety boundaries, ClaimBridge is ready. Thank you!"

---

## Track 2: Optional Live PayPal Sandbox & Ollama Execution

*(Only record this track when `go run ./cmd/readiness -profile=submission` succeeds with real PayPal OAuth and local Ollama)*

1. **Pre-flight Verification:** Show terminal executing `go run ./cmd/readiness -profile=submission` reporting `[STATUS: READY]`.
2. **Intake & Approval:** Create a fresh, non-fixture payout case for `$260.00` USD, initiate operation, and execute distinct officer dual-approval.
3. **Live Sandbox Submission:** Click **Submit to PayPal Sandbox**. The browser displays genuine PayPal-assigned batch and item IDs from `api-m.sandbox.paypal.com`.
4. **Live Polling:** Click **Sync Latest Status** to retrieve live provider status updates directly from PayPal Sandbox REST API v1.
5. **Live Ollama Inference:** Run investigation with model-backed inference from local `llama3.2`.
6. **Controlled Cancellation:** If and only if the item is verified in `UNCLAIMED` status, click **Cancel Payout** to execute a real PayPal Sandbox cancellation.
