---
name: contract-first-api
description: Add or change z-Bot daemon HTTP/SSE API endpoints contract-first via contracts/openapi.yaml. Use when adding endpoints, changing request/response shapes or SSE events, regenerating the API client, or implementing chi handlers in backend/internal/server/.
---

# Contract-First API (contracts/openapi.yaml)

Authority: root `AGENTS.md` (contract-first rule). `contracts/openapi.yaml` is
the single source of truth for the daemon API. Handler-first changes are
violations.

## The Flow (never reorder)

1. **Contract** — update `contracts/openapi.yaml`: paths, schemas, and SSE
   event definitions for Timeline Tree streaming.
2. **Codegen** — run `task codegen` to regenerate server stubs and the
   frontend client into `frontend/src/shared/api/`.
3. **Handlers** — implement/adjust handlers in `backend/internal/server/`
   (`net/http` + `chi` + SSE) to satisfy the contract.
4. **Clients** — frontend and CLI consume only the generated client; no
   hand-rolled bindings anywhere.

## Rules

- An endpoint that is not in the contract does not exist; a contract change
  without regenerated clients is incomplete.
- Streaming endpoints use SSE with Timeline Tree event payloads; every event
  shape belongs in the contract, not just REST schemas.
- Breaking contract changes (removing/renaming fields, changing status codes)
  are called out explicitly in the change proposal.
- `server/` never imports UI, CLI, or channel code; it serves the contract.

## Checklist Before Finishing

- [ ] `contracts/openapi.yaml` updated first
- [ ] `task codegen` run; generated client committed
- [ ] Handlers match the contract exactly (paths, shapes, status codes, SSE events)
- [ ] No hand-rolled fetch/API code in `frontend/` or `cmd/zbot`
