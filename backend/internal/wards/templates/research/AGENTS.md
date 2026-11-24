# Working in this research ward

The living readme for agent executions in this ward. Keep it current so
the next agent (human or model) can pick up cold.

Suggested sections:

- **Purpose** — the research domain this ward covers.
- **Flow** — spec.md (what we are answering) → plan.md (how) →
  tasks/*.md (decomposed work) → canonical page (accumulating
  synthesis). Specialists implement tasks; the canonical page
  accumulates the knowledge.
- **Conventions** — where material lands: pages/ (notes), subjects/
  (topic notes), sources/ (source documents), data/ (datasets),
  outputs/ (generated artifacts).
- **State** — settled / in flight / blocked.

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
