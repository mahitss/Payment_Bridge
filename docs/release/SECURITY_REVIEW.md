# ClaimBridge — Security Review & Threat Remediation

**Security Baseline:** OWASP ASVS v4.0.3 Level 2 / OWASP Top 10 (2021)  
**Scope:** Go Orchestrator, Rust Policy Engine, Next.js Frontend, PostgreSQL Storage, and PayPal REST Boundary  
**Audit Status:** APPROVED FOR HACKATHON RELEASE  

---

## 1. Executive Summary

ClaimBridge is designed as a **fail-closed, defense-in-depth platform**. The financial authorization and money-moving boundaries are strictly decoupled from non-deterministic AI inference.

A comprehensive security audit was executed across all components. All identified vulnerabilities have been remediated, verified with automated test suites, and validated against regression gates.

---

## 2. Threat Vector Assessment & Remediation Matrix

### 2.1 Injection & Data Sanitization (OWASP A03:2021)

| Vulnerability Vector | Threat Impact | ClaimBridge Remediation | Verification Status |
| :--- | :--- | :--- | :--- |
| **SQL Injection** | Attacker extracts customer data or bypasses authentication via malformed inputs. | All database queries in `services/api/internal/db/postgres.go` use strictly parameterized SQL via `pgx/v5`. Zero raw string interpolation exists. | **VERIFIED CLEAN** (Static audit + acceptance runs) |
| **Cross-Site Scripting (XSS)** | Malicious scripts embedded in case descriptions or recipient names execute in user browsers. | React/Next.js automatic contextual escaping; AI Markdown rendering uses text sanitization without rendering raw HTML; `X-Content-Type-Options: nosniff` header enforced. | **VERIFIED CLEAN** (Frontend inspection & Scenario 32) |
| **Prompt Injection** | Attacker embeds adversarial prompt overrides inside payout references, descriptions, or provider error messages to trick the LLM. | User-supplied fields are strictly sanitized, capped to bounded lengths, and enclosed within `<UNTRUSTED_EVIDENCE>` boundary tags in the versioned system prompt (`claimbridge-investigation-v1.0`). The system prompt explicitly instructs the model to treat content inside the envelope as data, never as commands. Furthermore, Go and Rust enforce strict deterministic bounds regardless of model output. | **VERIFIED CLEAN** (Scenario 32 PASS) |
| **Fabricated Evidence / Hallucination** | Model invents fake PayPal batch IDs, amounts, or timestamps to justify a false payout status. | The Go grounding engine (`ValidateGrounding()`) collects all valid snapshot identifiers (`case_id`, `operation_id`, `sender_batch_id`, `paypal_payout_batch_id`, `paypal_payout_item_id`, `snapshot_id`). If any model-cited ID is missing from this set, the investigation is rejected with HTTP 422 `INVALID_EVIDENCE_REFERENCE`. | **VERIFIED CLEAN** (Scenario 27 PASS) |

---

### 2.2 Broken Access Control & Multi-Tenancy (OWASP A01:2021)

| Vulnerability Vector | Threat Impact | ClaimBridge Remediation | Verification Status |
| :--- | :--- | :--- | :--- |
| **Insecure Direct Object Reference (IDOR)** | User in Organization A accesses payout cases, operations, or investigations belonging to Organization B. | Every query in `postgres.go` includes `WHERE organization_id = $1` verified from the authenticated user's active session. Cross-tenant access returns HTTP 404 Not Found without leaking existence. | **VERIFIED CLEAN** (Scenarios 11, 23, 30 PASS) |
| **Dual-Custody Separation-of-Duties Bypass** | Initiator of a high-value payout case self-approves their own operation. | `services/api/internal/payouts/service.go` explicitly asserts `op.CreatedByUserID != actor.UserID`. Self-approval attempts return HTTP 403 Forbidden with `ERR_SELF_APPROVAL_FORBIDDEN`. | **VERIFIED CLEAN** (Scenario 18 PASS) |
| **AI Action Escalation** | Model suggests arbitrary commands (e.g., `TRANSFER_FUNDS` or `EXECUTE_SQL`). | Output is validated against a strict closed whitelist of 6 pre-approved types (`REQUEST_RECIPIENT_INFORMATION`, `CHECK_PROVIDER_STATUS`, `REVIEW_RECIPIENT_DETAILS`, `REVIEW_CANCELLATION_ELIGIBILITY`, `ESCALATE_TO_OPERATOR`, `NO_ACTION_UNTIL_RECONCILED`). Unrecognized actions trigger HTTP 422. | **VERIFIED CLEAN** (Unit test suite PASS) |

---

### 2.3 Cryptographic Failures & Session Security (OWASP A02:2021)

| Vulnerability Vector | Threat Impact | ClaimBridge Remediation | Verification Status |
| :--- | :--- | :--- | :--- |
| **Weak Password Storage** | Compromise of DB leaks user passwords. | Password hashing uses **Argon2id** (64MB memory, 3 iterations, 4 parallelism threads, 32-byte salt, 32-byte key) with constant-time verification. | **VERIFIED CLEAN** (`crypto_test.go` PASS) |
| **Session Hijacking / Fixation** | Stolen session cookies grant attacker unauthorized access. | 32-byte cryptographically secure random session tokens (`crypto/rand`). Only the SHA-256 hash is persisted in PostgreSQL. Cookies are marked `HttpOnly`, `SameSite=Lax` (or `Strict`), and `Secure`. Session is destroyed server-side on logout. | **VERIFIED CLEAN** (Scenarios 03, 05, 06 PASS) |
| **Token Replay Attacks** | Email verification or password reset tokens reused after consumption. | Single-use consumption enforced via atomic SQL transactions (`UPDATE ... WHERE consumed_at IS NULL`). Replay attempts return HTTP 400. | **VERIFIED CLEAN** (Scenarios 02, 08 PASS) |

---

### 2.4 Denial of Service & Resource Exhaustion (OWASP A05:2021)

| Vulnerability Vector | Threat Impact | ClaimBridge Remediation | Verification Status |
| :--- | :--- | :--- | :--- |
| **Large Payload Memory Exhaustion** | Attacker transmits multi-megabyte payloads to exhaust backend memory. | `bodyLimitMiddleware` in `server.go` enforces an explicit 1MB maximum request body size using `http.MaxBytesReader`. Oversized payloads are aborted with HTTP 400. | **VERIFIED CLEAN** (`TestServer_BodyLimitExceeded` PASS) |
| **Slowloris & Upstream Hangs** | Hanging external requests exhaust Go goroutines. | All inbound and outbound requests enforce bounded deadlines via `context.WithTimeout` (15s PayPal timeout, 30s AI timeout). | **VERIFIED CLEAN** (Timeout middleware verified) |
| **Model Downtime Cascade** | Failure of local Ollama or hosted inference engine crashes the payment platform. | All AI inference calls are wrapped in non-blocking, fail-closed handlers. An unreachable model returns HTTP 503 `AI_UNAVAILABLE` while leaving core payout operations 100% functional. | **VERIFIED CLEAN** (Scenario 29 PASS) |

---

### 2.5 Security Misconfiguration & Secret Protection (OWASP A05:2021)

| Vulnerability Vector | Threat Impact | ClaimBridge Remediation | Verification Status |
| :--- | :--- | :--- | :--- |
| **Accidental Production Fund Movement** | Automated test or misconfiguration executes real payouts against `api-m.paypal.com`. | Configuration loading asserts `PAYPAL_ENV=sandbox`. Any production configuration during testing or development triggers an immediate startup panic. | **VERIFIED CLEAN** (`config_test.go` PASS) |
| **Credential Bleed in Logs** | Access tokens or client secrets printed in stdout or APM logs. | Custom `RedactedTransport` and `RedactedLogger` automatically regex-mask `Bearer [A-Za-z0-9._-]+` tokens and secret parameters with `[REDACTED_ACCESS_TOKEN]`. | **VERIFIED CLEAN** (Log inspection verified) |
| **Missing Security Headers** | Clickjacking, MIME-sniffing, or frame injection attacks. | Server enforces `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy: camera=(), microphone=(), geolocation=()`, and `Content-Security-Policy: default-src 'self'`. | **VERIFIED CLEAN** (`TestServer_SecurityHeaders` PASS) |

---

## 3. Residual Risk Assessment

| Risk Description | Severity | Likelihood | Mitigation in Place |
| :--- | :--- | :--- | :--- |
| **Local Ollama Inference Latency** | Low | Medium | Local LLM inference on CPU may take 5–15 seconds. Frontend displays responsive animated pending states with timeout cancel options. Core payout operations remain completely independent of AI latency. |
| **Sandbox Rate Limiting by PayPal** | Low | Low | Token caching with 60-second safety buffer prevents redundant OAuth token requests; idempotent `sender_batch_id` prevents duplicate submissions. |
