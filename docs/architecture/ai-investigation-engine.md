# ClaimBridge AI Investigation Engine Architecture

## 1. Overview
The ClaimBridge AI Investigation Engine provides automated, evidence-grounded analysis of payout cases and payment lifecycle anomalies. It assists human claims officers and financial auditors by interpreting status records, detecting missing information, and recommending the safest next actions without ever executing financial transactions.

```
+-------------------------------------------------------------------------+
|                              Next.js Web                                |
|  - Case Details Page -> Investigation Panel                             |
|  - Distinct badges: [VERIFIED PAYPAL DATA] vs [AI ADVISORY OPINION]     |
|  - Action buttons route to existing Human Approval / Sync workflows     |
+-------------------------------------------------------------------------+
                                    |
                          HTTPS / Same-Site Session
                                    v
+-------------------------------------------------------------------------+
|                              Go API Server                              |
|  1. Auth & Organization Tenant Guard                                    |
|  2. Load Case, Operations & Snapshots from PostgreSQL                   |
|  3. Freshness Policy / Optional PayPal Sync Check                       |
|  4. Assemble Normalized Investigation Snapshot                          |
|  5. Redact Sensitive Fields & Apply Prompt-Injection Defense Envelope   |
|  6. Invoke AIProvider (Local Ollama / Free OpenAI-compatible Endpoint)  |
|  7. Validate JSON Schema & Grounding (Cross-check all Evidence IDs)     |
|  8. Evaluate Policy Precedence (Rust Policy Engine verification)        |
|  9. Persist Audit Record in `payout_investigations`                     |
+-------------------------------------------------------------------------+
           |                                              |
           v                                              v
+-------------------------+                   +-------------------------+
|   PostgreSQL Database   |                   |   Rust Policy Engine    |
| - payout_investigations |                   | - Verifies whether any  |
| - payout_cases          |                   |   recommended action is |
| - payout_operations     |                   |   legally permissible   |
| - payout_snapshots      |                   +-------------------------+
+-------------------------+
```

---

## 2. Security & Threat Model

| Threat | Risk Level | Mitigation in ClaimBridge |
|---|---|---|
| **Prompt Injection via Case Description or Recipient Details** | HIGH | All untrusted input fields are length-bounded (max 500 chars), stripped of control characters, and wrapped in strict `<UNTRUSTED_EVIDENCE>` delimiters. Versioned system prompt explicitly mandates treating inputs as passive evidentiary facts and ignoring embedded instructions. |
| **Evidence Hallucination & Fake IDs** | HIGH | The Go service collects valid snapshot IDs (`case_id`, `operation_id`, `sender_batch_id`, `paypal_payout_batch_id`, `paypal_payout_item_id`, `snapshot_id`). If the model cites an ID not present in this set, backend validation fails closed (`INVALID_EVIDENCE_REFERENCE`). |
| **Autonomous / Unauthorized Money Movement** | CRITICAL | The investigation endpoint is strictly read-only and analytical. It possesses zero credentials or endpoints for executing payouts. Recommended next steps are advisory reason codes. Subsequent actions MUST go through the existing dual-approval submission pipeline. |
| **Secret Exfiltration to Inference Provider** | CRITICAL | Credentials (passwords, Argon2id hashes, PayPal client secrets, OAuth access tokens, session cookies) are excluded from prompt construction. |
| **Cross-Tenant Data Exposure** | HIGH | Database queries and repository lookups enforce `organization_id` foreign keys. Users cannot trigger or read investigations for other tenants. |
| **Denial of Service / Model Hanging** | MEDIUM | Inference requests are bounded by request timeouts (`AI_REQUEST_TIMEOUT_SECONDS`, default 30s), payload limits (max 8KB context, max 2048 response tokens), and concurrency limits. |

---

## 3. Whitelist of Allowed Recommendations

The investigation engine only accepts recommendations that belong to the following closed set:

1. `REQUEST_RECIPIENT_INFORMATION`: When recipient email or details appear missing, invalid, or unclaimed.
2. `CHECK_PROVIDER_STATUS`: When an operation is in-flight (`SUBMITTED`, `SUBMITTING`, or `RECONCILIATION_REQUIRED`) and requires fresh reconciliation from PayPal.
3. `REVIEW_RECIPIENT_DETAILS`: When recipient name, account status, or currency mismatch is suspected.
4. `REVIEW_CANCELLATION_ELIGIBILITY`: When an unclaimed payment may be cancelled, subject to deterministic policy checks.
5. `ESCALATE_TO_OPERATOR`: When anomaly scores or error messages require human compliance investigation.
6. `NO_ACTION_UNTIL_RECONCILED`: When ambiguous network timeouts require waiting for provider batch settlement.

---

## 4. Database Schema (`payout_investigations`)

```sql
CREATE TABLE IF NOT EXISTS payout_investigations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payout_case_id UUID NOT NULL REFERENCES payout_cases(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    requested_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    provider VARCHAR(64) NOT NULL,
    model VARCHAR(128) NOT NULL,
    prompt_version VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL, -- COMPLETED, FAILED, INVALID_EVIDENCE_REFERENCE
    summary TEXT NOT NULL DEFAULT '',
    confidence VARCHAR(32) NOT NULL DEFAULT 'INSUFFICIENT_EVIDENCE',
    requires_human_review BOOLEAN NOT NULL DEFAULT true,
    observed_facts JSONB NOT NULL DEFAULT '[]'::jsonb,
    findings JSONB NOT NULL DEFAULT '[]'::jsonb,
    recommended_next_steps JSONB NOT NULL DEFAULT '[]'::jsonb,
    missing_information JSONB NOT NULL DEFAULT '[]'::jsonb,
    risk_flags JSONB NOT NULL DEFAULT '[]'::jsonb,
    limitations JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_response JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 5. Free-First Inference Support

ClaimBridge is built to run 100% free with local model runtimes:
- **Default**: Local Ollama running at `http://127.0.0.1:11434` with model `llama3.2`, `mistral`, or `qwen2.5`.
- **OpenAI-Compatible Hosted Endpoints**: Any compatible free endpoint configured through `AI_BASE_URL` and `AI_API_KEY`.
- **Fail-Closed Graceful Degradation**: If AI is offline, the API reports `503 Service Unavailable` with message `"Investigation assistant is currently unavailable. Core payout operations remain fully functional."`

---

## 6. Supported Deployment Configurations & Readiness Probing

ClaimBridge supports two verified deployment configurations for model-backed AI investigation:

### Configuration A: Native Go API + Native Host Ollama
- **Go API**: Running natively on host (`go run ./cmd/server` in `services/api`)
- **Ollama**: Running natively on host (`ollama serve`)
- **Ollama Endpoint**: `http://localhost:11434` (or `http://127.0.0.1:11434`)
- **Environment Settings**:
  ```bash
  AI_ENABLED=true
  AI_PROVIDER=ollama
  AI_BASE_URL=http://localhost:11434
  AI_MODEL=llama3.2
  ```

### Configuration B: Containerized Go API + Host Ollama (Docker Desktop)
- **Go API**: Running in Docker container (`infra/docker-compose.full.yml`)
- **Ollama**: Running on host machine
- **Networking**: Configured with `extra_hosts: ["host.docker.internal:host-gateway"]`
- **Ollama Endpoint**: `http://host.docker.internal:11434`
- **Environment Settings**:
  ```bash
  AI_ENABLED=true
  AI_PROVIDER=ollama
  AI_BASE_URL=http://host.docker.internal:11434
  AI_MODEL=llama3.2
  ```
> [!NOTE]
> Container `localhost` refers to the container itself, NOT the host machine. Always use `host.docker.internal` when the Go API is containerized.

### Granular Readiness Diagnostics (`GET /api/v1/ai/status`)
The system distinguishes four distinct inference states:
1. **Ollama Reachable**: Verified via `/api/tags` or `/v1/models` socket probe.
2. **Model Available**: Confirmed by matching the configured model against Ollama's local catalog.
3. **Genuine Inference Succeeded**: Confirmed when a response satisfies JSON schema and evidence-grounding assertions.
4. **Inference Failed / Timed Out**: Explicitly categorized (`provider_http_error`, `malformed_model_json`, `network_transport_failure`, `AI_TIMEOUT`).

### Sensitive Logging Guarantees
- Raw model outputs, prompts, recipient details, and provider error bodies are **never** logged to disk or console.
- Log attributes record only sanitized classifications (`error_class`), payload size metadata (`response_bytes`, `raw_length`), and `case_id` correlation.
- Model failures **never** modify financial state. Investigations are strictly advisory and read-only.

