package registry

// The MCP mutation family: validation, marshaling, and the create /
// create-from-key / update / delete write paths (invariants in mutate.go).

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func validateMCP(spec MCPSpec) *MutationError {
	if spec.Command == "" && spec.URL == "" {
		return mutationErr(ErrInvalidManifest, "at least one transport is required: command (stdio) and/or url (http)")
	}
	if spec.Command == "" && (len(spec.Args) > 0 || len(spec.Env) > 0) {
		return mutationErr(ErrInvalidManifest, "a stdio transport requires a command (args/env without one are invalid)")
	}
	if len(spec.Command) > 1024 {
		return mutationErr(ErrInvalidManifest, "command exceeds 1024 characters")
	}
	if len(spec.Args) > 256 {
		return mutationErr(ErrInvalidManifest, "args exceed 256 entries")
	}
	for _, arg := range spec.Args {
		if len(arg) > 1024 {
			return mutationErr(ErrInvalidManifest, "an arg exceeds 1024 characters")
		}
	}
	if len(spec.Env) > 32 {
		return mutationErr(ErrInvalidManifest, "env exceeds 32 entries")
	}
	for k, v := range spec.Env {
		if len(k) > 128 || len(v) > 4096 {
			return mutationErr(ErrInvalidManifest, "env keys are limited to 128 and values to 4096 characters (key %q)", k)
		}
	}
	if spec.URL != "" {
		if len(spec.URL) > 2048 || !httpURLPattern.MatchString(spec.URL) {
			return mutationErr(ErrInvalidManifest, "url must be an http(s) URL of at most 2048 characters")
		}
		if _, err := url.Parse(spec.URL); err != nil {
			return mutationErr(ErrInvalidManifest, "url does not parse: %v", err)
		}
	}
	return nil
}

// marshalMCP renders a manifest exactly as the loader reads it (strict
// decode compatible). Only a DISABLED server writes `enabled: false` —
// enabled manifests stay clean and absent-means-enabled round-trips.
func marshalMCP(spec MCPSpec) ([]byte, error) {
	file := mcpFileSpec{Command: spec.Command, Args: spec.Args, Env: spec.Env, URL: spec.URL}
	if spec.Disabled {
		f := false
		file.Enabled = &f
	}
	return yaml.Marshal(file)
}

// CreateMCP installs config/mcps/<name>.yaml and publishes it.
func (r *Registry) CreateMCP(name string, spec MCPSpec) (MCPSpec, error) {
	if !validManagedName(name) {
		return MCPSpec{}, mutationErr(ErrInvalidName, "name %q is not a managed name (lowercase kebab, 1-64 chars, not reserved)", name)
	}
	if err := validateMCP(spec); err != nil {
		return MCPSpec{}, err
	}
	raw, err := marshalMCP(spec)
	if err != nil {
		return MCPSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}

	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	dir, err := r.kindDirLocked(KindMCP)
	if err != nil {
		return MCPSpec{}, err
	}
	if len(r.snap.Load().MCPs) >= mcpCapacity {
		return MCPSpec{}, mutationErr(ErrCapacity, "the MCP server list is full (%d)", mcpCapacity)
	}
	if _, dup := r.snap.Load().MCPs[name]; dup {
		return MCPSpec{}, mutationErr(ErrDuplicateName, "an MCP server named %q already exists", name)
	}
	target, err := installNoClobber(dir, name+".yaml", raw)
	if err != nil {
		if errors.Is(err, errFileExists) {
			return MCPSpec{}, mutationErr(ErrDuplicateName, "the target file %s already exists", target)
		}
		return MCPSpec{}, r.ioFail(KindMCP, err)
	}
	newSnap := r.reloadKindLocked(KindMCP)
	created := newSnap.MCPs[name]
	return created, nil
}

// CreateMCPFromKey is the import path's create: the caller supplies
// the ORIGINAL key (e.g. a pasted mcpServers JSON key); the key is
// normalized by Slugify (the one normalizer) and duplicates are
// checked BEFORE capacity — an existing manifest, a target path
// occupied on disk (invalid files the snapshot never saw included), or
// an entry created earlier in the same paste all skip as
// ErrDuplicateName even when the registry is full. Returns the stored
// name (the slug) beside the created spec.
func (r *Registry) CreateMCPFromKey(key string, spec MCPSpec) (string, MCPSpec, error) {
	spec.Disabled = false // imports create enabled servers; foreign disabled entries skip instead
	name := Slugify(key)
	if !validManagedName(name) {
		return "", MCPSpec{}, mutationErr(ErrInvalidName, "key %q normalizes to %q, which is not a usable name", key, name)
	}
	if err := validateMCP(spec); err != nil {
		return name, MCPSpec{}, err
	}

	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	dir, err := r.kindDirLocked(KindMCP)
	if err != nil {
		return name, MCPSpec{}, err
	}
	// Duplicates outrank capacity: snapshot name, then the occupied
	// target path (whatever occupies it), then the capacity bound.
	snap := r.snap.Load()
	if _, dup := snap.MCPs[name]; dup {
		return name, MCPSpec{}, mutationErr(ErrDuplicateName, "an MCP server named %q already exists", name)
	}
	if _, statErr := os.Lstat(filepath.Join(dir, name+".yaml")); statErr == nil {
		return name, MCPSpec{}, mutationErr(ErrDuplicateName, "the target file %s already exists", filepath.Join(dir, name+".yaml"))
	}
	if len(snap.MCPs) >= mcpCapacity {
		return name, MCPSpec{}, mutationErr(ErrCapacity, "the MCP server list is full (%d)", mcpCapacity)
	}
	raw, err := marshalMCP(spec)
	if err != nil {
		return name, MCPSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}
	target, err := installNoClobber(dir, name+".yaml", raw)
	if err != nil {
		if errors.Is(err, errFileExists) {
			return name, MCPSpec{}, mutationErr(ErrDuplicateName, "the target file %s already exists", target)
		}
		return name, MCPSpec{}, r.ioFail(KindMCP, err)
	}
	newSnap := r.reloadKindLocked(KindMCP)
	return name, newSnap.MCPs[name], nil
}

// UpdateMCP replaces the named server's manifest wholesale. A non-empty
// newName renames: the new manifest is installed (no-clobber), published,
// and the old file removed. A removal failure leaves disk truth
// published; retrying the SAME rename recognizes its own byte-identical
// target and completes (idempotent resume) without clobbering an
// unrelated occupant.
func (r *Registry) UpdateMCP(name, newName string, spec MCPSpec) (MCPSpec, error) {
	if err := validateMCP(spec); err != nil {
		return MCPSpec{}, err
	}
	if newName != "" && !validManagedName(newName) {
		return MCPSpec{}, mutationErr(ErrInvalidName, "name %q is not a managed name (lowercase kebab, 1-64 chars, not reserved)", newName)
	}
	raw, err := marshalMCP(spec)
	if err != nil {
		return MCPSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}

	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	dir, err := r.kindDirLocked(KindMCP)
	if err != nil {
		return MCPSpec{}, err
	}
	cur, ok := r.snap.Load().MCPs[name]
	if !ok {
		return MCPSpec{}, mutationErr(ErrNotFound, "no MCP server named %q", name)
	}

	if newName == "" || newName == name {
		// In-place edit of the loaded source file (legacy stems and .yml
		// extensions included — the trusted Path, never a synthesized one).
		if err := replaceFile(cur.Path, raw); err != nil {
			return MCPSpec{}, r.ioFail(KindMCP, err)
		}
		newSnap := r.reloadKindLocked(KindMCP)
		return newSnap.MCPs[name], nil
	}

	// Rename. A byte-identical target on disk is our own prior install
	// (a removal-failure residual): resume instead of colliding.
	target := filepath.Join(dir, newName+".yaml")
	resume := false
	if existing, readErr := os.ReadFile(target); readErr == nil && bytes.Equal(existing, raw) {
		resume = true
	} else if _, dup := r.snap.Load().MCPs[newName]; dup {
		return MCPSpec{}, mutationErr(ErrDuplicateName, "an MCP server named %q already exists", newName)
	}
	if !resume {
		installed, err := installNoClobber(dir, newName+".yaml", raw)
		if err != nil {
			if errors.Is(err, errFileExists) {
				return MCPSpec{}, mutationErr(ErrDuplicateName, "the target file %s already exists", installed)
			}
			return MCPSpec{}, r.ioFail(KindMCP, err)
		}
	}
	if err := os.Remove(cur.Path); err != nil && !os.IsNotExist(err) {
		// Disk truth (both files) is published; the residual notifies as
		// an added entry, and retrying the same rename completes it.
		r.reloadKindLocked(KindMCP)
		return MCPSpec{}, mutationErr(ErrIO, "renamed to %s but removing %s failed: %v (retry to complete)", newName, cur.Path, err)
	}
	newSnap := r.reloadKindLocked(KindMCP)
	return newSnap.MCPs[newName], nil
}

// DeleteMCP removes the named server's manifest file and unregisters it.
func (r *Registry) DeleteMCP(name string) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if _, err := r.kindDirLocked(KindMCP); err != nil {
		return err
	}
	cur, ok := r.snap.Load().MCPs[name]
	if !ok {
		return mutationErr(ErrNotFound, "no MCP server named %q", name)
	}
	if err := os.Remove(cur.Path); err != nil && !os.IsNotExist(err) {
		return r.ioFail(KindMCP, err)
	}
	r.reloadKindLocked(KindMCP)
	return nil
}

// ---------------------------------------------------------------------------
// Skills
// ---------------------------------------------------------------------------

// skillBodyLimit bounds a SKILL.md body in BYTES (multibyte-safe: the
// check is on the encoded payload, not rune count).
const skillBodyLimit = 256 * 1024

// validateSkill checks a skill payload's frontmatter rules and bounds.
