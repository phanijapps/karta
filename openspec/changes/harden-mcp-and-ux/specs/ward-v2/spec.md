# Spec Delta

## MODIFIED Requirements

### Requirement: The retrieval plane (live MCP tools on hosted runs)

The daemon SHALL execute configured stdio MCP servers and mount their
tools on root engine runs, prefixed `mcp_<server>_<tool>`. Every call
SHALL be bounded (typed `timeout` past the bound) and every failure
SHALL carry a typed kind (timeout / unavailable / protocol /
server_error / bad_arguments) the Timeline surfaces. Server commands
SHALL resolve as written first, then against the user's common tool
directories (the daemon's PATH is thinner than a shell's). A server
that fails to start SHALL retry on a bounded backoff (not on every
tool-plane assembly). Quick chat mounts no MCP tools.

#### Scenario: A shell-style command resolves

- **WHEN** a config names a bare binary (`npx`, `uvx`) that exists in
  the user's tool directories but not on the daemon's PATH
- **THEN** the server starts (no absolute path required)

#### Scenario: A crash-looping server backs off

- **WHEN** a server fails to start repeatedly
- **THEN** retries space out (2s doubling to 2min) instead of firing
  on every model round

#### Scenario: A timed-out call is typed

- **WHEN** an MCP tool exceeds the per-call bound
- **THEN** the result carries kind `timeout` naming the server and
  tool, and the Timeline shows why

### Requirement: Warden sessions — every ward has a persistent selectable agent

*(unchanged core; ADDED scenario for leaving)*

#### Scenario: A session can leave its ward

- **WHEN** the user leaves a research session's ward
- **THEN** the binding clears (the session re-groups under Research),
  the engine host drops the ward so the next run starts unhosted, the
  transcript is untouched, and the quick chat refuses (409)
