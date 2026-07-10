# 01 — Architecture

## 1. Topology

```
                        Internet
                           │
                    ┌──────▼───────┐   *.DOMAIN wildcard TLS
                    │ forge-gateway │  :443/:80
                    └───┬───┬───┬───┘
        app.DOMAIN /api │   │   │ {slug}.apps.DOMAIN
                        │   │ {pid}.preview.DOMAIN
                 ┌──────▼┐  └───────────────┐
                 │forged │◄────NATS────┐    │ mTLS :7443
                 └──┬──┬─┘             │    │
     Postgres ◄─────┘  │        ┌──────▼────▼─────┐
     S3/MinIO ◄────────┤        │  forge-noded    │  (× N worker nodes)
                       │        │  docker+runsc   │
                ┌──────▼─────┐  │ ┌─────────────┐ │
                │forge-llm   │◄─┼─┤ sandbox ctr │ │  egress ─► noded egress
                │proxy :8180 │  │ │ forge-shim  │ │  proxy :3128 ─► allowlist
                └─────┬──────┘  │ │  ├─ pi RPC  │ │
                      ▼         │ │  └─ dev srv │ │
               Anthropic API    │ └─────────────┘ │
                                └─────────────────┘
```

- **ARC-01 (MUST):** `forged`, `forge-gateway`, `forge-llmproxy` are stateless (state in Postgres/NATS/S3) and horizontally scalable. v1 deploys one of each on the control node; nothing may assume single-instance except where a NATS queue group is specified.
- **ARC-02 (MUST):** Worker nodes run only `forge-noded` + Docker with the `runsc` runtime. Workers hold **no provider secrets** and no Postgres access. Everything a worker knows arrives via NATS or its own disk.
- **ARC-03 (MUST):** All internal messaging (control-plane→node commands, node heartbeats, agent event streams, route invalidations, credit notifications) goes over **NATS** (core NATS; JetStream not required in v1). Subjects in `docs/02 §4`.
- **ARC-04 (MUST):** Node agents connect **outbound** to NATS; the only inbound port a worker exposes is `:7443` (mTLS, internal CA) used exclusively by gateways for preview/app proxying.
- **ARC-05 (MUST):** Sandboxes have no inbound ports published. Preview traffic path: `gateway → noded:7443 (mTLS) → container bridge IP:3000`.
- **ARC-06 (MUST):** Sandboxes have no direct internet. All egress goes through the node-local egress proxy (`noded` component) with a domain allowlist; the LLM endpoint is just another allowlisted domain. See `docs/04 §6`.

## 2. Internal authentication

- **ARC-07 (MUST):** A deploy-time internal CA (`deploy/ca/gen-ca.sh`, plain openssl) issues certs for gateway (client) and each noded (server). Gateway↔noded uses mTLS with SAN checks (`node-<name>.internal`).
- **ARC-08 (MUST):** `forged` internal endpoints (`/internal/*`, used by gateway for authz/route sync and by llmproxy for balance debit callbacks if colocated) require header `Authorization: Bearer $FORGE_INTERNAL_TOKEN` (random ≥32 bytes, from config). They bind on a separate listener/port (`:8081`) never exposed by the gateway.
- **ARC-09 (MUST):** Per-project **LLM session tokens**: opaque 32-byte random, stored hashed in Postgres (`projects.llm_token_hash`), rotated on every container start. Injected into the sandbox as `ANTHROPIC_API_KEY`. `forge-llmproxy` resolves token→(user, project) via Postgres with a 30 s in-memory cache. The real provider key exists **only** in llmproxy's environment.
- **ARC-10 (MUST):** shim↔noded transport is an HTTP/1.1 API over a per-sandbox **unix socket** bind-mounted into the container (`/run/forge/ctl.sock` inside ↔ `<ws>/ctl.sock` on host). No TCP between noded and shim.

## 3. Core flows (sequence specs)

### 3.1 Create project

1. `POST /v1/projects {name, template}` → forged validates, inserts row (`runtime_state='cold'`), picks node via scheduler (§4), publishes NATS request `forge.node.<id>.rpc {op:"project.start", project, template}`.
2. noded: creates `WS_ROOT/<project_id>/{work,home}`, applies XFS quota, extracts template tarball (local cache, else S3 `templates/<name>.tar.zst`), writes workspace `models.json`/`settings.json` for pi, generates nothing secret itself.
3. noded starts container (flags in `docs/04 §3`), waits for shim `/healthz`, replies `{ok, preview_id}`.
4. forged sets `runtime_state='running'`, `node_id`, rotates+stores llm token, emits `project.status` event on `forge.proj.<id>.events`.
- **ARC-11 (MUST):** `project.start` is idempotent per project (noded checks existing container by name `forge-sbx-<project_id>`).

### 3.2 Prompt → agent task

1. `POST /v1/projects/{id}/tasks {prompt, model?}` → forged checks: session auth, ownership, balance > 0, **no running task for this project** (else 409), rate limit. Inserts `tasks` row (`queued`).
2. forged → NATS `forge.node.<n>.rpc {op:"agent.prompt", project_id, task_id, prompt, model}` → noded → shim `POST /agent/prompt` → shim ensures `pi --mode rpc` is running (cwd `/workspace`, continue session) and forwards the prompt.
3. shim reads pi's RPC event stream (stdout), normalizes each event to the Forge event envelope (`docs/05 §4`), assigns per-project monotonic `seq`, streams ndjson to noded, which republishes on `forge.proj.<id>.events`.
4. forged: (a) every instance fans events out to that project's WebSocket clients; (b) a **NATS queue group `persist`** consumer writes events to `agent_events` in batches (≤50 events or 500 ms).
5. Meanwhile pi's LLM calls hit llmproxy, which meters, debits, and publishes `forge.user.<uid>.notify {balance}` after each call (forwarded to the client as `usage.update`).
6. Terminal pi event → shim emits `task.done {status}` → forged updates `tasks` row (status, token totals, cost aggregated from `llm_calls` where `task_id` matches — see ARC-12).
- **ARC-12 (MUST):** llmproxy attributes calls to the **project's current running task**: forged records `(project_id → task_id)` in `tasks` (status='running'); llmproxy resolves it via token→project + `status='running'` lookup (cached 5 s). One running task per project makes this unambiguous.
- **ARC-13 (MUST):** Abort: `POST /v1/tasks/{id}/abort` → node rpc `agent.abort` → shim asks pi to abort (RPC) and, if not acknowledged in 5 s, SIGINTs pi; task → `aborted`.
- **ARC-14 (MUST):** Budget guard: forged watches `usage.update` per task; if task cost exceeds `FORGE_TASK_COST_CAP_MICROCREDITS` (default 50 credits) it triggers ARC-13 with status `aborted` + error `task_budget_exceeded`. llmproxy independently hard-denies calls when user balance ≤ 0.

### 3.3 Preview

1. shim starts/supervises the dev process (template's `dev` script) on `0.0.0.0:3000` when a project is opened or on first prompt (`docs/04 §5`).
2. Browser loads `https://<preview_id>.preview.DOMAIN` (iframe in the SPA). Gateway resolves host → project → node (route table, `docs/07`), verifies the requester's Forge session cookie owns the project (or a share-link cookie), proxies via noded:7443 with `X-Forge-Target: sbx:<project_id>:3000`. WebSocket (HMR) passthrough MUST work.

### 3.4 Hibernate / wake

- Idle rules (noded-enforced, activity = prompts, WS attach, preview hits, fs API): stop container after `IDLE_STOP_MIN` (default 20 min; workspace stays on disk, `runtime_state='stopped'`). After `IDLE_EVICT_HOURS` (default 36 h) noded snapshots (`tar --zstd`, excludes per `.forgeignore`: `node_modules`, `.cache`, `dist`) → S3 `snapshots/<project>/<ts>.tar.zst`, deletes local, forged sets `runtime_state='cold'` + `snapshot_key`, frees scheduler allocation.
- Wake (`POST /wake`, opening the project, preview hit on cold project): scheduler picks a node (may differ), `project.start` with `restore_key`; noded downloads+extracts, starts container; shim runs `bun install` if `package.json` exists and `node_modules` doesn't, before starting dev. pi session history lives in `<home>/.pi/...` inside the snapshot, so conversation context survives moves.
- **ARC-15 (MUST):** Restore-on-different-node is a tested invariant (e2e in M5).

### 3.5 Publish

See `docs/07 §4`. Static: shim builds → noded tars `dist` → S3 → gateway serves from LRU cache. Server: workspace snapshot → app container (`npm start`, smaller limits) with scale-to-zero: gateway gets `503 route-cold` → asks forged `/internal/wake-app` → polls up to 30 s → proxies.

## 4. Scheduler (inside forged)

- **ARC-16 (MUST):** Nodes register via heartbeat (`forge.node.<id>.hb` every 10 s: capacity, allocated, versions). Missing 3 beats ⇒ `down`; its `running` projects are marked `stopped/unknown` and wake elsewhere from last snapshot (data loss window is acceptable and documented — snapshots also run every `SNAPSHOT_INTERVAL_HOURS`, default 12, for running projects).
- **ARC-17 (MUST):** Placement = filter (`status='ready'`, free mem ≥ request, free disk ≥ quota) then **most-free-RAM first**. Allocation ledger kept in Postgres (`nodes.alloc_*` updated transactionally on assign/release).
- **ARC-18 (MUST):** `drain` (admin API): node stops accepting placements; running sandboxes hibernate at next idle-stop; published server apps are re-placed on next wake.

## 5. Failure semantics

- NATS request timeouts: node rpc default 30 s (project.start with restore: 180 s). On timeout forged retries once, then surfaces `502 node_unavailable`.
- shim crash ⇒ container restarts it (it is PID 1 → container exits → noded restarts container ≤3 times/10 min, then `runtime_state='error'`).
- pi crash mid-task ⇒ shim emits `task.done {status:"error", error:"agent_crashed"}`; restart lazily on next prompt (`pi -c` resumes the session file).
- Postgres is the single source of truth; NATS messages are fire-and-forget except `rpc` request-reply. Event persistence loss on forged crash ≤ one batch (acceptable; UI replays from last stored seq + live stream).
