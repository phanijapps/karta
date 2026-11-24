package wards

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindingKind classifies one lint violation.
type FindingKind string

const (
	// FindingMissingRequired reports a required node that has no match.
	FindingMissingRequired FindingKind = "missing_required"
	// FindingUnexpectedEntry reports an entry no layout rule matches (also
	// the extra entries of a non-repeatable node).
	FindingUnexpectedEntry FindingKind = "unexpected_entry"
	// FindingFormatMismatch reports a matched file breaking its format rule.
	FindingFormatMismatch FindingKind = "format_mismatch"
	// FindingInvalidConf reports a ward-conf.yaml that failed to parse.
	FindingInvalidConf FindingKind = "invalid_conf"
)

// Finding is one typed lint violation against a ward. NodeID is the conf
// node's id — or its match pattern when the node declares no id
// (contract: WardFinding.node_id) — and "" when the finding has no node
// at all (invalid confs). Path is the offending location relative to the
// ward root.
type Finding struct {
	NodeID  string
	Kind    FindingKind
	Path    string
	Message string
}

// nodeRef returns the identifier findings use for a node: its id, or its
// match pattern when the id is omitted.
func nodeRef(n Node) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Match
}

// Lint walks a ward's actual filesystem tree against its conf and
// returns every violation. The walk covers each directory the conf
// describes (nodes without children — freeform role dirs like pages/ —
// are matched but not descended into). Hidden entries (dot-prefixed)
// and the ward-conf.yaml file itself are tolerated at every level:
// tooling owns those (.git, .zbot, the conf, ...). A nil conf is a
// clean freeform ward: nothing to check.
func Lint(ctx context.Context, wardDir, name string, conf *Conf) ([]Finding, error) {
	if conf == nil {
		return nil, nil
	}
	var findings []Finding
	if err := lintDir(ctx, wardDir, name, conf.Layout.Children, "", &findings); err != nil {
		return nil, err
	}
	return findings, nil
}

// lintDir checks one directory level: every node's matches, then the
// entries no node claimed. rel is this directory's path relative to the
// ward root ("" at the root).
func lintDir(ctx context.Context, dir, ward string, nodes []Node, rel string, out *[]Finding) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wards: lint %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("wards: lint %s: %w", dir, err)
	}
	consumed := make(map[string]bool, len(entries))
	for i := range nodes {
		node := nodes[i]
		pattern := expandWard(node.Match, ward)
		var matches []string
		for _, e := range entries {
			kindOK := (node.Kind == KindDirectory) == e.IsDir()
			if !kindOK {
				continue
			}
			ok, matchErr := filepath.Match(pattern, e.Name())
			if matchErr != nil {
				// ParseConf already rejects malformed patterns; a
				// pattern that still fails here is unreachable.
				return fmt.Errorf("wards: lint %s: node %q: bad match pattern %q: %w", dir, nodeRef(node), node.Match, matchErr)
			}
			if ok {
				matches = append(matches, e.Name())
				consumed[e.Name()] = true
			}
		}
		lintMatches(rel, ward, node, matches, out)
		if node.Kind == KindDirectory && len(node.Children) > 0 {
			for _, m := range matches {
				childRel := m
				if rel != "" {
					childRel = rel + string(filepath.Separator) + m
				}
				if err := lintDir(ctx, filepath.Join(dir, m), ward, node.Children, childRel, out); err != nil {
					return err
				}
			}
		}
	}
	for _, e := range entries {
		// Root-level v2 homes (wiki/reports/apps) are built-in optional
		// nodes in EVERY ward — legacy confs lint clean post-migration.
		if rel == "" && v2Homes[e.Name()] {
			continue
		}
		if consumed[e.Name()] || strings.HasPrefix(e.Name(), ".") || e.Name() == confFile {
			continue
		}
		path := e.Name()
		if rel != "" {
			path = rel + string(filepath.Separator) + path
		}
		*out = append(*out, Finding{
			Kind:    FindingUnexpectedEntry,
			Path:    path,
			Message: fmt.Sprintf("no layout rule matches %q", path),
		})
	}
	return nil
}

// lintMatches applies one node's rules to its matched entries.
func lintMatches(rel, ward string, node Node, matches []string, out *[]Finding) {
	pathOf := func(name string) string {
		if rel == "" {
			return name
		}
		return rel + string(filepath.Separator) + name
	}
	ref := nodeRef(node)
	if node.IsRequired() && len(matches) == 0 {
		wanted := expandWard(node.Match, ward)
		*out = append(*out, Finding{
			NodeID:  ref,
			Kind:    FindingMissingRequired,
			Path:    pathOf(wanted),
			Message: fmt.Sprintf("required %s %q is missing (match %q)", node.Kind, wanted, node.Match),
		})
	}
	if !node.IsRepeat() && len(matches) > 1 {
		for _, extra := range matches[1:] {
			*out = append(*out, Finding{
				NodeID:  ref,
				Kind:    FindingUnexpectedEntry,
				Path:    pathOf(extra),
				Message: fmt.Sprintf("node %q is not repeatable but matched %d entries", ref, len(matches)),
			})
		}
	}
	if node.Format == FormatMarkdown {
		for _, m := range matches {
			if filepath.Ext(m) != ".md" {
				*out = append(*out, Finding{
					NodeID:  ref,
					Kind:    FindingFormatMismatch,
					Path:    pathOf(m),
					Message: fmt.Sprintf("node %q requires markdown, got %q", ref, filepath.Ext(m)),
				})
			}
		}
	}
}

// expandWard substitutes the {ward} placeholder in a match pattern with
// the ward's name (agentzero convention: the canonical page is
// <ward-name>.md).
func expandWard(pattern, ward string) string {
	return strings.ReplaceAll(pattern, "{ward}", ward)
}
