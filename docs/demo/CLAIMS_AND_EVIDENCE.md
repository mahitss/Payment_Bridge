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
