package registry

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// waitFor polls cond until it holds or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type recorder struct {
	mu      sync.Mutex
	changes []Change
}

func (rec *recorder) record(c Change) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.changes = append(rec.changes, c)
}

func (rec *recorder) count(kind Kind, name string, typ ChangeType) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for _, c := range rec.changes {
		if c.Kind == kind && c.Name == name && c.Type == typ {
			n++
		}
	}
	return n
}

func (rec *recorder) total() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.changes)
}

func startWatch(t *testing.T, d Dirs) (*Registry, *Watcher, *recorder) {
	t.Helper()
	r := New()
	if _, err := r.LoadAll(d); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w, err := r.Watch(ctx, d)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(w.Close)
	rec := &recorder{}
	w.OnChange(rec.record)
	return r, w, rec
}

func TestWatchAddedChangedRemoved(t *testing.T) {
	d := tempDirs(t)
	r, _, rec := startWatch(t, d)

	// Added: a hook manifest appears.
	write(t, filepath.Join(d.Hooks, "guard.yaml"), "event: PreToolUse\ncommand: /bin/true\n")
	waitFor(t, 3*time.Second, func() bool { return rec.count(KindHook, "guard", ChangeAdded) == 1 })
	if _, ok := r.Snapshot().Hooks["guard"]; !ok {
		t.Fatal("added hook not in snapshot")
	}

	// Changed: a prompt appears, then is edited.
	prompt := filepath.Join(d.Prompts, "note.md")
	write(t, prompt, "first")
	waitFor(t, 3*time.Second, func() bool { return rec.count(KindPrompt, "note", ChangeAdded) == 1 })
	write(t, prompt, "second")
	waitFor(t, 3*time.Second, func() bool { return rec.count(KindPrompt, "note", ChangeChanged) == 1 })

	// Removed: an agent spec is deleted.
	agent := filepath.Join(d.Agents, "temp.yaml")
	write(t, agent, "name: temp\nsystem_prompt: x\n")
	waitFor(t, 3*time.Second, func() bool { return rec.count(KindAgent, "temp", ChangeAdded) == 1 })
	if err := removeFile(agent); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return rec.count(KindAgent, "temp", ChangeRemoved) == 1 })
	if _, ok := r.Snapshot().Agents["temp"]; ok {
		t.Fatal("removed agent still in snapshot")
	}
}

func TestWatchSkillDirCreatedMidRun(t *testing.T) {
	d := tempDirs(t)
	r, _, rec := startWatch(t, d)

	write(t, filepath.Join(d.Skills, "pdf-tools", "SKILL.md"),
		"---\nname: pdf-tools\ndescription: PDF extraction\n---\nbody\n")
	waitFor(t, 3*time.Second, func() bool { return rec.count(KindSkill, "pdf-tools", ChangeAdded) == 1 })
	if _, ok := r.Snapshot().Skills["pdf-tools"]; !ok {
		t.Fatal("mid-run skill not in snapshot")
	}
}

func TestWatchBurstCoalesces(t *testing.T) {
	d := tempDirs(t)
	_, _, rec := startWatch(t, d)

	prompt := filepath.Join(d.Prompts, "bursty.md")
	// Three rapid writes within one debounce window: exactly one reload.
	write(t, prompt, "one")
	write(t, prompt, "two")
	write(t, prompt, "three")

	waitFor(t, 3*time.Second, func() bool { return rec.total() >= 1 })
	time.Sleep(500 * time.Millisecond) // let any stray duplicates arrive
	if got := rec.count(KindPrompt, "bursty", ChangeAdded) + rec.count(KindPrompt, "bursty", ChangeChanged); got != 1 {
		t.Fatalf("burst not coalesced: %d notifications for bursty (want 1)", got)
	}
}

func TestWatchConfigFileNotifiesOnly(t *testing.T) {
	d := tempDirs(t)
	_, _, rec := startWatch(t, d)

	write(t, d.ConfigFile, "engine: eino\n")
	waitFor(t, 3*time.Second, func() bool {
		return rec.count(KindConfigFile, "config.yaml", ChangeChanged) == 1
	})
}

func TestWatcherCloseNoLeak(t *testing.T) {
	d := tempDirs(t)
	r := New()
	if _, err := r.LoadAll(d); err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	w, err := r.Watch(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	cancel()
	// The loop goroutine plus fsnotify internals must exit.
	waitFor(t, 2*time.Second, func() bool { return runtime.NumGoroutine() <= before })
}

func removeFile(path string) error {
	return os.Remove(path)
}
