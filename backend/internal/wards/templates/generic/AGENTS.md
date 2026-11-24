# Working in this ward

This is the ward's living readme for agent executions. Keep it current:
what this ward is for, its current state, conventions the next agent
(human or model) must follow to work here.

Suggested sections:

- **Purpose** — the domain this ward covers (reusable category, not a
  one-off task).
- **Conventions** — where material lands (pages/, sources/, outputs/),
  naming habits, anything unusual.
- **State** — what is settled, what is in flight, what is blocked.

## Homes (v2 discipline)

Outputs land in the right home, always:

- **reports/YYYY-MM-DD/** — every run's deliverables (HTML, PDF, MD,
  charts) in a dated folder per run day. Name files for the question
  they answer.
- **wiki/YYYY-MM-DD-<slug>.md** — timebound detailed knowledge: dated
  entries with a one-line summary header. The canonical page carries
  the durable summary; wiki holds the timestamped depth.
- **apps/** — the ward's code, organized like a small project
  (src/, data/, README). Scripts that a run stages to gather or
  compute live here, not in tmp.
- **sources/** — immutable intake (verbatim captures, provenance).
