# 06 — LLM metering proxy & credits (`forge-llmproxy` :8180)

## 1. Role

Anthropic-Messages-API-compatible reverse proxy. Sandboxes (pi) call it as if it were the provider; it authenticates the **project session token**, enforces balance/limits, swaps in the real provider key, streams the response through, computes cost from the provider's reported usage, debits the ledger, and notifies.

- **LLM-01 (MUST):** Public path `POST /anthropic/v1/messages` (+ pass-through of exactly the endpoints pi needs — verify at build; anything else → 404). Reached by sandboxes via hostname `llm.internal.<DOMAIN>` (resolvable on the internal network; TLS via internal CA or plain HTTP on a trusted network segment — operator choice, default: internal-CA TLS since traffic transits the egress proxy CONNECT).
- **LLM-02 (MUST):** Auth: token from the provider-key header/env pi uses (`x-api-key` for Anthropic wire format). Resolve SHA-256(token) → `projects` row (30 s cache). Unknown/rotated token → 401. Suspended user → 403.
- **LLM-03 (MUST):** Pre-flight checks, cheap and in order: balance > 0 (5 s cached, but re-checked uncached when cached balance < 5 credits) → per-project rate ≤ 30 req/min and 1 concurrent → model allowed. Deny responses use Anthropic-style error JSON with HTTP 402/429 and `llm_calls.status` `denied_balance|denied_rate`.
- **LLM-04 (MUST):** Streaming (SSE) is passed through unbuffered; usage is taken from the terminal usage payload of the stream (or response body when non-streaming). Request `model` (exposed id, e.g. `forge/sonnet`) is rewritten to `provider_model`; request `max_tokens` clamped per AGT-11.
- **LLM-05 (MUST):** After completion: insert `llm_calls` row; **transactionally** debit ledger (DM-05) with `reason='llm_usage', ref_type='llm_call'`; resolve `task_id` per ARC-12; publish `forge.user.<uid>.notify {type:"balance",...}` and `forge.proj.<pid>.events` `usage.update`. Provider errors are recorded (`provider_error`) and **not billed**.
- **LLM-06 (MUST):** Timeouts: connect 10 s, total 10 min (long streams). Retries: none (pi handles retry semantics). Real key(s) from `FORGE_ANTHROPIC_API_KEY`; architecture leaves room for more providers later but v1 implements Anthropic wire format only (pi's `forge` provider is configured as anthropic-compatible).

## 2. Pricing config (`deploy/pricing.yaml`, hot-reload on SIGHUP)

```yaml
credit_usd_hint: 0.01          # informational only
models:
  - id: forge/sonnet           # exposed id (UI + pi models.json)
    provider_model: <operator sets, e.g. claude-sonnet-latest>
    display: "Sonnet (balanced)"
    input_per_mtok_microcredits:        300000   # 0.3 credit / 1M in
    output_per_mtok_microcredits:      1500000
    cache_write_per_mtok_microcredits:  375000
    cache_read_per_mtok_microcredits:    30000
    default: true
  - id: forge/eco
    provider_model: <small model>
    display: "Eco (fast & cheap)"
    input_per_mtok_microcredits:   50000
    output_per_mtok_microcredits: 250000
    cache_write_per_mtok_microcredits: 62500
    cache_read_per_mtok_microcredits:   5000
```
- **LLM-07 (MUST):** `cost = Σ(tokens × rate)/1e6`, integer math, round **up** to ≥1 µcr per call. Numbers above are placeholders; operator sets real rates (provider cost × margin). forged reads the same file to serve `GET /v1/models` (exposed list for the UI picker) and to generate sandbox `models.json` content.

## 3. Credit ledger rules

- **LLM-08 (MUST):** Balance may go slightly negative (a call in flight when balance hits 0 completes and is billed); new calls are denied at ≤0. No clawback.
- **LLM-09 (MUST):** Signup grant `FORGE_SIGNUP_GRANT_CREDITS` (default 50) written by forged at signup. Admin grants via API. `purchase` reason reserved (no payment integration in v1).
- **LLM-10 (SHOULD):** Daily per-user spend soft cap `FORGE_DAILY_SPEND_CAP_CREDITS` (default 0 = off): exceeding ⇒ denials with `denied_rate` + UI notice.

## 4. Observability

- **LLM-11 (MUST):** Prometheus: `forge_llm_requests_total{model,status}`, `forge_llm_tokens_total{model,dir}`, `forge_llm_cost_microcredits_total`, `forge_llm_latency_seconds` histogram, `forge_llm_active_streams`.

## 5. Fake LLM mode (`FORGE_FAKE_LLM=1`) — required test seam

- **LLM-12 (MUST):** Implements the same Anthropic wire surface (incl. SSE streaming and `tool_use` blocks) with deterministic scripted behavior, no upstream calls, fixed usage numbers (so credit math is assertable):
  - user text contains `SMOKE:` → turn 1: `tool_use` invoking pi's file-write tool to create `smoke.txt` with content `hello-forge`; turn 2 (after tool_result): short text "Done." + `end_turn`. Usage: 1000 in / 200 out per call.
  - contains `ERROR:` → provider 500. `SLOW:` → 5 s delay then normal. Anything else → echo text completion.
  - The tool names/schemas the fake must emit are taken from the actual request's `tools` array (echo pi's own declared write-tool name), keeping the fake independent of pi versions.
- **LLM-13 (MUST):** Fake mode still runs the full metering/ledger path — e2e asserts the exact debit for a SMOKE task.
