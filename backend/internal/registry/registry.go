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
	// KindPrompt — Kind values — the registry managed entry kinds.
	KindPrompt Kind = "prompts"
	// KindAgent — one of the Kind values.
	KindAgent Kind = "agents"
	// KindSkill — one of the Kind values.
	KindSkill Kind = "skills"
	// KindHook — one of the Kind values.
	KindHook Kind = "hooks"
	// KindMCP — one of the Kind values.
	KindMCP Kind = "mcps"
	// KindSchedules — the cron schedule kind.
	KindSchedules Kind = "schedules"

	// KindConfigFile names config.yaml change notifications (notify only;
	// the daemon reloads configuration itself).
	KindConfigFile Kind = "config-file"
)

// Kinds lists every file-loading registry kind in canonical order.
var Kinds = []Kind{KindPrompt, KindAgent, KindSkill, KindHook, KindMCP, KindSchedules}

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
	Schedules  string
	ConfigFile string // watched for notifications only
}

func (d Dirs) byKind() map[Kind]string {
	return map[Kind]string{
		KindPrompt:    d.Prompts,
		KindAgent:     d.Agents,
		KindSkill:     d.Skills,
		KindHook:      d.Hooks,
		KindMCP:       d.MCPs,
		KindSchedules: d.Schedules,
	}
}

// Snapshot is an immutable registry state; readers receive it whole and
// reloads swap it atomically, so no reader ever sees partial state.
type Snapshot struct {
	Prompts   map[string]Prompt
	Agents    map[string]AgentSpec
	Skills    map[string]SkillSpec
	Hooks     map[string]HookSpec
	MCPs      map[string]MCPSpec
	Schedules map[string]ScheduleSpec
}

func emptySnapshot() *Snapshot {
	return &Snapshot{
		Prompts:   map[string]Prompt{},
		Agents:    map[string]AgentSpec{},
		Skills:    map[string]SkillSpec{},
		Hooks:     map[string]HookSpec{},
		MCPs:      map[string]MCPSpec{},
		Schedules: map[string]ScheduleSpec{},
	}
}

// Registry holds the current snapshot behind an atomic pointer.
type Registry struct {
	snap atomic.Pointer[Snapshot]
	// reloadMu serializes snapshot swaps (LoadAll, watcher flushes, and
	// mutations). Without it, two concurrent reloads can interleave
	// load→store and silently revert each other's kind rebuild (a
	// lost-update race that is not a data race, so -race cannot catch it).
	reloadMu sync.Mutex

	// dirsPtr holds the last configured Dirs (set by LoadAll/Watch) so
	// mutations can resolve their kind directories.
	dirsPtr atomic.Pointer[Dirs]

	// errByKind holds the most recent per-kind load errors (guarded by
	// reloadMu); errsFlat is the flattened copy published for lock-free
	// readers via Errors(). Errors describe the current snapshot: an
	// entry whose file no longer loads is absent, its error names the
	// file that went away.
	errByKind map[Kind][]LoadError
	errsFlat  atomic.Pointer[[]LoadError]

	// notifyMu guards subs; mutations and watcher flushes deliver
	// entry-level changes through notify.
	notifyMu sync.Mutex
	subs     []func(Change)
}

// New returns a registry with an empty snapshot.
func New() *Registry {
	r := &Registry{errByKind: map[Kind][]LoadError{}}
	r.snap.Store(emptySnapshot())
	return r
}

// Snapshot returns the current immutable state.
func (r *Registry) Snapshot() Snapshot { return *r.snap.Load() }

// Errors returns the most recent load errors across kinds, coherent with
// the current snapshot (refreshed on initial load, watcher reloads, and
// mutations). The returned slice must not be modified.
func (r *Registry) Errors() []LoadError {
	if errs := r.errsFlat.Load(); errs != nil {
		return *errs
	}
	return nil
}

// storeErrorsLocked replaces one kind's load errors and republishes the
// flattened copy. Caller holds reloadMu (or is single-threaded setup).
func (r *Registry) storeErrorsLocked(kind Kind, errs []LoadError) {
	if r.errByKind == nil {
		r.errByKind = map[Kind][]LoadError{}
	}
	r.errByKind[kind] = errs
	r.publishErrorsLocked()
}

func (r *Registry) publishErrorsLocked() {
	flat := make([]LoadError, 0)
	for _, kind := range Kinds {
		flat = append(flat, r.errByKind[kind]...)
	}
	r.errsFlat.Store(&flat)
}

// Subscribe registers an entry-level change callback (adds, edits,
// renames, deletes — from mutations and watcher reloads alike).
// Callbacks run synchronously on the mutating/reloading goroutine and
// must not block.
func (r *Registry) Subscribe(fn func(Change)) {
	r.notifyMu.Lock()
	defer r.notifyMu.Unlock()
	r.subs = append(r.subs, fn)
}

// notify delivers one change to every subscriber.
func (r *Registry) notify(c Change) {
	r.notifyMu.Lock()
	subs := make([]func(Change), len(r.subs))
	copy(subs, r.subs)
	r.notifyMu.Unlock()
	for _, fn := range subs {
		fn(c)
	}
}

// LoadAll reads every file in every configured directory, swaps in a new
// snapshot, and returns the snapshot with all per-file errors it collected.
// A missing directory loads as empty; one bad file never blocks the rest.
func (r *Registry) LoadAll(dirs Dirs) (Snapshot, []LoadError) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	r.dirsPtr.Store(&dirs)
	snap := emptySnapshot()
	var errs []LoadError
	for _, kind := range Kinds {
		loadKind(snap, dirs.byKind()[kind], kind, &errs)
	}
	r.snap.Store(snap)
	byKind := map[Kind][]LoadError{}
	for _, e := range errs {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	r.errByKind = byKind
	r.publishErrorsLocked()
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
	case KindSchedules:
		loadSchedules(snap, dir, errs)
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
