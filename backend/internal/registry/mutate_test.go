package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// contextWithCancel builds a cancellable context cleaned up with the test.
func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx, cancel
}

// waitCond polls cond (over the watcher's waitFor, which needs an explicit
// timeout argument) with a generous settle window.
func waitCond(t *testing.T, cond func() bool) {
	t.Helper()
	waitFor(t, 5*time.Second, cond)
}

// --- helpers -------------------------------------------------------------

func mcode(t *testing.T, err error) MutationErrorCode {
	t.Helper()
	var me *MutationError
	if !errors.As(err, &me) {
		t.Fatalf("want *MutationError, got %T: %v", err, err)
	}
	return me.Code
}

func wantCode(t *testing.T, err error, code MutationErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %s, got nil", code)
	}
	if got := mcode(t, err); got != code {
		t.Fatalf("want error %s, got %s (%v)", code, got, err)
	}
}

func loadedReg(t *testing.T) (*Registry, Dirs) {
	t.Helper()
	d := tempDirs(t)
	r := New()
	if _, errs := r.LoadAll(d); len(errs) > 0 {
		t.Fatal(errs)
	}
	return r, d
}

// --- MCP mutations ---------------------------------------------------------

func TestMCPCreatePublishesSynchronously(t *testing.T) {
	r, d := loadedReg(t)
	var rec recorder
	r.Subscribe(rec.record)

	spec, err := r.CreateMCP("github", MCPSpec{Command: "gh-mcp", Args: []string{"serve"}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != filepath.Join(d.MCPs, "github.yaml") {
		t.Fatalf("path wrong: %s", spec.Path)
	}
	// Immediate visibility: no reload, no restart.
	if s := r.Snapshot(); s.MCPs["github"].Command != "gh-mcp" {
		t.Fatalf("not in snapshot: %+v", s.MCPs)
	}
	if rec.count(KindMCP, "github", ChangeAdded) != 1 {
		t.Fatalf("want one added notification, got %v", rec.changes)
	}
	// The written file is exactly what the loader reads (strict decode).
	raw, _ := os.ReadFile(spec.Path)
	if !strings.Contains(string(raw), "command: gh-mcp") {
		t.Fatalf("manifest wrong: %s", raw)
	}
}

func TestMCPCreateValidation(t *testing.T) {
	r, _ := loadedReg(t)
	wantCode(t, mcpCreate(r, "Bad_Name", MCPSpec{Command: "x"}), ErrInvalidName)
	wantCode(t, mcpCreate(r, "con", MCPSpec{Command: "x"}), ErrInvalidName)
	wantCode(t, mcpCreate(r, "github", MCPSpec{Args: []string{"x"}}), ErrInvalidManifest) // no transport
	// Dual-transport create is valid.
	if _, err := r.CreateMCP("dual", MCPSpec{Command: "x", URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("dual-transport create: %v", err)
	}
	// Duplicate name.
	wantCode(t, mcpCreate(r, "dual", MCPSpec{Command: "x"}), ErrDuplicateName)
}

func TestMCPCreateOccupiedPathConflicts(t *testing.T) {
	r, d := loadedReg(t)
	// An INVALID manifest occupying the target stem: absent from the
	// snapshot, invisible to name checks — the no-clobber install must
	// still refuse.
	write(t, filepath.Join(d.MCPs, "zombie.yaml"), "args:\n    - nope\n")
	r.LoadAll(d) // zombie does not load
	if _, ok := r.Snapshot().MCPs["zombie"]; ok {
		t.Fatal("zombie should not load")
	}
	wantCode(t, mcpCreate(r, "zombie", MCPSpec{Command: "x"}), ErrDuplicateName)
	// The occupant is untouched.
	if raw, _ := os.ReadFile(filepath.Join(d.MCPs, "zombie.yaml")); !strings.Contains(string(raw), "nope") {
		t.Fatalf("occupant clobbered: %s", raw)
	}
}

func TestMCPCapacity(t *testing.T) {
	r, d := loadedReg(t)
	for i := 0; i < mcpCapacity; i++ {
		write(t, filepath.Join(d.MCPs, fmt.Sprintf("srv%03d.yaml", i)), "command: x\n")
	}
	r.LoadAll(d)
	if len(r.Snapshot().MCPs) != mcpCapacity {
		t.Fatalf("want %d, got %d", mcpCapacity, len(r.Snapshot().MCPs))
	}
	wantCode(t, mcpCreate(r, "overflow", MCPSpec{Command: "x"}), ErrCapacity)
	// Hand-edited overflow at load is a per-file error, not truncation.
	write(t, filepath.Join(d.MCPs, "zz-last.yaml"), "command: x\n")
	_, errs := r.LoadAll(d)
	if len(r.Snapshot().MCPs) != mcpCapacity || !errMentions(errs, "zz-last.yaml") {
		t.Fatalf("overflow semantics wrong: n=%d errs=%v", len(r.Snapshot().MCPs), errs)
	}
	if !errMentions(r.Errors(), "zz-last.yaml") {
		t.Fatal("overflow error not queryable via Errors()")
	}
}

func TestMCPUpdateInPlace(t *testing.T) {
	r, _ := loadedReg(t)
	if _, err := r.CreateMCP("srv", MCPSpec{Command: "old"}); err != nil {
		t.Fatal(err)
	}
	spec, err := r.UpdateMCP("srv", "", MCPSpec{Command: "new", URL: "http://127.0.0.1:2"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "new" || spec.URL == "" {
		t.Fatalf("update wrong: %+v", spec)
	}
	// Full replace: clearing both transports is invalid.
	wantCode(t, mcpUpdate(r, "srv", "", MCPSpec{Env: map[string]string{"A": "b"}}), ErrInvalidManifest)
	wantCode(t, mcpUpdate(r, "missing", "", MCPSpec{Command: "x"}), ErrNotFound)
}

func TestMCPRename(t *testing.T) {
	r, d := loadedReg(t)
	var rec recorder
	r.Subscribe(rec.record)
	if _, err := r.CreateMCP("srv", MCPSpec{Command: "keep", Args: []string{"-v"}}); err != nil {
		t.Fatal(err)
	}
	spec, err := r.UpdateMCP("srv", "renamed", MCPSpec{Command: "keep", Args: []string{"-v"}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != filepath.Join(d.MCPs, "renamed.yaml") {
		t.Fatalf("rename path wrong: %s", spec.Path)
	}
	s := r.Snapshot()
	if _, old := s.MCPs["srv"]; old {
		t.Fatal("old name still listed")
	}
	if s.MCPs["renamed"].Command != "keep" || len(s.MCPs["renamed"].Args) != 1 {
		t.Fatalf("config not preserved: %+v", s.MCPs["renamed"])
	}
	if _, err := os.Stat(filepath.Join(d.MCPs, "srv.yaml")); !os.IsNotExist(err) {
		t.Fatal("old file still on disk")
	}
	if rec.count(KindMCP, "srv", ChangeRemoved) != 1 || rec.count(KindMCP, "renamed", ChangeAdded) != 1 {
		t.Fatalf("rename notifications wrong: %v", rec.changes)
	}
	// Rename onto an existing name and onto an occupied path. The
	// occupant's manifest differs from the rename payload, so it is an
	// unrelated entry (a byte-identical target would be our own prior
	// install and resume instead — see the removal-failure retry test).
	if _, err := r.CreateMCP("other", MCPSpec{Command: "distinct"}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, mcpUpdate(r, "renamed", "other", MCPSpec{Command: "x"}), ErrDuplicateName)
	write(t, filepath.Join(d.MCPs, "ghost.yaml"), "args:\n    - broken\n")
	wantCode(t, mcpUpdate(r, "renamed", "ghost", MCPSpec{Command: "x"}), ErrDuplicateName)
}

func TestMCPRenameRemovalFailurePublishesDiskTruth(t *testing.T) {
	r, d := loadedReg(t)
	if _, err := r.CreateMCP("srv", MCPSpec{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	// Replace the old manifest's file with a non-empty directory:
	// os.Remove fails on it, exercising the removal-failure boundary.
	old := filepath.Join(d.MCPs, "srv.yaml")
	_ = os.Remove(old)
	write(t, filepath.Join(old, "child"), "x")
	_, err := r.UpdateMCP("srv", "moved", MCPSpec{Command: "x"})
	wantCode(t, err, ErrIO)
	// Disk truth: the new file loads; the un-removable old path (now a
	// directory) stops loading. The published snapshot equals a fresh
	// full load — never an in-memory expectation.
	if _, ok := r.Snapshot().MCPs["moved"]; !ok {
		t.Fatal("moved entry missing after removal failure")
	}
	fresh, _ := r.LoadAll(d)
	if len(fresh.MCPs) != len(r.Snapshot().MCPs) {
		t.Fatalf("snapshot diverged from disk: %v vs %v", r.Snapshot().MCPs, fresh.MCPs)
	}
	// The retry converges (the old name no longer loads, so the rename
	// is complete from the registry's view; the husk stays on disk).
	if _, err := r.UpdateMCP("srv", "moved", MCPSpec{Command: "x"}); mcode(t, err) != ErrNotFound {
		t.Fatalf("retry should report the already-renamed state: %v", err)
	}
	if s := r.Snapshot(); len(s.MCPs) != 1 || s.MCPs["moved"].Command != "x" {
		t.Fatalf("final state wrong: %+v", s.MCPs)
	}
	_ = os.RemoveAll(old)
}

func TestMCPDelete(t *testing.T) {
	r, d := loadedReg(t)
	var rec recorder
	r.Subscribe(rec.record)
	if _, err := r.CreateMCP("srv", MCPSpec{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteMCP("srv"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Snapshot().MCPs["srv"]; ok {
		t.Fatal("still listed after delete")
	}
	if _, err := os.Stat(filepath.Join(d.MCPs, "srv.yaml")); !os.IsNotExist(err) {
		t.Fatal("file still on disk")
	}
	if rec.count(KindMCP, "srv", ChangeRemoved) != 1 {
		t.Fatalf("delete notification wrong: %v", rec.changes)
	}
	wantCode(t, r.DeleteMCP("srv"), ErrNotFound)
}

func TestMCPLegacyStemManageableInPlace(t *testing.T) {
	r, d := loadedReg(t)
	legacy := filepath.Join(d.MCPs, "Old-Server.yml")
	write(t, legacy, "command: old\n")
	r.LoadAll(d)
	// In-place edit rewrites the very same .yml file.
	if _, err := r.UpdateMCP("Old-Server", "", MCPSpec{Command: "new"}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(legacy); !strings.Contains(string(raw), "command: new") {
		t.Fatalf("legacy file not edited in place: %s", raw)
	}
	if r.Snapshot().MCPs["Old-Server"].Path != legacy {
		t.Fatal("path should stay the legacy file")
	}
	// Rename normalizes to a managed-name .yaml manifest.
	if _, err := r.UpdateMCP("Old-Server", "modern", MCPSpec{Command: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy file should be gone after rename")
	}
	if r.Snapshot().MCPs["modern"].Path != filepath.Join(d.MCPs, "modern.yaml") {
		t.Fatal("rename did not normalize")
	}
}

// --- Skill mutations -------------------------------------------------------

func TestSkillCreateAndSlug(t *testing.T) {
	r, d := loadedReg(t)
	var rec recorder
	r.Subscribe(rec.record)
	spec, err := r.CreateSkill("Project Planning!", "Break work into steps.", "# Planning\n")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != filepath.Join(d.Skills, "project-planning") {
		t.Fatalf("slug dir wrong: %s", spec.Path)
	}
	if s := r.Snapshot().Skills["Project Planning!"]; s.Description != "Break work into steps." {
		t.Fatalf("not listed: %+v", s)
	}
	if rec.count(KindSkill, "Project Planning!", ChangeAdded) != 1 {
		t.Fatalf("notifications: %v", rec.changes)
	}
	// Invalid names/slugs and empty descriptions.
	wantCode(t, skillCreateErr(r, "!!!", "d", ""), ErrInvalidName)
	wantCode(t, skillCreateErr(r, "ok", "  ", ""), ErrInvalidManifest)
	wantCode(t, skillCreateErr(r, "", "d", ""), ErrInvalidManifest)
}

func TestSkillCreateOccupiedDirConflicts(t *testing.T) {
	r, d := loadedReg(t)
	// Assets-only dir at the target slug: absent from the snapshot, but
	// the no-clobber Mkdir must refuse.
	write(t, filepath.Join(d.Skills, "planner", "notes.txt"), "x")
	r.LoadAll(d)
	wantCode(t, skillCreateErr(r, "planner", "d", ""), ErrDuplicateName)
	// Duplicate name across a different directory.
	if _, err := r.CreateSkill("planner two", "d", ""); err != nil {
		t.Fatal(err)
	}
	wantCode(t, skillCreateErr(r, "planner two", "other", ""), ErrDuplicateName)
}

func TestSkillEditPreservesBundleAndBody(t *testing.T) {
	r, _ := loadedReg(t)
	if _, err := r.CreateSkill("keeper", "one", "body-one"); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(r.Snapshot().Skills["keeper"].Path, "refs.md")
	write(t, bundle, "bundle")
	// Full replace (body is required): an edit re-sending the current
	// body changes only the description.
	if _, err := r.UpdateSkill("keeper", "", "two", "body-one"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(bundle); string(raw) != "bundle" {
		t.Fatal("bundle not preserved")
	}
	_, body, err := r.SkillDetail("keeper")
	if err != nil || body != "body-one" {
		t.Fatalf("body not round-tripped: %q %v", body, err)
	}
	// Full replace with a new body.
	if _, err := r.UpdateSkill("keeper", "", "three", "body-two"); err != nil {
		t.Fatal(err)
	}
	_, body, _ = r.SkillDetail("keeper")
	if body != "body-two" {
		t.Fatalf("body not replaced: %q", body)
	}
	// Description round-trips through the loader.
	if s := r.Snapshot().Skills["keeper"]; s.Description != "three" {
		t.Fatalf("description wrong: %+v", s)
	}
}

func TestSkillFrontmatterPathIsIgnored(t *testing.T) {
	r, d := loadedReg(t)
	outside := filepath.Join(filepath.Dir(d.Skills), "outside")
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	// Hand-authored skill whose frontmatter carries a hostile path.
	write(t, filepath.Join(d.Skills, "hostile", "SKILL.md"),
		fmt.Sprintf("---\nname: hostile\ndescription: d\npath: %s\n---\nbody\n", outside))
	r.LoadAll(d)
	spec, ok := r.Snapshot().Skills["hostile"]
	if !ok {
		t.Fatal("hostile skill did not load")
	}
	if spec.Path != filepath.Join(d.Skills, "hostile") {
		t.Fatalf("authored path trusted: %s", spec.Path)
	}
	// Operations target the real directory, never the authored path.
	if _, err := r.UpdateSkill("hostile", "", "new-desc", "body"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("authored path materialized")
	}
	if err := r.DeleteSkill("hostile"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Skills, "hostile")); !os.IsNotExist(err) {
		t.Fatal("real dir not removed")
	}
}

func TestSkillRenameMovesDirectoryAndBundle(t *testing.T) {
	r, d := loadedReg(t)
	var rec recorder
	r.Subscribe(rec.record)
	if _, err := r.CreateSkill("old name", "d", "b"); err != nil {
		t.Fatal(err)
	}
	oldDir := r.Snapshot().Skills["old name"].Path
	write(t, filepath.Join(oldDir, "bundle.txt"), "b")
	spec, err := r.UpdateSkill("old name", "New Name", "d2", "b")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != filepath.Join(d.Skills, "new-name") {
		t.Fatalf("rename dir wrong: %s", spec.Path)
	}
	if _, err := os.Stat(filepath.Join(spec.Path, "bundle.txt")); err != nil {
		t.Fatal("bundle did not travel")
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("old dir still present")
	}
	if _, ok := r.Snapshot().Skills["New Name"]; !ok {
		t.Fatal("renamed skill not listed")
	}
	if rec.count(KindSkill, "old name", ChangeRemoved) != 1 || rec.count(KindSkill, "New Name", ChangeAdded) != 1 {
		t.Fatalf("rename notifications: %v", rec.changes)
	}
	// Conflicts: existing name, existing dir.
	if _, err := r.CreateSkill("third", "d", "b"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, skillUpdateErr(r, "third", "New Name", "d", "b"), ErrDuplicateName)
	write(t, filepath.Join(d.Skills, "occupied", "x.txt"), "x")
	wantCode(t, skillUpdateErr(r, "third", "occupied", "d", "b"), ErrDuplicateName)
}

func TestSkillRenameRollsBackOnWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based injection does not work as root")
	}
	r, _ := loadedReg(t)
	if _, err := r.CreateSkill("mover", "d", "b"); err != nil {
		t.Fatal(err)
	}
	dir := r.Snapshot().Skills["mover"].Path
	// The directory is read-only: the (parent-driven) atomic dir move
	// succeeds, the temp-file commit inside it fails, and the rename
	// must roll back with the ORIGINAL SKILL.md intact and loadable.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err := r.UpdateSkill("mover", "mover two", "d", "new-body")
	wantCode(t, err, ErrIO)
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("rollback failed, old dir gone: %v", statErr)
	}
	if _, ok := r.Snapshot().Skills["mover"]; !ok {
		t.Fatal("skill lost after failed rename")
	}
}

func TestSkillInPlaceEditFailureKeepsOriginalLoadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based injection does not work as root")
	}
	r, _ := loadedReg(t)
	if _, err := r.CreateSkill("fragile", "d", "original-body"); err != nil {
		t.Fatal(err)
	}
	dir := r.Snapshot().Skills["fragile"].Path
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err := r.UpdateSkill("fragile", "", "d2", "replacement")
	wantCode(t, err, ErrIO)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The original file survived the failed atomic replace verbatim.
	_, body, err := r.SkillDetail("fragile")
	if err != nil || body != "original-body" {
		t.Fatalf("original not intact after failed edit: %q %v", body, err)
	}
	if s := r.Snapshot().Skills["fragile"]; s.Description != "d" {
		t.Fatalf("description changed despite failed edit: %+v", s)
	}
}

func TestSkillDeleteRemovesBundle(t *testing.T) {
	r, _ := loadedReg(t)
	var rec recorder
	r.Subscribe(rec.record)
	if _, err := r.CreateSkill("gone", "d", "b"); err != nil {
		t.Fatal(err)
	}
	dir := r.Snapshot().Skills["gone"].Path
	write(t, filepath.Join(dir, "deep", "asset.bin"), "x")
	if err := r.DeleteSkill("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("bundle not removed")
	}
	if _, ok := r.Snapshot().Skills["gone"]; ok {
		t.Fatal("still listed")
	}
	if rec.count(KindSkill, "gone", ChangeRemoved) != 1 {
		t.Fatalf("delete notification: %v", rec.changes)
	}
	wantCode(t, r.DeleteSkill("gone"), ErrNotFound)
}

func TestSkillHandAuthoredNonSlugDirEditableInPlace(t *testing.T) {
	r, d := loadedReg(t)
	weird := filepath.Join(d.Skills, "Not_A_Slug")
	write(t, filepath.Join(weird, "SKILL.md"), "---\nname: weird\ndescription: d\n---\nb\n")
	r.LoadAll(d)
	if _, err := r.UpdateSkill("weird", "", "d2", "b"); err != nil {
		t.Fatal(err)
	}
	if r.Snapshot().Skills["weird"].Path != weird {
		t.Fatal("edit should not move a hand-authored dir")
	}
}

func TestSkillBodyCapIsBytes(t *testing.T) {
	r, _ := loadedReg(t)
	// "€" is 3 bytes in UTF-8: 87382 runes = 262146 bytes = cap+2.
	body := strings.Repeat("€", skillBodyLimit/3+1)
	if len(body) <= skillBodyLimit {
		t.Fatalf("test body not over the byte cap: %d", len(body))
	}
	wantCode(t, skillCreateErr(r, "big", "d", body), ErrInvalidManifest)
	// Exactly at the cap passes (multibyte boundary).
	if _, err := r.CreateSkill("edge", "d", strings.Repeat("é", skillBodyLimit/3)); err != nil {
		t.Fatalf("body at byte cap rejected: %v", err)
	}
}

func TestSkillCapacity(t *testing.T) {
	r, d := loadedReg(t)
	for i := 0; i < skillCapacity; i++ {
		write(t, filepath.Join(d.Skills, fmt.Sprintf("s%03d", i), "SKILL.md"),
			fmt.Sprintf("---\nname: s%03d\ndescription: d\n---\n", i))
	}
	r.LoadAll(d)
	wantCode(t, skillCreateErr(r, "overflow", "d", ""), ErrCapacity)
}

func TestMCPRenameRemovalFailureRetryResumes(t *testing.T) {
	r, d := loadedReg(t)
	// Hand-craft the removal-failure residual: both the old and the new
	// manifest exist and are valid (exactly what a failed os.Remove of
	// the old file leaves behind), and both load.
	spec := MCPSpec{Command: "resume-me"}
	raw, _ := marshalMCP(spec)
	write(t, filepath.Join(d.MCPs, "srv.yaml"), string(raw))
	write(t, filepath.Join(d.MCPs, "moved.yaml"), string(raw))
	r.LoadAll(d)
	if len(r.Snapshot().MCPs) != 2 {
		t.Fatal("setup: both manifests should load")
	}
	// Retrying the SAME rename recognizes its own byte-identical target
	// and completes: install skipped, old file removed, one entry left.
	updated, err := r.UpdateMCP("srv", "moved", spec)
	if err != nil {
		t.Fatalf("resume retry: %v", err)
	}
	if updated.Command != "resume-me" || r.Snapshot().MCPs["moved"].Path != filepath.Join(d.MCPs, "moved.yaml") {
		t.Fatalf("resume result wrong: %+v", updated)
	}
	if len(r.Snapshot().MCPs) != 1 {
		t.Fatalf("old entry still listed: %+v", r.Snapshot().MCPs)
	}
	if _, err := os.Stat(filepath.Join(d.MCPs, "srv.yaml")); !os.IsNotExist(err) {
		t.Fatal("old manifest still on disk")
	}
	// A DIFFERENT manifest at the target is an unrelated occupant, never
	// resumed over.
	write(t, filepath.Join(d.MCPs, "foreign.yaml"), "command: foreign\n")
	r.LoadAll(d)
	wantCode(t, mcpUpdate(r, "moved", "foreign", MCPSpec{Command: "x"}), ErrDuplicateName)
}

func TestMCPFieldValidation(t *testing.T) {
	r, _ := loadedReg(t)
	wantCode(t, mcpCreate(r, "badurl", MCPSpec{URL: "x"}), ErrInvalidManifest)
	wantCode(t, mcpCreate(r, "badurl2", MCPSpec{URL: "ftp://example.com"}), ErrInvalidManifest)
	wantCode(t, mcpCreate(r, "debris", MCPSpec{URL: "http://127.0.0.1:1", Args: []string{"orphan"}}), ErrInvalidManifest)
	wantCode(t, mcpCreate(r, "debris2", MCPSpec{URL: "http://127.0.0.1:1", Env: map[string]string{"A": "b"}}), ErrInvalidManifest)
	long := strings.Repeat("a", 1025)
	wantCode(t, mcpCreate(r, "longcmd", MCPSpec{Command: long}), ErrInvalidManifest)
	wantCode(t, mcpCreate(r, "longarg", MCPSpec{Command: "x", Args: []string{long}}), ErrInvalidManifest)
	wantCode(t, mcpCreate(r, "longenv", MCPSpec{Command: "x", Env: map[string]string{"K": strings.Repeat("v", 4097)}}), ErrInvalidManifest)
	// Valid bounds pass (url with path, max-ish env value).
	if _, err := r.CreateMCP("okurl", MCPSpec{URL: "http://127.0.0.1:1/mcp?x=1"}); err != nil {
		t.Fatal(err)
	}
}

// --- watcher coexistence ---------------------------------------------------

// TestMutationsCoexistWithWatcher runs the watcher and proves: mutations
// notify correctly on their own, the post-mutation settle emits nothing,
// external edits still hot-reload, and an external edit racing a mutation
// never reverts it.
func TestMutationsCoexistWithWatcher(t *testing.T) {
	d := tempDirs(t)
	r := New()
	r.LoadAll(d)
	ctx, _ := contextWithCancel(t)
	w, err := r.Watch(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var rec recorder
	w.OnChange(rec.record)

	// Create: exactly one added notification, settle adds nothing.
	if _, err := r.CreateMCP("srv", MCPSpec{Command: "one"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * Debounce)
	if got := rec.count(KindMCP, "srv", ChangeAdded); got != 1 {
		t.Fatalf("want exactly 1 added (mutation-owned), got %d: %v", got, rec.changes)
	}
	if rec.count(KindMCP, "srv", ChangeChanged) != 0 {
		t.Fatalf("settle misclassified: %v", rec.changes)
	}
	if r.Snapshot().MCPs["srv"].Command != "one" {
		t.Fatal("mutation not visible")
	}

	// External edit lands through hot-reload.
	write(t, filepath.Join(d.MCPs, "srv.yaml"), "command: two\n")
	waitCond(t, func() bool {
		return r.Snapshot().MCPs["srv"].Command == "two"
	})
	if got := rec.count(KindMCP, "srv", ChangeChanged); got != 1 {
		t.Fatalf("external edit should notify changed once, got %d: %v", got, rec.changes)
	}

	// Edit and delete via the API with the watcher running.
	if _, err := r.UpdateMCP("srv", "", MCPSpec{Command: "three"}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteMCP("srv"); err != nil {
		t.Fatal(err)
	}
	waitCond(t, func() bool {
		_, ok := r.Snapshot().MCPs["srv"]
		return !ok
	})
	time.Sleep(4 * Debounce)
	if rec.count(KindMCP, "srv", ChangeChanged) != 2 { // external + api edit
		t.Fatalf("changed count wrong: %v", rec.changes)
	}
	if got := rec.count(KindMCP, "srv", ChangeRemoved); got != 1 {
		t.Fatalf("want exactly 1 removed, got %d: %v", got, rec.changes)
	}

	// External write racing a create at the same stem: no-clobber wins.
	write(t, filepath.Join(d.MCPs, "race.yaml"), "args:\n    - broken\n")
	wantCode(t, mcpCreate(r, "race", MCPSpec{Command: "x"}), ErrDuplicateName)
}

// TestMutationReloadNotifiesAbsorbedExternalEdits proves the whole-kind
// diff: an external edit landing inside the debounce window is published
// AND notified by the next mutation's reload, exactly once.
func TestMutationReloadNotifiesAbsorbedExternalEdits(t *testing.T) {
	d := tempDirs(t)
	r := New()
	r.LoadAll(d)
	var rec recorder
	r.Subscribe(rec.record)
	write(t, filepath.Join(d.MCPs, "mutated.yaml"), "command: old\n")
	r.LoadAll(d)
	// An external edit straight to disk AFTER the last load (no watcher
	// running — simulating an event the debounce has not flushed yet):
	// the next mutation's whole-kind reload absorbs and must notify it.
	write(t, filepath.Join(d.MCPs, "ext.yaml"), "command: ext\n")
	// The mutation on another entry reloads the whole kind and must
	// notify the absorbed external add plus its own change.
	if _, err := r.UpdateMCP("mutated", "", MCPSpec{Command: "new"}); err != nil {
		t.Fatal(err)
	}
	if rec.count(KindMCP, "ext", ChangeAdded) != 1 {
		t.Fatalf("absorbed external add not notified exactly once: %v", rec.changes)
	}
	if rec.count(KindMCP, "mutated", ChangeChanged) != 1 {
		t.Fatalf("mutation change not notified exactly once: %v", rec.changes)
	}
}

// TestLoadErrorWalkValidInvalidRepairedDeleted proves the error lifecycle:
// errors describe the CURRENT snapshot (swap despite errors), refreshed on
// watcher reloads, queryable at every step.
func TestLoadErrorWalkValidInvalidRepairedDeleted(t *testing.T) {
	d := tempDirs(t)
	r := New()
	r.LoadAll(d)
	ctx, _ := contextWithCancel(t)
	w, err := r.Watch(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Valid.
	write(t, filepath.Join(d.MCPs, "a.yaml"), "command: x\n")
	waitCond(t, func() bool { return len(r.Errors()) == 0 && len(r.Snapshot().MCPs) == 1 })
	// Invalid: entry disappears (swap despite errors), error names the file.
	write(t, filepath.Join(d.MCPs, "a.yaml"), "args:\n    - x\n")
	waitCond(t, func() bool {
		return len(r.Snapshot().MCPs) == 0 && errMentions(r.Errors(), "a.yaml")
	})
	// Repaired: back with no errors.
	write(t, filepath.Join(d.MCPs, "a.yaml"), "command: x\n")
	waitCond(t, func() bool {
		return len(r.Snapshot().MCPs) == 1 && len(r.Errors()) == 0
	})
	// Deleted: gone with no errors.
	_ = os.Remove(filepath.Join(d.MCPs, "a.yaml"))
	waitCond(t, func() bool {
		return len(r.Snapshot().MCPs) == 0 && len(r.Errors()) == 0
	})
}

// error-only wrappers so wantCode reads cleanly at call sites.
func mcpCreate(r *Registry, name string, spec MCPSpec) error {
	_, err := r.CreateMCP(name, spec)
	return err
}

func mcpUpdate(r *Registry, name, newName string, spec MCPSpec) error {
	_, err := r.UpdateMCP(name, newName, spec)
	return err
}

func skillCreateErr(r *Registry, name, desc, body string) error {
	_, err := r.CreateSkill(name, desc, body)
	return err
}

func skillUpdateErr(r *Registry, name, newName, desc, body string) error {
	_, err := r.UpdateSkill(name, newName, desc, body)
	return err
}

func TestSlugifyNormalizer(t *testing.T) {
	cases := map[string]string{
		"Filesystem (local)": "filesystem-local",
		"PDF  Tools!!":       "pdf-tools",
		"__init__":           "init",
		"Already-fine":       "already-fine",
		"UPPER":              "upper",
		"  spaced  out  ":    "spaced-out",
		"traversal/../evil":  "traversal-evil",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	// Unicode-only keys normalize to nothing usable.
	if got := Slugify("!!!"); got != "" {
		t.Fatalf("Slugify(%q) = %q, want empty", "!!!", got)
	}
}

func TestCreateMCPFromKeyDuplicateOutranksCapacity(t *testing.T) {
	r, d := loadedReg(t)
	for i := 0; i < mcpCapacity; i++ {
		write(t, filepath.Join(d.MCPs, fmt.Sprintf("srv%03d.yaml", i)), "command: x\n")
	}
	r.LoadAll(d)
	// A full registry: an EXISTING name skips as duplicate, a new one
	// fails capacity, an invisible occupant (invalid file) skips too.
	name, _, err := r.CreateMCPFromKey("srv000", MCPSpec{Command: "x"})
	wantCode(t, err, ErrDuplicateName)
	if name != "srv000" {
		t.Fatalf("stored name wrong: %q", name)
	}
	_, _, err = r.CreateMCPFromKey("fresh", MCPSpec{Command: "x"})
	wantCode(t, err, ErrCapacity)
	write(t, filepath.Join(d.MCPs, "ghost.yaml"), "args:\n  - x\n")
	_, _, err = r.CreateMCPFromKey("ghost", MCPSpec{Command: "x"})
	wantCode(t, err, ErrDuplicateName)
}

func TestCreateMCPFromKeySlugsAndSamePasteCollisions(t *testing.T) {
	r, _ := loadedReg(t)
	name, spec, err := r.CreateMCPFromKey("Filesystem (local)", MCPSpec{Command: "fs"})
	if err != nil || name != "filesystem-local" || spec.Command != "fs" {
		t.Fatalf("create from key: %q %+v %v", name, spec, err)
	}
	// A distinct key normalizing to the same slug skips as duplicate.
	_, _, err = r.CreateMCPFromKey("filesystem-local", MCPSpec{Command: "fs"})
	wantCode(t, err, ErrDuplicateName)
	// Unusable keys fail as invalid names and reserve nothing.
	_, _, err = r.CreateMCPFromKey("!!!", MCPSpec{Command: "x"})
	wantCode(t, err, ErrInvalidName)
	if _, ok := r.Snapshot().MCPs["!!!"]; ok {
		t.Fatal("failed entry reserved a name")
	}
}

func TestMCPEnablementRoundTrip(t *testing.T) {
	r, d := loadedReg(t)
	// Default create: enabled; file carries no enabled field.
	spec, err := r.CreateMCP("plain", MCPSpec{Command: "x"})
	if err != nil || spec.Disabled {
		t.Fatalf("default create should be enabled: %+v %v", spec, err)
	}
	raw, _ := os.ReadFile(filepath.Join(d.MCPs, "plain.yaml"))
	if strings.Contains(string(raw), "enabled") {
		t.Fatalf("enabled manifests stay clean: %s", raw)
	}
	// Disable via full-replace edit; persists and reloads.
	if _, err := r.UpdateMCP("plain", "", MCPSpec{Command: "x", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if !r.Snapshot().MCPs["plain"].Disabled {
		t.Fatal("not disabled after edit")
	}
	raw, _ = os.ReadFile(filepath.Join(d.MCPs, "plain.yaml"))
	if !strings.Contains(string(raw), "enabled: false") {
		t.Fatalf("manifest must persist disabled: %s", raw)
	}
	// Restart-persist: a fresh load keeps the state.
	fresh := New()
	snap, errs := fresh.LoadAll(d)
	if len(errs) > 0 || !snap.MCPs["plain"].Disabled {
		t.Fatalf("disabled state lost on reload: %+v %v", snap.MCPs, errs)
	}
	// Hand-authored explicit enabled: true stays enabled; imports ignore the field.
	write(t, filepath.Join(d.MCPs, "explicit.yaml"), "command: x\nenabled: true\n")
	snap, _ = fresh.LoadAll(d)
	if snap.MCPs["explicit"].Disabled {
		t.Fatal("explicit enabled must load enabled")
	}
	// Rename carries the state.
	if _, err := r.UpdateMCP("plain", "renamed", MCPSpec{Command: "x", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if !r.Snapshot().MCPs["renamed"].Disabled {
		t.Fatal("rename lost disabled state")
	}
}
