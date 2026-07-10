# 05 — Agent integration (pi)

## 1. Why pi, and the integration stance

pi (`@earendil-works/pi-coding-agent`, repo `github.com/earendil-works/pi`, formerly `badlogic/pi-mono`) is a minimal, token-efficient agent harness with interactive, print/JSON, **RPC (JSON over stdio)**, and SDK modes. It has **no built-in sandboxing** — isolation is entirely Forge's job (docs/04). Sessions persist as files under `~/.pi/agent/sessions/`, it loads `AGENTS.md` context files, supports custom providers via `models.json`, and auto-compacts long sessions.

- **AGT-01 (MUST):** Run pi in **RPC mode** as a child of `forge-shim`, one instance per project, cwd `/workspace`, sessions stored under `/home/dev/.pi/...` (inside the snapshot → history survives hibernate/node moves).
- **AGT-02 (MUST):** *Verify-at-build:* transcribe the exact RPC command/event JSON schema, the `models.json` custom-provider schema, and relevant `settings.json` keys from the pinned pi version's docs (`packages/coding-agent/docs/`) and TypeScript types into `VERIFIED.md`, and encapsulate 100% of pi-specific knowledge in Go package `internal/piproto` (used only by forge-shim). One integration test drives a real pi process against the fake LLM proxy and asserts the mapping.
- **AGT-03 (MUST):** Pin the pi version in the sandbox image; upgrades are a deliberate image rebuild + `piproto` re-verification.

## 2. LLM wiring (no keys in sandbox)

- **AGT-04 (MUST):** noded writes `<ws>/home/.pi/agent/models.json` defining a custom provider `forge` whose `baseUrl` is `https://llm.internal.<DOMAIN>/anthropic` (Anthropic-compatible; reached via the egress proxy allowlist), with the platform's exposed model list (docs/06 §2). pi authenticates with `ANTHROPIC_API_KEY` = the per-project session token (SBX-07). If the pinned pi version's custom-provider schema needs the key under a different env/name, follow pi's docs — the invariant is: **sandbox holds only the session token; llmproxy holds the real key.**
- **AGT-05 (MUST):** Model selection: task's `model` (API) > project `default_model` > `FORGE_MODEL_DEFAULT`. shim passes the model per-prompt if pi's RPC supports it, else sets default in settings.json at container start and per-task model switching is deferred (record in DECISIONS.md).

## 3. pi configuration written per workspace

- `settings.json`: default model `forge/<id>`, disable update checks/telemetry-ish features, keep pi's default minimal system prompt (do **not** add a custom SYSTEM.md), enable auto-compaction (defaults).
- `AGENTS.md`: from template (TPL-04), ≤60 lines.
- **AGT-06 (MUST):** No MCP servers, no extra pi extensions/skills in v1. The three built-in-ish tools pi ships with + bash are sufficient; every addition costs context.

## 4. Forge event envelope (normalized; the only schema FE/DB see)

```json
{"seq":123,"ts":"RFC3339","task_id":"uuid|null","type":"...","data":{...}}
```
Types (closed set, `internal/types/events.go`):
`task.started {prompt,model}` · `assistant.delta {text}` · `assistant.done` ·
`thinking.delta {text}` (if surfaced by pi; else omit) ·
`tool.start {name, summary}` · `tool.end {name, ok, summary, duration_ms}` ·
`file.changed {path, kind:"write|edit|delete"}` (derived from write/edit tool ends) ·
`usage.update {task_cost_microcredits, balance_microcredits, input_tokens, output_tokens}` (forged-injected from llmproxy notifications) ·
`compaction {note}` · `system.note {text}` · `dev.status {state:"starting|running|crashed|stopped", url?}` ·
`task.done {status:"done|error|aborted", error?}` · `project.status {runtime_state}` · `hello` (WS only).

- **AGT-07 (MUST):** `tool.start/summary` MUST be compact: tool name + first line/args digest ≤120 chars; full raw args/results are **not** shipped to the browser or DB (token- and storage-frugal UI; the file diff view reads via fs API on demand).
- **AGT-08 (MUST):** Mapping pi-RPC→envelope lives in `piproto` with an exhaustive switch; unknown pi event kinds map to `system.note` with a warn log (forward-compatible).

## 5. Token-efficiency requirements (product requirement, not a nicety)

- **AGT-09 (MUST):** Fixed per-task context overhead (pi system prompt + tool definitions + AGENTS.md + settings-injected text) ≤ **3,000 tokens**, asserted by a CI test that runs one fake-LLM task and measures the first request's `input_tokens` reported by the fake proxy (< 3500 incl. the prompt itself).
- **AGT-10 (MUST):** AGENTS.md ≤ 60 lines / ≤ 500 tokens per template (CI-linted).
- **AGT-11 (MUST):** Per-task caps enforced platform-side: max LLM calls per task `FORGE_TASK_MAX_CALLS` (default 60), cost cap ARC-14, single running task per project (DM-04). llmproxy caps `max_tokens` per request at `FORGE_MAX_OUTPUT_TOKENS` (default 8192) by rewriting the request field downward when necessary.
- **AGT-12 (MUST):** Rely on pi's auto-compaction for long sessions; never inject file trees, previous diffs, or logs into prompts platform-side. The user prompt is passed verbatim.
- **AGT-13 (SHOULD):** Default `FORGE_MODEL_DEFAULT` to a mid-tier model; expose an "eco" small model in the UI picker with its cheaper rate visible. Prompt-cache-friendly: nothing platform-side mutates the session prefix between turns.

## 6. Test seams

- **AGT-14 (MUST):** `forge-shim --fake-agent` (SHM-04) — no pi, no LLM; deterministic envelopes + real file writes. Used for shim/noded/forged/FE tests.
- **AGT-15 (MUST):** Fake LLM (`FORGE_FAKE_LLM=1` in llmproxy, docs/06 §5) — real pi, scripted Anthropic-compatible responses incl. tool_use, so `piproto` is tested against the real binary with zero spend. Scenario `SMOKE:` must cause pi to create `smoke.txt` via its write tool and finish in ≤3 turns.
