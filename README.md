# Forge

**Forge** is a self-hostable Lovable/Emergent-style vibe-coding platform. It provides a complete environment featuring a Go control plane, gVisor-sandboxed per-project workspaces, the [pi](https://github.com/earendil-works/pi) coding agent via RPC, a metering LLM proxy with a credit ledger, live previews, and a publish-to-subdomain system featuring scale-to-zero capability.

## Overview

Forge allows you to leverage an autonomous agent to build, test, and publish code dynamically within a secure sandbox environment. Key features include:

- **Isolated Workspaces**: Built on gVisor to ensure untrusted code is tightly contained.
- **Agent Integration**: Leverages the `pi` AI agent over RPC to manipulate code, read files, and interact via a streamlined event pipeline.
- **Metering & Proxies**: A specialized LLM proxy handles token counting, limits, and credit balances transparently.
- **Scale-to-zero Publishing**: Apps spin down when inactive, saving resources, and wake immediately on the next request.

## Tech Stack

- **Backend**: Go (chi, pgx, nats.go), statically compiled.
- **Database**: PostgreSQL (with JSONB for flexible models).
- **Messaging**: NATS for high-speed event streaming and RPC.
- **Storage**: MinIO / S3 for workspace snapshots.
- **Sandboxing**: Docker, containerd, runc, and gVisor (runsc) for execution environments.
- **Frontend**: SolidJS SPA.

## Project Structure

- `cmd/` - Entrypoints for the various microservices (`forged`, `forge-gateway`, `forge-llmproxy`, `forge-noded`, `forge-shim`).
- `internal/` - Core domain logic, database operations, and application context.
- `deploy/` - Deployment manifests (Docker compose files for dev and prod environments).
- `e2e/` - End-to-end testing scripts.
- `docs/` - Technical requirements, architectural decisions, and other extensive documentation.
- `agents/` - Guidelines, states, and memories for AI agents interacting with this repo.

## AI Agents

If you are an AI agent or language model interacting with this codebase, you must review the specific guidelines and memory configurations located in the `agents/` directory before proceeding. Start by reading [agents/AGENTS.md](agents/AGENTS.md) for critical context and behavioral instructions.
