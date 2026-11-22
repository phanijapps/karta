package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSched(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSchedules(t *testing.T) {
	dir := t.TempDir()
	writeSched(t, dir, "digest.yaml", "name: digest\nschedule: \"0 7 * * *\"\ngoal: Summarize today's timeline.\nagent: researcher\n")
	writeSched(t, dir, "catchup.yaml", "name: catchup\nschedule: \"@daily\"\ngoal: G.\nmissed_policy: catch_up\n")
	writeSched(t, dir, "bad-policy.yaml", "name: bad\nschedule: \"@daily\"\ngoal: G\nmissed_policy: sometimes\n")
	writeSched(t, dir, "missing-goal.yaml", "name: noGoal\nschedule: \"@daily\"\n")
	writeSched(t, dir, "unknown-field.yaml", "name: u\nschedule: \"@daily\"\ngoal: G\nsurprise: 1\n")

	reg := New()
	snap, errs := reg.LoadAll(Dirs{Schedules: dir})
	if len(errs) != 3 {
		for _, e := range errs {
			t.Logf("err: %v", e)
		}
		t.Fatalf("want 3 errors (bad policy, missing goal, unknown field), got %d", len(errs))
	}
	if s, ok := snap.Schedules["digest"]; !ok || s.Agent != "researcher" || s.MissedPolicy != "skip" {
		t.Fatalf("digest wrong: %+v", s)
	}
	if s, ok := snap.Schedules["catchup"]; !ok || s.MissedPolicy != "catch_up" {
		t.Fatalf("catchup wrong: %+v", s)
	}
	// Error text is file-addressed and names the violation.
	joined := ""
	for _, e := range errs {
		joined += e.Error()
	}
	if !strings.Contains(joined, "missed_policy") || !strings.Contains(joined, "goal are required") || !strings.Contains(joined, "surprise") {
		t.Fatalf("error messages lack specifics: %s", joined)
	}
}
