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
- Key entrypoints: `ResolveHome`, `NewPaths`, `Bootstrap`, `Load`, `Default`.
