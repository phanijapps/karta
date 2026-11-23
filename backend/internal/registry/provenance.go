package registry

// Provenance: the reserved .karta/provenance.yaml sidecar recording
// where an installed skill came from. Descriptive metadata only — never
// authenticity proof. A missing or malformed sidecar degrades the skill
// to origin `local`; the loader never rejects a skill over provenance.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Skill origins.
const (
	OriginLocal       = "local"
	OriginMarketplace = "marketplace"
	OriginImport      = "import"
)

// ProvenancePath is reserved: bundles containing it reject at preview,
// and it is excluded from the content hash.
const provenancePath = ".karta/provenance.yaml"

// provenanceFile is the bounded, versioned on-disk schema.
type provenanceFile struct {
	APIVersion  string    `yaml:"apiVersion"`
	Origin      string    `yaml:"origin"`
	Source      string    `yaml:"source,omitempty"`
	Ref         string    `yaml:"ref,omitempty"`
	Commit      string    `yaml:"commit,omitempty"`
	InstalledAt time.Time `yaml:"installedAt"`
	ContentHash string    `yaml:"contentHash"`
}

// applyProvenance derives Origin/Source/Ref from the sidecar. The read
// is bounded and validation is SEMANTIC: any missing or malformed field
// degrades the skill to local — never a half-trusted record.
func applyProvenance(spec *SkillSpec, skillDir string) {
	spec.Origin = OriginLocal
	raw, err := os.ReadFile(filepath.Join(skillDir, provenancePath))
	if err != nil {
		return
	}
	if len(raw) > 4096 {
		return // oversized: malformed
	}
	var pf provenanceFile
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return
	}
	if pf.APIVersion != "karta/v1" || pf.InstalledAt.IsZero() || !validContentHash(pf.ContentHash) {
		return
	}
	switch pf.Origin {
	case OriginMarketplace:
		if pf.Source == "" || pf.Commit == "" {
			return // marketplace records require source and commit
		}
		spec.Origin = pf.Origin
		spec.Source = pf.Source
		spec.Ref = pf.Ref
	case OriginImport:
		spec.Origin = pf.Origin
	default:
		// unknown origin values degrade to local
	}
}

// validContentHash accepts exactly 64 lowercase hex chars.
func validContentHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// SkillFile is one bundle file: a validated relative path, a mode from
// the allowed subset (0o644/0o755 — executable bits survive for
// scripts; special bits never), and its bytes.
type SkillFile struct {
	Path string
	Mode fs.FileMode
	Data []byte
}

// SkillBundle is a fully validated skill ready to install atomically.
// SKILL.md must be among Files with loader-valid frontmatter.
type SkillBundle struct {
	// Name/Description come from the SKILL.md frontmatter (validated).
	Name        string
	Description string
	Files       []SkillFile
	// Provenance is written for non-local installs; nil for local.
	Provenance *ProvenanceInput
}

// ProvenanceInput describes the install for the sidecar.
type ProvenanceInput struct {
	Origin string
	Source string
	Ref    string
	Commit string
}

// ContentHash canonicalizes the payload tree EXCLUDING the provenance
// sidecar: sorted relative paths, `path\0size\0sha256\n` tuples.
func (b *SkillBundle) ContentHash() string {
	entries := make([]string, 0, len(b.Files))
	for _, f := range b.Files {
		if f.Path == provenancePath {
			continue
		}
		sum := sha256.Sum256(f.Data)
		entries = append(entries, fmt.Sprintf("%s\x00%d\x00%s\n", f.Path, len(f.Data), hex.EncodeToString(sum[:])))
	}
	sort.Strings(entries)
	total := sha256.Sum256([]byte(strings.Join(entries, "")))
	return hex.EncodeToString(total[:])
}

// ValidBundlePath validates one bundle-relative path (shared rule for
// preview and install): slash-separated, no absolute/drive/UNC forms,
// no `..`, no backslashes, no NUL, and never the reserved provenance
// path. Called on raw names before normalization and again here.
func ValidBundlePath(rel string) error {
	if rel == "" || rel == "/" || strings.ContainsRune(rel, 0) {
		return fmt.Errorf("empty or NUL-bearing path")
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("backslash separator %q", rel)
	}
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "./") || rel == "." {
		return fmt.Errorf("absolute or dot-relative path %q", rel)
	}
	if len(rel) >= 2 && rel[1] == ':' { // Windows drive form C:/x
		return fmt.Errorf("drive-letter path %q", rel)
	}
	if strings.HasPrefix(rel, "//") || strings.HasPrefix(rel, "\\\\") {
		return fmt.Errorf("UNC path %q", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("empty, dot, or parent component in %q", rel)
		}
	}
	if rel == provenancePath || strings.HasPrefix(rel, provenancePath+"/") {
		return fmt.Errorf("reserved provenance path %q", rel)
	}
	return nil
}

// validateBundle applies every pre-write rule: paths, modes, SKILL.md
// presence and loader-exact frontmatter, normalized duplicates, and
// file/directory collisions.
func validateBundle(b *SkillBundle) *MutationError {
	if b.Name == "" || !validManagedName(Slugify(b.Name)) {
		return mutationErr(ErrInvalidName, "skill name %q is not usable", b.Name)
	}
	if b.Description == "" {
		return mutationErr(ErrInvalidManifest, "skill description is required")
	}
	seen := map[string]bool{}
	fileSet := map[string][]byte{}
	for _, f := range b.Files {
		if err := ValidBundlePath(f.Path); err != nil {
			return mutationErr(ErrInvalidManifest, "bundle path invalid: %v", err)
		}
		if f.Mode != 0o644 && f.Mode != 0o755 {
			return mutationErr(ErrInvalidManifest, "file mode %o not allowed for %q (0o644/0o755 only)", f.Mode, f.Path)
		}
		if seen[f.Path] {
			return mutationErr(ErrInvalidManifest, "duplicate bundle path %q", f.Path)
		}
		seen[f.Path] = true
		fileSet[f.Path] = f.Data
	}
	// Order-independent file/directory collision check: every ancestor of
	// every file must not itself be a file.
	for path := range fileSet {
		for dir := filepath.Dir(path); dir != "."; dir = filepath.Dir(dir) {
			if _, isFile := fileSet[dir]; isFile {
				return mutationErr(ErrInvalidManifest, "path %q is both a file and a directory prefix", dir)
			}
		}
	}
	raw, hasSkillMD := fileSet["SKILL.md"]
	if !hasSkillMD {
		return mutationErr(ErrInvalidManifest, "bundle has no SKILL.md")
	}
	// Loader-exact validation BEFORE staging: the frontmatter must parse,
	// and its name/description are authoritative — metadata mismatch is
	// an invalid bundle, never a silently renamed skill.
	fmName, fmDesc, _, perr := ParseSkillMD(string(raw))
	if perr != nil {
		return mutationErr(ErrInvalidManifest, "SKILL.md invalid: %v", perr)
	}
	if b.Name != fmName {
		return mutationErr(ErrInvalidManifest, "bundle name %q does not match SKILL.md frontmatter name %q", b.Name, fmName)
	}
	if b.Description != fmDesc {
		return mutationErr(ErrInvalidManifest, "bundle description does not match SKILL.md frontmatter")
	}
	return nil
}

// renderProvenance writes the sidecar bytes for the bundle.
func renderProvenance(p *ProvenanceInput, hash string, at time.Time) ([]byte, error) {
	pf := provenanceFile{
		APIVersion: "karta/v1", Origin: p.Origin, Source: p.Source,
		Ref: p.Ref, Commit: p.Commit, InstalledAt: at.UTC(), ContentHash: hash,
	}
	return yaml.Marshal(&pf)
}

// InstallSkill atomically installs a validated bundle under
// skills/<slug>/ (top-level root): build in a private hidden staging sibling
// (0o700), then the directory no-clobber rename (frontmatter-name
// duplicates and occupied slug paths — snapshot-invisible occupants
// included — refuse), publish, notify. Disk truth wins on failure.
func (r *Registry) InstallSkill(b SkillBundle) (SkillSpec, error) {
	if verr := validateBundle(&b); verr != nil {
		return SkillSpec{}, verr
	}
	slug := Slugify(b.Name)

	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	root, err := r.kindDirLocked(KindSkill)
	if err != nil {
		return SkillSpec{}, err
	}
	if len(r.snap.Load().Skills) >= skillCapacity {
		return SkillSpec{}, mutationErr(ErrCapacity, "the skill list is full (%d)", skillCapacity)
	}
	if existing, dup := r.snap.Load().Skills[b.Name]; dup {
		return SkillSpec{}, mutationErr(ErrDuplicateName, "a skill named %q already exists (%s)", b.Name, existing.Path)
	}
	target := filepath.Join(root, slug)
	if _, statErr := os.Lstat(target); statErr == nil {
		return SkillSpec{}, mutationErr(ErrDuplicateName, "the target directory %s already exists", target)
	}

	// Build in a hidden staging sibling (loader and watcher skip dot-dirs).
	stage, err := os.MkdirTemp(root, ".stage-*")
	if err != nil {
		return SkillSpec{}, r.ioFail(KindSkill, fmt.Errorf("stage: %w", err))
	}
	defer func() { _ = os.RemoveAll(stage) }() // no-op after successful rename
	if err := os.Chmod(stage, 0o700); err != nil {
		return SkillSpec{}, r.ioFail(KindSkill, err)
	}
	for _, f := range b.Files {
		dest := filepath.Join(stage, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
		if err := os.WriteFile(dest, f.Data, f.Mode); err != nil {
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
	}
	if b.Provenance != nil {
		if err := os.MkdirAll(filepath.Join(stage, ".karta"), 0o755); err != nil {
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
		raw, err := renderProvenance(b.Provenance, b.ContentHash(), time.Now())
		if err != nil {
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
		if err := os.WriteFile(filepath.Join(stage, provenancePath), raw, 0o644); err != nil {
			return SkillSpec{}, r.ioFail(KindSkill, err)
		}
	}

	if err := renameDirNoClobber(stage, target); err != nil {
		if os.IsExist(err) {
			return SkillSpec{}, mutationErr(ErrDuplicateName, "the target directory %s already exists", target)
		}
		return SkillSpec{}, r.ioFail(KindSkill, err)
	}
	newSnap := r.reloadKindLocked(KindSkill)
	installed, ok := newSnap.Skills[b.Name]
	if !ok {
		return SkillSpec{}, mutationErr(ErrIO, "skill installed but did not load (see registry errors)")
	}
	return installed, nil
}
