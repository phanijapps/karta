package wards

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedVault bootstraps a fresh vault at a temp path.
func seedVault(t *testing.T) *Vault {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wards")
	if err := Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	v, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return v
}

func TestBootstrapSeedsTemplates(t *testing.T) {
	v := seedVault(t)
	for _, archetype := range Archetypes() {
		dir := filepath.Join(v.Root(), templatesDir, archetype)
		if _, err := os.Stat(filepath.Join(dir, "ward-conf.yaml")); err != nil {
			t.Fatalf("%s ward-conf.yaml missing: %v", archetype, err)
		}
		// Starter pages exist and the seeded conf parses.
		raw, err := os.ReadFile(filepath.Join(dir, "ward-conf.yaml"))
		if err != nil {
			t.Fatalf("read %s conf: %v", archetype, err)
		}
		conf, err := ParseConf(raw, "ward-conf.yaml")
		if err != nil {
			t.Fatalf("seeded %s conf must parse: %v", archetype, err)
		}
		if conf.Archetype != archetype {
			t.Fatalf("seeded %s conf declares %q", archetype, conf.Archetype)
		}
	}
	if _, err := os.Stat(filepath.Join(v.Root(), ".gitignore")); err != nil {
		t.Fatalf(".gitignore starter missing: %v", err)
	}
	// The seeded templates lint clean against themselves EXCEPT the
	// canonical starter (canonical.md vs {ward}.md) — templates are
	// copied and renamed on ward creation, so the mismatch is expected
	// and lives only under .templates/, which Index skips.
	infos, err := v.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("fresh vault must index empty (templates are machinery), got %+v", infos)
	}
}

func TestBootstrapNeverOverwrites(t *testing.T) {
	v := seedVault(t)
	edited := filepath.Join(v.Root(), templatesDir, "generic", "ward-conf.yaml")
	if err := os.WriteFile(edited, []byte("# user-owned now\narchetype: mine\nlayout:\n  kind: directory\n  children: []\n"), 0o644); err != nil {
		t.Fatalf("edit template: %v", err)
	}
	if err := Bootstrap(context.Background(), v.Root()); err != nil {
		t.Fatalf("re-Bootstrap: %v", err)
	}
	got, _ := os.ReadFile(edited)
	if !strings.Contains(string(got), "user-owned now") {
		t.Fatalf("re-bootstrap overwrote a user edit:\n%s", got)
	}
	// Also true for the .gitignore starter.
	gi := filepath.Join(v.Root(), ".gitignore")
	if err := os.WriteFile(gi, []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("edit gitignore: %v", err)
	}
	if err := Bootstrap(context.Background(), v.Root()); err != nil {
		t.Fatalf("re-Bootstrap: %v", err)
	}
	gotGI, _ := os.ReadFile(gi)
	if string(gotGI) != "custom\n" {
		t.Fatalf("re-bootstrap overwrote .gitignore: %q", gotGI)
	}
}

// TestSeedTemplatesLintClean proves the SEEDED archetypes' confs and
// starter pages describe a coherent ward: copying the template dir as
// <name>/ (renaming canonical.md → <name>.md) lints clean.
func TestSeedTemplatesLintClean(t *testing.T) {
	for _, archetype := range Archetypes() {
		t.Run(archetype, func(t *testing.T) {
			src := filepath.Join("templates", archetype)
			raw, err := os.ReadFile(filepath.Join(src, "ward-conf.yaml"))
			if err != nil {
				t.Fatalf("read conf: %v", err)
			}
			conf, err := ParseConf(raw, "ward-conf.yaml")
			if err != nil {
				t.Fatalf("parse conf: %v", err)
			}
			ward := t.TempDir()
			entries, err := os.ReadDir(src)
			if err != nil {
				t.Fatalf("read dir: %v", err)
			}
			for _, e := range entries {
				if e.Name() == "ward-conf.yaml" || e.Name() == "canonical.md" {
					continue
				}
				data, err := os.ReadFile(filepath.Join(src, e.Name()))
				if err != nil {
					t.Fatalf("read %s: %v", e.Name(), err)
				}
				if err := os.WriteFile(filepath.Join(ward, e.Name()), data, 0o644); err != nil {
					t.Fatalf("write %s: %v", e.Name(), err)
				}
			}
			// The canonical starter lands under the ward's own name.
			canonical, err := os.ReadFile(filepath.Join(src, "canonical.md"))
			if err != nil {
				t.Fatalf("read canonical starter: %v", err)
			}
			name := "my-" + archetype
			if err := os.WriteFile(filepath.Join(ward, name+".md"), canonical, 0o644); err != nil {
				t.Fatalf("write canonical: %v", err)
			}
			findings, err := Lint(context.Background(), ward, name, &conf)
			if err != nil {
				t.Fatalf("Lint: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("seeded %s template must lint clean when copied, got %+v", archetype, findings)
			}
		})
	}
}

func TestIndexAndDetail(t *testing.T) {
	v := seedVault(t)

	// A clean research ward (canonical title from its heading).
	clean := filepath.Join(v.Root(), "amdfinancials")
	writeWardFiles(t, clean, map[string]string{
		"amdfinancials.md": "# AMD financials\nthe synthesis\n",
		"AGENTS.md":        "readme\n",
		"log.md":           "log\n",
		"ward-conf.yaml":   researchConf,
	})
	// A violating ward: missing canonical (and an unexpected entry).
	dirty := filepath.Join(v.Root(), "quant-strategy")
	writeWardFiles(t, dirty, map[string]string{
		"AGENTS.md":      "readme\n",
		"log.md":         "log\n",
		"ward-conf.yaml": researchConf,
		"stray.txt":      "stray\n",
	})
	// A conf-less freeform ward.
	free := filepath.Join(v.Root(), "scraps")
	writeWardFiles(t, free, map[string]string{
		"scraps.md": "# Scraps\nfreeform\n",
	})
	// A ward with an unparseable conf.
	broken := filepath.Join(v.Root(), "broken")
	writeWardFiles(t, broken, map[string]string{
		"ward-conf.yaml": "layout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: file, bogus: 1 }\n",
	})

	infos, err := v.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(infos) != 4 {
		t.Fatalf("want 4 wards, got %+v", infos)
	}
	byName := map[string]WardInfo{}
	for _, info := range infos {
		byName[info.Name] = info
	}
	if infos[0].Name != "amdfinancials" || infos[3].Name != "scraps" {
		t.Fatalf("index must be name-ordered: %+v", infos)
	}
	if got := byName["amdfinancials"]; got.Archetype != "research" || got.LintStatus() != "clean" || got.CanonicalTitle != "AMD financials" || !got.HasConf {
		t.Fatalf("clean research ward summary: %+v", got)
	}
	if got := byName["quant-strategy"]; got.Archetype != "research" || got.LintStatus() != "violations" || got.FindingCount != 2 {
		t.Fatalf("violating ward summary: %+v", got)
	}
	if got := byName["scraps"]; got.Archetype != "generic" || got.LintStatus() != "clean" || got.HasConf || got.CanonicalTitle != "Scraps" {
		t.Fatalf("freeform ward summary: %+v", got)
	}
	if got := byName["broken"]; got.Archetype != "unknown" || got.LintStatus() != "violations" {
		t.Fatalf("broken-conf ward summary: %+v", got)
	}

	t.Run("detail returns tree and findings", func(t *testing.T) {
		detail, err := v.Detail(context.Background(), "amdfinancials")
		if err != nil {
			t.Fatalf("Detail: %v", err)
		}
		if detail.Layout == nil || len(detail.Layout.Children) == 0 {
			t.Fatalf("detail must carry the parsed layout: %+v", detail.Layout)
		}
		if detail.Layout.Children[0].ID != "canonical" {
			t.Fatalf("tree node ids must survive: %+v", detail.Layout.Children[0])
		}
		if len(detail.Findings) != 0 {
			t.Fatalf("clean ward findings: %+v", detail.Findings)
		}
		if detail.Info.CanonicalTitle != "AMD financials" {
			t.Fatalf("canonical title: %q", detail.Info.CanonicalTitle)
		}
	})

	t.Run("detail unknown ward is typed 404", func(t *testing.T) {
		if _, err := v.Detail(context.Background(), "nope"); !errors.Is(err, ErrUnknownWard) {
			t.Fatalf("want ErrUnknownWard, got %v", err)
		}
	})

	t.Run("detail rejects path-shaped names", func(t *testing.T) {
		for _, name := range []string{"../..", "a/b", ".templates", ".git"} {
			if _, err := v.Detail(context.Background(), name); !errors.Is(err, ErrUnknownWard) {
				t.Fatalf("name %q must be typed-unknown, got %v", name, err)
			}
		}
	})

	// Membership rules match Index: a symlink (even to a real directory
	// outside the vault) and a root-level regular file are not wards.
	t.Run("detail never follows symlinks or files", func(t *testing.T) {
		external := t.TempDir()
		if err := os.WriteFile(filepath.Join(external, "stolen.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(v.Root(), "linked")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.WriteFile(filepath.Join(v.Root(), "plainfile"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"linked", "plainfile"} {
			if _, err := v.Detail(context.Background(), name); !errors.Is(err, ErrUnknownWard) {
				t.Fatalf("%q must be typed-unknown, got %v", name, err)
			}
		}
		// And the index never listed them either.
		infos, err := v.Index(context.Background())
		if err != nil {
			t.Fatalf("Index: %v", err)
		}
		for _, info := range infos {
			if info.Name == "linked" || info.Name == "plainfile" {
				t.Fatalf("non-directory entries must not index: %+v", info)
			}
		}
	})

	t.Run("detail broken conf carries invalid_conf finding", func(t *testing.T) {
		detail, err := v.Detail(context.Background(), "broken")
		if err != nil {
			t.Fatalf("Detail: %v", err)
		}
		if len(detail.Findings) != 1 || detail.Findings[0].Kind != FindingInvalidConf {
			t.Fatalf("want invalid_conf finding, got %+v", detail.Findings)
		}
		if detail.Layout != nil {
			t.Fatalf("unparseable conf must not yield a layout: %+v", detail.Layout)
		}
	})
}

// A canonical node nested under a declared directory is not part of the
// v1 canonical convention (root-level only, like both templates) — such
// a ward lints clean but yields no canonical title.
func TestNestedCanonicalNodeYieldsNoTitle(t *testing.T) {
	v := seedVault(t)
	conf := `archetype: nested
layout:
  kind: directory
  children:
    - id: pages
      match: pages
      kind: directory
      children:
        - { id: canonical, match: "*.md", kind: file, format: markdown }
`
	ward := filepath.Join(v.Root(), "nested")
	writeWardFiles(t, ward, map[string]string{
		"pages/page.md":  "# A page\nnot the canonical page\n",
		"ward-conf.yaml": conf,
	})
	infos, err := v.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	var found *WardInfo
	for i := range infos {
		if infos[i].Name == "nested" {
			found = &infos[i]
		}
	}
	if found == nil {
		t.Fatal("nested ward not indexed")
	}
	if found.LintStatus() != "clean" {
		t.Fatalf("ward must lint clean: %+v", *found)
	}
	if found.CanonicalTitle != "" {
		t.Fatalf("nested canonical is outside the v1 convention — no title, got %q", found.CanonicalTitle)
	}
}

// Bootstrap never follows a dangling symlink at a seed destination:
// the vault's own files stay user-owned.
func TestBootstrapDanglingSymlinkSeed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wards")
	if err := os.MkdirAll(filepath.Join(root, templatesDir, "generic"), 0o755); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(root, templatesDir, "generic", "ward-conf.yaml")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.yaml"), dangling); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	fi, err := os.Lstat(dangling)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("seed must not replace or follow the dangling symlink: %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "outside.yaml")); !os.IsNotExist(err) {
		t.Fatal("seed wrote through the dangling symlink")
	}
}

// A canonical heading longer than the contract's 256-char cap is
// truncated (by runes) so list responses stay schema-valid.
func TestCanonicalTitleCappedAtContractLimit(t *testing.T) {
	v := seedVault(t)
	ward := filepath.Join(v.Root(), "longtitle")
	long := strings.Repeat("宽", 300) // multi-byte: the cap is runes, not bytes
	writeWardFiles(t, ward, map[string]string{
		"longtitle.md":   "# " + long + "\nbody\n",
		"AGENTS.md":      "readme\n",
		"log.md":         "log\n",
		"ward-conf.yaml": researchConf,
	})
	infos, err := v.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(infos) != 1 || len([]rune(infos[0].CanonicalTitle)) != 256 {
		t.Fatalf("title must cap at 256 runes, got %d: %q", len([]rune(infos[0].CanonicalTitle)), infos[0].CanonicalTitle)
	}
}

// A ward directory whose name exceeds the old 128 cap (but is legal
// for the filesystem) indexes, details, and serializes — the contract
// bound is 255, matching what a filesystem can produce.
func TestLongWardNameRoundTrips(t *testing.T) {
	v := seedVault(t)
	name := strings.Repeat("w", 200)
	ward := filepath.Join(v.Root(), name)
	writeWardFiles(t, ward, map[string]string{name + ".md": "# Hello\nfreeform\n"})
	infos, err := v.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != name || infos[0].CanonicalTitle != "Hello" {
		t.Fatalf("long-name ward must round-trip: %+v", infos)
	}
	detail, err := v.Detail(context.Background(), name)
	if err != nil || detail.Info.Name != name {
		t.Fatalf("detail must round-trip: %+v %v", detail, err)
	}
}

// An invalid conf whose unknown-field name is huge produces a
// CLAMPED diagnostic — the response message bound always holds.
func TestInvalidConfDiagnosticClamped(t *testing.T) {
	v := seedVault(t)
	ward := filepath.Join(v.Root(), "bignotes")
	writeWardFiles(t, ward, map[string]string{
		"ward-conf.yaml": "layout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: file, " + strings.Repeat("z", 2000) + ": 1 }\n",
	})
	detail, err := v.Detail(context.Background(), "bignotes")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Findings) != 1 || detail.Findings[0].Kind != FindingInvalidConf {
		t.Fatalf("want invalid_conf finding: %+v", detail.Findings)
	}
	if got := len([]rune(detail.Findings[0].Message)); got > maxFindingTextLen {
		t.Fatalf("diagnostic must clamp at %d runes, got %d", maxFindingTextLen, got)
	}
}

// Canonical title falls back to the file name when the page has no H1.
func TestCanonicalTitleFallback(t *testing.T) {
	v := seedVault(t)
	ward := filepath.Join(v.Root(), "plain")
	writeWardFiles(t, ward, map[string]string{
		"plain.md":       "no heading here\n",
		"AGENTS.md":      "readme\n",
		"log.md":         "log\n",
		"ward-conf.yaml": researchConf,
	})
	infos, err := v.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(infos) != 1 || infos[0].CanonicalTitle != "plain" {
		t.Fatalf("want filename fallback title, got %+v", infos)
	}
}

func TestOpenMissingVault(t *testing.T) {
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Open on missing dir must fail")
	}
}

// writeWardFiles writes entries under an explicit directory (the vault
// fixture variant of writeWard — here the dir is not fresh).
func writeWardFiles(t *testing.T, dir string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir ward %s: %v", dir, err)
	}
	for rel, content := range entries {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir parent %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}
