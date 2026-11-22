# engine

The seam between the daemon and agent engines: agent resolution from the
registry snapshot, tool filtering/execution, and the shared conformance
suite. Framework-free — only the adapter packages under `engine/<name>/`
may import an engine framework (constitution: engine isolation).

- `ResolveAgent` maps a name to a run's agent definition (empty name =
  the minimal wiring fallback; unknown names return an error listing
  configured agents).
- `FilterTools`/`ExecuteTool` adapt the tools plane to engine needs
  (allowlist filtering; typed error results the model can see).
- `contracttest/` is the reusable `domain.AgentEngine` conformance suite
  — run it against fakes AND every real adapter (rule 4.5,
  docs/development/go-rules.md).
- Imports registry + tools; never imports a framework, the server, or
  any client code.
