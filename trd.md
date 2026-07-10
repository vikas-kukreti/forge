# Forge — Technical Requirements Document (TRD)

**Product codename:** Forge (rename freely; all binaries are prefixed `forge`)
**Document version:** 1.0
**Audience:** an autonomous coding agent that will build this platform end-to-end, and the human operator who deploys it.

---

## 1. What Forge is

Forge is a self-hostable "vibe coding" platform in the spirit of Lovable / Emergent. A user signs up, creates a project, and chats with an AI coding agent that builds a **full-stack web app (frontend + backend + database)** inside a **private, sandboxed workspace**. The user sees a live preview while the agent works and can **publish** the finished app to a public subdomain (`myapp.apps.<DOMAIN>`).

Core product loop:

1. Sign up → receive free credit grant.
2. Create project from a template.
3. Prompt the agent ("build me a habit tracker with login").
4. Watch streamed agent output + live preview (`<id>.preview.<DOMAIN>`).
5. Iterate. Credits are debited per LLM token used.
6. Publish → `myslug.apps.<DOMAIN>`.

## 2. Locked decisions (do not revisit)

| # | Decision | Value |
|---|----------|-------|
| D1 | User apps | Full-stack (backend + SQLite DB per project) |
| D2 | Sandbox isolation | Docker containers with **gVisor (`runsc`)** runtime |
| D3 | Platform backend language | **Go** (single module, multiple binaries) |
| D4 | Coding agent | **pi** (`@earendil-works/pi-coding-agent`) run in **RPC mode** inside each sandbox |
| D5 | LLM access | **Platform-managed provider keys** behind a metering proxy + **credit system**. Provider keys never enter sandboxes. |
| D6 | Scale model | **Multi-node from day one**: stateless control plane + N worker nodes |
| D7 | Shipping | Live preview **and** publish-to-subdomain (static + server apps with scale-to-zero) |
| D8 | Platform frontend | **SolidJS + Vite + TypeScript + Tailwind** (smallest runtime footprint; UI layer is swappable) |
| D9 | Datastores | PostgreSQL (system of record), NATS (internal messaging), S3-compatible object storage (MinIO for self-host) |
| D10 | Efficiency stance | Low idle footprint everywhere: Go static binaries, hibernating sandboxes, scale-to-zero published apps, token-frugal agent harness |

## 3. Non-goals (v1)

- No team/collaboration features, no realtime multi-user editing.
- No GitHub import/export (post-v1; leave seams, don't build).
- No custom domains for published apps (subdomains only).
- No Stripe/payments (credit **ledger** is built; purchases are admin-granted; leave a `purchase` ledger reason for later).
- No Windows hosts. Linux amd64/arm64 only.
- No multi-region. One control plane, N workers in one network.
- No mobile app.

## 4. System components (build all of these)

| Binary / artifact | Runs on | Purpose |
|---|---|---|
| `forged` | control node(s) | REST + WebSocket API, auth, project/task orchestration, scheduler, event persistence. Embeds and serves the SolidJS SPA via `go:embed`. |
| `forge-gateway` | edge (control node) | TLS termination (CertMagic wildcard), routes app/API, `*.preview.*`, `*.apps.*`; serves published static apps from S3-backed cache. |
| `forge-llmproxy` | control node(s) | Anthropic-compatible metering proxy. Validates per-project tokens, injects real provider key, computes cost, debits credits, publishes usage. Has a **fake LLM mode** for tests. |
| `forge-noded` | every worker node | Manages Docker/gVisor containers, workspaces, disk quotas, snapshots to S3, per-node egress proxy, preview reverse-proxy endpoint, talks to shims. |
| `forge-shim` | inside every sandbox (PID 1) | Static Go binary. Supervises the `pi` RPC process and the dev server; exposes a control API on a unix socket; streams agent events. |
| `web/` | built into `forged` | SolidJS SPA (chat, preview, files, publish, credits). |
| `sandbox/` | Docker image + templates | `forge-sandbox` image (Node LTS, bun, pi, git, sqlite) and project templates. |
| `deploy/` | ops | docker-compose for dev, worker bootstrap script, systemd units, internal CA generation. |

Detailed specs live in `docs/`:

| File | Contents |
|---|---|
| `docs/01-architecture.md` | Topology, data flows, sequence flows, internal auth |
| `docs/02-data-model.md` | Full PostgreSQL schema, NATS subjects, S3 layout, ID/units conventions |
| `docs/03-api.md` | REST + WebSocket API contract |
| `docs/04-sandbox.md` | Sandbox image, gVisor flags, shim spec, workspace lifecycle, quotas, egress control |
| `docs/05-agent-integration.md` | pi integration, RPC bridging, token-efficiency requirements, fake-agent test seam |
| `docs/06-llm-proxy-credits.md` | Metering proxy, pricing config, credit ledger, budget enforcement |
| `docs/07-gateway-routing.md` | Routing rules, TLS, preview auth, publish serving, scale-to-zero |
| `docs/08-frontend.md` | SPA pages, components, WS handling, performance budgets |
| `docs/09-deployment-ops.md` | Dev environment, prod deployment, config reference, observability, backups |
| `docs/10-security.md` | Threat model and mandatory controls checklist |
| `docs/11-milestones.md` | Build order M0–M8 with acceptance criteria per milestone |

## 5. Requirement conventions

- Requirements carry IDs like `SBX-04`, `API-12`, `SEC-07`. **MUST** = mandatory for v1. **SHOULD** = do it unless it conflicts with a MUST. Milestone acceptance criteria in `docs/11-milestones.md` reference these IDs.
- All quantities have explicit units in code and schema (`_mb`, `_ms`, `_microcredits`, `_millicores`).
- Credits are integers: **1 credit = 1,000,000 microcredits**. Never use floats for money/credits.
- All timestamps UTC, `timestamptz` in Postgres, RFC3339 on the wire.
- IDs are UUIDv4 unless specified (short public IDs are lowercase base32, see `docs/02`).

## 6. Instructions to the builder agent

You (the builder agent) are expected to produce a working system from this TRD alone. Rules:

1. **Read order:** this file → `docs/01` → `docs/02` → `docs/11`, then each component doc as its milestone begins. Build strictly in milestone order M0→M8; each milestone's acceptance script must pass before starting the next.
2. **Verify external interfaces at build time.** This TRD deliberately does **not** transcribe third-party wire formats that may drift. Before integrating, read the pinned version's own docs/types and record findings in `VERIFIED.md`:
   - pi RPC message schema, `models.json` custom-provider schema, `settings.json` keys → from the `pi` repo (`github.com/earendil-works/pi`, package `packages/coding-agent`, docs + TypeScript types; npm `@earendil-works/pi-coding-agent`).
   - gVisor install + `runsc` Docker runtime config → gvisor.dev docs.
   - Anthropic Messages API request/response/streaming shapes → docs.claude.com (needed to implement the metering proxy and fake LLM).
   - CertMagic DNS-01 provider config.
3. **Pin versions.** Create `VERSIONS.md` at repo root listing exact versions chosen for Go, Node, pi, gVisor, Postgres, NATS, MinIO, SolidJS, Vite, Tailwind, and every direct Go/npm dependency. Renovate-style drift is out of scope.
4. **Decide small things yourself.** Where this TRD is silent, pick the simplest option consistent with D1–D10 and log it in `DECISIONS.md` (one line each). Do not expand scope.
5. **Testability seams are mandatory, not optional:** `FORGE_RUNTIME=runc` fallback (CI without gVisor), `FORGE_FAKE_LLM=1` (deterministic Anthropic-compatible responses), `forge-shim --fake-agent` (bypasses pi). All e2e tests run with zero real LLM spend.
6. **Quality bar:** `go vet` + `golangci-lint` clean; `gofmt`/`goimports` enforced; frontend `tsc --noEmit` + eslint clean; every package with logic has unit tests; each milestone ships its `make e2e-mN` script. CI = GitHub Actions (`.github/workflows/ci.yml`) running lint, unit tests, and the runc-mode e2e.
7. **Resource budgets (enforced by review, measured in M8):**
   - `forged` + `forge-gateway` + `forge-llmproxy` combined idle RSS < 150 MB.
   - `forge-noded` idle RSS < 50 MB. `forge-shim` RSS < 15 MB.
   - Agent fixed context overhead (system prompt + AGENTS.md + tool defs) ≤ ~3k tokens (see AGT-10).
   - SPA initial route ≤ 150 KB gzipped JS (code editor lazy-loaded).
8. **Repository layout (create exactly this):**

```
forge/
├── trd.md                  # this document set, copied in
├── docs/                   # 01..11
├── VERSIONS.md  DECISIONS.md  VERIFIED.md  README.md  Makefile
├── go.mod
├── cmd/
│   ├── forged/  forge-gateway/  forge-llmproxy/  forge-noded/  forge-shim/
├── internal/
│   ├── api/  auth/  config/  credits/  db/  events/  gatewaycore/
│   ├── llmproxy/  natsutil/  nodemgr/  piproto/  scheduler/
│   ├── sandbox/  shimcore/  snapshot/  store/  types/
├── migrations/             # NNNN_name.sql, embedded, applied by forged at boot
├── web/                    # SolidJS app (Vite)
├── sandbox/
│   ├── Dockerfile
│   └── templates/ (static/  vite-react/  fullstack-hono/)
├── deploy/
│   ├── dev/docker-compose.yml     # postgres, nats, minio
│   ├── prod/ (systemd units, worker-bootstrap.sh, Caddy-free: gateway does TLS)
│   └── ca/gen-ca.sh
└── e2e/                    # bash+curl milestone scripts
```

9. **Definition of done (v1):** all MUST requirements satisfied; M0–M8 acceptance green; fresh 2-node prod install from `docs/09` succeeds; the golden-path e2e (signup → build via fake LLM → preview → publish static + server → subdomain serves) passes on real gVisor.

## 7. Glossary

- **Sandbox / workspace:** one project's container + its on-disk directory on a worker node.
- **Task:** one user prompt processed by the agent (one running task per project at a time).
- **Hibernate:** workspace tar+zstd snapshot uploaded to S3, local copy deleted; restorable on any node.
- **Publish:** immutable artifact (static tarball or workspace snapshot) served at `slug.apps.<DOMAIN>`.
- **Credits:** prepaid balance debited by the LLM proxy per token usage.
