# Forge TRD bundle

A complete Technical Requirements Document for **Forge** — a self-hostable Lovable/Emergent-style vibe-coding platform: Go control plane, gVisor-sandboxed per-project workspaces, the [pi](https://github.com/earendil-works/pi) coding agent in RPC mode, a metering LLM proxy with a credit ledger, live previews, and publish-to-subdomain with scale-to-zero.

## How to use this bundle

Hand the whole folder to an autonomous coding agent with an instruction like:

> Build the system specified in `trd.md` and `docs/`. Follow `trd.md` §6 (builder instructions) exactly: read `trd.md` → `docs/01` → `docs/02` → `docs/11`, then build milestone-by-milestone (M0→M8), keeping `VERSIONS.md`, `DECISIONS.md`, and `VERIFIED.md` up to date. Do not start a milestone before the previous one's `make e2e-mN` passes.

The TRD is written to be buildable without further human input: locked decisions are in `trd.md` §2, everything ambiguous is delegated to `DECISIONS.md` with a "pick the simplest option" rule, and third-party wire formats (pi RPC, Anthropic API, gVisor setup, CertMagic DNS-01) are deliberately *not* transcribed — the builder verifies them against the pinned versions at build time (`trd.md` §6.2) so the spec can't rot.

## File map

| File | Contents |
|---|---|
| `trd.md` | Product definition, locked decisions D1–D10, non-goals, component inventory, builder-agent instructions, repo layout, definition of done. |
| `docs/01-architecture.md` | Topology, internal auth (mTLS/tokens), core sequence flows, scheduler, failure semantics. |
| `docs/02-data-model.md` | Full PostgreSQL schema, S3 layout, NATS subjects, route table. |
| `docs/03-api.md` | REST + WebSocket contract, error codes, rate limits. |
| `docs/04-sandbox.md` | gVisor container spec, forge-shim (PID 1), egress control, templates, snapshots. |
| `docs/05-agent-integration.md` | pi wiring, event envelope, **token-efficiency requirements**, test seams. |
| `docs/06-llm-proxy-credits.md` | Metering proxy, pricing config, credit ledger, fake-LLM mode. |
| `docs/07-gateway-routing.md` | TLS, host routing, preview auth, publish serving, scale-to-zero. |
| `docs/08-frontend.md` | SolidJS SPA: pages, workspace panes, WS handling, perf budgets. |
| `docs/09-deployment-ops.md` | Dev & prod install, **master config reference**, observability, backups. |
| `docs/10-security.md` | Threat model, threat→control matrix, SEC checklist, release audit. |
| `docs/11-milestones.md` | Build order M0–M8 with per-milestone acceptance scripts. |

## Reading shortcuts (for humans)

- *What gets built?* → `trd.md` §4. *In what order?* → `docs/11`.
- *Why is it cheap to run?* → `trd.md` D10 + §6.7 budgets; hibernation in `docs/04` §7; scale-to-zero in `docs/07` §4; token frugality in `docs/05` §5.
- *Why is it safe to run strangers' AI-written code?* → `docs/10` §3 matrix, backed by `docs/04` (gVisor + egress allowlist) and `docs/01` (secret-free workers).
- *How is LLM spend controlled?* → `docs/06` + ARC-14/AGT-11 caps.

Document set version 1.0 · requirement IDs (`ARC-…`, `SBX-…`, `SEC-…`) are stable and cross-referenced throughout.
