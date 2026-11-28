package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Frontmatter agents with the delegation fields load; strict decoding
// rejects unknown fields; description-less agents load but are marked
// non-delegatable by consumers (MaxDepth semantics).
func TestLoadAgentsFrontmatterDelegationFields(t *testing.T) {
	dir := t.TempDir()
	ag := filepath.Join(dir, "agents")
	if err := os.MkdirAll(ag, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(ag, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("research.md", `---
name: research-agent
description: Gathers and synthesizes evidence.
display_name: Research Agent
model: zai/glm-5.3
tools: [clock_now]
max_depth: 1
---
You research things.`)
	write("plain.md", "---\nname: plain\n---\nNo description, no depth.")
	write("bad.md", "---\nname: bad\nsurprise: yes\n---\nUnknown field.")
	reg := New()
	snap, errs := reg.LoadAll(Dirs{Agents: ag})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "bad") {
		t.Fatalf("want exactly the bad-file error, got %v", errs)
	}
	r := snap.Agents["research-agent"]
	if r.Description == "" || r.DisplayName == "" || r.MaxDepth != 1 || r.SystemPrompt != "You research things." {
		t.Fatalf("frontmatter fields lost: %+v", r)
	}
	if p := snap.Agents["plain"]; p.Description != "" || p.MaxDepth != 0 {
		t.Fatalf("plain agent should have zero delegation metadata: %+v", p)
	}
}

// require_tool_use loads from both spec forms; absent reads false.
func TestAgentSpecRequireToolUse(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("guard.yaml", "name: guard\ndescription: d\nrequire_tool_use: true\n")
	write("plain.md", "---\nname: plain\ndescription: d\n---\nbody\n")
	snap, errs := New().LoadAll(Dirs{Agents: dir})
	if len(errs) > 0 {
		t.Fatalf("load errors: %+v", errs)
	}
	if !snap.Agents["guard"].RequireToolUse {
		t.Fatal("require_tool_use must load from the YAML form")
	}
	if snap.Agents["plain"].RequireToolUse {
		t.Fatal("an absent require_tool_use reads false")
	}
}

// reasoning_effort loads from both spec forms; absent reads ""; an
// unknown value fails THAT agent with a naming error and leaves the
// others loaded (per-file error isolation) — engine-adk-spine D5.
func TestAgentSpecReasoningEffort(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("builder.yaml", "name: builder\ndescription: d\nreasoning_effort: low\n")
	write("writer.md", "---\nname: writer\ndescription: d\nreasoning_effort: none\n---\nbody\n")
	write("plain.md", "---\nname: plain\ndescription: d\n---\nbody\n")
	write("turbo.md", "---\nname: turbo\ndescription: d\nreasoning_effort: turbo\n---\nbody\n")
	snap, errs := New().LoadAll(Dirs{Agents: dir})
	if len(errs) != 1 || !strings.Contains(errs[0].Err.Error(), "reasoning_effort") || !strings.Contains(errs[0].Err.Error(), "turbo") {
		t.Fatalf("the unknown effort fails its agent naming the field: %+v", errs)
	}
	if snap.Agents["builder"].ReasoningEffort != EffortLow || snap.Agents["writer"].ReasoningEffort != EffortNone || snap.Agents["plain"].ReasoningEffort != "" {
		t.Fatalf("efforts: builder %q writer %q plain %q", snap.Agents["builder"].ReasoningEffort, snap.Agents["writer"].ReasoningEffort, snap.Agents["plain"].ReasoningEffort)
	}
	if _, ok := snap.Agents["turbo"]; ok {
		t.Fatal("an invalid agent must not load")
	}
}
