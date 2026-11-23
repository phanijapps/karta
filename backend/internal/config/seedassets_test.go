package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// wantSeededSkills is the example skill shelf (operator prompt-craft set).
var wantSeededSkills = []string{"book-reader", "clean-code", "eagle-eye", "explain-like-socrates", "humanizer"}

// The embedded shelf is complete and carries its provenance footers.
// (The strict-loader and registry round-trip live in the external test
// package — seedassets_integration_test.go — which parses what
// Bootstrap actually wrote, keeping this file free of sibling imports.)
func TestSeedEmbedsComplete(t *testing.T) {
	for _, name := range wantSeededSkills {
		raw, err := seedFS.ReadFile("seedassets/skills/" + name + "/SKILL.md")
		if err != nil {
			t.Fatalf("read seed %s: %v", name, err)
		}
		if !strings.Contains(string(raw), "Seeded from ~/Documents/zbot/skills/") {
			t.Fatalf("%s: provenance footer missing", name)
		}
	}
	// book-reader bundles its references.
	for _, ref := range []string{"chunking.md", "epub.md", "pdf.md", "txt.md"} {
		if _, err := seedFS.ReadFile("seedassets/skills/book-reader/references/" + ref); err != nil {
			t.Fatalf("book-reader reference %s: %v", ref, err)
		}
	}
}

// Seeding is write-once: user edits survive re-bootstrap byte-for-byte.
func TestSeedExamplesNeverOverwrite(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	edited := filepath.Join(paths.AgentsDir, "planner-agent.example.md")
	if err := os.WriteFile(edited, []byte("# user-owned now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillMD := filepath.Join(paths.ConfigDir("skills"), "clean-code", "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("---\nname: clean-code\ndescription: mine now\n---\ntuned by the user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("re-Bootstrap: %v", err)
	}
	got, _ := os.ReadFile(edited)
	if string(got) != "# user-owned now\n" {
		t.Fatalf("agent edit overwritten: %q", got)
	}
	gotSkill, _ := os.ReadFile(skillMD)
	if !strings.Contains(string(gotSkill), "tuned by the user") {
		t.Fatalf("skill edit overwritten: %q", gotSkill)
	}
}

// Deleted examples do NOT resurrect on re-bootstrap (spec scenario).
func TestDeletedExampleStaysDeleted(t *testing.T) {
	_, paths := tempHome(t)
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	victim := filepath.Join(paths.AgentsDir, "writing-agent.example.md")
	if err := os.Remove(victim); err != nil {
		t.Fatal(err)
	}
	skillVictim := filepath.Join(paths.ConfigDir("skills"), "humanizer")
	if err := os.RemoveAll(skillVictim); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("re-Bootstrap: %v", err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatal("deleted agent example resurrected")
	}
	if _, err := os.Stat(skillVictim); !os.IsNotExist(err) {
		t.Fatal("deleted skill example resurrected")
	}
	// The others remain.
	if _, err := os.Stat(filepath.Join(paths.AgentsDir, "planner-agent.example.md")); err != nil {
		t.Fatalf("untouched seed should remain: %v", err)
	}
}

// An EXISTING home (user config.yaml, pre-marker) receives the roster
// ONCE: missing examples are filled in, the user's files and config
// stay untouched, and the marker drops (phase-1 gate finding).
func TestUpgradeHomeReceivesRosterOnce(t *testing.T) {
	_, paths := tempHome(t)
	// Simulate a pre-roster install: user config, one OLD example agent
	// (the add-engine-delegation seed), no marker, no skills.
	if err := os.MkdirAll(paths.AgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	user := "engine: custom\nserver:\n    address: 127.0.0.1:9999\ncompaction:\n    max_context_tokens: 1000\n    reserve_output_tokens: 100\n"
	if err := os.WriteFile(paths.ConfigFile, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(paths.AgentsDir, "research-agent.example.md")
	if err := os.WriteFile(old, []byte("---\nname: research-agent\ndescription: the old seed, user-edited\n---\nold body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("upgrade Bootstrap: %v", err)
	}
	// config.yaml untouched.
	got, _ := os.ReadFile(paths.ConfigFile)
	if string(got) != user {
		t.Fatalf("user config overwritten:\n%s", got)
	}
	// The user-owned old seed is untouched.
	gotOld, _ := os.ReadFile(old)
	if !strings.Contains(string(gotOld), "user-edited") {
		t.Fatalf("user-owned example overwritten: %q", gotOld)
	}
	// Missing examples arrived.
	for _, name := range []string{"planner-agent.example.md", "adversarial-reviewer.example.md"} {
		if _, err := os.Stat(filepath.Join(paths.AgentsDir, name)); err != nil {
			t.Fatalf("upgrade missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir("skills"), "clean-code", "SKILL.md")); err != nil {
		t.Fatalf("upgrade missing skill: %v", err)
	}
	// The marker dropped — and a deletion after the upgrade sticks.
	if err := os.Remove(filepath.Join(paths.AgentsDir, "builder-agent.example.md")); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.AgentsDir, "builder-agent.example.md")); !os.IsNotExist(err) {
		t.Fatal("post-marker deletion resurrected")
	}
}

// A seeding run that fails leaves no marker, so a retry completes the
// roster (phase-1 gate finding). Cancellation before the first file
// stands in for a mid-seed failure.
func TestInterruptedSeedingRetries(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := ensureDirs(paths); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before seeding: every seed file is skipped
	if err := seedRoster(ctx, paths); err == nil {
		t.Fatal("cancelled seedRoster must fail")
	}
	if _, err := os.Stat(filepath.Join(paths.Home, exampleRosterMarker)); !os.IsNotExist(err) {
		t.Fatal("failed seeding must not drop the marker")
	}
	// A retried (live) bootstrap completes the roster.
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("retry Bootstrap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.AgentsDir, "planner-agent.example.md")); err != nil {
		t.Fatalf("retry did not seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir("skills"), "eagle-eye", "SKILL.md")); err != nil {
		t.Fatalf("retry did not seed skills: %v", err)
	}
}

// Interruption between temp-write and publish leaves an INERT leftover
// (a dotfile the loaders skip) and never a partial destination — the
// retry then publishes complete content (phase-2 gate finding).
func TestInterruptedPublishLeavesNoPartialDestination(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := os.MkdirAll(paths.AgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed writer: a stale temp dotfile, no destination.
	stale, err := os.CreateTemp(paths.AgentsDir, ".seed-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stale.WriteString("partial bytes from a crashed process"); err != nil {
		t.Fatal(err)
	}
	stale.Close()
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("Bootstrap after crash: %v", err)
	}
	// The destination is COMPLETE content, never the partial bytes.
	got, err := os.ReadFile(filepath.Join(paths.AgentsDir, "research-agent.example.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "partial bytes") {
		t.Fatal("partial temp content leaked into a destination")
	}
	if !strings.Contains(string(got), "Research contract") {
		t.Fatal("seed content incomplete")
	}
}

// Concurrent bootstraps over one home stay consistent: complete roster,
// exactly one marker, no failures (phase-2 gate finding; run under
// -race by the suite).
func TestConcurrentBootstrap(t *testing.T) {
	paths := NewPaths(t.TempDir())
	const writers = 4
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Bootstrap(context.Background(), paths)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	for _, name := range []string{"planner-agent.example.md", "writing-agent.example.md", "adversarial-reviewer.example.md"} {
		got, err := os.ReadFile(filepath.Join(paths.AgentsDir, name))
		if err != nil {
			t.Fatalf("%s missing after concurrent bootstrap: %v", name, err)
		}
		if !strings.Contains(string(got), "Seeded from ~/Documents/zbot/agents/") {
			t.Fatalf("%s incomplete", name)
		}
	}
	// Exactly one marker.
	marker, err := os.ReadDir(paths.Home)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range marker {
		if e.Name() == exampleRosterMarker {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("marker count: %d", count)
	}
	// No leftover temp files after a clean concurrent run.
	for _, e := range marker {
		if strings.HasPrefix(e.Name(), ".seed-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// The persona seeds carry their own marker: a home already marked by
// the roster change still receives the persona ONCE (phase-1 gate
// finding), and persona deletions stick after that.
func TestPersonaSeedUpgradeAndLifecycle(t *testing.T) {
	_, paths := tempHome(t)
	// Simulate a pre-persona install: roster marker present, no prompts.
	if err := os.MkdirAll(paths.AgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Home, exampleRosterMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatalf("upgrade Bootstrap: %v", err)
	}
	// The persona arrived.
	role := filepath.Join(paths.ConfigDir("prompts"), "role.md")
	if _, err := os.Stat(role); err != nil {
		t.Fatalf("upgrade must seed role.md: %v", err)
	}
	// Edit + delete stick after the persona marker dropped.
	if err := os.WriteFile(role, []byte("# my own role\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(paths.ConfigDir("prompts"), "soul.md")); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(role)
	if string(got) != "# my own role\n" {
		t.Fatalf("persona edit overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir("prompts"), "soul.md")); !os.IsNotExist(err) {
		t.Fatal("deleted persona resurrected")
	}
}
