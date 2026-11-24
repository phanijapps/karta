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
	sort.Strings(names) // lexical = version order for the vNN.N.N form
	var out []string
	for i := len(names) - 1; i >= 0; i-- { // newest first
		out = append(out, ".local/"+names[i]+"/bin")
	}
	return out
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
