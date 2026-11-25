package registry

import "testing"

func TestIsSystemAgent(t *testing.T) {
	if !IsSystemAgent(SystemAgentIntentAnalyst) {
		t.Fatal("the intent analyst is a system agent")
	}
	for _, n := range []string{"", "root", "planner-agent", "intent-analyst-2", "Intent-Analyst"} {
		if IsSystemAgent(n) {
			t.Fatalf("%q must not be a system agent (exact, case-sensitive match)", n)
		}
	}
}
