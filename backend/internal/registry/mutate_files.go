package registry

// Filesystem install helpers for the write paths: atomic no-clobber
// installs and replaces (the mechanics behind duplicate_name semantics —
// see mutate.go's invariants block).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var errFileExists = fmt.Errorf("target exists")

// httpURLPattern is the contract's URL constraint for HTTP transports.
var httpURLPattern = regexp.MustCompile(`^https?://`)

// installNoClobber writes raw to dir/name atomically, failing if the
// target already exists (temp file + hard link — the link is the atomic
// no-clobber commit).
func installNoClobber(dir, name string, raw []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("ensure %s: %w", dir, err)
	}
	target := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, "."+name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("temp write for %s: %w", target, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Link(tmp.Name(), target); err != nil {
		if os.IsExist(err) {
			return target, errFileExists
		}
		return "", fmt.Errorf("install %s: %w", target, err)
	}
	ok = true
	_ = os.Remove(tmp.Name())
	return target, nil
}

// replaceFile atomically overwrites path with raw (temp + rename), for
// files a mutation already owns or creates fresh: readers see the old
// bytes or the new bytes, never a truncated write.
func replaceFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("temp write for %s: %w", path, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	ok = true
	return nil
}

// ---------------------------------------------------------------------------
// MCP manifests
// ---------------------------------------------------------------------------

// validateMCP applies the loader's manifest rules AND the contract's
// field bounds to a mutation payload: at least one transport (both is a
// valid dual-transport server), and every declared transport internally
// valid — a present-but-commandless stdio section is debris, never
// silently dropped.
