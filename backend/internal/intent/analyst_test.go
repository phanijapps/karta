package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/registry"
)

// The rubric is the analyst agent's own: an inline body carrying every
// slot wins, else the prompt the spec references, else the seeded
// intent-analysis prompt, else the embedded default. A broken candidate
// at any level falls through (never blocks a run).
func TestRubricResolution(t *testing.T) {
	const slots = "s={{.Skills}} a={{.Agents}} m={{.MCPs}} w={{.Wards}}"
	mk := func(spec *registry.AgentSpec, prompts map[string]registry.Prompt) func() registry.Snapshot {
		return func() registry.Snapshot {
			snap := registry.Snapshot{Prompts: prompts}
			if spec != nil {
				snap.Agents = map[string]registry.AgentSpec{spec.Name: *spec}
			}
			return snap
		}
	}
	cases := []struct {
		name    string
		spec    *registry.AgentSpec
		prompts map[string]registry.Prompt
		want    string
		wantNot string
	}{
		{"no spec → intent-analysis prompt", nil, map[string]registry.Prompt{"intent-analysis": {Raw: "SEEDED " + slots}}, "SEEDED", ""},
		{"no spec, no prompt → embedded default", nil, nil, "You are an intent analyzer", ""},
		{"spec references its prompt", &registry.AgentSpec{Name: AnalystAgentName, Prompt: "my-rubric"}, map[string]registry.Prompt{"my-rubric": {Raw: "REFERENCED " + slots}, "intent-analysis": {Raw: "SEEDED " + slots}}, "REFERENCED", "SEEDED"},
		{"spec reference missing → intent-analysis", &registry.AgentSpec{Name: AnalystAgentName, Prompt: "gone"}, map[string]registry.Prompt{"intent-analysis": {Raw: "SEEDED " + slots}}, "SEEDED", ""},
		{"inline body with slots wins", &registry.AgentSpec{Name: AnalystAgentName, Prompt: "my-rubric", SystemPrompt: "INLINE " + slots}, map[string]registry.Prompt{"my-rubric": {Raw: "REFERENCED " + slots}}, "INLINE", "REFERENCED"},
		{"comment body without slots → reference", &registry.AgentSpec{Name: AnalystAgentName, Prompt: "my-rubric", SystemPrompt: "<!-- a note, no slots -->"}, map[string]registry.Prompt{"my-rubric": {Raw: "REFERENCED " + slots}}, "REFERENCED", ""},
		{"broken referenced prompt → intent-analysis", &registry.AgentSpec{Name: AnalystAgentName, Prompt: "bad"}, map[string]registry.Prompt{"bad": {Raw: "broken {{.Skills}"}, "intent-analysis": {Raw: "SEEDED " + slots}}, "SEEDED", ""},
		{"everything broken → default", &registry.AgentSpec{Name: AnalystAgentName, Prompt: "bad"}, map[string]registry.Prompt{"bad": {Raw: "broken {{.Skills}"}, "intent-analysis": {Raw: "no slots"}}, "You are an intent analyzer", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeCompleter{responses: []string{validContract}}
			a := Analyzer{Completer: fc, Snapshot: mk(tc.spec, tc.prompts)}
			if _, err := a.Analyze(context.Background(), "goal", smallPacket(), "p", "m", Limits{}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(fc.lastSystem, tc.want) || (tc.wantNot != "" && strings.Contains(fc.lastSystem, tc.wantNot)) {
				t.Fatalf("rubric source: want %q not %q in:\n%.200s", tc.want, tc.wantNot, fc.lastSystem)
			}
		})
	}
}

func TestSettingsFile(t *testing.T) {
	dir := t.TempDir()
	if s := ReadSettings(dir); s.Selected() || s.Qualified() != "" || s.Deadline() != DefaultDeadline || s.Attempts() != DefaultAttempts {
		t.Fatalf("absent file must read as defaults: %+v", s)
	}
	file := filepath.Join(dir, SettingsFileName)
	if err := os.WriteFile(file, []byte(`{"provider":"zai","model":"glm-5.3-flash","deadline_seconds":20,"max_attempts":2,"reasoning_effort":"low"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := ReadSettings(dir)
	if !s.Selected() || s.Qualified() != "zai/glm-5.3-flash" || s.Deadline() != 20*time.Second || s.Attempts() != 2 {
		t.Fatalf("settings: %+v", s)
	}
	a := &Analyzer{}
	s.ApplyTo(a)
	if a.Deadline != 20*time.Second || a.MaxAttempts != 2 || a.ReasoningEffort != "low" {
		t.Fatalf("ApplyTo: %+v", a)
	}
	// A broken file is the zero value; absurd bounds fall to defaults.
	_ = os.WriteFile(file, []byte(`{not json`), 0o644)
	if s := ReadSettings(dir); s.Selected() {
		t.Fatal("broken file must read as zero")
	}
	if s := (Settings{DeadlineSeconds: -5, MaxAttempts: 0}); s.Deadline() != DefaultDeadline || s.Attempts() != DefaultAttempts {
		t.Fatalf("absurd bounds: %+v", s)
	}
	// The analyst name is the registry's reserved system agent.
	if AnalystAgentName != registry.SystemAgentIntentAnalyst || !registry.IsSystemAgent(AnalystAgentName) {
		t.Fatal("the analyst binding must use the reserved system name")
	}
}
