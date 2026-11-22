package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// managedNamePattern constrains daemon-created and daemon-renamed entry
// names (file stems and skill directory slugs). Mirrors the providers
// package's rule; kept local because the two packages are siblings in the
// dependency graph and must not import each other.
var managedNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// reservedWindowsNames are device names that cannot be file stems on
// Windows; rejecting them keeps every home portable.
var reservedWindowsNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {},
	"com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {},
	"lpt7": {}, "lpt8": {}, "lpt9": {},
}

// validManagedName reports whether name is a legal stem/slug for
// daemon-created entries (existing hand-authored entries may be broader).
func validManagedName(name string) bool {
	if !managedNamePattern.MatchString(name) {
		return false
	}
	_, reserved := reservedWindowsNames[name]
	return !reserved
}

// Slugify derives a managed file stem from a name (skill directories,
// MCP import keys): lowercase, runs of anything outside [a-z0-9]
// collapsed to single dashes, trimmed — no truncation, no invented
// fallback. The result still needs validManagedName before use; this
// is the one normalizer (exported for the import path).
func Slugify(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = true
		default:
			// collapse runs of separators/other characters
			if dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = false
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// mcpCapacity and skillCapacity bound their kinds: creates are refused
// beyond the bound and hand-edited overflow becomes per-file load errors
// (sorted directory walks make the lexicographically-last files the
// overflow), so API lists are always complete within the bound.
const (
	mcpCapacity   = 128
	skillCapacity = 256
)

// MutationErrorCode drives the daemon's HTTP error mapping.
type MutationErrorCode string

const (
	// ErrDuplicateName — the entry name or target path is occupied on
	// disk (valid file, invalid file, watcher window, assets-only dir).
	ErrDuplicateName MutationErrorCode = "duplicate_name"
	// ErrInvalidName — create/rename target fails the managed-name or
	// slug rules.
	ErrInvalidName MutationErrorCode = "invalid_name"
	// ErrInvalidManifest — the payload violates a load rule (e.g. an MCP
	// manifest with no transport, a skill with no description).
	ErrInvalidManifest MutationErrorCode = "invalid_manifest"
	// ErrNotFound — no such entry in the current snapshot.
	ErrNotFound MutationErrorCode = "not_found"
	// ErrCapacity — the per-kind capacity is full.
	ErrCapacity MutationErrorCode = "capacity"
	// ErrIO — a filesystem commit failed after validation; the published
	// snapshot matches disk.
	ErrIO MutationErrorCode = "io_error"
	// ErrUnconfigured — LoadAll/Watch never ran, so no dirs are known.
	ErrUnconfigured MutationErrorCode = "unconfigured"
)

// MutationError is a typed mutation failure; Code is stable API surface.
type MutationError struct {
	Code    MutationErrorCode
	Message string
}

func (e *MutationError) Error() string { return "registry: " + string(e.Code) + ": " + e.Message }

func mutationErr(code MutationErrorCode, format string, args ...any) *MutationError {
	return &MutationError{Code: code, Message: fmt.Sprintf(format, args...)}
}
