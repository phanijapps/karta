package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tempDirs builds an isolated control plane (all five dirs exist, empty).
func tempDirs(t *testing.T) Dirs {
	t.Helper()
	root := t.TempDir()
	d := Dirs{ConfigFile: filepath.Join(root, "config.yaml")}
	for _, k := range Kinds {
		dir := filepath.Join(root, string(k))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		switch k {
		case KindPrompt:
			d.Prompts = dir
		case KindAgent:
			d.Agents = dir
		case KindSkill:
			d.Skills = dir
		case KindHook:
			d.Hooks = dir
		case KindMCP:
			d.MCPs = dir
		}
	}
	return d
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func errMentions(errs []LoadError, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), substr) {
			return true
		}
	}
	return false
}

func TestEmptyPlaneLoadsEmptyNoErrors(t *testing.T) {
	r := New()
	snap, errs := r.LoadAll(tempDirs(t))
	if len(errs) != 0 {
		t.Fatalf("empty plane errors: %v", errs)
	}
	if len(snap.Prompts)+len(snap.Agents)+len(snap.Skills)+len(snap.Hooks)+len(snap.MCPs) != 0 {
		t.Fatal("empty plane loaded entries")
	}
}

func TestOneBadFileAmongMany(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Hooks, "good.yaml"), "event: PreToolUse\ncommand: /bin/true\ntimeout_ms: 1000\n")
	write(t, filepath.Join(d.Hooks, "bad.yaml"), "event: NotAnEvent\ncommand: /bin/true\n")
	write(t, filepath.Join(d.Hooks, "alsogood.yaml"), "event: SessionStart\ncommand: /bin/true\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(snap.Hooks) != 2 {
		t.Fatalf("want 2 hooks, got %d", len(snap.Hooks))
	}
	if len(errs) != 1 || !errMentions(errs, "bad.yaml") {
		t.Fatalf("want exactly one error naming bad.yaml, got: %v", errs)
	}
}

func TestPrompts(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Prompts, "reviewer.md"), "You review {{.Kind}} work.")
	write(t, filepath.Join(d.Prompts, "broken.md"), "{{.Unclosed")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 1 || !errMentions(errs, "broken.md") {
		t.Fatalf("want one error naming broken.md, got: %v", errs)
	}
	p, ok := snap.Prompts["reviewer"]
	if !ok {
		t.Fatal("reviewer prompt missing")
	}
	var sb strings.Builder
	if err := p.Template().Execute(&sb, map[string]string{"Kind": "go"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if sb.String() != "You review go work." {
		t.Fatalf("unexpected render: %q", sb.String())
	}
}

func TestAgentsYAMLAndFrontmatter(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Agents, "researcher.yaml"),
		"name: researcher\nsystem_prompt: you research\nmodel: big\ntools:\n    - read\n")
	write(t, filepath.Join(d.Agents, "writer.md"),
		"---\nname: writer\nmodel: small\n---\nYou write things.\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	res, ok := snap.Agents["researcher"]
	if !ok || res.SystemPrompt != "you research" || res.Model != "big" || len(res.Tools) != 1 {
		t.Fatalf("yaml agent wrong: %+v", res)
	}
	wri, ok := snap.Agents["writer"]
	if !ok || wri.SystemPrompt != "You write things." {
		t.Fatalf("frontmatter agent wrong: %+v", wri)
	}
}

func TestAgentDuplicateNames(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Agents, "a-first.yaml"), "name: dup\nsystem_prompt: first\n")
	write(t, filepath.Join(d.Agents, "b-second.yaml"), "name: dup\nsystem_prompt: second\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 1 || !errMentions(errs, "duplicate agent name") {
		t.Fatalf("want duplicate-name error, got: %v", errs)
	}
	if got := snap.Agents["dup"].SystemPrompt; got != "first" {
		t.Fatalf("deterministic winner wrong: %q", got)
	}
}

func TestSkills(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Skills, "pdf-tools", "SKILL.md"),
		"---\nname: pdf-tools\ndescription: Extract text from PDFs.\n---\n# PDF tools\n")
	write(t, filepath.Join(d.Skills, "broken", "SKILL.md"),
		"---\nname: broken\n---\nno description\n")
	write(t, filepath.Join(d.Skills, "assets-only", "notes.txt"), "not a skill")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 1 || !errMentions(errs, "name and description") {
		t.Fatalf("want frontmatter error, got: %v", errs)
	}
	s, ok := snap.Skills["pdf-tools"]
	if !ok || s.Path == "" || s.Description == "" {
		t.Fatalf("skill wrong: %+v", s)
	}
	if _, ok := snap.Skills["assets-only"]; ok {
		t.Fatal("assets-only dir loaded as skill")
	}
}

func TestHooks(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Hooks, "guard.yaml"),
		"event: PreToolUse\nmatcher: \"execute|write\"\ncommand: ~/.zbot/scripts/guard.sh\ntimeout_ms: 5000\n")
	write(t, filepath.Join(d.Hooks, "bad-event.yaml"), "event: OnFriday\ncommand: /bin/true\n")
	write(t, filepath.Join(d.Hooks, "bad-matcher.yaml"), "event: PreToolUse\nmatcher: \"[unclosed\"\ncommand: /bin/true\n")
	write(t, filepath.Join(d.Hooks, "no-command.yaml"), "event: PreToolUse\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(snap.Hooks) != 1 {
		t.Fatalf("want 1 hook, got %d", len(snap.Hooks))
	}
	h := snap.Hooks["guard"]
	if h.Matcher == nil || !h.Matcher.MatchString("write_file") {
		t.Fatalf("matcher wrong: %+v", h)
	}
	if len(errs) != 3 || !errMentions(errs, "unknown event") || !errMentions(errs, "does not compile") || !errMentions(errs, "command is required") {
		t.Fatalf("want event/matcher/command errors, got: %v", errs)
	}
}

func TestMCPs(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.MCPs, "stdio.yaml"), "command: npx\nargs:\n    - -y\n    - some-mcp\nenv:\n    KEY: val\n")
	write(t, filepath.Join(d.MCPs, "http.yaml"), "url: https://mcp.example.com/mcp\n")
	write(t, filepath.Join(d.MCPs, "both.yaml"), "command: npx\nurl: https://x.example.com\n")
	write(t, filepath.Join(d.MCPs, "neither.yaml"), "args:\n    - x\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(snap.MCPs) != 2 {
		t.Fatalf("want 2 mcps, got %d", len(snap.MCPs))
	}
	if snap.MCPs["stdio"].Command != "npx" || len(snap.MCPs["stdio"].Args) != 2 {
		t.Fatalf("stdio spec wrong: %+v", snap.MCPs["stdio"])
	}
	if snap.MCPs["http"].URL == "" {
		t.Fatal("http spec wrong")
	}
	if len(errs) != 2 || !errMentions(errs, "both.yaml") || !errMentions(errs, "neither.yaml") {
		t.Fatalf("want both/neither errors, got: %v", errs)
	}
}

func TestHiddenAndTempFilesSkipped(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Prompts, ".hidden.md"), "x")
	write(t, filepath.Join(d.Prompts, "draft.md~"), "x")
	write(t, filepath.Join(d.Prompts, "swap.md.swp"), "x")
	write(t, filepath.Join(d.Prompts, "real.md"), "real")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 0 || len(snap.Prompts) != 1 {
		t.Fatalf("noise not skipped: snap=%v errs=%v", snap.Prompts, errs)
	}
}

func TestSnapshotIsImmutableAcrossReloads(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Prompts, "one.md"), "one")
	r := New()
	first, _ := r.LoadAll(d)
	write(t, filepath.Join(d.Prompts, "two.md"), "two")
	second, _ := r.LoadAll(d)
	if len(first.Prompts) != 1 || len(second.Prompts) != 2 {
		t.Fatalf("snapshots share state: first=%d second=%d", len(first.Prompts), len(second.Prompts))
	}
	if r.Snapshot().Prompts["two"].Name != "two" {
		t.Fatal("current snapshot did not update")
	}
}

func TestDuplicateStemsReported(t *testing.T) {
	d := tempDirs(t)
	// guard.yaml + guard.yml collide on the stem key: reported, first wins.
	write(t, filepath.Join(d.Hooks, "guard.yaml"), "event: PreToolUse\ncommand: /bin/a\n")
	write(t, filepath.Join(d.Hooks, "guard.yml"), "event: PreToolUse\ncommand: /bin/b\n")
	write(t, filepath.Join(d.MCPs, "same.yaml"), "command: /bin/a\n")
	write(t, filepath.Join(d.MCPs, "same.yml"), "command: /bin/b\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 2 || !errMentions(errs, "duplicate hook") || !errMentions(errs, "duplicate mcp") {
		t.Fatalf("want duplicate hook+mcp errors, got: %v", errs)
	}
	if snap.Hooks["guard"].Command != "/bin/a" || snap.MCPs["same"].Command != "/bin/a" {
		t.Fatal("deterministic winner wrong")
	}
}

func TestCRLFFrontmatterLoads(t *testing.T) {
	d := tempDirs(t)
	write(t, filepath.Join(d.Skills, "writer", "SKILL.md"),
		"---\r\nname: writer\r\ndescription: writes\r\n---\r\nYou write.\r\n")
	write(t, filepath.Join(d.Agents, "writer.md"),
		"---\r\nname: writer\r\nmodel: small\r\n---\r\nYou write.\r\n")
	r := New()
	snap, errs := r.LoadAll(d)
	if len(errs) != 0 {
		t.Fatalf("CRLF files rejected: %v", errs)
	}
	if _, ok := snap.Skills["writer"]; !ok {
		t.Fatal("CRLF skill not loaded")
	}
	if snap.Agents["writer"].SystemPrompt != "You write." {
		t.Fatalf("CRLF agent body wrong: %q", snap.Agents["writer"].SystemPrompt)
	}
}
