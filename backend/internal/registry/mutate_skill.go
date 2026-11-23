package registry

// The skill mutation family: validation, SKILL.md rendering, and the
// create / update / delete / detail write paths (invariants in mutate.go).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func validateSkill(name, description string) *MutationError {
	if strings.TrimSpace(name) == "" {
		return mutationErr(ErrInvalidManifest, "skill name is required")
	}
	if len(name) > 128 {
		return mutationErr(ErrInvalidManifest, "skill name exceeds 128 characters")
	}
	if strings.TrimSpace(description) == "" {
		return mutationErr(ErrInvalidManifest, "skill description is required")
	}
	if len(description) > 512 {
		return mutationErr(ErrInvalidManifest, "skill description exceeds 512 characters")
	}
	return nil
}

// renderSkillMD writes a SKILL.md: frontmatter (yaml-marshalled for safe
// quoting) plus the body verbatim.
func renderSkillMD(name, description, body string) ([]byte, error) {
	fm, err := yaml.Marshal(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{name, description})
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm)
	// Single newline after the closing delimiter: splitFrontmatter trims
	// exactly one, so the body round-trips byte-for-byte.
	b.WriteString("---\n")
	b.WriteString(body)
	return []byte(b.String()), nil
}

// skillDirFor returns the managed directory for slug under the skills root.
func skillDirFor(root, slug string) string { return filepath.Join(root, slug) }

// CreateSkill writes skills/<slug>/SKILL.md (top-level root (no-clobber directory,
// atomic file) and publishes it.
func (r *Registry) CreateSkill(name, description, body string) (SkillSpec, error) {
	if err := validateSkill(name, description); err != nil {
		return SkillSpec{}, err
	}
	if len(body) > skillBodyLimit {
		return SkillSpec{}, mutationErr(ErrInvalidManifest, "skill body exceeds %d bytes", skillBodyLimit)
	}
	slug := Slugify(name)
	if !validManagedName(slug) {
		return SkillSpec{}, mutationErr(ErrInvalidName, "name %q slugifies to %q, which is not a valid managed name", name, slug)
	}
	raw, err := renderSkillMD(name, description, body)
	if err != nil {
		return SkillSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}

	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	root, err := r.kindDirLocked(KindSkill)
	if err != nil {
		return SkillSpec{}, err
	}
	if len(r.snap.Load().Skills) >= skillCapacity {
		return SkillSpec{}, mutationErr(ErrCapacity, "the skill list is full (%d)", skillCapacity)
	}
	if _, dup := r.snap.Load().Skills[name]; dup {
		return SkillSpec{}, mutationErr(ErrDuplicateName, "a skill named %q already exists", name)
	}
	dir := skillDirFor(root, slug)
	// os.Mkdir is the no-clobber create: it fails if anything occupies
	// the path, whatever the snapshot thinks of it.
	if err := os.Mkdir(dir, 0o755); err != nil {
		if os.IsExist(err) {
			return SkillSpec{}, mutationErr(ErrDuplicateName, "the target directory %s already exists", dir)
		}
		return SkillSpec{}, r.ioFail(KindSkill, fmt.Errorf("mkdir %s: %w", dir, err))
	}
	// Atomic SKILL.md install: no reader (or a failure path) ever sees a
	// partial file; a crash before this leaves an assets-only directory
	// the loader already skips by design.
	if err := replaceFile(filepath.Join(dir, "SKILL.md"), raw); err != nil {
		_ = os.Remove(dir) // compensation: leave no assets-only husk
		return SkillSpec{}, r.ioFail(KindSkill, err)
	}
	newSnap := r.reloadKindLocked(KindSkill)
	created, ok := newSnap.Skills[name]
	if !ok {
		// The written file failed the loader (should be impossible after
		// validation); publish disk truth and report it.
		return SkillSpec{}, mutationErr(ErrIO, "skill written but did not load (see registry errors)")
	}
	return created, nil
}

// UpdateSkill replaces the named skill's SKILL.md wholesale (frontmatter
// AND body — send the current body for a frontmatter-only edit). A
// non-empty newName renames: the directory moves to the new slug with an
// atomic no-clobber directory rename and the frontmatter updates; a
// failed rewrite rolls the directory back, and if rollback itself fails
// disk truth is published and the retry is idempotent. Bundled files are
// preserved verbatim by every operation that keeps the skill.
func (r *Registry) UpdateSkill(name, newName, description, body string) (SkillSpec, error) {
	targetName := firstNonEmpty(newName, name)
	if err := validateSkill(targetName, description); err != nil {
		return SkillSpec{}, err
	}
	if len(body) > skillBodyLimit {
		return SkillSpec{}, mutationErr(ErrInvalidManifest, "skill body exceeds %d bytes", skillBodyLimit)
	}
	var newSlug string
	if targetName != name {
		newSlug = Slugify(targetName)
		if !validManagedName(newSlug) {
			return SkillSpec{}, mutationErr(ErrInvalidName, "name %q slugifies to %q, which is not a valid managed name", targetName, newSlug)
		}
	}
	raw, err := renderSkillMD(targetName, description, body)
	if err != nil {
		return SkillSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}

	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	root, err := r.kindDirLocked(KindSkill)
	if err != nil {
		return SkillSpec{}, err
	}
	cur, ok := r.snap.Load().Skills[name]
	if !ok {
		return SkillSpec{}, mutationErr(ErrNotFound, "no skill named %q", name)
	}
	if targetName != name {
		if other, dup := r.snap.Load().Skills[targetName]; dup && other.Path != cur.Path {
			return SkillSpec{}, mutationErr(ErrDuplicateName, "a skill named %q already exists", targetName)
		}
	}

	if targetName == name {
		// In-place edit of the trusted source file: atomic replace keeps
		// the original loadable until the commit and never truncates it.
		if err := replaceFile(filepath.Join(cur.Path, "SKILL.md"), raw); err != nil {
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
		newSnap := r.reloadKindLocked(KindSkill)
		return newSnap.Skills[name], nil
	}

	// Rename: atomic no-clobber directory move, then an atomic SKILL.md
	// rewrite inside the moved directory; on a rewrite failure move back,
	// and if that fails too, publish disk truth. The original SKILL.md
	// stays intact on disk until its replacement commits.
	targetDir := skillDirFor(root, newSlug)
	moved := false
	if targetDir != cur.Path {
		if err := renameDirNoClobber(cur.Path, targetDir); err != nil {
			if os.IsExist(err) {
				return SkillSpec{}, mutationErr(ErrDuplicateName, "the target directory %s already exists", targetDir)
			}
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
		moved = true
	}
	if err := replaceFile(filepath.Join(targetDir, "SKILL.md"), raw); err != nil {
		if moved {
			if rbErr := os.Rename(targetDir, cur.Path); rbErr != nil {
				r.reloadKindLocked(KindSkill)
				return SkillSpec{}, mutationErr(ErrIO, "rename to %s failed and rollback failed: %v (state matches disk; retry)", newSlug, rbErr)
			}
		}
		return SkillSpec{}, r.ioFail(KindSkill, err)
	}
	newSnap := r.reloadKindLocked(KindSkill)
	return newSnap.Skills[targetName], nil
}

// DeleteSkill removes the named skill's whole directory (bundle included).
// A partially failed removal publishes disk truth and reports the residual.
func (r *Registry) DeleteSkill(name string) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if _, err := r.kindDirLocked(KindSkill); err != nil {
		return err
	}
	cur, ok := r.snap.Load().Skills[name]
	if !ok {
		return mutationErr(ErrNotFound, "no skill named %q", name)
	}
	if err := os.RemoveAll(cur.Path); err != nil {
		r.reloadKindLocked(KindSkill)
		return mutationErr(ErrIO, "removing %s failed: %v (state matches disk; residual visible as load error or assets-only directory)", cur.Path, err)
	}
	r.reloadKindLocked(KindSkill)
	return nil
}

// SkillDetail returns the skill's loaded spec and SKILL.md body. A pure
// read: it never publishes state or mutates errors (a failed read is a
// typed error for the caller).
func (r *Registry) SkillDetail(name string) (SkillSpec, string, error) {
	cur, ok := r.Snapshot().Skills[name]
	if !ok {
		return SkillSpec{}, "", mutationErr(ErrNotFound, "no skill named %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(cur.Path, "SKILL.md"))
	if err != nil {
		return SkillSpec{}, "", mutationErr(ErrIO, "read %s: %v", filepath.Join(cur.Path, "SKILL.md"), err)
	}
	_, body, ok := splitFrontmatter(string(raw))
	if !ok {
		return SkillSpec{}, "", mutationErr(ErrIO, "%s: no YAML frontmatter", filepath.Join(cur.Path, "SKILL.md"))
	}
	return cur, body, nil
}

// ---------------------------------------------------------------------------
// Shared internals
// ---------------------------------------------------------------------------
