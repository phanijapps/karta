package skillhub

// Archive import + the capability store (design D2/D8): .skill/.zip
// uploads stage server-side as opaque one-shot bearer capabilities;
// repo previews hold digest-bound capabilities; expiry is synchronous,
// cleanup happens on every transition, and daemon startup reclaims
// abandoned stages.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"zbot/backend/internal/registry"
)

// ErrStageBusy is the aggregate staging bound.
var ErrStageBusy = fmt.Errorf("staging busy")

// ArchivePreview is the staged result of a .skill/.zip upload. After
// commit, Files carries the staged bundle (read back from disk before
// the stage is removed — staging durability is proven, not assumed).
type ArchivePreview struct {
	Skill  PreviewSkill
	staged string // staging dir while staged; empty after commit
	files  []registry.SkillFile
}

// Store stages skill-import archives under root (the caller passes
// ~/.zbot/tmp/skill-import) and issues one-shot repo capabilities.
type Store struct {
	mu      chanMutex
	repo    map[string]*repoCap
	archive map[string]*archiveCap
	root    string
}

type chanMutex struct{ mu *sync.Mutex }

func (m chanMutex) Lock()   { m.mu.Lock() }
func (m chanMutex) Unlock() { m.mu.Unlock() }

type repoCap struct {
	source, commit string
	skills         map[string]map[string]FileDigest // skill name → digests
	expiry         time.Time                        // binding TTL (long-lived metadata)
	used           bool
}

type archiveCap struct {
	dir    string
	skill  PreviewSkill
	files  []registry.SkillFile
	expiry time.Time
	used   bool
}

// NewStore creates the store and RECLAIMS abandoned staging
// directories from previous runs (startup cleanup).
func NewStore(stagingRoot string) (*Store, error) {
	s := &Store{
		mu:      chanMutex{mu: &sync.Mutex{}},
		repo:    map[string]*repoCap{},
		archive: map[string]*archiveCap{},
		root:    stagingRoot,
	}
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stagingRoot)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				_ = os.RemoveAll(filepath.Join(stagingRoot, e.Name()))
			}
		}
	}
	return s, nil
}

// RegisterRepoPreview records a repo preview capability.
func (s *Store) RegisterRepoPreview(source, commit string, skills map[string]map[string]FileDigest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictRepoLocked()
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	s.repo[id] = &repoCap{source: source, commit: commit, skills: skills, expiry: time.Now().Add(24 * time.Hour)}
	return id, nil
}

// ConsumeRepo atomically consumes a repository capability (one-shot):
// the binding (source, pinned commit, digests) outlives the preview
// cache so expired-preview installs re-fetch the pinned commit and
// verify digests; unknown, expired, or replayed ids fail.
func (s *Store) ConsumeRepo(id string) (source, commit string, skills map[string]map[string]FileDigest, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, present := s.repo[id]
	if !present || c.used || time.Now().After(c.expiry) {
		if present {
			delete(s.repo, id)
		}
		return "", "", nil, false
	}
	c.used = true
	return c.source, c.commit, c.skills, true
}

// StageArchive validates and stages an uploaded .skill/.zip archive.
func (s *Store) StageArchive(data []byte) (*ArchivePreview, string, error) {
	files, skill, err := parseZipArchive(data)
	if err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Lazy TTL sweep: abandoned expired stages clean up here, not only at
	// commit/eviction/restart.
	now := time.Now()
	for id, c := range s.archive {
		if c.used || now.After(c.expiry) {
			_ = os.RemoveAll(c.dir)
			delete(s.archive, id)
		}
	}
	if len(s.archive) >= MaxArchiveStages {
		return nil, "", ErrStageBusy // observable bound: no live eviction
	}
	dirToken, err := randomToken() // staging dir name: independent random token
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(s.root, dirToken)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	for _, f := range files {
		dest := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			_ = os.RemoveAll(dir)
			return nil, "", err
		}
		if err := os.WriteFile(dest, f.Data, f.Mode); err != nil {
			_ = os.RemoveAll(dir)
			return nil, "", err
		}
	}
	id, err := randomToken()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, "", err
	}
	cap0 := &archiveCap{dir: dir, skill: skill, files: files, expiry: time.Now().Add(CapabilityTTL)}
	s.archive[id] = cap0
	ap := &ArchivePreview{Skill: skill, staged: dir}
	ap.files = ap.readStaged() // staged preview carries the verified bundle too
	return ap, id, nil
}

// CommitArchive consumes a staging capability ONE-SHOT: expired,
// unknown, or already-consumed ids fail; success removes the stage.
func (s *Store) CommitArchive(id string) (*ArchivePreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, present := s.archive[id]
	if !present || c.used || time.Now().After(c.expiry) {
		if present {
			_ = os.RemoveAll(c.dir)
			delete(s.archive, id)
		}
		return nil, os.ErrNotExist
	}
	c.used = true
	ap := &ArchivePreview{Skill: c.skill, staged: c.dir}
	ap.files = ap.readStaged() // read back BEFORE removing the stage
	_ = os.RemoveAll(c.dir)
	delete(s.archive, id)
	return ap, nil
}

// StagedFiles returns the staged bundle.
func (a *ArchivePreview) StagedFiles() []registry.SkillFile {
	if a == nil {
		return nil
	}
	return a.files
}

// readStaged reads the bundle back from the staging dir, verifying the
// digests recorded at parse time.
func (a *ArchivePreview) readStaged() []registry.SkillFile {
	if a.staged == "" || len(a.Skill.Files) == 0 {
		return nil
	}
	var out []registry.SkillFile
	for _, dig := range a.Skill.Files {
		raw, err := os.ReadFile(filepath.Join(a.staged, filepath.FromSlash(dig.Path)))
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(raw)
		if shortHex(sum[:]) != dig.Sum {
			return nil
		}
		mode := fs.FileMode(0o644)
		if dig.Exec {
			mode = 0o755
		}
		out = append(out, registry.SkillFile{Path: dig.Path, Mode: mode, Data: raw})
	}
	return out
}

// CompareDigests verifies extracted per-skill digests against the
// capability's recorded ones — the install-time half of byte binding.
func CompareDigests(expected, extracted map[string]map[string]FileDigest) error {
	for skill, digs := range expected {
		got, ok := extracted[skill]
		if !ok {
			return fmt.Errorf("%w: skill %q vanished upstream", ErrChangedUpstream, skill)
		}
		if len(got) != len(digs) {
			return fmt.Errorf("%w: skill %q file set changed", ErrChangedUpstream, skill)
		}
		for path, d := range digs {
			g, ok := got[path]
			if !ok || g.Sum != d.Sum || g.Size != d.Size {
				return fmt.Errorf("%w: %s/%s digest mismatch", ErrChangedUpstream, skill, path)
			}
			if g.Exec != d.Exec {
				return fmt.Errorf("%w: %s/%s executable mode changed", ErrChangedUpstream, skill, path)
			}
		}
	}
	return nil
}

func (s *Store) evictRepoLocked() {
	if len(s.repo) < MaxRepoPreviews {
		return
	}
	for id, c := range s.repo {
		if time.Now().After(c.expiry) {
			delete(s.repo, id)
		}
	}
	for id := range s.repo { // still full: drop arbitrary quarter
		delete(s.repo, id)
		if len(s.repo) <= MaxRepoPreviews*3/4 {
			break
		}
	}
}

// parseZipArchive applies the SafeBundle envelope to an uploaded zip:
// header-level caps, streaming re-enforcement (headers lie), regular
// files only, shared path validation, SKILL.md at root or exactly one
// top-level directory, and the reserved provenance path rejection.
func parseZipArchive(data []byte) ([]registry.SkillFile, PreviewSkill, error) {
	if int64(len(data)) > 10<<20 {
		return nil, PreviewSkill{}, fmt.Errorf("%w: upload over 10 MiB", ErrArchiveViolation)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, PreviewSkill{}, fmt.Errorf("%w: corrupt zip: %w", ErrArchiveViolation, err)
	}
	if len(zr.File) > MaxEntries {
		return nil, PreviewSkill{}, fmt.Errorf("%w: over %d entries", ErrArchiveViolation, MaxEntries)
	}
	headerTotal := int64(0)
	for _, f := range zr.File {
		if err := rawEntryName(f.Name); err != nil {
			return nil, PreviewSkill{}, fmt.Errorf("%w: %w", ErrArchiveViolation, err)
		}
		if f.UncompressedSize64 > uint64(MaxFileBytes) {
			return nil, PreviewSkill{}, fmt.Errorf("%w: file %q over %d bytes", ErrArchiveViolation, f.Name, MaxFileBytes)
		}
		headerTotal += int64(f.UncompressedSize64)
		if headerTotal > MaxUncompressedByte {
			return nil, PreviewSkill{}, fmt.Errorf("%w: archive over %d uncompressed bytes", ErrArchiveViolation, MaxUncompressedByte)
		}
	}
	files := map[string]registry.SkillFile{}
	symlinks := 0
	total := int64(0)
	for _, f := range zr.File {
		mode := f.Mode()
		if mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			symlinks++
			continue
		}
		name := f.Name
		if err := registry.ValidBundlePath(name); err != nil {
			return nil, PreviewSkill{}, fmt.Errorf("%w: %w", ErrArchiveViolation, err)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, PreviewSkill{}, fmt.Errorf("%w: open %q: %w", ErrArchiveViolation, name, err)
		}
		raw, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, PreviewSkill{}, fmt.Errorf("%w: read %q: %w", ErrArchiveViolation, name, err)
		}
		if int64(len(raw)) > MaxFileBytes {
			return nil, PreviewSkill{}, fmt.Errorf("%w: file %q exceeded its stated size", ErrArchiveViolation, name)
		}
		total += int64(len(raw))
		if total > MaxUncompressedByte {
			return nil, PreviewSkill{}, fmt.Errorf("%w: archive over %d uncompressed bytes while extracting", ErrArchiveViolation, MaxUncompressedByte)
		}
		fm := fs.FileMode(0o644)
		if mode.Perm()&0o111 != 0 {
			fm = 0o755
		}
		if _, dup := files[name]; dup {
			return nil, PreviewSkill{}, fmt.Errorf("%w: duplicate entry %q", ErrArchiveViolation, name)
		}
		files[name] = registry.SkillFile{Path: name, Mode: fm, Data: raw}
	}
	if symlinks > 0 {
		return nil, PreviewSkill{}, fmt.Errorf("%w: %d non-regular (symlink/special) entries rejected", ErrArchiveViolation, symlinks)
	}

	// SKILL.md at the root, or exactly one top-level directory holding it.
	prefix := ""
	if _, ok := files["SKILL.md"]; !ok {
		roots := map[string]bool{}
		for name := range files {
			if strings.HasSuffix(name, "/SKILL.md") {
				roots[strings.TrimSuffix(strings.TrimSuffix(name, "/SKILL.md"), "/")] = true
			}
		}
		if len(roots) != 1 {
			return nil, PreviewSkill{}, fmt.Errorf("%w: no SKILL.md at root or in exactly one top-level directory", ErrArchiveViolation)
		}
		for r := range roots {
			if strings.Contains(r, "/") {
				return nil, PreviewSkill{}, fmt.Errorf("%w: SKILL.md deeper than one top-level directory", ErrArchiveViolation)
			}
			prefix = r + "/"
		}
		// Everything in the archive must live under the chosen wrapper.
		for n := range files {
			if !strings.HasPrefix(n, prefix) {
				return nil, PreviewSkill{}, fmt.Errorf("%w: entry %q is outside the skill directory", ErrArchiveViolation, n)
			}
		}
	}
	skillRaw, ok := files[prefix+"SKILL.md"]
	if !ok {
		return nil, PreviewSkill{}, fmt.Errorf("%w: SKILL.md missing", ErrArchiveViolation)
	}
	name, desc, body, err := registry.ParseSkillMD(string(skillRaw.Data))
	if err != nil {
		return nil, PreviewSkill{}, fmt.Errorf("%w: %w", ErrArchiveViolation, err)
	}
	// ONE canonical skill-relative file list: staging, digests, and the
	// preview all use stripped paths.
	skill := PreviewSkill{Name: name, Description: desc, Body: body, Root: strings.TrimSuffix(prefix, "/")}
	var rels []string
	for n := range files {
		rels = append(rels, n)
	}
	sortStrings(rels)
	out := make([]registry.SkillFile, 0, len(rels))
	for _, n := range rels {
		f := files[n]
		rel := strings.TrimPrefix(n, prefix)
		if rel == ".karta/provenance.yaml" || strings.HasPrefix(rel, ".karta/provenance.yaml/") {
			return nil, PreviewSkill{}, fmt.Errorf("%w: reserved provenance path", ErrArchiveViolation)
		}
		sum := sha256.Sum256(f.Data)
		out = append(out, registry.SkillFile{Path: rel, Mode: f.Mode, Data: f.Data})
		skill.Files = append(skill.Files, FileDigest{Path: rel, Size: int64(len(f.Data)), Sum: shortHex(sum[:]), Exec: f.Mode == 0o755})
	}
	return out, skill, nil
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// ContextDeadline aliases the fetch deadline for callers.
func ContextDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, FetchDeadline)
}
