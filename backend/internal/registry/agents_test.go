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
