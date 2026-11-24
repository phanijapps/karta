package mcpclient

// Binary resolution + restart backoff for the execution bridge: the
// daemon's PATH is thinner than a shell's, so configured commands
// resolve as written first, then against the user's common tool dirs;
// a crash-looping server retries on a bounded, growing schedule.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"zbot/backend/internal/registry"
)

// binaryDirs are common tool directories a SHELL user has on PATH but
// the daemon (launched from a desktop/systemd env) often does not.
var binaryDirs = []string{
	".local/node/bin", ".local/bin", ".bun/bin",
	"go/bin", ".cargo/bin", ".rye/shims", ".local/share/pnpm",
}

// nodeVersionDirs lists ~/.local/node*/bin (versioned installs —
// "node-v24.19.0"), newest version first, deterministically.
func nodeVersionDirs(home string) []string {
	entries, err := os.ReadDir(filepath.Join(home, ".local"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "node-v") {
			names = append(names, e.Name())
		}
	}
	// Version order: compare the dotted numeric segments (v9 < v10 —
	// lexical order would misplace them), newest first.
	sort.Slice(names, func(i, j int) bool { return versionLess(names[i], names[j]) })
	var out []string
	for i := len(names) - 1; i >= 0; i-- {
		out = append(out, ".local/"+names[i]+"/bin")
	}
	return out
}

// atoiSeg parses a dotted version segment ("" → 0).
func atoiSeg(s string) int {
	n, _ := strconv.Atoi(s) //nolint:errcheck // non-numeric segments compare as 0 (documented)
	return n
}

// versionLess orders "node-vA.B.C" names by numeric dotted segments.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "node-v"), "."), strings.Split(strings.TrimPrefix(b, "node-v"), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		xa, xb := atoiSeg(pa[i]), atoiSeg(pb[i]) // non-numeric segments compare as 0
		if xa != xb {
			return xa < xb
		}
	}
	return len(pa) < len(pb)
}

// resolveBinary finds an executable: as written first (absolute or on
// PATH), else by name across binaryDirs under HOME. Falls back to the
// original so exec reports the familiar "not found" when absent.
func resolveBinary(command string) string {
	if strings.Contains(command, "/") {
		return command
	}
	if p, err := exec.LookPath(command); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return command
	}
	for _, d := range append(nodeVersionDirs(home), binaryDirs...) {
		cand := filepath.Join(home, d, command)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return cand
		}
	}
	return command
}

// backoffFor a server's consecutive start failures (2s·2^n, cap 2min).
func backoffFor(failures int) time.Duration {
	d := 2 * time.Second
	for i := 1; i < failures && d < 2*time.Minute; i++ {
		d *= 2
	}
	if d > 2*time.Minute {
		d = 2 * time.Minute
	}
	return d
}

// ServerSpec names one enabled stdio server to run.
type ServerSpec struct {
	Name string
	Spec registry.MCPSpec
}
