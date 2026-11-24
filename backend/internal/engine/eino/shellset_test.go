package eino

import (
	"testing"

	"zbot/backend/internal/wardtools"
)

// TestWardToolSetIncludesShell: the hosted plane offers ward_shell
// (root and child shapes).
func TestWardToolSetIncludesShell(t *testing.T) {
	host := wardtools.NewHost(nil)
	set := wardToolSet(runInput{wardHost: host, isChild: true})
	found := ""
	for _, tl := range set {
		if tl.Def().Name == "ward_shell" {
			found = "yes"
			// The def must convert to eino's parameter shape cleanly.
			p := paramsOneOfFromJSON(tl.Def().ParamsJSON)
			if p == nil {
				t.Fatal("shell params failed to convert")
			}
		}
	}
	if found == "" {
		names := make([]string, 0, len(set))
		for _, tl := range set {
			names = append(names, tl.Def().Name)
		}
		t.Fatalf("ward_shell missing from hosted set: %v", names)
	}
}
