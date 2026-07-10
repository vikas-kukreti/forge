# 10 — Security: threat model & mandatory controls

Scope: platform v1 as specified in docs/01–09. This doc (a) states what we protect, (b) maps threats → controls already required elsewhere, and (c) adds the remaining **SEC-xx** requirements. Every MUST here is release-blocking; M8 audits this checklist (docs/11).

## 1. Assets & security goals

| Asset | Goal |
|---|---|
| User workspaces (code, SQLite data, chat history) | Confidentiality + integrity per tenant; private by default. |
| Provider LLM key | Never leaves `forge-llmproxy` (ARC-09). Compromise = platform-wide spend. |
| Credit ledger / balances | Integrity (no free usage, no unbilled spend), auditability (DM-05). |
| Platform sessions & accounts | No hijack via XSS/CSRF/user content. |
| Worker hosts | Sandbox escape contained; workers hold no secrets/DB access (ARC-02). |
| Availability | One tenant cannot starve others (quotas/limits everywhere). |

## 2. Trust boundaries & adversaries

Boundaries: browser↔gateway (public TLS) · gateway↔forged/noded (internal token / mTLS: ARC-07/08) · **sandbox↔node kernel (gVisor)** · sandbox↔network (egress proxy, SBX-10) · sandbox↔LLM (session token → llmproxy, ARC-09) · published-app visitor↔user app (untrusted↔untrusted; platform only routes).

Adversaries considered: (A1) malicious signed-up user (incl. prompting the agent to attack the platform — treat **all agent-written code as attacker-controlled**); (A2) malicious visitor of a preview/published app; (A3) compromised/hostile content reached via egress (prompt injection into pi); (A4) network attacker (internal + external); (A5) abuser farming signup credits or mining CPU. Out of scope v1: malicious operator, physical access, DDoS beyond basic rate limits (front with an upstream CDN/WAF if needed).

## 3. Threat → control matrix (controls specified elsewhere)

| Threat | Controls |
|---|---|
| Sandbox → host escape | gVisor `runsc` (SBX-01); `--cap-drop ALL`, `no-new-privileges`, read-only rootfs, uid 1000, `--pids-limit 256`, tmpfs `/tmp` (docs/04 §3, SBX-06); no docker socket; workers are secret-free (ARC-02) → blast radius = that node's workspaces only. |
| Sandbox → other sandbox | Bridge `enable_icc=false` + iptables default-DROP incl. RFC1918/link-local/host (SBX-03); no published ports (SBX-06). |
| Sandbox → internal services (SSRF) | Egress only via allowlist proxy on the bridge gateway (ARC-06, SBX-10/11/12); iptables blocks direct routes; llmproxy is just an allowlisted name and validates its own tokens (LLM-02). |
| Provider key theft | Key only in llmproxy env (ARC-09, OPS-11); sandboxes hold a per-project rotating session token useless elsewhere (SBX-07); tokens stored hashed (DM §1). |
| Free/unbilled LLM usage | Preflight balance/rate checks + deny at ≤0 (LLM-03/08); metering on the terminal usage payload (LLM-04/05); task cost caps + call caps + `max_tokens` clamp (ARC-14, AGT-11); nightly ledger reconciliation (DM-05). |
| Session hijack via CSRF | SameSite=Lax HttpOnly cookie + custom-header CSRF (`X-Forge-CSRF`) on all mutations (API-02). |
| XSS on platform | CSP on platform host (GW-13); SPA never `innerHTML`s API data; markdown sanitization (SEC-03). |
| Preview/app content attacking platform session | Separate origins per preview/app subdomain; iframe sandboxing (SEC-04); **cookie stripping at the proxy (SEC-02)**; user apps get no platform headers/cookies. |
| Cross-tenant data via API | Ownership checks return 404 (API-04); IDs are UUIDv4 / random base32 (DM-03); fs API path-confined to `/workspace` (docs/03 §Files) — see SEC-06. |
| Abuse: credit farming, mining, spam apps | Signup/login/task/publish rate limits (API-03); small signup grant (LLM-09); daily spend cap (LLM-10); CPU/mem/disk/pids caps (docs/04 §3, SBX-02); idle hibernation + scale-to-zero (flow 3.4, GW-09); per-user caps (SEC-07); admin suspend/unpublish (docs/03 §Admin, SEC-08). |
| Node loss / tampering | mTLS with per-node SAN certs (ARC-07); nodes are cattle — state re-creatable from S3 snapshots (OPS-18). |
| Secret leakage via logs/errors | No prompts/bodies/secrets in logs (OPS-14); stable error codes only to clients (API-01, FE-17). |

## 4. Mandatory controls added by this doc

- **SEC-01 (MUST) — Token & cookie hygiene.** All bearer/session/share/LLM tokens: ≥32 bytes from `crypto/rand`, stored as SHA-256 only (DM §1), compared via the hash lookup (no timing-sensitive string compare paths). `forge_preview` grant HMAC verification uses `hmac.Equal`. Session fixation: issue a fresh session token at every login; `POST /v1/logout` deletes the row. Password hashing per DM §1 (argon2id); password length 8–256, no composition rules, `login` failures are uniform (`invalid_credentials`) regardless of which factor failed.
- **SEC-02 (MUST) — Proxy header/cookie stripping.** Before proxying any request to a **preview or published-app upstream**, the gateway strips: all `forge_*` cookies (it may consume `forge_session`/`forge_preview` for authz first — GW-03 — but they MUST NOT reach noded or user code), `Authorization`, and any inbound `X-Forge-*` headers from the client (gateway sets its own `X-Forge-Target`, `X-Forge-Activity`, `X-Forwarded-*`). Response direction: user apps cannot set cookies scoped to `DOMAIN` — the gateway rewrites/drops `Set-Cookie` with a `Domain` attribute broader than the exact request host. Without this, the Domain-scoped `forge_session` (API-02/GW-03) would be exposed to attacker-controlled app servers; treat this as the single most security-critical requirement in the gateway.
- **SEC-03 (MUST) — Markdown/output sanitization.** `assistant.text` renders through a markdown pipeline with raw HTML disabled and URL schemes restricted to `http/https/mailto`; links get `rel="noopener noreferrer" target="_blank"`. Tool summaries/file paths render as plain text. Applies to anything that originated inside a sandbox (it is attacker-influenceable, A1/A3).
- **SEC-04 (MUST) — Preview iframe attributes.** The workspace iframe (FE-08) uses `sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-modals allow-downloads"` (same-origin here = the *preview* origin, which is already isolated from the platform origin) and never `allow-top-navigation`. `X-Robots-Tag: noindex` on previews per GW-13.
- **SEC-05 (MUST) — Prompt-injection stance (documented, tested).** The agent may be hijacked by fetched content (A3). Containment, not prevention, is the control: the sandbox can only reach allowlisted egress; the only credential present is the rotating LLM session token (SBX-07); worst case is corruption of the *user's own* workspace, bounded by task cost caps (ARC-14) and recoverable from snapshots (SBX-14). An e2e in M8 asserts a sandbox cannot reach a non-allowlisted domain, the host, another sandbox, or the noded control socket beyond the mounted `/run/forge` API.
- **SEC-06 (MUST) — Path confinement.** Every fs op (API + shim + noded) resolves the requested path with symlinks (`filepath.EvalSymlinks` equivalent inside the mount namespace) and rejects results outside `/workspace` (`fs.read/write/tree`) — no `..`, no absolute escapes, no reading `/home/dev/.pi` via the public fs API (session/token material lives there). Snapshot/extract code rejects tar entries with absolute paths, `..`, or symlinks targeting outside the extraction root (zip-slip class).
- **SEC-07 (MUST) — Per-user resource ceilings.** `FORGE_MAX_PROJECTS_PER_USER` (default 20) enforced at project create (`validation_failed`), and `FORGE_MAX_RUNNING_SANDBOXES_PER_USER` (default 2) enforced by the scheduler (excess wake/start requests queue or return `node_unavailable` with a clear message). Prevents one account from monopolizing worker RAM. (Vars added to the OPS-11 table.)
- **SEC-08 (MUST) — Suspension semantics & takedown.** `users.status='suspended'` ⇒ login denied, all API mutations denied, llmproxy 403 (LLM-02), running sandboxes stopped at next reconcile, previews 403. Published apps of a suspended user are stopped/unrouted by an explicit admin action (`unpublish`), which is the abuse-takedown path; both actions are audit-logged (SEC-09).
- **SEC-09 (MUST) — Admin audit trail.** Every admin mutation (grant, suspend, drain, unpublish, node ops) emits a structured audit log line `{actor_user_id, action, target, params}` at level `info` (OPS-14 pipeline). No separate table in v1; the log stream is the audit record — state this in ops docs.
- **SEC-10 (MUST) — Supply chain.** All versions pinned (VERSIONS.md, trd §6.3); lockfiles committed (`go.sum`, `bun.lock`/`package-lock.json` for web and templates); sandbox image installs pi with `--ignore-scripts` (SBX-04); CI runs `govulncheck` + `npm audit --omit=dev` (report always; build fails on known-critical in direct deps). Base image and Node LTS are digest-pinned in the Dockerfile.
- **SEC-11 (MUST) — Transport matrix.** External: TLS only (GW-01; HSTS per GW-13). Internal: gateway↔noded mTLS (ARC-07); sandbox↔llmproxy via internal-CA TLS through the egress proxy CONNECT (LLM-01); forged `/internal/*` bearer-token on a non-exposed listener (ARC-08); NATS with auth credentials (OPS-11) on the private network. Plain HTTP is permitted only in `FORGE_TLS=off` dev mode.
- **SEC-12 (MUST) — Key/secret rotation runbook.** Documented procedures (docs/09 references this): rotate `FORGE_INTERNAL_TOKEN` (update env both sides, rolling restart), `FORGE_COOKIE_SECRET` (invalidates preview grants + CSRF-adjacent cookies; sessions are DB-backed so unaffected), provider key (llmproxy restart), internal CA node/gateway certs (OPS-09), mass session revocation (`DELETE FROM auth_sessions WHERE user_id=…` or all). LLM session tokens self-rotate per container start (ARC-09).
- **SEC-13 (MUST) — Data protection.** Postgres backups (OPS-16) contain PII (emails) and hashes — operator guidance: encrypt at rest, restrict access. S3 snapshot objects contain user code/data: bucket must be private; enable provider server-side encryption where available (document as default-on recommendation). No PII beyond email + optional display name is collected; no analytics/tracking (FE-04).
- **SEC-14 (SHOULD) — Signup friction knob.** v1 ships no email verification (accepted risk, mitigated by API-03 IP limits + small grant + SEC-07 ceilings + admin suspend). Provide `FORGE_SIGNUPS=open|closed` (default `open`; `closed` ⇒ 403 `forbidden` on signup) so an operator can run invite-only by creating users via admin API. (Var added to OPS-11 table.)
- **SEC-15 (SHOULD) — Container hardening extras.** Where the pinned Docker/gVisor combo supports it cleanly: `--security-opt seccomp` default profile retained (do not disable), `--ulimit nofile=4096`, and a distinct subuid range via userns-remap documented as an optional operator hardening (not required with gVisor; record choice in DECISIONS.md).

## 5. Release audit (executed in M8)

A checklist run recorded in `SECURITY-AUDIT.md`: every SEC/ARC/SBX/GW/LLM/API control above verified as implemented (link to code/test), the SEC-05 isolation e2e green on **real gVisor**, `gitleaks` (or equivalent secret-scan) clean on the repo, dependency scans (SEC-10) clean of criticals, and a manual pass of: signup→session cookie flags, CSRF reject, preview 403 matrix (no cookie / wrong user / share token / owner), SEC-02 stripping verified with `curl` against a published echo app (prove `forge_session` never arrives), path traversal attempts on fs API, oversized upload/body rejects (GW-11), suspended-user matrix (SEC-08).
