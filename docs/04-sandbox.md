# 04 — Sandbox, workspaces, shim

## 1. Host prerequisites (worker nodes)

- **SBX-01 (MUST):** Docker Engine ≥ 27 with gVisor `runsc` registered as runtime `runsc` (installed per gvisor.dev apt instructions; verify with `docker run --rm --runtime=runsc alpine dmesg | grep -i gvisor`). runsc platform: default (`systrap`) — works without KVM.
- **SBX-02 (MUST):** `WS_ROOT` (default `/var/lib/forge/workspaces`) on an **XFS filesystem mounted with `pquota`**. noded assigns one XFS project quota per workspace = `disk_quota_mb`. Dev fallback `FORGE_DISK_QUOTA=soft`: hourly `du` check, over-quota ⇒ agent prompts blocked + UI warning (never silent data loss).
- **SBX-03 (MUST):** Dedicated bridge network `forge-sbx` created by noded (`com.docker.network.bridge.enable_icc=false` → no container↔container traffic; subnet from `FORGE_SBX_SUBNET`, default `10.66.0.0/16`). noded installs iptables rules on the bridge: ALLOW tcp→`<bridge_gw>:3128` (egress proxy) and udp/tcp→`<bridge_gw>:53` (dnsmasq or noded's built-in stub resolver), DROP everything else (including RFC1918, link-local/169.254.0.0/16, and the host).

## 2. Sandbox image (`sandbox/Dockerfile` → `forge-sandbox:<ver>`)

- **SBX-04 (MUST):** Base `debian:bookworm-slim`. Contents: Node.js (current LTS, pinned), `bun` (used as installer: fast, low-RAM), `git`, `sqlite3`, `curl`, `ca-certificates`, build-essential *omitted* (add only if a template needs native builds — prefer prebuilt binaries), user `dev` uid/gid 1000 with home `/home/dev`, `pi` installed globally (`npm i -g --ignore-scripts @earendil-works/pi-coding-agent@<pinned>`), `forge-shim` at `/usr/local/bin/forge-shim` (CGO_DISABLED static build, copied in). Image target < 900 MB; pre-warm on nodes at deploy (`docker pull`).
- **SBX-05 (MUST):** Baked global pi config at `/home/dev/.pi/agent/` (settings.json + models.json placeholders) is **overridden** by per-workspace files noded writes into `<ws>/home/.pi/agent/` (docs/05 §3) — the mount shadows the baked copy.

## 3. Container spec (noded creates; name `forge-sbx-<project_id>`)

```
docker create --runtime=runsc \
  --user 1000:1000 --cap-drop ALL --security-opt no-new-privileges \
  --read-only --tmpfs /tmp:rw,size=256m,mode=1777 \
  -v <ws>/work:/workspace:rw \
  -v <ws>/home:/home/dev:rw \
  -v <ws>/ctl:/run/forge:rw            # shim's unix socket dir
  --memory <mem_limit_mb>m --memory-swap <mem_limit_mb>m \
  --cpus <cpu_millicores/1000> --pids-limit 256 \
  --network forge-sbx --dns <bridge_gw> \
  -e HTTP_PROXY=http://<bridge_gw>:3128 -e HTTPS_PROXY=http://<bridge_gw>:3128 \
  -e NO_PROXY=localhost,127.0.0.1 \
  -e ANTHROPIC_API_KEY=<llm_session_token> \
  -e FORGE_PROJECT_ID=<id> -e PORT=3000 -e HOST=0.0.0.0 \
  --workdir /workspace forge-sandbox:<ver> /usr/local/bin/forge-shim
```
- **SBX-06 (MUST):** Rootfs read-only; only `/workspace`, `/home/dev`, `/tmp`, `/run/forge` writable. No docker socket, no host mounts beyond the three above, no `--privileged`, no added capabilities, no published ports.
- **SBX-07 (MUST):** The LLM session token is the *only* credential in the environment; it is useless outside the llmproxy (ARC-09) and rotates each container start.

## 4. forge-shim (PID 1) spec

- **SHM-01 (MUST):** HTTP/1.1 server on unix socket `/run/forge/ctl.sock`. Endpoints:
  - `GET /healthz` → `{ok, pi_running, dev_running, last_activity_unix}`
  - `POST /agent/prompt {task_id, prompt, model?}` → 202; ensures pi RPC child (`pi --mode rpc`, cwd `/workspace`, resume most-recent session; exact flags per VERIFIED.md) then submits the prompt via pi's RPC protocol
  - `POST /agent/abort {task_id}` → RPC abort, escalate SIGINT after 5 s (ARC-13)
  - `GET /agent/events` → chunked `application/x-ndjson` live stream of Forge envelopes (docs/05 §4); noded keeps exactly one consumer attached and republishes to NATS
  - `POST /dev/start {cmd?}` / `POST /dev/stop` / `GET /dev/logs?tail=200` — dev server management; default cmd = template `dev` script via `bun run dev`; auto-restart with backoff ≤3/min; `GET /dev/status`
  - `POST /exec {argv[], timeout_ms≤120000, cwd?}` → `{exit_code, stdout_b64, stderr_b64}` caps 1 MB each — used by noded for installs/builds only (never exposed to users directly)
  - `GET /fs/tree?path=` `GET /fs/file?path=` `PUT /fs/file` — semantics of API fs endpoints; path traversal hard-blocked (clean + must remain under `/workspace`)
  - `POST /build` → runs `bun install` (if needed) then `bun run build`; response `{ok, dist_path?|null, log_tail}`; detects `dist/`, `build/`, `out/` in that order
- **SHM-02 (MUST):** Event `seq`: monotonic int64 persisted at `/home/dev/.forge/seq` (survives container restarts; on missing file resume from noded-provided `start_seq` in an init call `POST /init {start_seq}` that noded issues using MAX(seq) from forged… simpler: noded passes `start_seq` env `FORGE_START_SEQ` obtained via `fs`-independent forged lookup at `project.start`).
- **SHM-03 (MUST):** On start: if `/workspace/package.json` exists and `node_modules` absent → run `bun install` (emit `system.note` events with progress). Then idle until first prompt/dev request.
- **SHM-04 (MUST):** `--fake-agent` flag: instead of spawning pi, respond to `/agent/prompt` with a deterministic scripted event sequence that also writes real files (e.g., prompt containing `SMOKE:` → write `index.html` "Hello from Forge", emit tool events, `task.done`). Used by unit/e2e tests below M3.
- **SHM-05 (MUST):** Activity tracking: any prompt, dev start, fs call, or proxied preview hit (noded reports via `POST /activity`) updates `last_activity`; noded polls `/healthz` every 60 s for idle enforcement (ARC flow 3.4).

## 5. Dev server / preview contract (templates MUST honor)

- **SBX-08 (MUST):** `dev` script binds `0.0.0.0:$PORT` (3000), works behind an https reverse proxy on a different origin (Vite: `server: { host: true, allowedHosts: true, hmr: { clientPort: 443 } }` — builder verifies exact Vite options for the pinned version), single port for UI+API in dev (Vite `server.proxy` → local API on 3001 where applicable).
- **SBX-09 (MUST):** `start` script (server templates) binds `0.0.0.0:$PORT`, serves built frontend + API, uses `DATA_DIR=/data` when set (published apps) else `./data` for its SQLite file.

## 6. Egress control (noded component)

- **SBX-10 (MUST):** HTTP(S) forward proxy (CONNECT + plain) listening on the bridge gateway IP :3128, host-side. Allowlist matching on hostname (exact or `*.suffix`), default file `deploy/egress-allowlist.yaml`:
  `registry.npmjs.org, registry.yarnpkg.com, *.jsdelivr.net, esm.sh, github.com, codeload.github.com, objects.githubusercontent.com, raw.githubusercontent.com, fonts.googleapis.com, fonts.gstatic.com, llm.internal.<DOMAIN>` (+operator additions). Deny → 403 with body `forge-egress: domain not allowed`. Per-sandbox cap: 100 concurrent conns, 10 GB/day transfer (counter in noded, over-cap ⇒ deny + event `system.note`).
- **SBX-11 (MUST):** CONNECT tunnels are opaque (no MITM). Attribution: proxy identifies the sandbox by source IP↔container mapping from noded's own container inventory.
- **SBX-12 (MUST):** DNS: stub resolver on `<bridge_gw>:53` (noded-embedded, forwards to node resolver). Blocking is enforced at connect time by iptables+proxy, not DNS, so DNS may answer freely.

## 7. Workspace lifecycle & snapshots

- **SBX-13 (MUST):** Layout `<WS_ROOT>/<project_id>/{work,home,ctl}`. Template extraction into `work/`; `home/` seeded with `.pi/agent/{settings.json,models.json}` + `.forge/`.
- **SBX-14 (MUST):** Snapshot = `tar -I 'zstd -3' -cf` of `work/` + `home/` honoring `.forgeignore` (template-provided; default excludes `node_modules/`, `.cache/`, `dist/`, `*.log`). Upload S3 `snapshots/<project>/<ts>.tar.zst`, then prune to last 3. Restore = download + extract + fix ownership 1000:1000 + re-apply quota.
- **SBX-15 (MUST):** `project.destroy` removes container, workspace dir, quota assignment; forged deletes S3 snapshots.

## 8. Templates (`sandbox/templates/<name>/`)

Each: `template.json {display_name, description, kind_hint}`, `AGENTS.md` (≤60 lines), `.forgeignore`, app files. `make templates` builds tarballs + uploads.

- **TPL-01 `static`:** plain `index.html/style.css/main.js`; `dev` = tiny static server w/ live reload (`vite` with no framework is acceptable); build = copy to `dist/`.
- **TPL-02 `vite-react`:** Vite + React + TS + Tailwind SPA. (React chosen for user apps: broadest agent competence; platform's own UI stays Solid per D8.)
- **TPL-03 `fullstack-hono`:** Hono (Node) API + better-sqlite3 + Vite/React front. Dev: Hono on 3001, Vite on 3000 proxying `/api`. Prod `start`: single Hono process serving `dist/` + `/api` on `$PORT`. Schema bootstrap in `src/server/db.ts` (idempotent `CREATE TABLE IF NOT EXISTS`).
- **TPL-04 (MUST):** Every `AGENTS.md` states: stack; `bun run dev|build|start` commands; port/host rules (SBX-08/09); "SQLite only, file at `$DATA_DIR/app.db`"; "no new heavyweight deps without need"; "after changes ensure `bun run build` passes"; "never bind ports other than $PORT".
