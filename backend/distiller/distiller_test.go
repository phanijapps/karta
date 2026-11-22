package distiller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"zbot/backend/chitta"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
)

// fakeChitta implements the distiller's Chitta subset over a real fake
// driver System (memory semantics for free).
type fakeChitta struct {
	sys *chitta.System
}

func (f fakeChitta) Remember(ctx context.Context, obs []domain.Observation, host domain.ChittaHostContext) ([]domain.RememberResult, error) {
	return f.sys.Remember(ctx, obs, host)
}
func (f fakeChitta) Connect(ctx context.Context, req domain.ConnectRequest, host domain.ChittaHostContext) (domain.Relationship, error) {
	return f.sys.Connect(ctx, req, host)
}
func (f fakeChitta) UpsertEntity(ctx context.Context, req domain.EntityUpsert) (domain.Entity, error) {
	return f.sys.UpsertEntity(ctx, req)
}
func (f fakeChitta) MarkDistilled(ctx context.Context, sessionID string, attempts int, deadLetter bool) error {
	return f.sys.MarkDistilled(ctx, sessionID, attempts, deadLetter)
}
func (f fakeChitta) DistillationMarkers(ctx context.Context) (map[string]domain.DistillationMarker, error) {
	return f.sys.DistillationMarkers(ctx)
}

// fakeLLM returns a canned extraction (or error).
type fakeLLM struct {
	mu       sync.Mutex
	response string
	err      error
	calls    int
}

func (f *fakeLLM) Complete(ctx context.Context, provider, model, system, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.response, f.err
}

// fakeSource serves sessions.
type fakeSource struct{ sessions []Session }

func (f *fakeSource) FinishedSessions(ctx context.Context, limit int) ([]Session, error) {
	return f.sessions, nil
}

type staticSettings struct{ s Settings }

func (s staticSettings) Read(ctx context.Context) (Settings, error) { return s.s, nil }

func newDistillerForTest(t *testing.T, llm LLMCaller, sessions []Session) (*Distiller, *chitta.System, *fakeLLM) {
	t.Helper()
	bus := eventbus.New()
	sys, _ := chitta.OpenInMemory(bus)
	d := New(fakeChitta{sys}, &fakeSource{sessions}, llm,
		staticSettings{Settings{Provider: "ollama", Model: "x", Prompt: defaultPrompt}}, nil)
	return d, sys, llm.(*fakeLLM)
}

func sampleExtraction() string {
	return `{
  "observations": [
    {"kind": "preference", "subject": "user", "content": "Prefers concise answers with code examples",
     "tags": ["user.communication", "epistemic:convention"], "confidence": 0.9, "entities": []},
    {"kind": "fact", "subject": "user", "content": "Uses pnpm as the package manager",
     "tags": ["user.tooling", "epistemic:current"], "confidence": 0.95, "entities": ["pnpm"]},
    {"kind": "observation", "subject": "user", "content": "Delegating chart generation worked well",
     "tags": ["episode", "epistemic:procedural"], "confidence": 0.7, "entities": []}
  ],
  "entities": [
    {"name": "pnpm", "type": "tool", "aliases": ["npm alternative"]}
  ],
  "relationships": [
    {"from": "user", "to": "pnpm", "type": "uses"}
  ]
}`
}

func TestPassDistillsAndMarks(t *testing.T) {
	llm := &fakeLLM{response: sampleExtraction()}
	sess := Session{ID: "s1", ExecutionID: "e1", Finished: true, Messages: []SessionMessage{
		{Role: "user", Content: "I use pnpm. Keep answers concise with code examples."},
		{Role: "assistant", Content: "Noted."},
	}}
	d, sys, _ := newDistillerForTest(t, llm, []Session{sess})
	d.Pass(context.Background())

	// Observations persisted with distiller provenance; transcript text absent.
	page, err := sys.History(context.Background(), domain.HistoryRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("want 3 observations, got %d", len(page.Items))
	}
	found := map[string]bool{}
	for _, r := range page.Items {
		if r.Provenance.Source != domain.SourceDistiller || r.Provenance.SessionID != "s1" {
			t.Fatalf("distiller provenance expected: %+v", r.Provenance)
		}
		if strings.Contains(r.Content, "Noted.") || strings.Contains(r.Content, "I use pnpm. Keep answers") {
			t.Fatal("raw transcript text must never persist")
		}
		found[string(r.Kind)] = true
	}
	if !found["preference"] || !found["fact"] || !found["observation"] {
		t.Fatalf("kinds mapped: %v", found)
	}
	// Markers: s1 marked.
	markers, _ := sys.DistillationMarkers(context.Background())
	if m, ok := markers["s1"]; !ok || m.DeadLetter {
		t.Fatalf("s1 must be marked distilled: %+v", m)
	}
	// Second pass is a no-op.
	d.Pass(context.Background())
	page2, _ := sys.History(context.Background(), domain.HistoryRequest{Limit: 20})
	if len(page2.Items) != 3 {
		t.Fatalf("re-pass must not duplicate: %d", len(page2.Items))
	}
}

func TestMalformedOutputFailsAttempts(t *testing.T) {
	llm := &fakeLLM{response: "not json at all"}
	sess := Session{ID: "s-bad", ExecutionID: "e", Finished: true, Messages: []SessionMessage{{Role: "user", Content: "hi"}}}
	d, sys, _ := newDistillerForTest(t, llm, []Session{sess})
	d.Pass(context.Background())
	markers, _ := sys.DistillationMarkers(context.Background())
	m, ok := markers["s-bad"]
	if !ok || m.Attempts != 1 || m.DeadLetter {
		t.Fatalf("attempt 1 recorded, not dead: %+v", m)
	}
	// Two more failing passes → dead-letter at 3 attempts.
	d.Pass(context.Background())
	d.Pass(context.Background())
	markers, _ = sys.DistillationMarkers(context.Background())
	if m := markers["s-bad"]; !m.DeadLetter {
		t.Fatalf("dead-letter after 3 attempts: %+v", m)
	}
	// Dead-lettered sessions are skipped.
	llm.mu.Lock()
	llm.response = sampleExtraction()
	llm.mu.Unlock()
	d.Pass(context.Background())
	page, _ := sys.History(context.Background(), domain.HistoryRequest{Limit: 20})
	if len(page.Items) != 0 {
		t.Fatal("dead-lettered session must be skipped")
	}
}

func TestRelationshipsAndEntitiesDistilled(t *testing.T) {
	llm := &fakeLLM{response: sampleExtraction()}
	sess := Session{ID: "s-rel", ExecutionID: "e", Finished: true, Messages: []SessionMessage{{Role: "user", Content: "pnpm stuff"}}}
	d, sys, _ := newDistillerForTest(t, llm, []Session{sess})
	d.Pass(context.Background())
	dossier, err := sys.About(context.Background(), domain.ExploreRequest{Target: "user", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	// "user uses pnpm" edge exists (user auto-created by Connect).
	if len(dossier.Neighborhood.Edges) == 0 {
		t.Fatal("relationship must be connected")
	}
}

func TestModelSwapIdempotent(t *testing.T) {
	llm := &fakeLLM{response: sampleExtraction()}
	sess := Session{ID: "s-swap", ExecutionID: "e", Finished: true, Messages: []SessionMessage{{Role: "user", Content: "x"}}}
	d, sys, _ := newDistillerForTest(t, llm, []Session{sess})
	d.Pass(context.Background())
	// "Swap model": same extraction content, different session →
	// converges instead of duplicating (same chitta System).
	sess2 := Session{ID: "s-swap-2", ExecutionID: "e2", Finished: true, Messages: sess.Messages}
	d.source = &fakeSource{sessions: []Session{sess2}}
	d.Pass(context.Background())
	page, _ := sys.History(context.Background(), domain.HistoryRequest{Limit: 20})
	if len(page.Items) != 3 {
		t.Fatalf("cross-session re-extraction must merge (want 3 records, got %d)", len(page.Items))
	}
	for _, r := range page.Items {
		if r.MentionCount < 2 {
			t.Fatalf("distinct sessions must converge confidence/mentions: %+v", r)
		}
	}
}

func TestPromptResolution(t *testing.T) {
	dir := t.TempDir()
	fs := &FileSettings{Dir: dir}
	// No files → embedded default.
	s, _ := fs.Read(context.Background())
	if !strings.Contains(s.Prompt, "memory extraction system") {
		t.Fatal("embedded default expected")
	}
	// File override wins.
	custom := "CUSTOM PROMPT BODY"
	if err := writeFile(filepathJoin(dir, "distillation-prompt.md"), custom); err != nil {
		t.Fatal(err)
	}
	s, _ = fs.Read(context.Background())
	if !strings.Contains(s.Prompt, "CUSTOM PROMPT BODY") {
		t.Fatal("file override expected")
	}
	// JSON prompt wins over file.
	if err := writeFile(filepathJoin(dir, "distillation.json"), `{"provider":"p","model":"m","prompt":"JSON PROMPT"}`); err != nil {
		t.Fatal(err)
	}
	s, _ = fs.Read(context.Background())
	if s.Prompt != "JSON PROMPT" || s.Provider != "p" || s.Model != "m" {
		t.Fatalf("json settings must win: %+v", s)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func filepathJoin(parts ...string) string { return filepath.Join(parts...) }
