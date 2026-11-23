package wards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VaultRootError is a typed vault-configuration error: the configured
// value cannot name a vault. The message names the config field so the
// operator knows exactly what to fix.
type VaultRootError struct {
	Value  string
	Reason string
}

func (e *VaultRootError) Error() string {
	return fmt.Sprintf("wards: config.yaml: wards.vault_root %q: %s", e.Value, e.Reason)
}

// ResolveVaultRoot resolves the vault root from the raw configured value
// (config.yaml wards.vault_root) and the resolved daemon home (~/.zbot).
// Empty means the OS-aware default ~/Documents/wards — the vault lives
// OUTSIDE the daemon home by design: wards are user data, not daemon
// state (the constitution's second scoped ~/.zbot exception), so a root
// equal to or beneath the daemon home is a typed error. "~" and "~/"
// expand to the user home; any other relative path is a typed error
// (relative roots would resolve against the daemon's working directory,
// which is nobody's intent). Existing paths are also compared through
// symlinks so an aliased ~/.zbot cannot sneak the vault inside.
func ResolveVaultRoot(configured, daemonHome string) (string, error) {
	v := strings.TrimSpace(configured)
	if v == "" {
		return DefaultVaultRoot()
	}
	var root string
	switch {
	case v == "~":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("wards: resolve vault root: %w", err)
		}
		root = home
	case strings.HasPrefix(v, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("wards: resolve vault root: %w", err)
		}
		root = filepath.Join(home, v[2:])
	default:
		if !filepath.IsAbs(v) {
			return "", &VaultRootError{Value: configured, Reason: "must be an absolute path (e.g. ~/Documents/wards)"}
		}
		root = filepath.Clean(v)
	}
	root = filepath.Clean(root)
	if err := rejectInsideDaemonHome(root, daemonHome); err != nil {
		return "", err
	}
	return root, nil
}

// rejectInsideDaemonHome enforces the constitution's vault exception:
// the vault never lives inside the daemon home. Comparison is lexical
// over cleaned paths, plus a symlink-evaluated pass — symlinks are
// resolved through the deepest EXISTING ancestor so an ~/.zbot alias
// pointing at a not-yet-created vault path cannot bypass the rule
// (bootstrap creates the vault, not the home).
func rejectInsideDaemonHome(root, daemonHome string) error {
	if daemonHome == "" {
		return nil // no home context (unit helpers); the rule is vacuous
	}
	home := filepath.Clean(daemonHome)
	if within(root, home) {
		return &VaultRootError{Value: root, Reason: fmt.Sprintf("must live outside the daemon home %s (wards are user data, not daemon state)", home)}
	}
	realHome, homeErr := filepath.EvalSymlinks(home)
	if homeErr != nil {
		return nil // home does not exist yet — the lexical check above governs
	}
	if within(resolveExistingAncestors(root), realHome) {
		return &VaultRootError{Value: root, Reason: fmt.Sprintf("must live outside the daemon home %s (symlinks do not move it out)", home)}
	}
	return nil
}

// resolveExistingAncestors resolves symlinks of the deepest existing
// ancestor of path and re-appends the non-existing remainder.
func resolveExistingAncestors(path string) string {
	p := filepath.Clean(path)
	for {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			if p == path {
				return path
			}
			return filepath.Join(resolved, strings.TrimPrefix(strings.TrimPrefix(path, p), string(filepath.Separator)))
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path // reached the root without an existing ancestor
		}
		p = parent
	}
}

// within reports whether path is home itself or beneath it.
func within(path, home string) bool {
	return path == home || strings.HasPrefix(path, home+string(filepath.Separator))
}

// DefaultVaultRoot returns ~/Documents/wards. On headless Linux where no
// Documents dir exists, Bootstrap creates it — the default is only a
// default; config overrides win (mirrors agentzero's convention).
func DefaultVaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("wards: default vault root: %w", err)
	}
	return filepath.Join(home, "Documents", "wards"), nil
}
