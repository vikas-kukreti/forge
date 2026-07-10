# 09 — Deployment & operations

## 1. Topologies

- **OPS-01 (MUST):** Two roles. **Control node(s)** run `forged`, `forge-gateway`, `forge-llmproxy`, and the datastores Postgres + NATS + MinIO (single-box dev; managed/replicated in prod at operator discretion — the binaries assume none of it is co-located). **Worker node(s)** run only `forge-noded` + Docker (with `runsc`). v1 target: 1 control + ≥1 worker; the golden-path e2e uses 1 control + 2 workers (to prove cross-node restore, ARC-15).
- **OPS-02 (MUST):** Binaries are static (`CGO_ENABLED=0`), one Go module producing five commands (`cmd/*`). Distributed as plain binaries + systemd units; no container required for the platform itself (only user sandboxes/apps are containers). `make build` produces all five for linux amd64+arm64.

## 2. Dev environment (`deploy/dev/`)

- **OPS-03 (MUST):** `deploy/dev/docker-compose.yml` brings up Postgres 16, NATS, MinIO with fixed dev creds and a `mc`-based bucket-create init. `make dev-up` starts it; `make dev-down` tears it down (with volumes optional).
- **OPS-04 (MUST):** `make dev` runs the whole platform locally against that compose stack **on a single machine acting as both control and worker**, with:
  - `FORGE_TLS=off` (gateway on :8088, hosts under `localtest.me`, GW-02),
  - `FORGE_RUNTIME=runc` (no gVisor needed to hack locally; a `make dev-gvisor` variant flips to `runsc` when available),
  - `FORGE_FAKE_LLM=1` by default (zero spend; set a real `FORGE_ANTHROPIC_API_KEY` + unset to use live models),
  - internal CA generated once via `deploy/ca/gen-ca.sh` into `deploy/dev/ca/`.
  A `.env.dev` (committed, no secrets) holds these; `make dev` sources it. Document how to switch any single service to real mode.
- **OPS-05 (MUST):** First-run: `forged` auto-applies embedded migrations (DM-01) and, if `FORGE_ADMIN_EMAILS` matches the first signup, that user becomes admin. `make seed` (optional) creates a demo user + project via the API. `make templates` builds template tarballs and uploads them to the (dev) bucket so `project.start` can restore them.

## 3. Production install (`deploy/prod/`)

- **OPS-06 (MUST):** **DNS prerequisites (documented in README + this doc):** wildcard records `DOMAIN`, `*.preview.DOMAIN`, `*.apps.DOMAIN` (and `llm.internal.DOMAIN` on the internal network) pointing at the control node / gateway; the ACME DNS-01 provider’s API credentials available to the gateway (GW-01). CertMagic obtains + renews wildcard certs automatically.
- **OPS-07 (MUST):** Control-node install: systemd units `forged.service`, `forge-gateway.service`, `forge-llmproxy.service` (each `Restart=always`, run as an unprivileged `forge` user, `EnvironmentFile=/etc/forge/forge.env`). Ordering: forged after Postgres+NATS reachable (units use a small wait-for script or `Requires`/`After` where co-located). Gateway binds :80/:443 (needs `CAP_NET_BIND_SERVICE` or `AmbientCapabilities`).
- **OPS-08 (MUST):** Worker bootstrap script `deploy/prod/worker-bootstrap.sh` (idempotent) installs Docker + gVisor (`runsc` runtime registered in `/etc/docker/daemon.json`), creates the XFS-`pquota` workspace mount at `WS_ROOT` (SBX-02) — documenting how to provision the XFS volume — pulls `forge-sandbox:<ver>` (SBX-04), drops the noded internal-CA server cert + key, installs `forge-noded.service`, and starts it. The node self-registers via NATS heartbeat (ARC-16); no control-plane action needed to add a worker beyond giving it NATS creds + a signed cert.
- **OPS-09 (MUST):** Internal CA: `deploy/ca/gen-ca.sh` (plain openssl) creates the CA and issues gateway (client) + per-node (server, SAN `node-<name>.internal`) certs (ARC-07). Rotation procedure documented (regen node cert, restart noded; regen gateway cert, restart gateway). Keys never leave their host; only the CA public cert is distributed to both sides.
- **OPS-10 (MUST):** Upgrades: control-plane binaries are drop-in-and-restart (stateless, ARC-01). Sandbox-image/pi upgrades are a deliberate rebuild → push to nodes → `piproto` re-verify (AGT-03). Node upgrades: `drain` (ARC-18), replace binary, un-drain. Migrations are forward-only and applied by forged at boot under advisory lock (safe with multiple forged instances).

## 4. Master configuration reference

- **OPS-11 (MUST):** All config via environment variables, one `internal/config` loader per binary with **fail-fast validation** at startup (missing required → exit non-zero with a clear message; never boot half-configured). Secrets are read from env/`EnvironmentFile` (mode 0600), never logged, never in the DB except as hashes. The table below is authoritative; every referenced var elsewhere in the TRD appears here.

| Var | Consumed by | Default | Secret | Meaning |
|---|---|---|---|---|
| `DOMAIN` | all | — (req) | no | Base domain; derives `*.preview.*`, `*.apps.*`, `llm.internal.*`. |
| `FORGE_DATABASE_URL` | forged, llmproxy | — (req) | yes | Postgres DSN. (llmproxy needs read + ledger write.) |
| `FORGE_NATS_URL` | forged, noded, gateway, llmproxy | — (req) | partial | NATS server URL(s); creds embedded or via nkey file. |
| `FORGE_S3_ENDPOINT` / `FORGE_S3_BUCKET` / `FORGE_S3_ACCESS_KEY` / `FORGE_S3_SECRET_KEY` / `FORGE_S3_REGION` | forged, noded, gateway | — (req) | keys yes | Object storage (MinIO/S3) for snapshots, templates, publish artifacts. |
| `FORGE_INTERNAL_TOKEN` | forged, gateway, llmproxy | — (req) | yes | Bearer for `forged` `/internal/*` (ARC-08). |
| `FORGE_COOKIE_SECRET` | forged, gateway | — (req) | yes | HMAC for `forge_session`/`forge_preview` signing. |
| `FORGE_ANTHROPIC_API_KEY` | llmproxy only | — (req unless `FORGE_FAKE_LLM=1`) | yes | Real provider key; exists **only** here (ARC-09). |
| `FORGE_TLS` | gateway | `on` | no | `off` → dev plain-HTTP mode (:8088, `localtest.me`), GW-02. |
| `FORGE_ACME_DNS_PROVIDER` (+ provider envs) | gateway | — (req if TLS on) | provider keys yes | DNS-01 provider for wildcard certs (GW-01). |
| `FORGE_GW_CACHE_DIR` | gateway | `/var/lib/forge/gwcache` | no | Static publish LRU cache dir (GW-07). |
| `FORGE_GW_CACHE_MAX_MB` | gateway | `2048` | no | Cache size cap. |
| `FORGE_RUNTIME` | noded | `runsc` | no | `runc` fallback for CI/dev (SBX-01 seam). |
| `WS_ROOT` | noded | `/var/lib/forge/workspaces` | no | Workspace root (XFS+pquota, SBX-02). |
| `FORGE_DISK_QUOTA` | noded | `xfs` | no | `soft` → du-based fallback (SBX-02). |
| `FORGE_SBX_SUBNET` | noded | `10.66.0.0/16` | no | Sandbox bridge subnet (SBX-03). |
| `FORGE_SBX_BRIDGE` | noded | `forge-sbx` | no | Bridge network name (SBX-03); override only to co-host multiple noded in tests (docs/11 M5). |
| `FORGE_EGRESS_ALLOWLIST` | noded | `deploy/egress-allowlist.yaml` | no | Egress domain allowlist path (SBX-10). |
| `FORGE_MODEL_DEFAULT` | forged, noded | (a mid-tier model id) | no | Default exposed model (AGT-05/13). |
| `FORGE_MAX_OUTPUT_TOKENS` | llmproxy | `8192` | no | Per-request `max_tokens` clamp (AGT-11). |
| `FORGE_TASK_MAX_CALLS` | forged | `60` | no | Max LLM calls/task (AGT-11). |
| `FORGE_TASK_COST_CAP_MICROCREDITS` | forged | `50_000_000` (50 cr) | no | Per-task budget guard (ARC-14). |
| `FORGE_SIGNUP_GRANT_CREDITS` | forged | `50` | no | Free credits on signup (LLM-09). |
| `FORGE_DAILY_SPEND_CAP_CREDITS` | forged/llmproxy | `0` (off) | no | Daily per-user soft cap (LLM-10). |
| `FORGE_ADMIN_EMAILS` | forged | empty | no | Comma list auto-promoted to admin on signup/login. |
| `FORGE_MAX_PROJECTS_PER_USER` | forged | `20` | no | Project-create ceiling (SEC-07). |
| `FORGE_MAX_RUNNING_SANDBOXES_PER_USER` | forged | `2` | no | Concurrent running-sandbox ceiling, scheduler-enforced (SEC-07). |
| `FORGE_SIGNUPS` | forged | `open` | no | `closed` ⇒ invite-only via admin API (SEC-14). |
| `SNAPSHOT_INTERVAL_HOURS` | forged/noded | `12` | no | Periodic snapshot of running projects (ARC-16). |
| `IDLE_STOP_MIN` | noded | `20` | no | Hibernate idle sandboxes after N min (flow 3.4). |
| `APP_IDLE_STOP_MIN` | noded | `10` | no | Scale-to-zero published server apps (GW-09). |
| `FORGE_PRICING_FILE` | forged, llmproxy | `deploy/pricing.yaml` | no | Model rates (LLM-07), SIGHUP hot-reload. |
| `FORGE_FAKE_LLM` | llmproxy | `0` | no | `1` ⇒ deterministic fake provider, no upstream calls (LLM-12; test seam, trd §6.5). |
| `FORGE_LOG_LEVEL` | all | `info` | no | `debug|info|warn|error`. |
| `FORGE_METRICS_ADDR` | all | `127.0.0.1:9090` (per svc, distinct ports) | no | Prometheus listener (§5). |

Out of scope of this table: env injected *into* sandbox containers by noded (`FORGE_PROJECT_ID`, `FORGE_START_SEQ`, `PORT`, proxy vars — docs/04 §3) and e2e-only flags (`FORGE_GOLDEN_REAL_LLM`, docs/11 M8).

- **OPS-12 (MUST):** A `docs/config.md` (or `--help`/`--print-config-schema` on each binary) enumerates these with types + whether required; CI test asserts the loader rejects a config missing any required var.

## 5. Observability

- **OPS-13 (MUST):** Every binary exposes Prometheus metrics on `FORGE_METRICS_ADDR` (bound to localhost / internal only, scraped by the operator’s Prometheus) and a `GET /healthz` (liveness) + `GET /readyz` (dependencies reachable: DB/NATS/S3 as applicable) on a local admin port. Component metrics defined in their docs (GW-12, LLM-11) plus: forged — `forge_tasks_total{status}`, `forge_task_duration_seconds`, active WS gauge, scheduler placement outcomes, migration status; noded — sandbox count by state, container start/restore/snapshot durations, egress-proxy allow/deny counts, disk-quota usage per node, heartbeat publish success.
- **OPS-14 (MUST):** Structured JSON logs (slog) to stdout with a request/task/project correlation id. **No secrets, no prompts, no LLM message bodies, no full file contents in logs** (SEC requirement echoed here). Log the stable `error.code`, not stack traces, at info; stacks only at debug.
- **OPS-15 (SHOULD):** Ship a `deploy/prod/prometheus.example.yml` + a Grafana dashboard JSON covering the golden signals (task throughput/latency/error, active sandboxes, credit spend rate, gateway class latencies, node capacity headroom). Alert examples: node down (missed heartbeats), balance-debit vs ledger mismatch (DM-05), llmproxy provider error rate, disk quota > 90%.

## 6. Backups & data safety

- **OPS-16 (MUST):** **Postgres is the only system of record that must be backed up** (users, projects, ledger, publishes, routes). Document `pg_dump`/PITR guidance; the credit ledger is append-only and reconciled nightly (DM-05). Losing NATS loses only in-flight messages (tolerable). Losing the gateway cache re-fetches from S3.
- **OPS-17 (MUST):** S3 holds project snapshots (last 3/project), templates, and publish artifacts. Snapshots are best-effort durability (workspace can be rebuilt by re-prompting); publish artifacts and `appdata` snapshots (GW-08) should be retained per operator policy. Document bucket lifecycle rules from DM §3.
- **OPS-18 (MUST):** Disaster/restore runbook: (a) restore Postgres → control plane comes back; (b) workers re-register via heartbeat; (c) projects wake from their last S3 snapshot on next access (cross-node restore is a tested invariant, ARC-15); (d) published static apps serve straight from S3, server apps re-place on next request. Document the acceptable data-loss window (work since last snapshot; ARC-16) prominently.
- **OPS-19 (MUST):** Deletion honors data ownership: `project.destroy` removes container + workspace + quota + S3 snapshots (SBX-15); account deletion cascades per FK `ON DELETE CASCADE` (docs/02) and enqueues destroy of all the user’s sandboxes/publishes. No orphaned containers/quotas (noded reconciles its inventory against forged on reconnect and reaps unknown `forge-*` containers).
