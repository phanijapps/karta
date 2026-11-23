package wards

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Field-length limits mirror the contract's response schemas (WardNode,
// WardFinding, WardSummary) so a parsed conf can always be serialized.
const (
	maxArchetypeLen = 64
	maxNodeIDLen    = 128
	maxMatchLen     = 256
)

// NodeKind is one layout node's filesystem kind: a file or a directory.
type NodeKind string

const (
	// KindFile matches plain files.
	KindFile NodeKind = "file"
	// KindDirectory matches directories.
	KindDirectory NodeKind = "directory"
)

// Format constrains a file node's content format: markdown expects a
// .md file; raw means no constraint (any bytes).
type Format string

const (
	// FormatMarkdown expects a .md file.
	FormatMarkdown Format = "markdown"
	// FormatRaw accepts any bytes.
	FormatRaw Format = "raw"
)

// Node is one node of the declarative layout tree (agentzero's shape,
// trimmed): what entries may or must exist under a directory. The root
// node carries children only (no match of its own).
type Node struct {
	ID       string   `yaml:"id,omitempty"`
	Kind     NodeKind `yaml:"kind"`
	Match    string   `yaml:"match,omitempty"`
	Required *bool    `yaml:"required,omitempty"` // default true
	Repeat   *bool    `yaml:"repeat,omitempty"`   // default false
	Format   Format   `yaml:"format,omitempty"`   // markdown | raw; empty = unconstrained
	Children []Node   `yaml:"children,omitempty"`
	// Ref names a subtree definition to inline ($ref). v1 PARSES it only
	// — the linter ignores it; slice 3 lifts it if ward confs need it.
	Ref string `yaml:"$ref,omitempty"`
}

// IsRequired reports the node's required flag (default true).
func (n Node) IsRequired() bool { return n.Required == nil || *n.Required }

// IsRepeat reports the node's repeat flag (default false).
func (n Node) IsRepeat() bool { return n.Repeat != nil && *n.Repeat }

// Conf is a parsed ward-conf.yaml: the ward's archetype plus its layout
// tree. Archetype is informational (badges, templates); the tree is the
// lint contract.
type Conf struct {
	Archetype string `yaml:"archetype,omitempty"`
	Layout    Node   `yaml:"layout"`
}

// ConfParseError is a typed ward-conf.yaml parse failure naming the
// source file; Details carries per-line problems (unknown fields,
// invalid values) straight from the strict decoder or the semantic pass.
type ConfParseError struct {
	Source  string
	Details []string
}

func (e *ConfParseError) Error() string {
	return fmt.Sprintf("wards: %s: invalid ward-conf: %s", e.Source, strings.Join(e.Details, "; "))
}

// ParseConf strictly decodes one ward-conf.yaml. Unknown fields, wrong
// types, and semantic violations (bad kind, empty match, malformed glob
// syntax, file nodes with children, formatted directories) are typed
// errors naming the source file and the offending line/field. Node ids
// are OPTIONAL (design decision 3: `id?`) — findings identify id-less
// nodes by their match pattern.
func ParseConf(raw []byte, source string) (Conf, error) {
	var conf Conf
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&conf); err != nil {
		var te *yaml.TypeError
		if errors.As(err, &te) {
			return Conf{}, &ConfParseError{Source: source, Details: te.Errors}
		}
		return Conf{}, &ConfParseError{Source: source, Details: []string{err.Error()}}
	}
	if problems := validateNode(conf.Layout, "layout", true); len(problems) > 0 {
		return Conf{}, &ConfParseError{Source: source, Details: problems}
	}
	if len([]rune(conf.Archetype)) > maxArchetypeLen {
		return Conf{}, &ConfParseError{Source: source, Details: []string{fmt.Sprintf("archetype: must be at most %d characters", maxArchetypeLen)}}
	}
	return conf, nil
}

// validateNode walks the tree enforcing the schema's semantics. where is
// the human-readable path to this node (tree position), so every problem
// names its field.
func validateNode(n Node, where string, root bool) []string {
	var problems []string
	at := func(field, msg string) string { return fmt.Sprintf("%s: %s: %s", where, field, msg) }
	if root {
		if n.Kind != KindDirectory {
			problems = append(problems, at("kind", `must be "directory" for the layout root`))
		}
		if n.Match != "" {
			problems = append(problems, at("match", "must be empty on the layout root"))
		}
		if n.Format != "" {
			problems = append(problems, at("format", "must be empty on the layout root (formats belong to file nodes)"))
		}
	} else {
		switch n.Kind {
		case KindFile:
			if len(n.Children) > 0 {
				problems = append(problems, at("children", `only "directory" nodes carry children`))
			}
		case KindDirectory:
			if n.Format != "" {
				problems = append(problems, at("format", `only "file" nodes carry a format`))
			}
		default:
			problems = append(problems, at("kind", fmt.Sprintf("must be %q or %q", KindFile, KindDirectory)))
		}
		if n.Match == "" {
			problems = append(problems, at("match", "required on non-root nodes"))
		} else if _, matchErr := filepath.Match(expandWard(n.Match, "ward"), "probe"); matchErr != nil {
			// Malformed glob syntax must fail at parse time: a bad pattern
			// discovered during lint would break the whole vault index.
			problems = append(problems, at("match", fmt.Sprintf("invalid glob pattern %q", n.Match)))
		}
		if n.Format != "" && n.Format != FormatMarkdown && n.Format != FormatRaw {
			problems = append(problems, at("format", fmt.Sprintf("must be %q or %q", FormatMarkdown, FormatRaw)))
		}
	}
	// The contract caps what a response may carry; a conf that exceeds
	// the caps must fail at parse time, not emit invalid responses later.
	if len([]rune(n.ID)) > maxNodeIDLen {
		problems = append(problems, at("id", fmt.Sprintf("must be at most %d characters", maxNodeIDLen)))
	}
	if len([]rune(n.Match)) > maxMatchLen {
		problems = append(problems, at("match", fmt.Sprintf("must be at most %d characters", maxMatchLen)))
	}
	for i, c := range n.Children {
		child := fmt.Sprintf("%s.children[%d]", where, i)
		if c.ID != "" {
			child += fmt.Sprintf(" (%s)", c.ID)
		}
		problems = append(problems, validateNode(c, child, false)...)
	}
	return problems
}
