# 07 — Gateway & routing (`forge-gateway` :80/:443)

## 1. TLS & hosts

- **GW-01 (MUST):** CertMagic with DNS-01 for `DOMAIN`, `*.DOMAIN`, `*.preview.DOMAIN`, `*.apps.DOMAIN` (wildcards require DNS-01; provider creds via `FORGE_ACME_DNS_PROVIDER` + provider-specific envs; implement at least Cloudflare, structure for libdns providers). :80 serves ACME + 301→https.
- **GW-02 (MUST):** Dev mode `FORGE_TLS=off`: plain HTTP on :8088, hosts under `localtest.me` (public wildcard→127.0.0.1), e.g. `app.localtest.me:8088`, `<pid>.preview.localtest.me:8088`. All e2e uses this.

## 2. Routing decision (per request, by Host)

| Host | Action |
|---|---|
| `DOMAIN` / `app.DOMAIN` | reverse-proxy → `forged` :8080 (SPA is embedded there; WS upgrade passthrough for `/v1/*/stream`) |
| `<preview_id>.preview.DOMAIN` | preview flow §3 |
| `<slug>.apps.DOMAIN` | publish flow §4 |
| anything else | 421 |

Route data: in-memory table synced from forged `/internal/routes` (DM §5) — full sync at boot + per-host refetch on `forge.routes` invalidation + 5 min TTL backstop + on-miss fetch.

## 3. Preview serving

- **GW-03 (MUST):** AuthZ before proxying: (a) `?forge_share=<token>` present → forged `/internal/authz/preview` validates share link → set host-scoped cookie `forge_preview=<signed grant, 24 h>` (HMAC `FORGE_COOKIE_SECRET`, claims: host, exp) and 302 to clean URL; (b) else `forge_preview` cookie valid for this host → allow; (c) else `forge_session` cookie (Domain=DOMAIN, so it is sent to subdomains) → forged authz (owner?) → allow, minting `forge_preview` to skip repeat checks (30 s cache regardless); (d) else 403 branded page "This preview is private."
- **GW-04 (MUST):** Proxy to `nodes.internal_addr` (mTLS, ARC-07) with `X-Forge-Target: sbx:<project_id>:3000`; noded terminates mTLS and forwards to the container bridge IP. WebSockets (HMR) MUST pass through both hops. Response streaming unbuffered.
- **GW-05 (MUST):** Cold/stopped project: serve a minimal HTML "Waking your project…" page that polls `/__forge/status` (gateway-implemented on preview hosts) while gateway fires `/internal/wake-preview`; auto-reload when noded reports dev `running`. Gateway also reports preview hits to noded (`X-Forge-Activity` on the proxied request) for idle tracking.

## 4. Publish

Build & classify (forged orchestrates via node rpc `publish.build`):
- **GW-06 (MUST):** shim `/build` runs; **kind detection:** template `kind_hint` = `server` (fullstack-hono) or presence of a `start` script + server entry ⇒ `server`; else `static` with the detected dist dir. Static ⇒ noded tars dist → `apps/<slug>/v<n>.tar.zst`. Server ⇒ full workspace snapshot (post-build, includes dist, honors `.forgeignore` except keeps `dist/`) → `appsnaps/<publish_id>/v<n>.tar.zst`.
- **GW-07 (MUST) static serving:** gateway keeps an LRU disk cache (`FORGE_GW_CACHE_DIR`, 2 GB default) of extracted artifacts keyed `slug@version`; on miss, fetch from S3 + extract. Serve with sane content-types, `Cache-Control: public, max-age=60` for html and `max-age=31536000, immutable` for hashed assets, SPA fallback to `index.html` on 404 of non-asset paths. Target: static serving never touches forged.
- **GW-08 (MUST) server apps:** run as container `forge-app-<publish_id>` — same hardening as sandboxes (runsc, caps dropped, egress-proxied, no ports) but: image `forge-sandbox`, cmd `forge-shim --app` (shim in app mode only supervises `bun run start` + health), `-v <appdir>:/workspace -v <appdata>:/data`, `DATA_DIR=/data`, mem `mem_limit_mb` (default 384), cpu 500m. `/data` persists across versions/wakes on that node; MUST be included in a nightly `appdata` snapshot to S3 so re-placement on another node keeps user data (document the small loss window).
- **GW-09 (MUST) scale-to-zero:** noded stops app containers idle > `APP_IDLE_STOP_MIN` (default 10). Gateway hitting a cold server app: hold the request, call `/internal/wake-app {slug}`, poll route entry until node reports app `running` (≤30 s) then proxy; on timeout → 503 branded "starting" page with refresh. `last_request_at` updated (throttled 1/min) for idle logic.
- **GW-10 (MUST):** Unpublish: static → remove route + cache entry (artifact retained in S3); server → `app.stop` + route removal. Republish bumps `version`.

## 5. Limits, robustness, metrics

- **GW-11 (MUST):** Per-host token-bucket rate limit (default 50 rps burst 100, config), request body cap 25 MB, header timeouts, `X-Forwarded-For/Proto` set, HTTP/2 enabled, graceful drain on SIGTERM (stop accepting, finish in ≤30 s).
- **GW-12 (MUST):** Prometheus: `forge_gw_requests_total{class=app|preview|apps,status}`, latency histograms per class, cache hit ratio, active WS count, wake wait histogram.
- **GW-13 (MUST):** Security headers on platform host (CSP allowing self + preview/apps frame-src for the iframe, HSTS in TLS mode, X-Content-Type-Options, Referrer-Policy). Published apps get only HSTS + nosniff (user content otherwise untouched). Preview/apps responses add `X-Robots-Tag: noindex` for previews only.
