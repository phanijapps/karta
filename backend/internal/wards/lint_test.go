package wards

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// researchConf is the seeded research archetype's conf (mirrors
// templates/research/ward-conf.yaml) used to build lint fixtures.
const researchConf = `
archetype: research
layout:
  kind: directory
  children:
    - { id: canonical, match: "{ward}.md", kind: file, format: markdown }
    - { id: agent-instructions, match: AGENTS.md, kind: file, format: markdown }
    - { id: log, match: log.md, kind: file, format: markdown }
    - { id: spec, match: spec.md, kind: file, required: false, format: markdown }
    - { id: plan, match: plan.md, kind: file, required: false, format: markdown }
    - id: tasks
      match: tasks
      kind: directory
      required: false
      children:
        - { id: task, match: "*.md", kind: file, required: false, repeat: true, format: markdown }
    - { id: pages, match: pages, kind: directory, required: false }
    - { id: sources, match: sources, kind: directory, required: false }
    - { id: data, match: data, kind: directory, required: false }
    - { id: outputs, match: outputs, kind: directory, required: false }
`

// writeWard lays down a ward fixture in a fresh temp dir. Each key is a
// path relative to the ward root; keys ending in "/" are directories.
func writeWard(t *testing.T, entries map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range entries {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", rel, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir parent of %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

func mustConf(t *testing.T, raw string) *Conf {
	t.Helper()
	conf, err := ParseConf([]byte(raw), "ward-conf.yaml")
	if err != nil {
		t.Fatalf("fixture conf must parse: %v", err)
	}
	return &conf
}

func TestLint(t *testing.T) {
	t.Run("clean research ward", func(t *testing.T) {
		dir := writeWard(t, map[string]string{
			"amdfinancials.md": "# AMD financials\nsynthesis.\n",
			"AGENTS.md":        "readme\n",
			"log.md":           "log\n",
			"spec.md":          "spec\n",
			"plan.md":          "plan\n",
			"tasks/":           "",
			"tasks/t1.md":      "task one\n",
			"tasks/t2.md":      "task two\n",
			"sources/":         "",
		})
		findings, err := Lint(context.Background(), dir, "amdfinancials", mustConf(t, researchConf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("clean ward flagged: %+v", findings)
		}
	})

	t.Run("missing canonical names node id and match", func(t *testing.T) {
		dir := writeWard(t, map[string]string{
			"AGENTS.md": "readme\n",
			"log.md":    "log\n",
		})
		findings, err := Lint(context.Background(), dir, "quant-strategy", mustConf(t, researchConf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 1 || findings[0].Kind != FindingMissingRequired {
			t.Fatalf("want one missing_required, got %+v", findings)
		}
		f := findings[0]
		if f.NodeID != "canonical" || f.Path != "quant-strategy.md" {
			t.Fatalf("finding must name node id and expected path: %+v", f)
		}
		if !strings.Contains(f.Message, "quant-strategy.md") {
			t.Fatalf("message must carry the expected match: %q", f.Message)
		}
	})

	t.Run("unexpected directory flagged", func(t *testing.T) {
		dir := writeWard(t, map[string]string{
			"amdfinancials.md": "synthesis\n",
			"AGENTS.md":        "readme\n",
			"log.md":           "log\n",
			"junk-dir/":        "",
		})
		findings, err := Lint(context.Background(), dir, "amdfinancials", mustConf(t, researchConf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 1 || findings[0].Kind != FindingUnexpectedEntry || findings[0].Path != "junk-dir" {
			t.Fatalf("want unexpected_entry on junk-dir, got %+v", findings)
		}
	})

	t.Run("unexpected file inside tasks flagged", func(t *testing.T) {
		dir := writeWard(t, map[string]string{
			"amdfinancials.md": "synthesis\n",
			"AGENTS.md":        "readme\n",
			"log.md":           "log\n",
			"tasks/":           "",
			"tasks/README.txt": "not a task page\n",
		})
		findings, err := Lint(context.Background(), dir, "amdfinancials", mustConf(t, researchConf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 1 || findings[0].Kind != FindingUnexpectedEntry || findings[0].Path != filepath.Join("tasks", "README.txt") {
			t.Fatalf("want unexpected_entry on tasks/README.txt, got %+v", findings)
		}
	})

	t.Run("format mismatch", func(t *testing.T) {
		conf := `archetype: notes
layout:
  kind: directory
  children:
    - { id: notes, match: "notes*", kind: file, format: markdown }
`
		dir := writeWard(t, map[string]string{"notes-draft.txt": "plain text\n"})
		findings, err := Lint(context.Background(), dir, "scraps", mustConf(t, conf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 1 || findings[0].Kind != FindingFormatMismatch || findings[0].NodeID != "notes" {
			t.Fatalf("want format_mismatch on notes, got %+v", findings)
		}
	})

	t.Run("non-repeatable node matching twice flags the extra", func(t *testing.T) {
		conf := `archetype: single
layout:
  kind: directory
  children:
    - { id: one, match: "one*", kind: file, format: markdown }
`
		dir := writeWard(t, map[string]string{"one-a.md": "a\n", "one-b.md": "b\n"})
		findings, err := Lint(context.Background(), dir, "single", mustConf(t, conf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 1 || findings[0].Kind != FindingUnexpectedEntry || findings[0].NodeID != "one" {
			t.Fatalf("want unexpected_entry for the duplicate, got %+v", findings)
		}
	})

	t.Run("freeform ward without conf is clean", func(t *testing.T) {
		dir := writeWard(t, map[string]string{"anything.txt": "x\n"})
		findings, err := Lint(context.Background(), dir, "scraps", nil)
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if findings != nil {
			t.Fatalf("conf-less ward must lint clean, got %+v", findings)
		}
	})

	t.Run("id-less node findings identify by match pattern", func(t *testing.T) {
		conf := `archetype: bare
layout:
  kind: directory
  children:
    - { match: required-page.md, kind: file, format: markdown }
`
		dir := writeWard(t, map[string]string{"other.md": "x\n"})
		findings, err := Lint(context.Background(), dir, "bare", mustConf(t, conf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 2 {
			t.Fatalf("want missing required + unexpected, got %+v", findings)
		}
		for _, f := range findings {
			if f.Kind == FindingMissingRequired && f.NodeID != "required-page.md" {
				t.Fatalf("id-less node finding must carry the match pattern, got %+v", f)
			}
		}
	})

	t.Run("hidden entries tolerated at every level", func(t *testing.T) {
		dir := writeWard(t, map[string]string{
			"amdfinancials.md": "synthesis\n",
			"AGENTS.md":        "readme\n",
			"log.md":           "log\n",
			".git/":            "",
			".zbot":            "state\n",
		})
		findings, err := Lint(context.Background(), dir, "amdfinancials", mustConf(t, researchConf))
		if err != nil {
			t.Fatalf("Lint: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("hidden entries must be tolerated, got %+v", findings)
		}
	})
}
