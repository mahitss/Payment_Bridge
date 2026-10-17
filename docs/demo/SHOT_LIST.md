# ClaimBridge — Video Demo Shot List & Visual Cue Sheet

**Total Duration:** 2 minutes 35 seconds  
**Target Video Resolution:** 1920x1080 @ 60fps  
**Audio:** Voiceover (Mono/Stereo 48kHz, -14 LUFS)  

---

## Shot-by-Shot Execution Plan

| Shot # | Timecode | Screen / View | Visual Focus & On-Screen Action | Mouse / Audio Cues |
| :--- | :--- | :--- | :--- | :--- |
| **01** | `00:00 - 00:12` | Landing Page (`/`) | Full view of ClaimBridge header, title badge: "PayPal AI Hackathon 2026 • Verified Release". | Smooth mouse scroll over value proposition. Narration opens with problem statement. |
| **02** | `00:12 - 00:25` | Landing Page (`/`) | 4 architectural pillars: Go Gateway, Rust Engine, AI Investigation, PayPal REST Boundary. | Hover briefly over each pillar card to show subtle glowing border effect. |
| **03** | `00:25 - 00:35` | Landing Page Quick-Start Banner | Click "Sign In as Demo Operator". Quick redirect to `/login`. Pre-filled or paste `operator@claimbridge.internal`. | Click "Sign In". Immediate entry into `/app`. |
| **04** | `00:35 - 00:45` | Dashboard (`/app`) | Overview dashboard showing live PayPal status card, settled metrics, and recent cases table. | Click "New Payout" button in header navigation. |
| **05** | `00:45 - 00:55` | Intake Form (`/app/payouts/new`) | Type recipient email and amount: `$350.00`. | Point cursor to Rust policy preview badge: "Dual-Approval Mandatory (Amount > $250.00)". Click Submit. |
| **06** | `00:55 - 01:10` | Case Details (`/app/payouts/[id]`) | Click "Initiate Payout Operation". Status changes to `AWAITING_APPROVAL`. Click "Approve Payout Operation". | Red alert banner appears: "Self-approval forbidden: dual-custody requires a distinct authorized approver". |
| **07** | `01:10 - 01:25` | Login Screen & Case Details | Sign out. Sign in as `officer@claimbridge.internal`. Return to case details. Click "Approve Payout Operation". | Status badge turns emerald: `APPROVED`. |
| **08** | `01:25 - 01:38` | Case Details | Click "Submit to PayPal Sandbox". Status transitions to `SUBMITTED`. | PayPal Payout Batch ID appears: `PAYPAL-BATCH-...`. |
| **09** | `01:38 - 01:50` | Case Details | Click "Synchronize Status". | PayPal REST Sandbox queries item status. Item badge updates to amber: `UNCLAIMED`. |
| **10** | `01:50 - 02:05` | Case Details (AI Panel) | Scroll down to AI Investigation card. Click "Run AI Investigation". | Animated glowing brain spinner while Ollama Llama 3.2 processes evidence snapshot. |
| **11** | `02:05 - 02:20` | Case Details (AI Results) | Report appears: Summary, Findings with clickable evidence pills, Missing Information, and Recommended Actions. | Mouse highlights the evidence reference pills (`case_id`, `batch_id`) and the amber advisory disclaimer. |
| **12** | `02:20 - 02:30` | Case Details (Recovery) | Click "Cancel Payout Item". Confirmation modal appears. Click Confirm. | Item status immediately reconciles to `CANCELLED` via PayPal REST API. |
| **13** | `02:30 - 02:35` | Terminal Split Screen | Terminal running `go run ./cmd/acceptance` showing `All 32 acceptance scenarios PASSED successfully!`. | Fade out on ClaimBridge GitHub repository URL. |
