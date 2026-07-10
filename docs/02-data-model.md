# 02 — Data model

## 1. Conventions

- **DM-01 (MUST):** Postgres ≥ 16. Migrations are numbered SQL files in `migrations/`, embedded via `go:embed`, applied by `forged` at boot under advisory lock `pg_advisory_lock(0x464f5247)`.
- **DM-02 (MUST):** Credits stored as `bigint` microcredits (1 credit = 1e6 µcr). Sizes in MB, CPU in millicores.
- **DM-03 (MUST):** Public short IDs (`preview_id`, publish `slug` suffix candidates) are 10-char lowercase Crockford base32 from crypto/rand; DB PKs are UUIDv4.
- Secrets at rest: password → argon2id (m=64MiB, t=3, p=2, encoded PHC string); auth/session/share/llm tokens → store **SHA-256 hash** only.

## 2. Schema (migration 0001 — authoritative)

```sql
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email         citext NOT NULL UNIQUE,
  password_hash text   NOT NULL,
  display_name  text,
  is_admin      boolean NOT NULL DEFAULT false,
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
  balance_microcredits bigint NOT NULL DEFAULT 0,   -- cache; ledger is truth
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_sessions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  ip         inet,
  user_agent text,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
CREATE INDEX ON auth_sessions(user_id);

CREATE TABLE nodes (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name           text NOT NULL UNIQUE,          -- e.g. "worker-1"
  internal_addr  text NOT NULL,                 -- host:7443 for gateway mTLS proxying
  status         text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','draining','down')),
  cpu_millicores int  NOT NULL,
  mem_mb         int  NOT NULL,
  disk_mb        int  NOT NULL,
  alloc_cpu_millicores int NOT NULL DEFAULT 0,
  alloc_mem_mb   int  NOT NULL DEFAULT 0,
  alloc_disk_mb  int  NOT NULL DEFAULT 0,
  agent_version  text,
  last_heartbeat timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE projects (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name           text NOT NULL,
  template       text NOT NULL,                 -- 'static' | 'vite-react' | 'fullstack-hono'
  preview_id     text NOT NULL UNIQUE,          -- base32(10), host label
  runtime_state  text NOT NULL DEFAULT 'cold'
                 CHECK (runtime_state IN ('cold','starting','running','stopped','error')),
  node_id        uuid REFERENCES nodes(id),
  snapshot_key   text,                          -- s3 key when cold
  snapshot_at    timestamptz,
  llm_token_hash bytea,                         -- current sandbox LLM token
  mem_limit_mb   int NOT NULL DEFAULT 1024,
  cpu_millicores int NOT NULL DEFAULT 1000,
  disk_quota_mb  int NOT NULL DEFAULT 2048,
  default_model  text,                          -- exposed model id; NULL = platform default
  last_active_at timestamptz NOT NULL DEFAULT now(),
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  archived_at    timestamptz
);
CREATE INDEX ON projects(user_id) WHERE archived_at IS NULL;
CREATE INDEX ON projects(node_id) WHERE runtime_state IN ('starting','running','stopped');

CREATE TABLE tasks (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id    uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id       uuid NOT NULL REFERENCES users(id),
  prompt        text NOT NULL,
  model         text NOT NULL,
  status        text NOT NULL DEFAULT 'queued'
                CHECK (status IN ('queued','running','done','error','aborted')),
  error         text,
  input_tokens  bigint NOT NULL DEFAULT 0,
  output_tokens bigint NOT NULL DEFAULT 0,
  cost_microcredits bigint NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  started_at    timestamptz,
  finished_at   timestamptz
);
CREATE INDEX ON tasks(project_id, created_at DESC);
-- DM-04 (MUST): at most one non-terminal task per project
CREATE UNIQUE INDEX tasks_one_running ON tasks(project_id)
  WHERE status IN ('queued','running');

CREATE TABLE agent_events (
  project_id uuid   NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  seq        bigint NOT NULL,                   -- shim-assigned, monotonic per project
  task_id    uuid,
  type       text   NOT NULL,
  payload    jsonb  NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, seq)
);
-- retention: keep last 5000 events per project (nightly reaper in forged)

CREATE TABLE llm_calls (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id),
  project_id   uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id      uuid,
  model        text NOT NULL,                   -- exposed id
  provider_model text NOT NULL,
  input_tokens bigint NOT NULL DEFAULT 0,
  output_tokens bigint NOT NULL DEFAULT 0,
  cache_write_tokens bigint NOT NULL DEFAULT 0,
  cache_read_tokens  bigint NOT NULL DEFAULT 0,
  cost_microcredits  bigint NOT NULL DEFAULT 0,
  status       text NOT NULL CHECK (status IN ('ok','provider_error','denied_balance','denied_rate')),
  latency_ms   int,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON llm_calls(user_id, created_at DESC);
CREATE INDEX ON llm_calls(task_id);

CREATE TABLE credit_ledger (
  id            bigserial PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users(id),
  delta_microcredits bigint NOT NULL,           -- negative = debit
  balance_after bigint NOT NULL,
  reason        text NOT NULL CHECK (reason IN
                ('signup_grant','admin_grant','llm_usage','purchase','adjustment')),
  ref_type      text,   -- 'llm_call' | 'task' | NULL
  ref_id        uuid,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON credit_ledger(user_id, id DESC);

CREATE TABLE publishes (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id   uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug         citext NOT NULL UNIQUE,          -- host label; regex ^[a-z0-9](-?[a-z0-9]){2,40}$
  kind         text NOT NULL CHECK (kind IN ('static','server')),
  version      int  NOT NULL DEFAULT 1,
  artifact_key text NOT NULL,                   -- s3: static tarball or workspace snapshot
  status       text NOT NULL DEFAULT 'deploying'
               CHECK (status IN ('deploying','live','stopped','error')),
  node_id      uuid REFERENCES nodes(id),       -- server kind, when warm
  mem_limit_mb int NOT NULL DEFAULT 384,
  last_request_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX publishes_one_per_project ON publishes(project_id);

CREATE TABLE share_links (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
```

- **DM-05 (MUST):** Credit mutations use one transaction: `UPDATE users SET balance... RETURNING`, insert ledger row with `balance_after`. Nightly job asserts `users.balance == SUM(ledger.delta)` per user; mismatch → error log + metric.
- **DM-06 (MUST):** `slug` reserved list rejected at API: `www, api, app, admin, mail, preview, apps, static, assets, forge, status, docs, blog, help`.

## 3. S3 layout (one bucket, `FORGE_S3_BUCKET`)

```
templates/<name>.tar.zst            # built by `make templates`, uploaded at deploy
snapshots/<project_id>/<unix_ts>.tar.zst
apps/<slug>/v<version>.tar.zst      # static publish artifacts (extracted dist/)
appsnaps/<publish_id>/v<version>.tar.zst   # server publish workspace snapshots
```
Lifecycle (operator-applied): keep last 3 snapshots per project (noded prunes on successful upload); `apps/` immutable per version.

## 4. NATS subjects (all payloads JSON)

| Subject | Pattern | Semantics |
|---|---|---|
| `forge.node.<node_id>.rpc` | request-reply | Control→node ops. Envelope `{op, ...}`; reply `{ok, error?, data?}`. Ops: `project.start{project_id,template?,restore_key?,limits,llm_token,models_cfg}`, `project.stop{project_id}`, `project.snapshot{project_id}→{key}`, `project.destroy{project_id}`, `agent.prompt{project_id,task_id,prompt,model}`, `agent.abort{project_id,task_id}`, `preview.ensure{project_id}`, `fs.tree{project_id,path}`, `fs.read{project_id,path}→{b64,size,truncated}` (cap 256 KB), `fs.write{project_id,path,b64}`, `publish.build{project_id}→{artifact_key,kind_hint}`, `app.start{publish_id,slug,restore_key,limits}`, `app.stop{publish_id}` |
| `forge.node.<node_id>.hb` | pub every 10 s | `{name, internal_addr, caps{cpu_millicores,mem_mb,disk_mb}, alloc{...}, sandboxes:[{project_id,state}], apps:[{publish_id,state}], version}` |
| `forge.proj.<project_id>.events` | pub | Forge event envelope (docs/05 §4), from noded |
| `forge.user.<user_id>.notify` | pub | `{type:"balance", balance_microcredits}` etc. |
| `forge.routes` | pub | `{type:"invalidate", host}` — gateways drop cache entry and re-fetch |

- **DM-07 (MUST):** `forged` consumes `forge.proj.*.events` in queue group `persist` for DB writes; plain (non-queue) subscriptions feed WebSocket fanout. NATS `max_payload` left at default 1 MB; `fs.read` cap keeps under it.

## 5. Derived route table (served by forged `/internal/routes`)

```json
{ "previews": {"<preview_id>": {"project_id":"…","node":"host:7443","state":"running|stopped|cold"}},
  "apps":     {"<slug>": {"kind":"static","artifact":"apps/x/v3.tar.zst"} |
                         {"kind":"server","publish_id":"…","node":"host:7443|null"}} }
```
Gateway sync: full fetch at boot + on `forge.routes` invalidation (per-host re-fetch: `/internal/routes/host/<host>`); entries also expire after 5 min as backstop.
