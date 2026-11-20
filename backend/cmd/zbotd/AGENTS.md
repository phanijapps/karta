# cmd/zbotd

The Karta daemon entrypoint. Wiring only — all behavior lives in
`internal/*`. Resolves the home (`--home` flag > `ZBOT_HOME` > `~/.zbot`),
bootstraps the control plane, strictly loads `config.yaml` (loopback
addresses only — the daemon is unauthenticated), and serves the contract
on `cfg.Server.Address` with graceful SIGINT/SIGTERM shutdown. Provider
changes: managed files under `config/providers/` (create/edit/delete,
watched) apply immediately; `config.yaml` provider entries apply on
restart. `version` is stamped via `-ldflags "-X main.version=..."`.
