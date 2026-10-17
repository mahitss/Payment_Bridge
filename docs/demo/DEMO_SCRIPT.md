# ClaimBridge — Hackathon Video Demonstration Script

**Total Target Duration:** 2 minutes 35 seconds (155 seconds) — well within the 3:00 hackathon cutoff  
**Format:** Screen recording with voiceover narration  
**Presenter:** Single speaker (clear, measured, technical pace)  
**Resolution:** 1080p (1920x1080), browser at 100% zoom  

---

## Timeline & Scene Breakdown

### Scene A — Problem & Context [00:00 – 00:25] (25 seconds)

**Visual:**
- Open browser on ClaimBridge Landing Page (`http://localhost:3000`).
- Slowly scroll past the four architectural pillars (Go Gateway, Rust Policy Engine, AI Investigation, PayPal REST Boundary).

**Spoken Narration:**
> "In multi-party disbursement systems—like warranty reimbursements or marketplace payouts—between 2 and 5 percent of payouts fail or stall in unclaimed states.
> 
> Operators today face a dangerous dilemma: manual dashboard swiveling with cryptic errors, or naive AI chatbots that risk fund depletion through prompt injection and hallucinated payments.
> 
> Welcome to ClaimBridge. ClaimBridge solves this by decoupling non-deterministic AI investigation from deterministic, fail-closed Rust safety policies and direct PayPal Payouts settlement."

---

### Scene B — Authentication & Payout Intake [00:25 – 00:55] (30 seconds)

**Visual:**
- Click **Sign In as Demo Operator** from the quick-start banner.
- Log in with `operator@claimbridge.internal` / `OperatorPass123!`.
- Land on the Overview Dashboard (`/app`).
- Navigate to **New Payout** (`/app/payouts/new`).
- Enter Recipient: `claimant.unclaimed@example.com`, Amount: `$350.00` USD, Risk Score: `25.0`.
- Highlight the live policy preview badge: `Dual-Approval Mandatory (Amount > $250.00)`.

**Spoken Narration:**
> "Logging in as our primary operator, we enter our authenticated workspace.
> 
> When creating a payout case, all calculations occur strictly in integer minor units to eliminate floating-point rounding hazards.
> 
> As we enter $350.00, our Rust policy engine instantly detects that this exceeds the $250 threshold, enforcing mandatory dual-custody authorization."

---

### Scene C — Dual-Custody Approval & Policy Precedence [00:55 – 01:25] (30 seconds)

**Visual:**
- Submit the case. Land on Case Details (`/app/payouts/[id]`).
- Click **Initiate Payout Operation**. Operation shows status `AWAITING_APPROVAL`.
- Try to click **Approve Payout Operation** as the creator.
- Show the error alert: `Self-approval forbidden: dual-custody requires a distinct authorized approver`.
- Click the user profile icon, sign out, and sign in as `officer@claimbridge.internal` / `OfficerPass123!`.
- Return to the case and click **Approve Payout Operation**. Status transitions to `APPROVED`.

**Spoken Narration:**
> "Under separation-of-duties rules, the initiating officer is strictly forbidden from self-approving. The system rejects the attempt fail-closed with HTTP 403.
> 
> Logging in as our compliance officer, Dana Vance, we review the case and approve the operation. The payout is now unlocked for dispatch."

---

### Scene D — PayPal REST Sandbox Execution & Status Sync [01:25 – 01:50] (25 seconds)

**Visual:**
- Click **Submit to PayPal Sandbox**.
- Show the assigned PayPal Batch ID: `PAYPAL-BATCH-...` and Item ID: `PAYPAL-ITEM-...`.
- Click **Synchronize Status**. The provider status is queried directly from PayPal REST Sandbox, showing item status `UNCLAIMED`.

**Spoken Narration:**
> "When we submit, ClaimBridge dispatches the pre-persisted, durable sender batch ID directly to the PayPal Payouts REST API in sandbox mode.
> 
> Clicking Synchronize pulls live status snapshots from PayPal, confirming the payment reached PayPal, but is currently sitting in UNCLAIMED status because the recipient has not yet accepted it."

---

### Scene E — AI Forensic Investigation with Evidence Grounding [01:50 – 02:20] (30 seconds)

**Visual:**
- Scroll down to the **AI Payout Investigation Engine** card.
- Click **Run AI Investigation**.
- Show the animated pending state, followed by the generated report.
- Point out:
  1. Amber advisory notice: AI suggestions have zero direct execution authority.
  2. Grounded Evidence References: clickable pills linking to the exact Case UUID, Operation UUID, and PayPal Item ID.
  3. Recommendation: `REVIEW_CANCELLATION_ELIGIBILITY` with rule `RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY`.

**Spoken Narration:**
> "Now, we trigger our AI Investigation Engine. Running locally on Ollama Llama 3.2, the model correlates the PayPal status with case records.
> 
> Notice: AI advice is strictly advisory. Every finding is mathematically verified against the snapshot identifiers—if the model hallucinated a fake batch ID, our Go backend would reject it fail-closed with HTTP 422.
> 
> The model identifies that the item is UNCLAIMED and recommends reviewing cancellation eligibility."

---

### Scene F — Controlled Recovery & Closing [02:20 – 02:35] (15 seconds)

**Visual:**
- Click **Cancel Payout Item**. Confirm the cancellation.
- Status updates to `CANCELLED` via PayPal REST API.
- Switch to terminal showing `go run ./cmd/acceptance` with all 32 scenarios green.

**Spoken Narration:**
> "With one click, the operator executes a verified PayPal REST cancellation, cleanly recovering the funds.
> 
> With 32 automated acceptance tests passing and zero browser secrets, ClaimBridge unites the power of AI with the unbreakable guarantees of PayPal. Thank you!"
