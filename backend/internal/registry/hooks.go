package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"zbot/backend/internal/domain"
)

// hookEvents is the closed set of lifecycle hook events (from domain).
var hookEvents = map[domain.HookEventName]bool{
	domain.HookPreToolUse:   true,
	domain.HookPostToolUse:  true,
	domain.HookPreCompact:   true,
	domain.HookSessionStart: true,
	domain.HookSessionEnd:   true,
	domain.HookSubagentStop: true,
}

// HookSpec is a Claude Code-style lifecycle hook manifest loaded from
// hooks/*.yaml (top-level root).
type HookSpec struct {
	Event      domain.HookEventName
	Matcher    *regexp.Regexp // nil when the manifest has no matcher
	RawMatcher string
	Command    string
	TimeoutMS  int

	Path string
}

// hookWire is the on-disk manifest shape.
type hookWire struct {
	Event     string `yaml:"event"`
	Matcher   string `yaml:"matcher"`
	Command   string `yaml:"command"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

// loadHooks loads hooks/*.yaml (top-level root) manifests keyed by file stem. The
// event must be one of the six lifecycle names, the optional matcher must
// compile, and the command is required.
func loadHooks(snap *Snapshot, dir string, errs *[]LoadError) {
	for _, e := range readKindDir(dir, KindHook, errs) {
		if e.IsDir() || skipEntry(e.Name()) || !isYAML(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			addErr(errs, path, KindHook, err)
			continue
		}
		var wire hookWire
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&wire); err != nil {
			addErr(errs, path, KindHook, fmt.Errorf("decode: %w", err))
			continue
		}
		spec := HookSpec{Event: domain.HookEventName(wire.Event), RawMatcher: wire.Matcher, Command: wire.Command, TimeoutMS: wire.TimeoutMS, Path: path}
		if !hookEvents[spec.Event] {
			addErr(errs, path, KindHook, fmt.Errorf("unknown event %q", wire.Event))
			continue
		}
		if wire.Matcher != "" {
			matcher, err := regexp.Compile(wire.Matcher)
			if err != nil {
				addErr(errs, path, KindHook, fmt.Errorf("matcher does not compile: %w", err))
				continue
			}
			spec.Matcher = matcher
		}
		if spec.Command == "" {
			addErr(errs, path, KindHook, fmt.Errorf("command is required"))
			continue
		}
		key := stem(e.Name())
		if existing, dup := snap.Hooks[key]; dup {
			addErr(errs, path, KindHook, fmt.Errorf("duplicate hook %q (keeping %s)", key, existing.Path))
			continue
		}
		snap.Hooks[key] = spec
	}
}
