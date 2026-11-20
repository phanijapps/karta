# providers

Resolves configured model providers, performs OpenAI-compatible model
discovery, and — via `Store` — manages the provider lifecycle. Sits beside
`config` in the dependency graph: imports `internal/config` (loaded
entries + paths) and stdlib; never imports the server, clients, or engine
code.

- Sources: `config.yaml` entries (origin `config`, read-only via API,
  env-referenced credentials only) merge first, then daemon-managed files
  under `config/providers/<name>.yaml` (origin `file`, CRUD-able, may
  carry `api_key_env` or the `api_key: keyring` marker). Duplicate names,
  malformed files, stem≠name, and combined overflow keep the last good
  set serving (`Store.LastError` + logs).
- `Store` serializes mutations and watcher reloads on one mutex;
  committed mutations publish immediately (local merge), external file
  changes land via fsnotify debounce (~150ms). Mutation order is fixed —
  validate → prepare secret → commit file (temp+rename) → publish — with
  compensation (a file failure deletes the just-written secret; a failed
  cleanup leaves a logged orphan, overwritten on name reuse).
- Managed names are portable file stems: lowercase kebab, 1–64 chars,
  Windows-reserved names rejected (`ValidManagedName`).
- Credentials: env-var NAME references or the OS keyring behind the
  `SecretStore` seam (`KeyringStore` over zalando/go-keyring; addresses
  `provider:<name>@<homeHash>` so relocated homes never share secrets;
  every call deadline-bounded ~3s). Values exist only in memory, for
  request or status use — never under `~/.zbot/`, in responses, or logs.
  `ResolveCredential`/`CredentialStatusFor` map outcomes: missing → no
  credential sent; unavailable → typed `ErrKeyringUnavailable` (never a
  silent unauthenticated request).
- Discovery: `Client.ListModels(ctx, provider, token)` calls
  `{base_url}/models` with a ~10s bound; the caller resolves the token.
  Failures are typed (`Failure` reasons) — map reasons, never parse
  error strings (`IsUnknown`/`ReasonOf` helpers).
- Catalog: `Catalog` (catalog.go) is the in-memory model catalog —
  per-provider lists + refresh timestamps, nothing persisted, empty at
  startup. Manual-refresh semantics: only `Set` (the refresh endpoint and
  successful discovery share that path) populates it; reads are instant
  snapshot reads that never dial a provider.
- Chat completions live with the engine change; this package stays
  discovery + lifecycle only.
- Unit tests run against in-memory fakes (`fakeSecrets`) and in-process
  `httptest` servers — zero live keyring/network in `go test`.
