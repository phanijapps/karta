package skillhub

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zbot/backend/internal/registry"
)

// --- helpers ---------------------------------------------------------------

// tarGz builds a gzipped tarball with the archive-root prefix GitHub
// uses (<repo>-<ref>/); entries map rel path → content; exec lists
// executable paths; symlinks lists symlink entries to inject.
func tarGz(prefix string, entries map[string]string, exec map[string]bool, symlinks []string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sortStrings(names)
	for _, n := range names {
		content := entries[n]
		mode := int64(0o644)
		if exec[n] {
			mode = 0o755
		}
		_ = tw.WriteHeader(&tar.Header{Name: prefix + "/" + n, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(content))
	}
	for _, l := range symlinks {
		_ = tw.WriteHeader(&tar.Header{Name: prefix + "/" + l, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func zipBytes(t *testing.T, entries map[string][]byte, exec map[string]bool, symlinkNames []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sortStrings(names)
	for _, n := range names {
		hdr := &zip.FileHeader{Name: n, Method: zip.Deflate}
		if exec[n] {
			hdr.SetMode(0o755)
		} else {
			hdr.SetMode(0o644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entries[n]); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range symlinkNames {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		hdr.SetMode(os.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("/etc/passwd")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const sampleSkillMD = "---\nname: PDF Kit\ndescription: PDF tooling.\n---\n# PDF Kit\nBody text.\n"

// fakeGitHub serves repo metadata + pinned-commit tarballs.
func fakeGitHub(t *testing.T, tarball []byte, commit string, status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"default_branch":"main"}`)
	})
	mux.HandleFunc("/repos/o/r/commits/main", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"sha":%q}`, commit)
	})
	mux.HandleFunc("/o/r/tar.gz/", func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/"+commit) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(tarball)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fetcherFor(srv *httptest.Server) *GitHubRepoFetcher {
	f := NewGitHubRepoFetcher()
	f.APIBase = srv.URL
	f.CodeBase = srv.URL
	f.Client = srv.Client()
	return f
}

// --- source parsing ---------------------------------------------------------

func TestParseSourceCanonicalIdentity(t *testing.T) {
	for in, want := range map[string]string{
		"anthropics/skills":      "anthropics/skills",
		"https://github.com/a/b": "a/b",
		"github.com/a/b.git":     "a/b",
	} {
		got, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("ParseSource(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://evil.example/x", "a", "../etc", "a/b/../../c", "http://github.com/a/b", "https://github.com/a/b/blob/main/x", "https://user:pw@github.com/a/b"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("ParseSource(%q) should reject", bad)
		}
	}
	// Explicitly accepted: /tree/<ref> suffix drops the ref.
	if got, err := ParseSource("https://github.com/a/b/tree/main"); err != nil || got != "a/b" {
		t.Errorf("tree suffix: %q %v", got, err)
	}
}

// --- fetcher ----------------------------------------------------------------

func TestFetcherPreviewAndExtract(t *testing.T) {
	commit := "abc123"
	tarball := tarGz("r-abc123", map[string]string{
		"LICENSE":                 "MIT",
		"skills/pdf/SKILL.md":     sampleSkillMD,
		"skills/pdf/scripts/x.sh": "#!/bin/sh\n",
	}, map[string]bool{"skills/pdf/scripts/x.sh": true}, nil)
	srv := fakeGitHub(t, tarball, commit, 0)
	f := fetcherFor(srv)

	preview, err := f.Preview(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Commit != commit || len(preview.Skills) != 1 {
		t.Fatalf("preview wrong: %+v", preview)
	}
	skill := preview.Skills[0]
	if skill.Name != "PDF Kit" || skill.License != "LICENSE" || skill.Body == "" {
		t.Fatalf("skill wrong: %+v", skill)
	}
	if len(skill.Files) != 2 { // SKILL.md + scripts/x.sh
		t.Fatalf("files wrong: %+v", skill.Files)
	}

	// Extract the pinned commit: executable bit survives; digests match.
	bundles, _, err := f.ExtractCommit(context.Background(), "o/r", commit)
	if err != nil {
		t.Fatal(err)
	}
	bundle := bundles["PDF Kit"]
	if len(bundle) != 2 {
		t.Fatalf("bundle wrong: %+v", bundle)
	}
	for _, f2 := range bundle {
		if f2.Path == "scripts/x.sh" && f2.Mode != 0o755 {
			t.Fatalf("exec bit lost: %o", f2.Mode)
		}
	}

	// The install-time byte binding: the capability's recorded digests
	// against a re-fetch whose content changed under the same commit.
	other := tarGz("r-abc123", map[string]string{
		"skills/pdf/SKILL.md": strings.Replace(sampleSkillMD, "Body text.", "Changed!", 1),
	}, nil, nil)
	srv2 := fakeGitHub(t, other, commit, 0)
	f2 := fetcherFor(srv2)
	_, extracted, err := f2.ExtractCommit(context.Background(), "o/r", commit)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]map[string]FileDigest{}
	for _, sk := range preview.Skills {
		expected[sk.Name] = map[string]FileDigest{}
		for _, d := range sk.Files {
			expected[sk.Name][d.Path] = d
		}
	}
	if err := CompareDigests(expected, extracted); err == nil || !strings.Contains(err.Error(), "changed upstream") {
		t.Fatalf("want changed-upstream from digest comparison, got %v", err)
	}
}

func TestFetcherTypedFailuresAndLayouts(t *testing.T) {
	// 404 repo.
	srv := fakeGitHub(t, nil, "c", http.StatusNotFound)
	if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("want not-found, got %v", err)
	}
	// Rate limited.
	srv = fakeGitHub(t, nil, "c", http.StatusForbidden)
	if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want rate-limited, got %v", err)
	}
	// No skills found.
	srv = fakeGitHub(t, tarGz("r-c", map[string]string{"README.md": "x"}, nil, nil), "c", 0)
	if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); !errors.Is(err, ErrNoSkills) {
		t.Fatalf("want no-skills, got %v", err)
	}
	// Symlink entries reject the whole tarball.
	srv = fakeGitHub(t, tarGz("r-c", map[string]string{"skills/a/SKILL.md": sampleSkillMD}, nil, []string{"skills/a/evil"}), "c", 0)
	if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("want non-regular rejection, got %v", err)
	}
	// Traversal entry rejects.
	bad := tarGz("r-c", map[string]string{"skills/a/SKILL.md": sampleSkillMD, "../escape": "x"}, nil, nil)
	srv = fakeGitHub(t, bad, "c", 0)
	if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); err == nil || !strings.Contains(err.Error(), "archive violation") {
		t.Fatalf("want traversal rejection, got %v", err)
	}
	// Root SKILL.md layout and single-top-dir layout both detect.
	for name, entries := range map[string]map[string]string{
		"root":  {"SKILL.md": sampleSkillMD},
		"onDIR": {"kit/SKILL.md": sampleSkillMD},
		"agent": {".agents/skills/kit/SKILL.md": sampleSkillMD},
	} {
		srv = fakeGitHub(t, tarGz("r-c", entries, nil, nil), "c", 0)
		if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); err != nil {
			t.Errorf("layout %s: %v", name, err)
		}
	}
	// Loader-invalid frontmatter refuses at preview.
	invalid := "---\nname: broken\nextra: field\n---\n"
	srv = fakeGitHub(t, tarGz("r-c", map[string]string{"skills/a/SKILL.md": invalid}, nil, nil), "c", 0)
	if _, err := fetcherFor(srv).Preview(context.Background(), "o/r"); !errors.Is(err, ErrNoSkills) {
		t.Fatalf("want no-skills (loader-invalid refused), got %v", err)
	}
}

// --- index client -----------------------------------------------------------

func TestIndexClientCacheAndTypedFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("q") != "pdf" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"skills":[{"id":"anthropics/skills/pdf","source":"anthropics/skills","skillId":"pdf","name":"pdf","installs":234146}]}`))
	}))
	defer srv.Close()
	c := NewHTTPIndexClient()
	c.BaseURL = srv.URL
	c.Client = srv.Client()

	for i := 0; i < 3; i++ { // cached after first call
		res, err := c.Search(context.Background(), "pdf")
		if err != nil || len(res) != 1 || res[0].Source != "anthropics/skills" {
			t.Fatalf("search: %+v %v", res, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cache not effective: %d calls", calls)
	}

	down := NewHTTPIndexClient()
	down.BaseURL = "http://127.0.0.1:1"
	down.Client = &http.Client{Timeout: time.Second}
	if _, err := down.Search(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "index unreachable") {
		t.Fatalf("want typed unreachable, got %v", err)
	}
}

// --- archive import + capabilities ------------------------------------------

func TestArchiveImportLifecycle(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "stages"))
	if err != nil {
		t.Fatal(err)
	}
	data := zipBytes(t, map[string][]byte{
		"SKILL.md":     []byte(sampleSkillMD),
		"scripts/x.sh": []byte("#!/bin/sh\n"),
	}, map[string]bool{"scripts/x.sh": true}, nil)

	preview, id, err := store.StageArchive(data)
	if err != nil || preview.Skill.Name != "PDF Kit" {
		t.Fatalf("stage: %+v %v", preview, err)
	}
	files := preview.StagedFiles()
	if len(files) != 2 {
		t.Fatalf("staged files: %+v", files)
	}
	for _, f := range files {
		if f.Path == "scripts/x.sh" && f.Mode != 0o755 {
			t.Fatalf("exec bit lost in staging: %o", f.Mode)
		}
	}

	// Commit is one-shot; replay is expired.
	first, err := store.CommitArchive(id)
	if err != nil || first.Skill.Name != "PDF Kit" {
		t.Fatalf("commit: %v", err)
	}
	if f := first.StagedFiles(); len(f) != 2 {
		t.Fatalf("committed files: %+v", f)
	}
	if _, err := store.CommitArchive(id); err == nil {
		t.Fatal("replay should fail")
	}
}

func TestArchiveStartupReclamation(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "stages"))
	if err != nil {
		t.Fatal(err)
	}
	data := zipBytes(t, map[string][]byte{"SKILL.md": []byte(sampleSkillMD)}, nil, nil)
	_, id, err := store.StageArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	_ = id
	// A NEW store over the same root (daemon restart) reclaims orphans.
	if _, err := NewStore(filepath.Join(root, "stages")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "stages"))
	if len(entries) != 0 {
		t.Fatalf("startup reclamation left %d dirs", len(entries))
	}
}

func TestArchiveWrappedLayoutRoundTrip(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	// SKILL.md inside exactly one top-level directory (name.skill shape).
	data := zipBytes(t, map[string][]byte{
		"kit/SKILL.md":     []byte(sampleSkillMD),
		"kit/scripts/x.sh": []byte("#!/bin/sh\n"),
	}, map[string]bool{"kit/scripts/x.sh": true}, nil)
	preview, id, err := store.StageArchive(data)
	if err != nil || preview.Skill.Name != "PDF Kit" {
		t.Fatalf("wrapped stage: %v", err)
	}
	// Digests and staged files are SKILL-RELATIVE (the wrapper stripped).
	for _, f := range preview.Skill.Files {
		if strings.HasPrefix(f.Path, "kit/") {
			t.Fatalf("digest path not canonical: %q", f.Path)
		}
	}
	committed, err := store.CommitArchive(id)
	if err != nil {
		t.Fatal(err)
	}
	files := committed.StagedFiles()
	if len(files) != 2 {
		t.Fatalf("committed files: %+v", files)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Path, "kit/") {
			t.Fatalf("staged path not canonical: %q", f.Path)
		}
	}
}

func TestArchiveBusyBoundAndSweep(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	data := zipBytes(t, map[string][]byte{"SKILL.md": []byte(sampleSkillMD)}, nil, nil)
	ids := make([]string, 0, MaxArchiveStages)
	for i := 0; i < MaxArchiveStages; i++ {
		_, id, err := store.StageArchive(data)
		if err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	// The 9th upload: observable busy (no live eviction).
	if _, _, err := store.StageArchive(data); !errors.Is(err, ErrStageBusy) {
		t.Fatalf("want busy, got %v", err)
	}
	// Committing one frees a slot.
	if _, err := store.CommitArchive(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.StageArchive(data); err != nil {
		t.Fatalf("after commit should stage: %v", err)
	}
	// Abandoned expired stages sweep lazily on the next stage.
	store.mu.Lock()
	for _, c := range store.archive {
		c.expiry = time.Now().Add(-time.Second)
	}
	store.mu.Unlock()
	if _, _, err := store.StageArchive(data); err != nil {
		t.Fatalf("sweep should free slots: %v", err)
	}
}

func TestArchiveStreamingBomb(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	// 10 MiB+1 of zeros deflates tiny: headers look small, the stream
	// lies — streaming enforcement must reject.
	big := bytes.Repeat([]byte{0}, 10<<20+1)
	data := zipBytes(t, map[string][]byte{"SKILL.md": []byte(sampleSkillMD), "blob.bin": big}, nil, nil)
	if _, _, err := store.StageArchive(data); err == nil || !strings.Contains(err.Error(), "archive violation") {
		t.Fatalf("streaming bomb should reject: %v", err)
	}
}

func TestArchiveViolations(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	cases := map[string]func() ([]byte, error){
		"corrupt zip": func() ([]byte, error) { return []byte("not a zip"), nil },
		"no skill md": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"readme": []byte("x")}, nil, nil), nil
		},
		"two top dirs": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"a/SKILL.md": []byte(sampleSkillMD), "b/SKILL.md": []byte(sampleSkillMD)}, nil, nil), nil
		},
		"nested two deep": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"a/b/SKILL.md": []byte(sampleSkillMD)}, nil, nil), nil
		},
		"symlink entry": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"SKILL.md": []byte(sampleSkillMD)}, nil, []string{"evil"}), nil
		},
		"traversal": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"../escape": []byte("x"), "SKILL.md": []byte(sampleSkillMD)}, nil, nil), nil
		},
		"backslash": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{`a\b`: []byte("x"), "SKILL.md": []byte(sampleSkillMD)}, nil, nil), nil
		},
		"reserved provenance": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{".karta/provenance.yaml": []byte("forged"), "SKILL.md": []byte(sampleSkillMD)}, nil, nil), nil
		},
		"loader invalid": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"SKILL.md": []byte("---\nname: x\nextra: y\n---\n")}, nil, nil), nil
		},
		"outside wrapper": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"kit/SKILL.md": []byte(sampleSkillMD), "other/README": []byte("x")}, nil, nil), nil
		},
		"duplicate entries": func() ([]byte, error) {
			return zipBytes(t, map[string][]byte{"SKILL.md": []byte(sampleSkillMD), "a//b": []byte("x"), "a/b": []byte("y")}, nil, nil), nil
		},
	}
	for name, make := range cases {
		data, err := make()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, serr := store.StageArchive(data); serr == nil {
			t.Errorf("%s: want rejection", name)
		}
	}
}

func TestCompareDigestsExecChange(t *testing.T) {
	expected := map[string]map[string]FileDigest{"S": {"f": {Path: "f", Sum: "aa", Size: 1, Exec: true}}}
	extracted := map[string]map[string]FileDigest{"S": {"f": {Path: "f", Sum: "aa", Size: 1, Exec: false}}}
	if err := CompareDigests(expected, extracted); err == nil || !strings.Contains(err.Error(), "executable mode changed") {
		t.Fatalf("exec change should fail: %v", err)
	}
}

func TestRepoCapabilityExpiryAndBinding(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	skills := map[string]map[string]FileDigest{"PDF Kit": {"SKILL.md": {Path: "SKILL.md", Size: 10, Sum: "aa"}}}
	id, err := store.RegisterRepoPreview("o/r", "abc", skills)
	if err != nil || len(id) < 32 {
		t.Fatalf("register: %v %q", err, id)
	}
	// (a) A fresh registration resolves and binds source+commit+digests.
	src, commit, got, ok := store.ConsumeRepo(id)
	if !ok || src != "o/r" || commit != "abc" || got["PDF Kit"]["SKILL.md"].Sum != "aa" {
		t.Fatalf("consume: %q %q %+v %v", src, commit, got, ok)
	}
	// One-shot semantics: ConsumeRepo marks the entry used and DELETES it
	// on the next attempt, so a replayed id never resolves.
	if _, _, _, again := store.ConsumeRepo(id); again {
		t.Fatal("repo capability replay should fail")
	}
	// (b) Deletion-on-replay means the entry is gone: a second consume of
	// the same id fails identically.
	if _, _, _, third := store.ConsumeRepo(id); third {
		t.Fatal("consumed capability should stay deleted")
	}
	// (c) Expiry, asserted on an INDEPENDENTLY registered entry: aging it
	// past its deadline makes it unresolvable.
	id2, err := store.RegisterRepoPreview("o/r2", "def", skills)
	if err != nil {
		t.Fatalf("register 2: %v", err)
	}
	store.mu.Lock()
	store.repo[id2].expiry = time.Now().Add(-time.Second)
	store.mu.Unlock()
	if _, _, _, ok := store.ConsumeRepo(id2); ok {
		t.Fatal("expired capability should not resolve")
	}
	// Unknown ids never resolve.
	if _, _, _, ok := store.ConsumeRepo("nope"); ok {
		t.Fatal("unknown capability resolved")
	}
}

// --- end-to-end: registry InstallSkill from a fetched bundle -----------------

func TestInstallFromFetchedRepo(t *testing.T) {
	commit := "cafe42"
	tarball := tarGz("r-cafe42", map[string]string{
		"skills/pdf/SKILL.md": sampleSkillMD,
	}, nil, nil)
	srv := fakeGitHub(t, tarball, commit, 0)
	f := fetcherFor(srv)

	preview, err := f.Preview(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	bundles, _, err := f.ExtractCommit(context.Background(), "o/r", preview.Commit)
	if err != nil {
		t.Fatal(err)
	}
	// Registry over a temp home.
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.LoadAll(registry.Dirs{Skills: skillsDir})
	spec, err := reg.InstallSkill(registry.SkillBundle{
		Name: "PDF Kit", Description: "PDF tooling.", Files: bundles["PDF Kit"],
		Provenance: &registry.ProvenanceInput{Origin: registry.OriginMarketplace, Source: "o/r", Commit: commit},
	})
	if err != nil || spec.Path == "" {
		t.Fatalf("install: %+v %v", spec, err)
	}
	if s := reg.Snapshot().Skills["PDF Kit"]; s.Origin != registry.OriginMarketplace || s.Source != "o/r" {
		t.Fatalf("origin wrong: %+v", s)
	}
}
