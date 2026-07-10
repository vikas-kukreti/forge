# 08 — Frontend (`web/`, SolidJS + Vite, embedded in `forged`)

## 1. Stack & build

- **FE-01 (MUST):** SolidJS + TypeScript + Vite + Tailwind. Router: `@solidjs/router`. No global state library — use Solid stores/signals in small context providers. State kept intentionally thin (D8: UI is swappable; keep it boring).
- **FE-02 (MUST):** `vite build` emits to `web/dist/`; `forged` embeds it with `go:embed web/dist` and serves it at the platform host (docs/07 §2). Deep links fall back to `index.html`. Build is part of `make build` (frontend built before Go binary that embeds it).
- **FE-03 (MUST):** Talk to the API at same-origin `/v1/*` (no base URL config; the gateway routes the platform host to `forged`). All mutating requests send header `X-Forge-CSRF: 1` (API-02) and rely on the `forge_session` cookie (never read/store the token in JS — it is HttpOnly).
- **FE-04 (MUST):** No secrets, no provider keys, no analytics SDKs shipped. No `localStorage` for auth. `localStorage` is used only for cosmetic prefs (theme, last-open project id) — never for tokens.

## 2. Routes / pages

| Route | Page | Notes |
|---|---|---|
| `/login`, `/signup` | Auth | email+password; shows `invalid_credentials`/`email_taken`; on success redirect to `/`. |
| `/` | Dashboard | project grid (name, template, `runtime_state` badge, last active), "New project" modal (name + template picker), header shows credit balance (live). Empty state → template gallery. |
| `/p/:id` | **Workspace** (the core screen) | 3-pane layout §3. |
| `/account` | Account & credits | balance, credit ledger table (paginated `/v1/account/ledger`), model/rate list from `/v1/models`, sign-out, active sessions count. |
| `/admin` | Admin (only if `is_admin`) | tabs: Users (list, suspend, grant credits), Nodes (heartbeat/capacity/alloc, drain button), Publishes. Hidden entirely for non-admins (route guard + nav omitted). |
| `*` | 404 | in-app. |

- **FE-05 (MUST):** Auth guard: a `<RequireAuth>` wrapper calls `GET /v1/me` once at app boot; unauthenticated → redirect `/login`. `GET /v1/me` result (`{user, balance_microcredits}`) seeds an `AuthContext`.

## 3. Workspace layout (`/p/:id`)

Three resizable panes (default 34% / 33% / 33%; collapsible; layout persisted per-project in `localStorage`):

1. **Chat / agent pane (left).**
   - **FE-06 (MUST):** Message list rendered from the event stream (§4). Renders these envelope types (docs/05 §4): `user.prompt`, `assistant.text` (markdown, streamed token-appended), `tool.start`/`tool.summary` (compact one-liner with an icon, per AGT-07 — name + ≤120-char digest, **collapsed by default**, no raw args/results), `system.note`, `task.done`/`task.error` (status chip + token/cost summary for the task).
   - Composer at the bottom: textarea + model picker (from `/v1/models`, shows per-model rate; default = project `default_model` or platform default) + Send. Send = `POST /v1/projects/:id/tasks {prompt, model?}`.
   - **FE-07 (MUST):** While a task is non-terminal, disable Send, show a live "working…" indicator with an **Abort** button (`POST /v1/tasks/:taskId/abort`). Surface `task_running` (409) and `insufficient_credits` (402) inline; the latter links to `/account`.
2. **Preview pane (center).**
   - **FE-08 (MUST):** `<iframe>` to `https://<preview_id>.preview.<DOMAIN>/` (dev: `http://<preview_id>.preview.localtest.me:8088/`). Toolbar: reload, open-in-new-tab, viewport toggle (mobile/desktop width), and a **Share** control (creates/reveals a share link via `POST /v1/projects/:id/share`, shows the `?forge_share=…` URL, copy button). The iframe is `sandbox`-attributed appropriately but must allow scripts/forms for the user app; it is a separate origin so it cannot touch the platform session.
   - **FE-09 (MUST):** Cold/stopped project: show a "Preview is asleep — Wake" affordance; hitting Send or Wake triggers auto-wake (the API/gateway handle it); reflect `project.status`/`preview.status` events (waking → running) and only then load the iframe.
3. **Files pane (right).**
   - **FE-10 (MUST):** Lazy-loaded file tree from `GET /v1/projects/:id/files/tree`; clicking a file loads it via `GET /v1/projects/:id/files/read?path=` (read-only, cap 256 KB → show "file too large / binary" states). **Read-only** in v1 (agent owns writes, docs/03 §Files); no save button. The code viewer (CodeMirror 6 or Shiki highlighter) is **code-split** and only fetched when the pane is first opened (FE-15).

Above the panes: project header with name (rename inline → `PATCH /v1/projects/:id`), `runtime_state` badge, and a **Publish** button opening the publish dialog (§5).

## 4. Realtime (WebSocket) handling

- **FE-11 (MUST):** On entering `/p/:id`, open WS `/v1/projects/:id/stream` (API-10). First frame is `hello {last_seq, runtime_state, balance_microcredits}`. Then immediately gap-fill via `GET /v1/projects/:id/events?after_seq=<local_last_seq or 0>` and merge, **deduping by `seq`**; thereafter apply live frames. Maintain `last_seq` locally.
- **FE-12 (MUST):** Reconnect with capped exponential backoff (max ~15 s) on close; on reconnect repeat the hello→gap-fill→live sequence. Send `{type:"ping"}` every 30 s; treat 2 missed `pong`s as dead → reconnect. Only one socket per project; close on route leave (respect the 5-sockets-per-user cap, API-12).
- **FE-13 (MUST):** `usage.update` frames update the task cost/token display and the global balance in the header; `balance` notifications (also forwarded, API-12) update `AuthContext` balance. `preview.status`/`project.status` drive the preview pane state (FE-09). No action frames are sent over WS (prompts/aborts go via REST, API-11).

## 5. Publish dialog

- **FE-14 (MUST):** Inputs a slug (client-side validate against regex `^[a-z0-9](-?[a-z0-9]){2,40}$` and the reserved list from DM-06, mirrored in a shared TS constant); submit `POST /v1/projects/:id/publish {slug}`. Handle `slug_taken`/`slug_reserved`. Show deploy progress (poll `GET /v1/projects/:id/publish` or react to events) → on `live` show the public URL `https://<slug>.apps.<DOMAIN>` with copy/open. Republish (bumps version) and Unpublish controls when already published.

## 6. Performance budgets & quality

- **FE-15 (MUST):** Initial route JS ≤ **150 KB gzipped** (measured in CI via a bundle-size check on `web/dist`). The code viewer/highlighter, markdown renderer heavy paths, and admin page are **lazily imported** (route- and interaction-level `lazy()`), excluded from the initial chunk. Tailwind purged to used classes.
- **FE-16 (MUST):** No layout shift on stream append; virtualize the chat list if > ~300 messages. Accessibility: buttons/inputs labelled, keyboard-usable composer (Enter=send, Shift+Enter=newline), color-contrast AA, respects `prefers-reduced-motion`.
- **FE-17 (MUST):** Errors from the API render via a shared toast + inline handling keyed on the stable `error.code` set (API-01). Never surface raw stack traces. A global error boundary prevents a white screen.
- **FE-18 (MUST):** Quality gate: `tsc --noEmit` clean, eslint clean, `vite build` succeeds; a Playwright smoke (against `FORGE_TLS=off` + fake agent + fake LLM) drives signup → new project → prompt "SMOKE:" → sees streamed events + `task.done` → publish static → opens app URL. This smoke is part of `make e2e-m7`.

## 7. Shared types

- **FE-19 (SHOULD):** Hand-maintain a single `web/src/api/types.ts` mirroring the wire contracts (event envelope, entities, error codes). Keep it in sync with `docs/03`+`docs/05`; a lightweight test asserts the error-code union matches API-01’s list. (No codegen dependency required for v1.)
