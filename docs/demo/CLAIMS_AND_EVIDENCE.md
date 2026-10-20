# ClaimBridge — Video Demo Claims & Technical Evidence

This document establishes the factual basis for every statement made in the ClaimBridge video demonstration, mapping narrative claims directly to source code and automated test results.

---

## Claims and Ground Truth Evidence

### Claim 1: "AI Proposes, Policy Decides — AI has zero direct execution authority over payouts."
- **Verifying Source Code:**
  - `services/api/internal/payouts/service.go` (Operations can only be initiated, approved, submitted, or cancelled through explicit Go handler routes).
  - `services/api/internal/investigations/handler.go` (Investigations handler only executes read-only snapshot queries and model inference; it contains zero database mutation routes for funds).
  - `services/policy/src/engine.rs` (Pure Rust invariant validation).
- **Automated Test Evidence:**
  - `Scenario 28` in `services/api/cmd/acceptance/main.go`: Model recommends cancelling an item that is in `SUCCESS` status. Go cross-references the Rust policy engine and marks `deterministic_policy_permitted = false`.

---

### Claim 2: "All calculations occur in integer minor units (cents) to eliminate floating-point errors."
- **Verifying Source Code:**
  - `services/api/internal/db/models.go` (`AmountCents int64`).
  - `apps/web/src/app/app/payouts/new/page.tsx` (`parseAmountToCents` integer multiplication without float conversion).
  - `infra/postgres-init/init.sql` (`amount_cents BIGINT NOT NULL CHECK (amount_cents > 0)`).
- **Automated Test Evidence:**
  - Unit tests in `services/api/internal/payouts/service_test.go` and `services/policy/tests/deterministic_tests.rs`.

---

### Claim 3: "Under separation-of-duties rules, the initiating officer is strictly forbidden from self-approving (HTTP 403)."
- **Verifying Source Code:**
  - `services/api/internal/payouts/service.go`:
    ```go
    if op.InitiatedByUserID == actor.UserID {
        return ErrSelfApprovalForbidden // HTTP 403 Forbidden
    }
    ```
- **Automated Test Evidence:**
  - `Scenario 18` in `services/api/cmd/acceptance/main.go`:
    ```text
    [PASS] Scenario 18: Reject self-approval by initiator
          Detail: Self-approval blocked with HTTP 403 Forbidden under dual-custody mandate
    ```

---

### Claim 4: "Pre-persisted, durable sender batch ID prevents duplicate disbursements on network timeout."
- **Verifying Source Code:**
  - `services/api/internal/payouts/service.go`:
    `sender_batch_id` generated via `fmt.Sprintf("CB-BATCH-CASE-%d-%s-%x", time.Now().Unix(), shortID, randomSuffix)` and committed to PostgreSQL *before* the external HTTP call to PayPal.
- **Automated Test Evidence:**
  - `Scenario 24` in `services/api/cmd/acceptance/main.go`:
    ```text
    [PASS] Scenario 24: Ambiguous network timeout transitions to RECONCILIATION_REQUIRED
          Detail: Operation safely quarantined in RECONCILIATION_REQUIRED; duplicate submission blocked
    ```

---

### Claim 5: "Strict evidence grounding: If the model references a fake batch ID, Go rejects it fail-closed with HTTP 422."
- **Verifying Source Code:**
  - `services/api/internal/ai/schema.go` (`ValidateGrounding()`).
- **Automated Test Evidence:**
  - `Scenario 27` in `services/api/cmd/acceptance/main.go`:
    ```text
    [PASS] Scenario 27: Fabricated evidence identifier strictly rejected
          Detail: Hallucinated evidence reference rejected with HTTP 422 INVALID_EVIDENCE_REFERENCE
    ```

---

### Claim 6: "The model runs locally on Ollama Llama 3.2 with zero external API fees."
- **Verifying Source Code:**
  - `services/api/internal/ai/ollama.go` (Targets local port `http://localhost:11434/v1/chat/completions`).
  - `services/api/internal/readiness/probes.go` (`ProbeAIHealth` and `ProbeAIInference`).
- **Automated Test Evidence:**
  - `services/api/internal/ai/ai_test.go` (`TestOllamaProvider_Success`, `TestOllamaClient_CheckHealth`, and `TestOllamaClient_SensitiveLoggingSanitization`).
  - `services/api/internal/readiness/readiness_test.go` (`TestGenuineInferencePassesSchemaAndGrounding`, `TestMockProviderConfigured_FailsSubmission`, and `TestGenuineInferenceTimeout`).
- **Operational Boundary:**
  - Simulated and mock unit tests run in CI without external dependencies. Genuine model inference requires an active local daemon (`ollama serve`) and installed model (`ollama pull llama3.2`). Model availability alone does not satisfy inference verification; the diagnostic must execute a non-mutating synthetic probe and pass grounding checks.

---

### Claim 7: "Zero False-Green Readiness Gates: Mock providers and unverified credentials cannot pass submission profile."
- **Verifying Source Code:**
  - `services/api/internal/readiness/runner.go` (Evaluates `core` vs `submission` profiles; requires discrete `PASS`, `FAIL`, `BLOCKED`, `WARN`, or `SKIPPED` statuses).
  - `services/api/internal/readiness/probes.go` (Guards against mock AI or fake PayPal servers passing live submission checks).
- **Automated Test Evidence:**
  - `services/api/internal/readiness/readiness_test.go` (20 test scenarios verifying PostgreSQL down, API health failure, policy rejection, missing credentials, OAuth failures, model missing, mock provider rejection, and profile gate isolation).

---

### Claim 8: "Synthetic Fixture Isolation & Truthful PayPal Provenance — Demo fixtures are cryptographically and server-side isolated from live provider transactions."
- **Verifying Source Code:**
  - `services/api/internal/db/migrations/000005_fixture_provenance.up.sql`: Explicit `is_synthetic_fixture` boolean column and indexes on `payout_cases`, `payout_operations`, and `payout_status_snapshots`.
  - `services/api/internal/seeder/seeder.go`: Idempotent seeder logic; sets `IsSyntheticFixture = true`; assigns unmistakable identifiers (`SIMULATED-DEMO-BATCH-W9821`, `SIMULATED-DEMO-ITEM-W9821`); records provider as `"SIMULATED_DEMO"`; states in investigation text that findings derive from simulated data. Safe reset deletes only synthetic fixture records (`is_synthetic_fixture = true`) without touching non-demo accounts or transactions.
  - `services/api/internal/payouts/service.go`: Server-side mutation guards in `SubmitPayoutOperation`, `SyncPayoutCase`, and `CancelPayoutOperation` reject synthetic fixtures with `ErrFixtureExternalOperationForbidden` before making any outbound HTTP call.
  - `services/api/internal/payouts/handler.go`: Maps `ErrFixtureExternalOperationForbidden` to HTTP 403 Forbidden with error code `FIXTURE_EXTERNAL_OPERATION_FORBIDDEN`.
  - `apps/web/src/app/page.tsx`: Landing page evaluator credentials banner is gated behind `NEXT_PUBLIC_DEMO_MODE=true` and hidden by default.
  - `apps/web/src/app/app/payouts/[id]/page.tsx`: Displays prominent `SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION` banner; disables external mutation buttons for synthetic records.
- **Automated Test Evidence:**
  - `services/api/internal/seeder/seeder_test.go`:
    - `TestSeed_FirstRunCreatesAllFixturesAndMemberships`: Verifies creation of all fixture cases and adds `officer` to `demoOrg`.
    - `TestSeed_IdempotentSecondRunCreatesNoDuplicates`: Verifies repeated seeding produces zero duplicate records.
    - `TestReset_RemovesOnlySyntheticFixturesAndPreservesRealData`: Verifies reset purges only synthetic records and preserves non-demo cases and users.
    - `TestSeed_WriteErrorAborts`: Verifies fail-closed abort on database write errors.
    - `TestSeed_InactiveAccountFails`: Verifies disabled demo accounts are rejected.
  - `services/api/internal/payouts/service_fixture_guard_test.go`:
    - `TestFixtureExternalOperationGuards`: Asserts that `SubmitPayoutOperation`, `SyncPayoutCase`, and `CancelPayoutOperation` on synthetic fixtures return `ErrFixtureExternalOperationForbidden` and make exactly **zero outbound requests** to PayPal.
  - `services/api/internal/seeder/seeder_postgres_test.go`:
    - `TestSeeder_PostgresIntegration`: Verifies migrations, idempotent seeding (zero duplicates), scoped reset, and non-fixture preservation against a real disposable PostgreSQL database.

---

### Claim 9: "Truthful Diagnostic Probe — API Gateway reachability is strictly partitioned from PayPal Sandbox OAuth authentication."
- **Verifying Source Code:**
  - `apps/web/src/app/page.tsx` (`checkGatewayHealth()` probes `/healthz` for gateway and parses `/api/v1/sandbox/status` validating contract: `status === "authenticated"`, `token_valid === true`, `paypal_env === "sandbox"`).
  - Diagnostic UI renders discrete status pills (API Reachable vs PayPal Authenticated / Blocked / Unreachable) with zero secret dumps.
- **Automated Test Evidence:**
  - `apps/web/e2e/health.spec.ts`: Asserts prototype badge is rendered, "Verified Release" is absent, and diagnostic probe partitions states without raw token leaks.

---

### Claim 10: "End-to-End Browser Regression & Workflow Verification — Validates full stack in real browser sessions."
- **Verifying Source Code:**
  - `apps/web/playwright.config.ts` and `apps/web/e2e/*.spec.ts`.
- **Automated Test Evidence (27/27 Tests Across 7 Spec Files):**
  - `apps/web/e2e/health.spec.ts` (3 tests): Startup, landing page claims, and diagnostic health probe.
  - `apps/web/e2e/auth.spec.ts` (5 tests): Protected route redirects, invalid credentials, operator & officer logins, demo mode banner.
  - `apps/web/e2e/fixtures.spec.ts` (3 tests): DEMO-W9821 registry visibility, prominent provenance banner, offline synthetic investigation view, and disabled action buttons.
  - `apps/web/e2e/policy-workflow.spec.ts` (3 tests): $350 case creation, dual-custody detection, self-approval blockage, distinct officer approval, and no auto-submission side effect.
  - `apps/web/e2e/mutation-guards.spec.ts` (4 tests): Direct API rejection (HTTP 403 `FIXTURE_EXTERNAL_OPERATION_FORBIDDEN`) on fixture submit, sync, and cancel.
  - `apps/web/e2e/responsive-nav.spec.ts` (8 tests): Desktop nav, accessible mobile drawer toggle & Escape dismiss, automatic route close, skip link, 6-viewport overflow check, form semantics, password toggle, 44px touch targets.
  - `apps/web/e2e/demo-recording.spec.ts` (1 test): Deterministic 1080p browser walkthrough recording producing `claimbridge-demo-walkthrough.webm`, verified by automated media quality gate (`verify-recording-artifact.mjs`) asserting 1920×1080 resolution, 120.0s–175.0s duration, and silent WebM container.
