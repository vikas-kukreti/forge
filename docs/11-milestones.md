# 11 — Milestones & acceptance (build order M0→M8)

## 0. Rules of engagement

- **MIL-01 (MUST):** Build strictly in order; a milestone is *done* only when its `make e2e-mN` passes and its scope IDs are implemented with unit tests. Do not start M(N+1) before that (trd §6.1).
- **MIL-02 (MUST):** e2e scripts live in `e2e/` (bash, `set -euo pipefail`, curl+jq), are self-contained (they start whatever they need via Makefile targets against the dev compose stack), idempotent, exit non-zero on any failed assertion, and print a one-line PASS/FAIL summary per check. Default environment for every script: `FORGE_TLS=off`, `FORGE_RUNTIME=runc`, `FORGE_FAKE_LLM=1` (zero-spend, CI-runnable — trd §6.5). Only `e2e-golden` requires gVisor.
- **MIL-03 (MUST):** CI (`.github/workflows/ci.yml`): on every push — lint (Go + web), unit tests, `tsc --noEmit`; plus the e2e scripts of all *completed* milestones (m0..current) in runc mode. `e2e-golden` is a separate workflow (manual dispatch / nightly on a gVisor-capable runner) — if no such runner exists, it is a documented manual gate before release.
- **MIL-04 (MUST):** Each milestone ends with: `DECISIONS.md` updated (trd §6.4), `VERIFIED.md` updated when third-party surfaces were touched (trd §6.2), and `VERSIONS.md` reflecting any newly pinned dependency.

## M0 — Skeleton: repo, CI, config, datastores, migrations

**Goal:** everything boots empty; the build/test loop exists.
**Scope:** repo layout (trd §6.8); Makefile (`build`, `lint`, `test`, `dev-up/down`, `dev`, `templates`, `seed`, `e2e-m0`); compose stack (OPS-03); config loaders with fail-fast (OPS-11/12); migrations 0001 embedded + advisory-lock apply (DM-01, docs/02 §2); healthz/readyz + metrics listeners (OPS-13); JSON logging (OPS-14); CI workflow (MIL-03); stub `VERSIONS.md`/`DECISIONS.md`/`VERIFIED.md`/`README.md`.
**`make e2e-m0` asserts:** compose up healthy → `forged` boots, applies 0001, and is *restartable* (idempotent migrations, two boots in a row); all five binaries build for amd64+arm64; `forged`/`forge-llmproxy` (fake mode)/`forge-gateway` (`FORGE_TLS=off`) answer `/healthz` and `/readyz`; launching `forged` with a required var missing exits non-zero with a clear message; `go vet`/lint/unit suites green.

## M1 — Identity, projects (metadata), credits ledger

**Goal:** accounts, sessions, project rows, money integrity — no containers yet.
**Scope:** API-01..04; auth + `/v1/me` + logout (SEC-01); project CRUD with `runtime_state='cold'` and `preview_id` minting (DM-03); signup grant + ledger transaction + balance cache (LLM-09, DM-05) + on-demand reconciler job; admin list/grant/suspend (docs/03 §Admin, SEC-08 API-level parts, SEC-09 audit lines); ceilings + signup knob (SEC-07 create-path, SEC-14); rate limits (API-03).
**`make e2e` asserts (M1 part):** signup → ledger row `signup_grant` and `balance_after` == grant; mutation without `X-Forge-CSRF` → 403; user B fetching user A's project → 404 (API-04); 21st project → `validation_failed` (SEC-07); admin grant reflected in balance + audit log line present; suspended user: login → 403, existing session mutations → 403; login rate limit trips → 429 + `Retry-After`; reconciler reports zero mismatches; `FORGE_SIGNUPS=closed` blocks signup.

## M2 — Workers, sandboxes (runc), fake agent, event pipeline

**Goal:** a prompt produces streamed events and real files in a real (runc) container.
**Scope:** `forge-noded` heartbeat/registration + scheduler placement (ARC-16/17); NATS rpc ops `project.start/stop/destroy`, `fs.tree/read/write` (DM §4, ARC-11); workspace layout + template packaging/extraction (`make templates`, SBX-13, TPL-01..04 contents); container spec with `FORGE_RUNTIME=runc` seam (docs/04 §3, SBX-06/07); bridge + iptables + egress proxy + DNS stub (SBX-03, SBX-10..12); `forge-shim` complete in `--fake-agent` mode (SHM-01..05, AGT-14); event flow shim→noded→NATS→forged persist (DM-07) → WS + gap-fill (API-10..12); task lifecycle + single-runner index (DM-04) + abort plumbing (ARC-13, fake agent honors it); start-on-demand for never-started projects; path confinement (SEC-06) incl. tar-extract hardening.
**`make e2e` asserts (M2 part):** noded registers (row in `nodes`, heartbeats advance); create project → container `forge-sbx-<id>` running as uid 1000, read-only rootfs, no published ports (inspect flags); WS `hello` then task `SMOKE:` streams `user.prompt→tool.*→task.done` with monotonic `seq`; reconnect mid-task and gap-fill yields no gaps/dupes; `files/tree`+`read` return the fake-agent-written `index.html`; second concurrent task → 409 `task_running`; abort mid-task → `aborted`; fs read of `../../etc/passwd` and of `/home/dev/.pi/**` → rejected (SEC-06); from inside the sandbox: `curl` to a non-allowlisted domain, to the host gateway IP, and to another sandbox IP all fail (SBX-03/10); destroy removes container + workspace dir.

## M3 — Real pi + LLM proxy + credits end-to-end (fake LLM)

**Goal:** the actual agent works, metered and billed, with zero spend in tests.
**Scope:** sandbox image build + pin (SBX-04/05, AGT-03); `internal/piproto` + `VERIFIED.md` transcription (AGT-02); per-workspace `models.json`/`settings.json`/`AGENTS.md` written by noded (AGT-04..06); shim real mode: spawn pi RPC, map events (AGT-07/08), per-prompt model (AGT-05); `forge-llmproxy` complete (LLM-01..11) incl. SSE passthrough, model rewrite + `max_tokens` clamp (LLM-04, AGT-11), transactional debit + `usage.update` + balance notify (LLM-05, DM-05), attribution (ARC-12); fake-LLM scenarios (LLM-12/13, AGT-15); budget guards in forged (ARC-14, `FORGE_TASK_MAX_CALLS`); token-overhead CI test (AGT-09/10); insufficient-credits pre-check (docs/03 tasks 402).
**`make e2e-m3` asserts:** with real pi + fake LLM: `SMOKE:` task creates `smoke.txt` in ≤3 turns (AGT-15); `llm_calls` rows carry fixed fake usage and the ledger debit equals the pricing-file math exactly (LLM-07/13); task row aggregates tokens/cost; WS shows `usage.update` and header balance change; balance set to 0 → new task 402 and llmproxy records `denied_balance`; per-project 1-concurrent + rate denial path (`denied_rate`); abort → pi SIGINT fallback ≤5 s (ARC-13); unknown/rotated token → 401 (LLM-02); first-request `input_tokens` < 3,500 (AGT-09); AGENTS.md lint ≤60 lines (AGT-10).

## M4 — Gateway: platform host, previews, share links

**Goal:** the browser reaches everything through one front door; previews are private.
**Scope:** `forge-gateway` routing table + sync (docs/07 §2, DM §5, `forge.routes`); dev host mode (GW-02); platform-host proxy incl. WS passthrough; internal CA + mTLS hop to noded (ARC-04/07, GW-04, OPS-09); preview authz matrix + share links (GW-03, docs/03 share endpoints); cookie/header stripping (SEC-02) + response `Set-Cookie` domain guard; security headers (GW-13, SEC-04 noindex); waking page + `/internal/wake-preview` for stopped containers (GW-05); limits/drain/metrics (GW-11/12); activity reporting (GW-05→SHM-05).
**`make e2e-m4` asserts:** `app.localtest.me:8088` serves the API through the gateway (M1 curl suite re-run through it); preview host: anon → 403 branded; owner session → 200 (and `forge_preview` minted); share URL → 302 to clean URL + subsequent 200; foreign user → 403; Vite HMR websocket upgrades end-to-end (101 through both hops); gateway unit tests prove `forge_session`/`Authorization`/inbound `X-Forge-*` never reach the upstream and broad-`Domain` `Set-Cookie` is dropped (SEC-02); stop the container → preview shows waking page → auto-recovers when dev server returns; unknown host → 421; per-host rate limit trips.

## M5 — Hibernate, snapshots, wake, multi-node, resilience

**Goal:** projects are durable, movable objects; nodes are cattle.
**Scope:** snapshot/restore + prune-to-3 (SBX-14, DM §3); idle-stop + periodic snapshots (flow 3.4, ARC-16 timers); wake-on-demand from snapshot via API/gateway (GW-05 full path, tasks auto-wake); cross-node restore (ARC-15); LLM token rotation on every start (ARC-09) enforced; node-down detection + re-wake elsewhere (ARC-16); drain (ARC-18); noded reconcile + orphan reaping (OPS-19).
**`make e2e-m5` asserts:** two noded instances on one host (distinct `WS_ROOT`, `FORGE_SBX_BRIDGE`, `FORGE_SBX_SUBNET`, node names — MIL seam only); marker file written on node A; idle-stop (test-tuned `IDLE_STOP_MIN=1`) → snapshot in S3, container gone, `runtime_state='cold'`; drain A → wake → placed on B with marker intact (ARC-15); old LLM token → 401, new token works; kill A's heartbeats → its projects marked stopped within 3 intervals and wake on B from last snapshot; snapshots pruned to last 3; destroy deletes S3 snapshots (SBX-15).

## M6 — Publish: static + server apps, scale-to-zero

**Goal:** one click from private workspace to public URL.
**Scope:** publish API + slug rules (docs/03, DM-06); build + kind detection (GW-06); static artifact serve + LRU cache + headers + SPA fallback (GW-07); server apps: `forge-app-*` containers, shim `--app` mode, `/data` + nightly appdata snapshot (GW-08); scale-to-zero + wake-on-request (GW-09); unpublish/republish + route invalidation (GW-10, DM §5); publishes surfaced in admin.
**`make e2e-m6` asserts:** publish `static` template → `slug.apps.localtest.me:8088` serves; hashed asset → `immutable` cache header, html → `max-age=60`; republish → version bumps and new content serves (cache keyed `slug@version`); publish `fullstack-hono` → `/api` responds and data written to `/data` survives an app-container restart; an echo route (written into the workspace via `fs.write` before publishing) proves no `forge_*` cookie or client `X-Forge-*` header arrives (SEC-02); idle (test-tuned `APP_IDLE_STOP_MIN=1`) → container stops; next request wakes ≤30 s (GW-09); unpublish → route gone (404/421) and container stopped; `slug_taken`/`slug_reserved` rejected; static serving works with `forged` stopped (GW-07 "never touches forged").

## M7 — Frontend

**Goal:** the product is usable by a human, within budget.
**Scope:** FE-01..19 complete; embed + deep links (FE-02); workspace three panes + WS lifecycle (FE-06..13); publish dialog (FE-14); admin pages; bundle budget check in CI (FE-15); Playwright smoke (FE-18).
**`make e2e-m7` asserts:** Playwright (against fake agent + fake LLM, `FORGE_TLS=off`): signup → dashboard → new project → send `SMOKE:` → streamed events render, task chip shows cost → preview iframe loads template page → share link copied and opens in a fresh context → publish static → app URL opens → balance in header decreased; reconnect test (kill WS, events gap-fill, no dupes); bundle check: initial-route JS ≤150 KB gz (FE-15); `tsc --noEmit` + eslint clean; error-code union test (FE-19).

## M8 — Hardening, gVisor golden path, budgets, release audit

**Goal:** production-shaped, audited, measured.
**Scope:** gVisor on (SBX-01 verify incl. `dmesg` check); isolation e2e on runsc (SEC-05); resource budget measurements (trd §6.7: control-plane RSS <150 MB, noded <50 MB, shim <15 MB, AGT-09 re-assert, FE-15 re-assert); OpenAPI route-sync test (API-13); dependency + secret scans wired (SEC-10); `SECURITY-AUDIT.md` completed (docs/10 §5); prod deploy assets finalized + walked on the 2-node reference install (OPS-06..10, trd §6.9), incl. CertMagic DNS-01 verified against a real domain (manual step, documented with evidence in the audit file); observability examples (OPS-15); backup/restore runbook rehearsed once (OPS-18: restore Postgres dump into a fresh control node, workers re-register, project wakes from S3).
**`make e2e-golden` (gVisor host) asserts:** full loop — signup → project → fake-LLM build → private preview → share → publish static + server → both public URLs serve → server app scales to zero and wakes — all under `runsc`; SEC-05 isolation matrix green; RSS budgets measured and recorded in `BUDGETS.md`. Optional `FORGE_GOLDEN_REAL_LLM=1` variant runs one tiny real-model task (operator-triggered only, never CI-default).

## Definition of done (v1)

All MUST requirements across docs/01–10 implemented and traceable to a test or the M8 audit; `e2e-m0..m7` green in CI (runc); `e2e-golden` green on real gVisor; fresh 2-node install from docs/09 succeeds; `VERSIONS.md`, `DECISIONS.md`, `VERIFIED.md`, `SECURITY-AUDIT.md`, `BUDGETS.md` complete. (Mirrors trd §6.9.)
