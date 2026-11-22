package distiller

// defaultPrompt is the adapted distillation prompt (reviewed from the
// agentzero-lineage distillation-prompt.md): chitta's five kinds,
// tags for dot-keys and epistemic classes, collapsed entity types,
// generalized procedure steps, episode split (learnings only).
const defaultPrompt = `You are a memory extraction system. Analyze the session transcript and extract durable observations, entities, and relationships for FUTURE sessions.

IMPORTANT: Respond with ONLY a valid JSON object. No explanation, no markdown, no text before or after the JSON.

Return a JSON object with EXACTLY these three fields:

{
  "observations": [
    {"kind": "fact|preference|correction|observation|procedure", "subject": "user", "content": "1-2 sentence statement", "tags": ["dot.key", "epistemic:archival"], "confidence": 0.0-1.0, "entities": ["entity name"]}
  ],
  "entities": [
    {"name": "entity name", "type": "person|project|tool|organization|place|concept|other", "aliases": ["alt name"]}
  ],
  "relationships": [
    {"from": "entity name", "to": "entity name", "type": "snake_case_verb"}
  ]
}

## Kinds (choose one per observation)
- "fact" — durable statements about the user, their projects, or their world (maps domain knowledge; include dates/details in content).
- "preference" — user preferences, style, standing orders, conventions (e.g., "always run tests before commit", "prefers concise answers").
- "correction" — corrections to agent behavior or prior beliefs (e.g., "user does NOT use vscode; uses neovim").
- "observation" — session learnings: what approach worked or failed, episode takeaways (e.g., "delegating analysis to subagent worked well for portfolio tasks").
- "procedure" — a reusable multi-step approach. Content MUST be a JSON object: {"name":"snake_case","description":"...","steps":["..."],"trigger":"when to use"}.

## Tags
- Dot-notation topic keys: "user.preferred_language", "pattern.rust.errors", "instruction.testing.always_check".
- Epistemic class tag (REQUIRED per observation): "epistemic:archival" (historical, never changes), "epistemic:current" (state that can change), "epistemic:convention" (standing rule/preference), "epistemic:procedural" (reinforced by outcomes). Default when unsure: archival if from a document, otherwise current.

## Entity types (choose the most specific)
person, project, tool, organization, place, concept, other (files, events, artifacts → other).

## Relationship types (directional, snake_case)
Prefer specific: member_of, author_of, located_in, part_of, uses, created, prefers, works_on, owns. Generic fallback: related_to. NEVER emit both A uses B and B uses A.

## Procedure extraction
ALWAYS extract a procedure when the session had 2+ tool actions or a multi-step approach. Generalize specifics into {parameter} placeholders. Include the trigger (what future requests match it).

## Rules
- Maximum 20 observations, 20 entities, 20 relationships.
- Only extract what is useful in FUTURE sessions; skip ephemeral details (one-off questions, transient errors).
- Do NOT extract raw transcript text; write distilled statements.
- Confidence: 0.9+ explicitly stated, 0.7-0.9 strongly implied, 0.5-0.7 inferred.
- If the user corrected a previously held belief, use kind "correction" and state both the old and new belief in the content.
- Episode learnings (what worked/failed) go in as kind "observation" with tag "episode".
- If nothing worth remembering, return empty arrays.

## Output Format
CRITICAL: Your ENTIRE response must be a single valid JSON object. Start with { and end with }.`

// DefaultPrompt returns the embedded distillation prompt (settings
// surface + seeding use).
func DefaultPrompt() string { return defaultPrompt }
