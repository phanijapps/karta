---
name: intent-analyst
description: Pre-execution intent analysis — finds the hidden intent, builds the context packet, and hands the orchestrator one validated decision before a research run starts
display_name: Intent Analyst
model: ""
tools: []
max_depth: 0
prompt: intent-analysis
---
<!--
  The Intent Analyst is a HOST-RUN decision step, not a worker: before
  every root research run the daemon renders the rubric named above
  (config/prompts/intent-analysis.md — user-owned; its Skills, Agents,
  MCPs and Wards template slots are filled from the live catalog), makes
  ONE validated completion (tools: [] is deliberate — the research says
  a tool loop makes this step slower and less reliable), and records the
  decision on its own execution row with token accounting and
  analysis_* events. It is never offered for delegation.

  model: "" inherits — the ward's orchestrator model when a ward
  orchestrates, otherwise the Settings → Advanced → Intent Analysis
  card, then this file, then config.yaml. Pin "provider/model" here to
  override the card (outside a ward).

  Token limits are the model's own (its context window); nothing to set.

  To inline the rubric in this file, replace this comment with a body
  that carries all four slot markers — it then wins over the prompt
  reference. Seeded write-once; user-owned after first write.
-->
