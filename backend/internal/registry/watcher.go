package registry

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Debounce is the coalescing window per path; editor save bursts within it
// collapse into one reload.
const Debounce = 150 * time.Millisecond

// ChangeType is what happened to one registry entry.
type ChangeType string

const (
	// ChangeAdded — ChangeType values — watcher event kinds.
	ChangeAdded ChangeType = "added"
	// ChangeChanged — one of the ChangeType values.
	ChangeChanged ChangeType = "changed"
	// ChangeRemoved — one of the ChangeType values.
	ChangeRemoved ChangeType = "removed"
)

// Change is one entry-level notification delivered to subscribers.
type Change struct {
	Kind Kind
	Name string
	Type ChangeType
}

// Watcher hot-reloads registry entries on filesystem changes under the
// configured directories, with a path-keyed debounce and whole-kind
// rebuilds. Truth is the filesystem: after the debounce window the kind is
// re-read from disk and diffed, so event-type quirks across platforms
// cannot corrupt state.
type Watcher struct {
	reg  *Registry
	dirs Dirs
	fw   *fsnotify.Watcher

	mu      sync.Mutex
	pending map[string]*time.Timer
	quit    chan struct{}
	quitOne sync.Once
}

// Watch starts hot-reload watching for the registry. It walks the config
// directories adding fsnotify watches (new directories are watched and
// scanned as they appear), debounces bursts per path, rebuilds the
// affected kind, diffs against the previous snapshot, and notifies
// subscribers of entry-level changes. The watcher runs until ctx is
// cancelled or Close is called.
func (r *Registry) Watch(ctx context.Context, dirs Dirs) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	r.dirsPtr.Store(&dirs)
	w := &Watcher{reg: r, dirs: dirs, fw: fw, pending: map[string]*time.Timer{}, quit: make(chan struct{})}
	for _, dir := range dirs.byKind() {
		w.watchTree(dir)
	}
	if dirs.ConfigFile != "" {
		_ = fw.Add(filepath.Dir(dirs.ConfigFile))
	}
	go w.loop(ctx)
	return w, nil
}

// OnChange registers a subscriber callback for entry-level changes.
// Callbacks are invoked synchronously from watcher debounce goroutines
// and registry mutations: they must be safe for concurrent use and must
// not block (a blocking callback stalls that reload's swap and
// notification).
func (w *Watcher) OnChange(fn func(Change)) {
	w.reg.Subscribe(fn)
}

// Close stops the watcher and its debounce timers. It is idempotent.
func (w *Watcher) Close() {
	w.quitOne.Do(func() {
		close(w.quit)
		_ = w.fw.Close()
		w.mu.Lock()
		for _, timer := range w.pending {
			timer.Stop()
		}
		w.pending = map[string]*time.Timer{}
		w.mu.Unlock()
	})
}

// watchTree adds an fsnotify watch to root and every directory under it
// (fsnotify has no recursive watches). Missing roots are ignored.
func (w *Watcher) watchTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = w.fw.Add(path)
		}
		return nil
	})
}

func (w *Watcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			w.Close()
			return
		case <-w.quit:
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		case err := <-w.fw.Errors:
			// Watch errors are non-fatal; the debounce-then-reread design
			// self-corrects on the next event.
			_ = err
		}
	}
}

func (w *Watcher) handleEvent(ev fsnotify.Event) {
	base := filepath.Base(ev.Name)
	if skipEntry(base) {
		return
	}
	// A newly created directory needs watches before its files generate
	// events, and a scan of its contents; only registry-kind directories
	// qualify (never data/ or wards/ under the home root — watching the
	// SQLite tree would churn no-op flushes on every write).
	if ev.Has(fsnotify.Create) {
		if _, ok := w.classify(ev.Name); ok {
			if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
				w.watchTree(ev.Name)
			}
		}
	}
	w.schedule(ev.Name)
}

// schedule arms (or resets) the debounce timer for one path.
func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer, ok := w.pending[path]; ok {
		timer.Reset(Debounce)
		return
	}
	timer := time.AfterFunc(Debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()
		w.flush(path)
	})
	w.pending[path] = timer
}

// flush reloads the kind owning path: after the debounce window the whole
// kind directory is re-read from disk (truth is the filesystem), diffed
// against the previous snapshot, swapped in, and changes are notified.
func (w *Watcher) flush(path string) {
	select {
	case <-w.quit:
		return
	default:
	}

	// Serialized with LoadAll, mutations, and other flushes: load→store→diff
	// must be atomic per reload or a concurrent flush reverts this one
	// (lost update).
	w.reg.reloadMu.Lock()
	defer w.reg.reloadMu.Unlock()

	if w.dirs.ConfigFile != "" && path == w.dirs.ConfigFile {
		w.reg.notify(Change{Kind: KindConfigFile, Name: "config.yaml", Type: ChangeChanged})
		return
	}

	kind, ok := w.classify(path)
	if !ok {
		return // outside every registry directory
	}

	// Rebuild the kind from disk (truth is the filesystem) and publish.
	// reloadKindLocked is the shared publish path: it refreshes the kind's
	// load errors and emits the whole-kind diff — post-mutation settles
	// diff against the already-published snapshot and emit nothing when
	// nothing changed, while external edits absorbed here notify once.
	w.reg.reloadKindLocked(kind)
}

// classify maps a filesystem path back to its registry kind.
func (w *Watcher) classify(path string) (Kind, bool) {
	for _, kind := range Kinds {
		dir := w.dirs.byKind()[kind]
		if dir == "" {
			continue
		}
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return kind, true
		}
	}
	return "", false
}

// diffMaps compares old and new maps for one kind, emitting added and
// removed names and changed only for real content differences
// (reflect.DeepEqual) — an identical re-read (a mutation's own watcher
// settle, a touch, an editor save with no diff) emits nothing.
func diffMaps[T any](kind Kind, old, new map[string]T, emit func(Change)) {
	for name, entry := range new {
		prev, existed := old[name]
		if !existed {
			emit(Change{Kind: kind, Name: name, Type: ChangeAdded})
		} else if !reflect.DeepEqual(prev, entry) {
			emit(Change{Kind: kind, Name: name, Type: ChangeChanged})
		}
	}
	for name := range old {
		if _, still := new[name]; !still {
			emit(Change{Kind: kind, Name: name, Type: ChangeRemoved})
		}
	}
}
