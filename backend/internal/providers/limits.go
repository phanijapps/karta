package providers

// Model-derived limits (intent-analyst-agent S1): the context window of
// a model comes from (1) the provider itself at refresh — Ollama's
// native /api/show reports `<family>.context_length`, one suffix match
// covers every family; (2) the limits table — a small built-in plus the
// user-editable config/providers/model_limits.yaml keyed by model-id
// prefix; (3) the 32,000 fallback. No model reports a separate output
// cap through these paths, so one number — the window — is the model's
// limit. providers.ResolveWindow is the single chain every consumer
// (the intent wrapper's seam, the settings derivation, the test
// endpoint) walks.

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WindowSource names where a resolved context window came from.
type WindowSource string

const (
	// WindowOllama — discovered from the provider at refresh.
	WindowOllama WindowSource = "ollama"
	// WindowTable — the built-in or user-editable limits table.
	WindowTable WindowSource = "table"
	// WindowFallback — unknown model: the conservative default.
	WindowFallback WindowSource = "fallback"
)

// FallbackContextWindow is the window assumed for a model nobody
// reports: a size every current provider exceeds — conservative, never
// optimistic (operator decision 2026-11-24).
const FallbackContextWindow = 32000

// LimitsFileName is the user-editable table under config/providers/.
const LimitsFileName = "model_limits.yaml"

// LimitsStem is the reserved provider-file stem the table occupies
// (never a provider name).
const LimitsStem = "model_limits"

type limitEntry struct {
	prefix string // match: model id starts with prefix
	window int
}

// builtinLimits are the shipped entries (model-id prefix → window).
// Deliberately tiny: numbers are recorded only when verified.
var builtinLimits = []limitEntry{
	{"glm-5.3", 128000},
}

// Limits is an immutable prefix → window table (longest match wins).
type Limits struct {
	entries []limitEntry
}

// NewLimits merges entries into one table; later entries with the same
// prefix override earlier ones (file over built-in).
func NewLimits(builtin, file []limitEntry) *Limits {
	byPrefix := map[string]int{}
	for _, e := range builtin {
		byPrefix[e.prefix] = e.window
	}
	for _, e := range file {
		byPrefix[e.prefix] = e.window
	}
	entries := make([]limitEntry, 0, len(byPrefix))
	for p, w := range byPrefix {
		entries = append(entries, limitEntry{prefix: p, window: w})
	}
	// Longest prefix first so the most specific match wins.
	sort.Slice(entries, func(i, j int) bool {
		if len(entries[i].prefix) != len(entries[j].prefix) {
			return len(entries[i].prefix) > len(entries[j].prefix)
		}
		return entries[i].prefix < entries[j].prefix
	})
	return &Limits{entries: entries}
}

// BuiltinLimits returns the shipped table.
func BuiltinLimits() *Limits { return NewLimits(builtinLimits, nil) }

// Lookup resolves a model id against the table.
func (l *Limits) Lookup(modelID string) (int, bool) {
	if l == nil {
		return 0, false
	}
	for _, e := range l.entries {
		if e.window > 0 && len(e.prefix) > 0 && len(modelID) >= len(e.prefix) && modelID[:len(e.prefix)] == e.prefix {
			return e.window, true
		}
	}
	return 0, false
}

// limitsFile is the on-disk shape (strict: unknown keys rejected).
type limitsFile struct {
	Models map[string]int `yaml:"models"`
}

// LoadLimitsFile reads config/providers/model_limits.yaml. An absent
// file is an empty table; a present file must decode strictly.
func LoadLimitsFile(dir string) ([]limitEntry, error) {
	raw, err := os.ReadFile(filepath.Join(dir, LimitsFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f limitsFile
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	entries := make([]limitEntry, 0, len(f.Models))
	for pattern, window := range f.Models {
		// A trailing `*` is the documented glob form; both it and the
		// bare prefix match the same way.
		prefix := strings.TrimSuffix(pattern, "*")
		if prefix == "" || window <= 0 {
			continue
		}
		entries = append(entries, limitEntry{prefix: prefix, window: window})
	}
	return entries, nil
}

// reloadLimitsLocked re-reads the limits table (hot-reloaded with the
// directory). A broken table keeps the last good one serving and is
// logged — unlike a broken provider file it never blocks the set.
func (s *Store) reloadLimitsLocked() {
	file, err := LoadLimitsFile(s.dir)
	if err != nil {
		log.Printf("providers: %s: %v (last good limits table serving)", LimitsFileName, err)
		return
	}
	s.limits.Store(NewLimits(builtinLimits, file))
}

// Limits returns the current model-limits table (never nil).
func (s *Store) Limits() *Limits {
	if l := s.limits.Load(); l != nil {
		return l
	}
	return BuiltinLimits()
}

// ResolveWindow walks the chain for one catalog model: discovered →
// table → fallback.
func ResolveWindow(m Model, limits *Limits) (int, WindowSource) {
	if m.ContextWindow > 0 {
		return m.ContextWindow, WindowOllama
	}
	if w, ok := limits.Lookup(m.ID); ok {
		return w, WindowTable
	}
	return FallbackContextWindow, WindowFallback
}
