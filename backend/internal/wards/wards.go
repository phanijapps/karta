// Package wards owns the ward vault: user-owned directories of documented
// knowledge that live OUTSIDE ~/.zbot (default ~/Documents/wards). The
// package resolves the vault root (config override + OS-aware default),
// bootstraps archetype templates (write-once, user-owned after), indexes
// the vault, parses each ward's ward-conf.yaml layout schema, and lints a
// ward's tree against its conf. Everything here is read-only after
// bootstrap — wards are created by agents (slice 3) or by hand.
//
// Boundaries: imports stdlib + domain-tier only; NEVER engine/*, server,
// or client code (see backend/AGENTS.md dependency graph). I/O entry
// points take ctx first and check cancellation between files/directories
// (go-rules 2.2 — individual os calls are not cancellable, boundaries
// are). No eventbus emission: read-only listing involves no agent step,
// tool call, hook decision, or compaction.
package wards

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Vault is an opened ward vault root. All operations scan on demand (v1
// vaults are small; hot-reload lands with slice 3's sticky ward).
type Vault struct {
	root string
}

// WardInfo is one indexed ward: identity, archetype, lint summary, and
// the canonical page title when a canonical page exists.
type WardInfo struct {
	Name           string
	Archetype      string // conf archetype; "generic" when conf-less, "unknown" when unparseable
	HasConf        bool
	FindingCount   int
	CanonicalTitle string // "" when the ward has no canonical page yet
}

// LintStatus classifies a ward for the API surface.
func (w WardInfo) LintStatus() string {
	if w.FindingCount == 0 {
		return "clean"
	}
	return "violations"
}

// ErrUnknownWard is returned by Detail for a name that is not a ward in
// the vault (the HTTP layer maps it to a typed 404).
var ErrUnknownWard = errors.New("wards: no such ward")

// Open returns a Vault over an existing root directory (one stat; ctx
// for rule 2.2 signature shape). Creating the vault (and seeding
// templates) is Bootstrap's job; Open is read-only.
func Open(ctx context.Context, vaultRoot string) (*Vault, error) {
	info, err := os.Stat(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("wards: open vault %s: %w", vaultRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("wards: open vault %s: not a directory", vaultRoot)
	}
	return &Vault{root: vaultRoot}, nil
}

// Root reports the vault root path.
func (v *Vault) Root() string { return v.root }

// Index scans the vault and returns every ward ordered by name. Hidden
// entries (dot-prefixed: .templates, .git, ...) are vault machinery, not
// wards; non-directory entries (including symlinks) are not wards —
// Detail applies the same membership rule. Per-ward conf parse failures
// degrade to an "unknown" archetype plus an invalid_conf finding — a
// broken conf is a lint result, not an index failure.
func (v *Vault) Index(ctx context.Context) ([]WardInfo, error) {
	entries, err := os.ReadDir(v.root)
	if err != nil {
		return nil, fmt.Errorf("wards: index %s: %w", v.root, err)
	}
	infos := make([]WardInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wards: index: %w", err)
		}
		info, err := v.indexWard(ctx, e.Name())
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// indexWard builds one ward's summary: conf parse (or the failure
// finding), lint, and canonical title. Lint I/O failures propagate —
// an unreadable ward is an environment error, not a lint result.
func (v *Vault) indexWard(ctx context.Context, name string) (WardInfo, error) {
	dir := filepath.Join(v.root, name)
	info := WardInfo{Name: name, Archetype: "generic"}
	raw, err := os.ReadFile(filepath.Join(dir, confFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Conf-less wards are valid freeform wards (spec scenario).
			info.CanonicalTitle = canonicalTitle(ctx, dir, name, nil)
			return info, nil
		}
		return info, fmt.Errorf("wards: index %s: %w", name, err)
	}
	info.HasConf = true
	conf, perr := ParseConf(raw, confFile)
	if perr != nil {
		info.Archetype = "unknown"
		info.FindingCount = 1
		return info, nil
	}
	if conf.Archetype != "" {
		info.Archetype = conf.Archetype
	}
	findings, lerr := Lint(ctx, dir, name, &conf)
	if lerr != nil {
		return info, fmt.Errorf("wards: index %s: %w", name, lerr)
	}
	info.FindingCount = len(findings)
	info.CanonicalTitle = canonicalTitle(ctx, dir, name, &conf)
	return info, nil
}

// WardDetail is the per-ward read surface: the summary, full findings,
// and the parsed layout tree (nil when the ward has no conf).
type WardDetail struct {
	Info     WardInfo
	Findings []Finding
	Layout   *Node
}

// Detail returns one ward's full view. Unknown names, path-shaped
// names, and non-directory entries (a symlink to outside the vault
// included) return ErrUnknownWard (typed 404 upstream) — membership
// matches Index's rules, so Detail never escapes the vault through a
// link.
func (v *Vault) Detail(ctx context.Context, name string) (WardDetail, error) {
	if !validWardName(name) {
		return WardDetail{}, fmt.Errorf("wards: detail %q: %w", name, ErrUnknownWard)
	}
	dir := filepath.Join(v.root, name)
	// Lstat: a symlink (even to a directory) is not a member ward.
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WardDetail{}, fmt.Errorf("wards: detail %q: %w", name, ErrUnknownWard)
		}
		return WardDetail{}, fmt.Errorf("wards: detail %q: %w", name, err)
	}
	if !info.IsDir() {
		return WardDetail{}, fmt.Errorf("wards: detail %q: %w", name, ErrUnknownWard)
	}
	detail := WardDetail{Info: WardInfo{Name: name, Archetype: "generic"}, Findings: []Finding{}}
	raw, err := os.ReadFile(filepath.Join(dir, confFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			detail.Info.CanonicalTitle = canonicalTitle(ctx, dir, name, nil)
			return detail, nil
		}
		return WardDetail{}, fmt.Errorf("wards: detail %q: %w", name, err)
	}
	detail.Info.HasConf = true
	conf, perr := ParseConf(raw, confFile)
	if perr != nil {
		detail.Info.Archetype = "unknown"
		detail.Info.FindingCount = 1
		// Diagnostics are clamped to the contract's message bound: an
		// unknown-field name as long as the conf file itself must not
		// produce an invalid response.
		detail.Findings = []Finding{{Kind: FindingInvalidConf, Message: clampRunes(perr.Error(), maxFindingTextLen)}}
		return detail, nil
	}
	if conf.Archetype != "" {
		detail.Info.Archetype = conf.Archetype
	}
	var lerr error
	detail.Findings, lerr = Lint(ctx, dir, name, &conf)
	if lerr != nil {
		return WardDetail{}, fmt.Errorf("wards: detail %q: %w", name, lerr)
	}
	// Findings are user-derived (paths under the ward): clamp at emit so
	// the response always satisfies the contract's bounds.
	for i := range detail.Findings {
		detail.Findings[i].Path = clampRunes(detail.Findings[i].Path, maxFindingTextLen)
		detail.Findings[i].Message = clampRunes(detail.Findings[i].Message, maxFindingTextLen)
	}
	detail.Info.FindingCount = len(detail.Findings)
	detail.Info.CanonicalTitle = canonicalTitle(ctx, dir, name, &conf)
	detail.Layout = &conf.Layout
	return detail, nil
}

// validWardName accepts plain directory names only — no separators, no
// dot-hopping, no hidden names. Ward names are user-facing ids.
func validWardName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return false
	}
	if name == "." || name == ".." {
		return false
	}
	return true
}

// canonicalTitle finds the ward's canonical page and extracts its title:
// the first level-1 markdown heading, falling back to the file name. The
// canonical page is the file matched by the ROOT-LEVEL conf child with id
// "canonical" (the convention both archetype templates use — nested
// canonical nodes are not part of the v1 convention); without a conf it
// is <ward-name>.md at the ward root. A missing canonical page simply
// yields no title — the linter is what reports the miss.
func canonicalTitle(ctx context.Context, dir, name string, conf *Conf) string {
	path := ""
	if conf != nil {
		for i := range conf.Layout.Children {
			if conf.Layout.Children[i].ID == canonicalNodeID {
				path = matchOne(ctx, dir, conf.Layout.Children[i].Match, name)
				break
			}
		}
	} else {
		p := filepath.Join(dir, name+".md")
		if fileExists(p) {
			path = p
		}
	}
	if path == "" {
		return ""
	}
	return titleOf(ctx, path)
}

// matchOne returns the first existing file under dir that matches the
// pattern ({ward} substituted), or "".
func matchOne(ctx context.Context, dir, pattern, ward string) string {
	if err := ctx.Err(); err != nil {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	pat := expandWard(pattern, ward)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ok, matchErr := filepath.Match(pat, e.Name())
		if matchErr == nil && ok {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// titleOf reads at most headingScanLimit bytes of a page (bounded read —
// a huge user file must not be slurped whole for one heading) and
// returns the first "# " heading text, or the base name without its
// extension when no heading exists.
func titleOf(ctx context.Context, path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close() // read-only handle; close error carries no information
	data, err := io.ReadAll(io.LimitReader(f, headingScanLimit))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			// Cap by runes to keep the response inside the contract's
			// canonical_title maxLength (256); a longer H1 is still shown
			// whole in the page itself.
			if runes := []rune(title); len(runes) > maxCanonicalTitleLen {
				return string(runes[:maxCanonicalTitleLen])
			}
			return title
		}
	}
	base := filepath.Base(path)
	return clampRunes(strings.TrimSuffix(base, filepath.Ext(base)), maxCanonicalTitleLen)
}

// headingScanLimit bounds the canonical-title scan.
const headingScanLimit = 64 * 1024

// maxCanonicalTitleLen mirrors the contract's WardSummary.canonical_title
// maxLength.
const maxCanonicalTitleLen = 256

// maxFindingTextLen mirrors the contract's WardFinding path/message
// maxLength (both 1024).
const maxFindingTextLen = 1024

// clampRunes truncates s to at most limit Unicode code points.
func clampRunes(s string, limit int) string {
	if runes := []rune(s); len(runes) > limit {
		return string(runes[:limit])
	}
	return s
}

// canonicalNodeID is the conf node id convention that marks a ward's
// canonical synthesis page (both seeded archetypes use it).
const canonicalNodeID = "canonical"

// confFile is the ward layout schema file name (agentzero convention).
const confFile = "ward-conf.yaml"
