# MISSION_049 — AI Investigation Reliability Report

**Date:** November 12, 2026
**Branch:** `feat/mission-049-ai-reliability`
**Base:** `feat/claimbridge-investigation-workspace`

## 1. Root Cause

AI investigations timed out after 120 seconds because:

- `AI_MAX_OUTPUT_TOKENS` defaulted to 2048 (and `.env` did not override it), so `num_predict=2048` was sent to Ollama.
- On the user's local CPU, generating 2000+ tokens with `qwen3.5:4b` exceeded the 120-second `AI_REQUEST_TIMEOUT_SECONDS`.
- The Go HTTP client timeout and the request context timeout were both capped at 120s, so the request failed with HTTP 500 after ~120s.
- The Next.js proxy timeout (150s) was only marginally above the API timeout.
- Prompts requested up to 2 findings and 1 recommendation but did not enforce a tight token budget.

## 2. Benchmark Results (actual ClaimBridge investigation prompt)

Representative snapshot: DEMO-W9821 case, 1 operation, 1 status snapshot, 1217 prompt tokens.

| Model | num_predict | num_ctx | think | Elapsed | eval_count | Valid JSON | Evidence Valid |
|-------|-------------|---------|-------|---------|------------|------------|----------------|
| gemma3:4b | 512 | 4096 | n/a | ~75s | 141 | Yes | Yes |
| qwen3.5:4b | 512 | 4096 | false | ~43-45s | 217-220 | Yes | Yes |
| qwen3.5:4b | 256 | 4096 | false | ~43-45s | 218-220 | Yes | Yes |

Selected model: **qwen3.5:4b** (fastest valid model, ~43s end-to-end on CPU).

## 3. Optimizations Applied

- Default `AIMaxOutputTokens`: 2048 → 512.
- Default `AIRequestTimeout`: 60s → 180s (configurable via `AI_REQUEST_TIMEOUT_SECONDS`).
- Default `AIModel`: `llama3.2` → `qwen3.5:4b`.
- `.env` / `.env.example`: `AI_MAX_OUTPUT_TOKENS=512`, `AI_REQUEST_TIMEOUT_SECONDS=180`, `AI_CONTEXT_SIZE=4096`.
- Native Ollama requests already set `Think: false` (thinking disabled) and `Format: "json"`.
- Improved timeout classification in `ollama.go` to detect both context deadline exceeded and HTTP client timeout.
- Service now propagates `ErrAITimeout` directly; handler maps it to HTTP 504 with code `AI_TIMEOUT`.
- Frontend (both investigation route and payout detail) now handles `AI_TIMEOUT` with a useful message.
- Next.js `proxyTimeout` increased from 150s to 300s.
- Strengthened prompt grounding instructions to require evidence references exclusively from `valid_evidence_ids`.

## 4. Test Evidence (End-to-End)

- Ran a real investigation against the seeded `DEMO-W9821` case with AI enabled.
- Result: **COMPLETED**, confidence **HIGH**, model **qwen3.5:4b**, 2 findings, 1 recommendation.
- Inference duration: **86.2s** (within 180s timeout).
- Evidence references validated against `valid_evidence_ids` (case UUID, batch/item IDs).
- Durable report persisted (4 total investigation records for the case, latest COMPLETED).
- Frontend successfully fetches and renders the investigation list.
- Repeated requests create distinct records (no silent duplicates; each run is a new durable investigation).
- `go test ./...` — all packages pass.
- `npm run lint` and `npm run build` — pass.

## 5. Safety Guarantees Preserved

- AI remains advisory; no execution or authorization of payouts.
- Deterministic Go/Rust policy remains authoritative.
- Evidence references validated against `valid_evidence_ids`; fabricated references rejected.
- Human approval requirements unchanged.
- Timeout produces a failed record, never a fabricated report.
- PayPal Sandbox restrictions unchanged.

## 6. Remaining Limitations

- Inference still takes ~43-98s on CPU; not suitable for synchronous high-throughput use.
- Only `qwen3.5:4b` and `gemma3:4b` were benchmarked; other models may be faster.
- Prompt size (~1200 tokens) could be further reduced for larger cases.
- No streaming or partial-result support; full response required.
- Docker Compose seeder runs with `AI_ENABLED=false` (by design; AI is exercised via the API container).
