# config

Control-plane foundation: home resolution, first-run bootstrap, strict
`config.yaml` loading. Imports only stdlib + `yaml.v3`; sits above `domain`
in the dependency graph (nothing here imports registry/eventbus/engine).

- Resolution precedence: `--home` flag > `ZBOT_HOME` env > `~/.zbot`. The
  winner relocates the whole root. All paths derive from `config.Paths`.
- Bootstrap is create-only: default `config.yaml` written with `O_EXCL`
  (never overwrites), empty registry dirs, idempotent, seeds no content.
- Loading is strict and hard-fails: unknown keys, invalid YAML, and absent
  files are errors naming the file — never a fallback to defaults.
- `providers:` (optional list, absent = empty): one entry per model
  provider — `name` (unique), `type` (only `openai-compatible` today),
  `base_url` (absolute http(s), the provider's API root used **verbatim**,
  version prefix included, e.g. `http://localhost:11434/v1` — consumers
  append only their path), and optional `api_key_env` (an environment
  variable NAME; the daemon reads the value at request time — secret
  values are never stored under `~/.zbot/`). The keyring marker is NOT
  valid in this file (files under `config/providers/` are the managed
  source; see internal/providers). Invalid entries fail startup naming
  the file and entry.
- `server.address` must be host:port. Loopback is the default and
  recommended bind (the daemon is unauthenticated); a wildcard (0.0.0.0/::)
  or LAN address is an explicit operator choice that exposes the
  unauthenticated API to the network — the request guard adapts (see
  internal/server) and the operator owns that trust decision.
- Key entrypoints: `ResolveHome`, `NewPaths`, `Bootstrap`, `Load`, `Default`.
