package domain

import (
	"os/exec"
	"strings"
	"testing"
)

// TestPurity enforces the constitution's domain rule: this package imports
// nothing outside the standard library and no other internal package. It
// fails the build the moment a violating import lands.
func TestPurity(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps .: %v", err)
	}
	var offenders []string
	for _, dep := range strings.Fields(string(out)) {
		if dep == "." || dep == "zbot/backend/internal/domain" {
			continue
		}
		// Any internal package dependency is a violation (we are the only
		// allowed internal package, and we never import ourselves).
		if strings.HasPrefix(dep, "zbot/backend/") {
			offenders = append(offenders, dep)
			continue
		}
		// Stdlib paths never contain a dot in the first element; anything
		// that does is a third-party module.
		first := dep
		if i := strings.IndexByte(dep, '/'); i >= 0 {
			first = dep[:i]
		}
		if strings.Contains(first, ".") {
			offenders = append(offenders, dep)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("domain purity violated — package must import only stdlib; offending deps: %s",
			strings.Join(offenders, ", "))
	}
}
