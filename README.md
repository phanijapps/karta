# Karta

A local-first agent workspace. The current frontend is an Open WebUI-style **shell preview**: mock chat, two sidebars, and configuration-shaped Settings. It does not yet call agents or modify daemon configuration.

## Run the preview

Install Node.js 22.12+ and pnpm 9.15.9, then:

```sh
cd frontend
pnpm install --frozen-lockfile
pnpm dev
```

Open http://127.0.0.1:9245. No daemon, credentials, or external service is needed. See [frontend setup and conventions](frontend/README.md) for tests, builds, and structure.

## Verify the repository

Additional prerequisites: Go 1.26+, [Task](https://taskfile.dev/installation/), and golangci-lint v2 compatible with your Go toolchain.

```sh
cd frontend && pnpm exec playwright install chromium && cd ..
task verify
```

`task verify` validates/regenerates the local API types, lints and race-tests Go, builds the backend, checks/builds the SPA, and runs offline browser tests. Package installation and code generation require development network access; the running shell does not.

## Repository guide

- [Product context](docs/product/product-context.md): vision, concepts, and current delivery status.
- [Founding architecture](docs/architecture/architecture-brief.md): daemon/client boundaries and locked stack.
- [Frontend architecture](docs/architecture/frontend-shell.md): shell responsibilities, visual reference, and mock boundaries.
- [Frontend evidence](docs/architecture/frontend-shell-evidence.md): checks, screenshots, and known gaps.
- `contracts/openapi.yaml`: authoritative Karta HTTP/SSE contract.
- `openspec/`: maintained behavior specs and active change plans.
- `AGENTS.md`: repository contribution boundaries and verification rules.

Karta is the user-facing product name. Internal `zbot` identifiers and the existing storage layout are retained for compatibility. Persistent production state belongs under `~/.zbot/`. The preview keeps all state in page memory and does not create or access that tree. A Wails desktop wrapper and live agent/configuration integration are future work.
