package chitta

import (
	"context"
	"strings"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/tools"
)

// --- chitta-binding spec tests ---

func TestToolProviderEnumeratesAllSeven(t *testing.T) {
	s := newTestSystem(t)
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	names := map[string]bool{}
	for _, tl := range prov.Tools() {
		names[tl.Def().Name] = true
		if tl.Def().ParamsJSON == "" || tl.Def().Description == "" {
			t.Fatalf("%s: Def must carry schema + description", tl.Def().Name)
		}
	}
	for _, want := range ToolNames() {
		if !names[want] {
			t.Errorf("tool %s missing from provider", want)
		}
	}
	if len(names) != len(ToolNames()) {
		t.Fatalf("want %d tools, got %d", len(ToolNames()), len(names))
	}
}

func TestProviderRememberPersistsWithFallbackProvenance(t *testing.T) {
	s := newTestSystem(t)
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	tl := toolByName(t, prov, ToolRemember)
	res := tl.Exec(context.Background(), `{"observations":[{"kind":"preference","content":"binding carrier"}]}`)
	if res.Err != nil {
		t.Fatalf("remember through provider: %v", res.Err)
	}
	if !strings.Contains(res.Output, `"created"`) {
		t.Fatalf("dispositions missing from output: %s", res.Output)
	}
	recs, err := s.History(context.Background(), domain.HistoryRequest{Limit: 10})
	if err != nil || len(recs.Items) != 1 {
		t.Fatalf("record must persist: %d err %v", len(recs.Items), err)
	}
	prov2 := recs.Items[0].Provenance
	if prov2.AgentID != "root" || prov2.Source != domain.SourceAgent {
		t.Fatalf("fallback provenance expected: %+v", prov2)
	}
}

func TestProviderContextStampedProvenance(t *testing.T) {
	s := newTestSystem(t)
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	tl := toolByName(t, prov, ToolRemember)
	ctx := context.WithValue(context.Background(), HostContextKey{}, domain.ChittaHostContext{
		Source: domain.SourceAgent, SessionID: "sess-9", ExecutionID: "exec-9", AgentID: "sub-1",
	})
	if res := tl.Exec(ctx, `{"observations":[{"kind":"fact","content":"stamped provenance"}]}`); res.Err != nil {
		t.Fatalf("exec: %v", res.Err)
	}
	recs, _ := s.History(context.Background(), domain.HistoryRequest{Limit: 10})
	if len(recs.Items) != 1 {
		t.Fatal("one record expected")
	}
	p := recs.Items[0].Provenance
	if p.SessionID != "sess-9" || p.ExecutionID != "exec-9" || p.AgentID != "sub-1" {
		t.Fatalf("context-stamped provenance expected: %+v", p)
	}
}

func TestProviderInvalidArgsIsToolError(t *testing.T) {
	s := newTestSystem(t)
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	tl := toolByName(t, prov, ToolRemember)
	res := tl.Exec(context.Background(), `{"observations":[{"kind":"vibe","content":"x"}]}`)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "invalid_input") {
		t.Fatalf("invalid input must be a typed tool error: %v", res.Err)
	}
}

func TestMergedSurfaceExactlyNineUnique(t *testing.T) {
	s := newTestSystem(t)
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	merged := tools.Multi(tools.BuiltinProvider{}, prov)
	names := map[string]bool{}
	for _, tl := range merged.Tools() {
		if names[tl.Def().Name] {
			t.Fatalf("duplicate tool in merged surface: %s", tl.Def().Name)
		}
		names[tl.Def().Name] = true
	}
	if len(names) != 9 { // clock, echo + seven chitta
		t.Fatalf("merged surface must enumerate exactly 9 unique tools, got %d: %v", len(names), names)
	}
}

func TestClosedChittaDegradesVisibly(t *testing.T) {
	s := newTestSystem(t)
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	// Composition-time validation must not panic for the healthy surface.
	_ = tools.Multi(tools.BuiltinProvider{}, prov)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	merged := tools.Multi(tools.BuiltinProvider{}, prov)
	var clockOK, recallErr bool
	for _, tl := range merged.Tools() {
		switch tl.Def().Name {
		case "clock.now":
			if r := tl.Exec(context.Background(), `{}`); r.Err == nil {
				clockOK = true
			}
		case ToolRecall:
			if r := tl.Exec(context.Background(), `{"query":"x"}`); r.Err != nil && strings.Contains(r.Err.Error(), "unavailable") {
				recallErr = true
			}
		}
	}
	if !clockOK || !recallErr {
		t.Fatalf("builtin must work (%v) and chitta must degrade (%v)", clockOK, recallErr)
	}
}

func TestProviderEmitsOneEventPerCall(t *testing.T) {
	bus := eventbus.New()
	s := &System{store: newFakeDriver(), bus: bus}
	prov := s.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
	sub := bus.Subscribe(context.Background(), "s-bind")
	ctx := context.WithValue(context.Background(), HostContextKey{}, domain.ChittaHostContext{SessionID: "s-bind"})
	tl := toolByName(t, prov, ToolRemember)
	if res := tl.Exec(ctx, `{"observations":[{"kind":"fact","content":"event via binding"}]}`); res.Err != nil {
		t.Fatal(res.Err)
	}
	select {
	case ev := <-sub.Events():
		if ev.Kind != domain.EventChittaToolInvoked || ev.ChittaToolInvoked.Tool != ToolRemember {
			t.Fatalf("wrong event: %+v", ev)
		}
	default:
		t.Fatal("binding call must emit exactly one event")
	}
	// Exactly one: the channel must now be empty.
	select {
	case ev := <-sub.Events():
		t.Fatalf("second event emitted: %+v", ev)
	default:
	}
}

func toolByName(t *testing.T, p tools.Provider, name string) tools.Tool {
	t.Helper()
	for _, tl := range p.Tools() {
		if tl.Def().Name == name {
			return tl
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}
