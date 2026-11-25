package providers

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The resolution chain: discovered → table → fallback, with the source
// reported alongside.
func TestResolveWindowChain(t *testing.T) {
	limits := NewLimits(builtinLimits, []limitEntry{{prefix: "deepseek-v4", window: 163840}})
	cases := []struct {
		m    Model
		want int
		src  WindowSource
	}{
		{Model{ID: "glm5:latest", ContextWindow: 1048576}, 1048576, WindowOllama},
		{Model{ID: "glm-5.3-flash"}, 128000, WindowTable},                     // built-in
		{Model{ID: "deepseek-v4.1-flash"}, 163840, WindowTable},               // file entry
		{Model{ID: "glm-5.3-flash", ContextWindow: 4096}, 4096, WindowOllama}, // discovered beats table
		{Model{ID: "mystery"}, 32000, WindowFallback},
	}
	for _, c := range cases {
		w, src := ResolveWindow(c.m, limits)
		if w != c.want || src != c.src {
			t.Errorf("%+v: got %d/%s want %d/%s", c.m, w, src, c.want, c.src)
		}
	}
	if _, src := ResolveWindow(Model{ID: "x"}, nil); src != WindowFallback {
		t.Fatal("a nil table still resolves to the fallback")
	}
}

// The file is strict, overrides the built-in table, and accepts the
// documented glob form; an absent file is an empty table.
func TestLimitsFileOverridesBuiltIn(t *testing.T) {
	dir := t.TempDir()
	if entries, err := LoadLimitsFile(dir); err != nil || entries != nil {
		t.Fatalf("absent file: %v %v", entries, err)
	}
	file := filepath.Join(dir, LimitsFileName)
	raw := "models:\n  \"glm-5.3*\": 200000\n  \"gpt-x*\": 400000\n  \"bad\": 0\n"
	if err := os.WriteFile(file, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadLimitsFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	limits := NewLimits(builtinLimits, entries)
	if w, ok := limits.Lookup("glm-5.3-flash"); !ok || w != 200000 {
		t.Fatalf("file must override built-in: %d %v", w, ok)
	}
	if w, ok := limits.Lookup("gpt-x-1"); !ok || w != 400000 {
		t.Fatalf("glob form matches as a prefix: %d %v", w, ok)
	}
	if _, ok := limits.Lookup("bad"); ok {
		t.Fatal("a non-positive window is dropped")
	}
	// Unknown key → strict error (the table is user-edited; silent
	// typos would hide a mistyped prefix).
	if err := os.WriteFile(file, []byte("modelz:\n  a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLimitsFile(dir); err == nil {
		t.Fatal("unknown key must fail strictly")
	}
	// Longest prefix wins when two entries overlap.
	l2 := NewLimits([]limitEntry{{prefix: "glm", window: 1000}, {prefix: "glm-5", window: 2000}}, nil)
	if w, _ := l2.Lookup("glm-5.3"); w != 2000 {
		t.Fatalf("longest prefix wins: %d", w)
	}
	// The YAML glob form decodes as written.
	var f limitsFile
	if err := yaml.Unmarshal([]byte(raw), &f); err != nil || f.Models["glm-5.3*"] != 200000 {
		t.Fatalf("yaml shape: %v %v", f, err)
	}
}

// The limits table shares config/providers/ without entering the
// provider namespace: its stem is reserved and its file is skipped by
// the provider loader; the store serves the merged table.
func TestModelLimitsStemReserved(t *testing.T) {
	if ValidManagedName(LimitsStem) {
		t.Fatal("model_limits must not be a valid provider name")
	}
	s, _, paths := newTestStore(t)
	if err := os.WriteFile(filepath.Join(paths.ProvidersDir, LimitsFileName), []byte("models:\n  \"zz-*\": 123000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatalf("the limits file must not break the provider reload: %v", err)
	}
	if files, errs := loadProviderFiles(paths.ProvidersDir); len(errs) != 0 || len(files) != 0 {
		t.Fatalf("limits file leaked into the provider set: %v %v", files, errs)
	}
	if w, ok := s.Limits().Lookup("zz-1"); !ok || w != 123000 {
		t.Fatalf("store serves the file table: %d %v", w, ok)
	}
	// A broken table keeps the last good one serving.
	if err := os.WriteFile(filepath.Join(paths.ProvidersDir, LimitsFileName), []byte("nope: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s.Reload()
	if w, ok := s.Limits().Lookup("zz-1"); !ok || w != 123000 {
		t.Fatalf("last good table must keep serving after a broken edit: %d %v", w, ok)
	}
}
