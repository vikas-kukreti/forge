# 03 — API contract (`forged`, public listener :8080 behind gateway)

## 1. General

- **API-01 (MUST):** JSON everywhere. Errors: `{"error":{"code":"snake_case","message":"human text"}}` with proper HTTP status. Codes are stable API surface (used by the SPA): `invalid_credentials, email_taken, unauthorized, forbidden, not_found, task_running, insufficient_credits, slug_taken, slug_reserved, node_unavailable, rate_limited, validation_failed, project_cold`.
- **API-02 (MUST):** Auth = HttpOnly Secure SameSite=Lax cookie `forge_session` (opaque token, 32B, hashed in DB, 30-day sliding expiry). CSRF: all mutating endpoints require header `X-Forge-CSRF: 1` (custom-header pattern) — reject otherwise with 403 `forbidden`.
- **API-03 (MUST):** Rate limits (in-memory per forged instance, keyed by IP or user): signup 5/h/IP, login 10/15min/IP, task create 6/min/user, publish 4/h/user, fs.read 60/min/user. Exceed → 429 `rate_limited` + `Retry-After`.
- **API-04 (MUST):** All `/v1/projects/{id}/*` verify ownership (`user_id` match or admin) → else 404 (not 403) to avoid ID probing.
- Pagination: `?limit=` (≤100, default 50) + `?before=<cursor>`; responses include `next_cursor|null`.

## 2. Endpoints

### Auth & account
| Method+Path | Req body → Resp | Notes |
|---|---|---|
| `POST /v1/auth/signup` | `{email,password,display_name?}` → `201 {user}` + cookie | password ≥ 10 chars; grants `FORGE_SIGNUP_GRANT_CREDITS` (default 50) via ledger `signup_grant` |
| `POST /v1/auth/login` | `{email,password}` → `{user}` + cookie | constant-time compare; on suspend → 403 |
| `POST /v1/auth/logout` | → `204` | deletes session row |
| `GET /v1/me` | → `{user:{id,email,display_name,is_admin,balance_microcredits}}` | |
| `GET /v1/credits/ledger` | → `{entries:[{delta_microcredits,balance_after,reason,ref_type,created_at}],next_cursor}` | |

### Projects
| | |
|---|---|
| `GET /v1/projects` | `{projects:[ProjectSummary]}` (non-archived, by `updated_at` desc) |
| `POST /v1/projects` | `{name, template}` → `201 {project}`; template ∈ registry; enforces `FORGE_MAX_PROJECTS_PER_USER` (default 10) → 403 `validation_failed` |
| `GET /v1/projects/{id}` | `{project}` full: `{id,name,template,preview_id,runtime_state,default_model,limits:{mem_limit_mb,cpu_millicores,disk_quota_mb},preview_url,publish?:{slug,kind,status,url},created_at,last_active_at}` |
| `PATCH /v1/projects/{id}` | `{name?, default_model?}` → `{project}` |
| `DELETE /v1/projects/{id}` | `204`; async: abort task, `project.destroy` (container + workspace), delete snapshots, unpublish, then hard-delete row |
| `POST /v1/projects/{id}/wake` | `202 {runtime_state:"starting"}`; no-op if running |
| `POST /v1/projects/{id}/stop` | `202`; container stop, workspace retained |

### Agent & events
| | |
|---|---|
| `POST /v1/projects/{id}/tasks` | `{prompt, model?}` → `201 {task:{id,status:"queued"}}`. 409 `task_running` if DM-04 index conflict; 402 `insufficient_credits` if balance ≤ 0; auto-wakes cold project first (may take a few s; API returns after enqueue, events report progress) |
| `GET /v1/projects/{id}/tasks` | list summaries |
| `POST /v1/tasks/{id}/abort` | `202` |
| `GET /v1/projects/{id}/events?after_seq=N&limit=500` | `{events:[Envelope], last_seq}` — history replay from `agent_events` |
| `GET /v1/projects/{id}/stream` (WebSocket) | see §3 |

### Files (read-mostly; agent owns writes)
| | |
|---|---|
| `GET /v1/projects/{id}/fs/tree?path=/` | `{entries:[{name,path,dir,size}]}` (one level; SPA lazy-expands). 409 `project_cold` if not warm |
| `GET /v1/projects/{id}/fs/file?path=` | `{path,content_b64,size,truncated}` cap 256 KB |
| `PUT /v1/projects/{id}/fs/file` | `{path, content_b64}` (≤256 KB) — small manual edits |

### Preview & publish
| | |
|---|---|
| `GET /v1/projects/{id}/preview` | `{url:"https://<preview_id>.preview.DOMAIN", state:"running|starting|cold", dev_log_tail:[...]}` — triggers `preview.ensure` |
| `POST /v1/projects/{id}/share-links` | `{expires_hours?=72}` → `{url:"https://<preview_id>.preview.DOMAIN/?forge_share=<token>"}` |
| `POST /v1/projects/{id}/publish` | `{slug}` → `202 {publish:{slug,status:"deploying"}}`; validates slug regex + reserved (DM-06); kind auto-detected by build (docs/07 §4) |
| `GET /v1/projects/{id}/publish` | `{publish|null}` (+`url`) |
| `DELETE /v1/projects/{id}/publish` | `204` unpublish (static: artifact removed from route table; server: app.stop + snapshot retained) |

### Admin (`is_admin` only; bootstrap via `FORGE_ADMIN_EMAILS` promoting on signup/login)
`GET /v1/admin/users`, `POST /v1/admin/users/{id}/grant {credits}` (ledger `admin_grant`), `POST /v1/admin/users/{id}/suspend`, `GET /v1/admin/nodes`, `POST /v1/admin/nodes/{id}/drain`, `GET /v1/admin/stats` (counts, active sandboxes, 24h token spend).

### Meta & internal
Public: `GET /healthz` (200 static), `GET /readyz` (checks PG+NATS). Prometheus `GET /metrics` on the **internal** listener only.
Internal listener :8081 (ARC-08): `GET /internal/routes`, `GET /internal/routes/host/{host}`, `POST /internal/authz/preview {host, session_token? , share_token?} → {allow, project_id}`, `POST /internal/wake-app {slug} → {node|pending}`, `POST /internal/wake-preview {preview_id}`.

## 3. WebSocket `/v1/projects/{id}/stream`

- **API-10 (MUST):** Cookie-authenticated at upgrade; ownership enforced. Server→client frames are exactly the Forge event envelope (docs/05 §4) as JSON text frames. First frame after connect: `{"type":"hello","data":{"last_seq":N,"runtime_state":…,"balance_microcredits":…}}`. Client is expected to fetch `/events?after_seq=` for gap-fill, then rely on live frames (dedupe by `seq`).
- **API-11 (MUST):** Client→server frames: only `{"type":"ping"}` (server replies `pong`). All actions (prompt/abort) go via REST so limits/credits checks stay in one place.
- **API-12 (MUST):** Server also forwards `usage.update` (from `forge.user.<uid>.notify`) and `preview.status`, `project.status` events on this socket. Idle sockets pinged every 30 s; dead after 2 misses. Max 5 concurrent sockets per user.

## 4. OpenAPI

- **API-13 (SHOULD):** Maintain `docs/openapi.yaml` (3.1) generated or hand-written, kept in CI sync with handlers via a route-listing test (compare method+path sets).
