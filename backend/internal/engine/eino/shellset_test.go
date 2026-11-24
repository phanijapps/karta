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

// TestRestrictiveAllowlistGovernsWardPlane (final-review P2-7): a
// non-empty agent allowlist filters the ward plane (ward_shell drops);
// nil keeps everything.
func TestRestrictiveAllowlistGovernsWardPlane(t *testing.T) {
	host := wardtools.NewHost(nil)
	allow := []string{"ward_read"}
	filtered := filterWardTools(wardToolSet(runInput{wardHost: host, isChild: true}), allow)
	for _, tl := range filtered {
		if tl.Def().Name != "ward_read" {
			t.Fatalf("restrictive allowlist leaked %q", tl.Def().Name)
		}
	}
	if len(filtered) != 1 {
		t.Fatalf("want exactly ward_read, got %d", len(filtered))
	}
	all := filterWardTools(wardToolSet(runInput{wardHost: host, isChild: true}), nil)
	found := 0
	for _, tl := range all {
		if tl.Def().Name == "ward_shell" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("nil allowlist must keep ward_shell, found %d", found)
	}
}
