# ClaimBridge — Known Issues & Sandbox Limitations

**Document Version:** 1.0 (Mission 005 Release Gate)  
**Classification:** Public Hackathon Disclosure  

---

## 1. Known Sandbox Behaviors & Acceptable Tradeoffs

| Issue ID | Description | Severity | Impact | Mitigation / Current Behavior | Hackathon Acceptability Justification |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **ISSUE-001** | Local Ollama Inference Latency on CPU | Low | Response time for AI investigations may range between 4–12 seconds on machines without GPU acceleration. | Frontend features responsive animated pending indicators with cancellation support. Background HTTP request timeout is bounded at 30 seconds. Core payout operations remain completely unaffected. | **Acceptable:** Satisfies the zero-budget constraint without forcing users to purchase paid cloud API keys or GPUs. |
| **ISSUE-002** | Polling Synchronization vs. Public Webhooks in Local Dev | Low | Status reconciliation for asynchronous PayPal batches requires clicking "Synchronize Status" or invoking the scheduled reconciliation endpoint when testing without public ingress (e.g. ngrok). | Durable `sender_batch_id` and item polling via `GET /v1/payments/payouts/{payout_batch_id}` ensures exact status reconciliation. | **Acceptable:** In local development without open public tunneling, manual or periodic polling is the standard recommended pattern. |
| **ISSUE-003** | Currency Constrained to USD in Initial Release | Low | Payout case creation and policy verification are restricted to currency `USD`. | Rust policy engine strictly validates `currency == "USD"`; non-USD requests are rejected fail-closed. | **Acceptable:** Meets Hackathon scope; multi-currency settlement can be unlocked in future milestones by configuring currency exchange tables. |
| **ISSUE-004** | Dual-Approval Role Isolation within Single Organization | Low | The dual-approval policy requires a distinct user ID (`CreatedByUserID != ApproverUserID`) to approve transactions > $250.00 USD. | In personal workspaces with only one operator, high-value payouts cannot be self-approved. The user must invite or register a second test officer account to approve high-value claims. | **Acceptable:** This is the intended security invariant preventing fraudulent self-disbursement. |
| **ISSUE-005** | Production Host Hard Block | Informational | Configuring `PAYPAL_ENV=production` or `api-m.paypal.com` causes the application to immediately panic and terminate. | Intentional fail-safe: Prevents accidental real fund movement during testing or hackathon evaluation. | **Acceptable:** Mandatory hackathon safety invariant. |
