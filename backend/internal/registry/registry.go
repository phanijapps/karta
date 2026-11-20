// Package registry loads z-Bot's control-plane files (prompts, agents,
// skills, hooks, MCP manifests) into immutable snapshots with per-file
// error isolation and debounced fsnotify hot-reload. Nothing is hardcoded:
// if it is not a file under the config tree, the registry does not have it.
package registry

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Kind identifies one registry kind (a config subdirectory).
type Kind string

const (
	KindPrompt Kind = "prompts"
	KindAgent  Kind = "agents"
	KindSkill  Kind = "skills"
	KindHook   Kind = "hooks"
	KindMCP    Kind = "mcps"
	// KindConfigFile names config.yaml change notifications (notify only;
	// the daemon reloads configuration itself).
	KindConfigFile Kind = "config-file"
)

// Kinds lists every file-loading registry kind in canonical order.
var Kinds = []Kind{KindPrompt, KindAgent, KindSkill, KindHook, KindMCP}

// LoadError isolates one file's failure; it never aborts a load.
type LoadError struct {
	Path string
	Kind Kind
	Err  error
}

func (e *LoadError) Error() string {
	return "registry: " + string(e.Kind) + " " + e.Path + ": " + e.Err.Error()
}

// Dirs names the five config directories plus the watched config file. The
// daemon builds it from config.Paths; tests build it directly.
type Dirs struct {
	Prompts    string
	Agents     string
	Skills     string
	Hooks      string
	MCPs       string
	ConfigFile string // watched for notifications only
}

func (d Dirs) byKind() map[Kind]string {
	return map[Kind]string{
		KindPrompt: d.Prompts,
		KindAgent:  d.Agents,
		KindSkill:  d.Skills,
		KindHook:   d.Hooks,
		KindMCP:    d.MCPs,
	}
}

// Snapshot is an immutable registry state; readers receive it whole and
// reloads swap it atomically, so no reader ever sees partial state.
type Snapshot struct {
	Prompts map[string]Prompt
	Agents  map[string]AgentSpec
	Skills  map[string]SkillSpec
	Hooks   map[string]HookSpec
	MCPs    map[string]MCPSpec
}

func emptySnapshot() *Snapshot {
	return &Snapshot{
		Prompts: map[string]Prompt{},
		Agents:  map[string]AgentSpec{},
		Skills:  map[string]SkillSpec{},
		Hooks:   map[string]HookSpec{},
		MCPs:    map[string]MCPSpec{},
	}
}

// Registry holds the current snapshot behind an atomic pointer.
type Registry struct {
	snap atomic.Pointer[Snapshot]
	// reloadMu serializes snapshot swaps (LoadAll and watcher flushes).
	// Without it, two concurrent reloads can interleave load→store and
	// silently revert each other's kind rebuild (a lost-update race that
	// is not a data race, so -race cannot catch it).
	reloadMu sync.Mutex
}

// New returns a registry with an empty snapshot.
func New() *Registry {
	r := &Registry{}
	r.snap.Store(emptySnapshot())
	return r
}

// Snapshot returns the current immutable state.
func (r *Registry) Snapshot() Snapshot { return *r.snap.Load() }

// LoadAll reads every file in every configured directory, swaps in a new
// snapshot, and returns the snapshot with all per-file errors it collected.
// A missing directory loads as empty; one bad file never blocks the rest.
func (r *Registry) LoadAll(dirs Dirs) (Snapshot, []LoadError) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	snap := emptySnapshot()
	var errs []LoadError
	for _, kind := range Kinds {
		loadKind(snap, dirs.byKind()[kind], kind, &errs)
	}
	r.snap.Store(snap)
	return *snap, errs
}

// loadKind dispatches one kind's directory load into the snapshot.
func loadKind(snap *Snapshot, dir string, kind Kind, errs *[]LoadError) {
	switch kind {
	case KindPrompt:
		loadPrompts(snap, dir, errs)
	case KindAgent:
		loadAgents(snap, dir, errs)
	case KindSkill:
		loadSkills(snap, dir, errs)
	case KindHook:
		loadHooks(snap, dir, errs)
	case KindMCP:
		loadMCPs(snap, dir, errs)
	}
}

// readKindDir returns sorted entries for dir; a missing dir is an empty
// plane (no error), other read failures are reported per-file.
func readKindDir(dir string, kind Kind, errs *[]LoadError) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		addErr(errs, dir, kind, err)
		return nil
	}
	return entries
}

func addErr(errs *[]LoadError, path string, kind Kind, err error) {
	*errs = append(*errs, LoadError{Path: path, Kind: kind, Err: err})
}

// skipEntry reports whether a directory entry is registry noise: hidden
// files, editor temp files, or swap files.
func skipEntry(name string) bool {
	return strings.HasPrefix(name, ".") ||
		strings.HasSuffix(name, "~") ||
		strings.HasSuffix(name, ".swp")
}

func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

func stem(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}
