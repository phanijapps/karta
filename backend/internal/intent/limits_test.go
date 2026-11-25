package intent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

func noopSnap() func() registry.Snapshot {
	return func() registry.Snapshot { return registry.Snapshot{} }
}

// bareContract recommends nothing from the catalog (valid against any
// packet: the cost tests want exactly one attempt).
const bareContract = `{"primary_intent":"x","hidden_intents":[],"solution_path":"","complexity":"S",
"recommended_skills":[],"recommended_agents":[],"recommended_mcp_tools":[],
"ward_recommendation":{"action":"create_new","ward_name":"test-ward","reason":"r"},
"execution_strategy":{"approach":"simple","explanation":"e"}}`

// smallPacket carries the one skill validContract recommends.
func smallPacket() Packet {
	return Packet{Skills: []CatalogEntry{{ID: "clean-code", Description: "coding standards"}}}
}

func bigPacket(n int) Packet {
	p := Packet{}
	for i := 0; i < n; i++ {
		// ~400 chars of description per entry: 60 entries ≈ 24k chars ≈ 6k tokens.
		p.Skills = append(p.Skills, CatalogEntry{ID: fmt.Sprintf("skill-%02d", i), Description: strings.Repeat("d", 400)})
		p.Agents = append(p.Agents, CatalogEntry{ID: fmt.Sprintf("agent-%02d", i), Description: strings.Repeat("a", 400)})
		p.Wards = append(p.Wards, CatalogEntry{ID: fmt.Sprintf("ward-%02d", i), Description: strings.Repeat("w", 400)})
	}
	return p
}

// The analyst asks for the model's FULL window and never trims on a
// normal window: the request carries max_tokens = window and the packet
// is the one it was given.
func TestAnalyzeSendsMaxTokensWindow(t *testing.T) {
	fc := &fakeCompleter{responses: []string{bareContract}, usage: []Usage{{PromptTokens: 1500, CompletionTokens: 200}}}
	a := Analyzer{Completer: fc, Snapshot: noopSnap()}
	pkt := bigPacket(20)
	d, err := a.Analyze(context.Background(), "goal", pkt, "zai", "glm-5.3", Limits{ContextWindow: 128000, Source: domain.WindowTable})
	if err != nil {
		t.Fatal(err)
	}
	if fc.extraBodies[0]["max_tokens"] != 128000 {
		t.Fatalf("max_tokens must equal the window: %v", fc.extraBodies[0])
	}
	if d.Trimmed || len(d.Packet.Skills) != 20 || d.Packet.Skills[0].ID != "skill-00" {
		t.Fatalf("no trimming on a normal window: trimmed=%v packet=%d", d.Trimmed, len(d.Packet.Skills))
	}
	if d.TokensIn != 1500 || d.TokensOut != 200 || d.Attempts != 1 {
		t.Fatalf("usage: %+v", d)
	}
	if d.PromptChars == 0 {
		t.Fatal("prompt size not measured")
	}
}

func TestValveSilentOnNormalWindow(t *testing.T) {
	fc := &fakeCompleter{responses: []string{bareContract}}
	a := Analyzer{Completer: fc, Snapshot: noopSnap()}
	pkt := bigPacket(20)
	d, err := a.Analyze(context.Background(), "goal", pkt, "p", "m", Limits{ContextWindow: 32000, Source: domain.WindowFallback})
	if err != nil {
		t.Fatal(err)
	}
	if d.Trimmed || len(d.Packet.Wards) != 20 {
		t.Fatalf("the 32k fallback must not trim a default packet: trimmed=%v wards=%d", d.Trimmed, len(d.Packet.Wards))
	}
}

// On a tiny window the per-kind caps shrink until the composed prompt
// fits window − 4k (ranked heads stay), the decision still runs, and
// the outcome reports that trimming fired.
func TestValveShrinksCapsOnTinyWindow(t *testing.T) {
	fc := &fakeCompleter{responses: []string{bareContract}}
	a := Analyzer{Completer: fc, Snapshot: noopSnap()}
	pkt := bigPacket(20)
	const window = 8000 // budget 4k tokens: the default prompt (~7k tokens) overflows
	d, err := a.Analyze(context.Background(), "goal", pkt, "p", "m", Limits{ContextWindow: window, Source: domain.WindowFallback})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Trimmed {
		t.Fatal("the valve must fire on a tiny window")
	}
	if estimateTokens(d.PromptChars) > window-OutputReserve {
		t.Fatalf("composed prompt %d tokens still over budget %d", estimateTokens(d.PromptChars), window-OutputReserve)
	}
	// Shrink stops as soon as the prompt fits — kinds that already fit
	// are left alone (wards here); at least one kind gave way.
	if len(d.Packet.Skills) >= 20 && len(d.Packet.Agents) >= 20 && len(d.Packet.Wards) >= 20 {
		t.Fatalf("caps must shrink: skills=%d agents=%d wards=%d", len(d.Packet.Skills), len(d.Packet.Agents), len(d.Packet.Wards))
	}
	if len(d.Packet.Skills) > 0 && d.Packet.Skills[0].ID != "skill-00" {
		t.Fatal("the ranked head must survive the shrink")
	}
	if !strings.Contains(fc.lastSystem, "skill-00") {
		t.Fatal("the shrunk packet is what the model saw")
	}
}

// When even the floors do not fit, the request echo gives way — never
// the rubric.
func TestValveTruncatesRequestLast(t *testing.T) {
	fc := &fakeCompleter{responses: []string{bareContract}}
	a := Analyzer{Completer: fc, Snapshot: noopSnap()}
	long := strings.Repeat("please research everything ", 2000) // ~52k chars
	_, err := a.Analyze(context.Background(), long, bigPacket(20), "p", "m", Limits{ContextWindow: 6000, Source: domain.WindowFallback})
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.lastUser) > (6000-OutputReserve)*4 {
		t.Fatalf("user block %d chars exceeds the whole budget", len(fc.lastUser))
	}
}

// A provider whose output cap sits below the window rejects the
// full-window request: the SAME attempt runs once more without
// max_tokens (not counted as an attempt) and the decision records it.
func TestMaxTokensDroppedOn400(t *testing.T) {
	fc := &fakeCompleter{
		errs:      []error{fmt.Errorf("%w: provider answered 400 Bad Request", ErrRequestRejected)},
		responses: []string{"", validContract},
		usage:     []Usage{{}, {PromptTokens: 900, CompletionTokens: 100}},
	}
	a := Analyzer{Completer: fc, Snapshot: noopSnap()}
	d, err := a.Analyze(context.Background(), "goal", smallPacket(), "deepseek", "chat", Limits{ContextWindow: 32000, Source: domain.WindowFallback})
	if err != nil {
		t.Fatal(err)
	}
	if fc.calls != 2 {
		t.Fatalf("one retry without max_tokens: %d calls", fc.calls)
	}
	if _, has := fc.extraBodies[0]["max_tokens"]; !has {
		t.Fatalf("first body must carry max_tokens: %v", fc.extraBodies[0])
	}
	if _, has := fc.extraBodies[1]["max_tokens"]; has {
		t.Fatalf("retry body must drop max_tokens: %v", fc.extraBodies[1])
	}
	if !d.MaxTokensDropped || d.Attempts != 1 || d.TokensIn != 900 {
		t.Fatalf("decision: %+v", d)
	}
	// Any OTHER failure still degrades at once — and a rejected request
	// never loops: a second rejection (nothing left to drop) degrades.
	fc2 := &fakeCompleter{errs: []error{ErrRequestRejected, ErrRequestRejected}}
	a2 := Analyzer{Completer: fc2, Snapshot: noopSnap()}
	_, err = a2.Analyze(context.Background(), "goal", Packet{}, "p", "m", Limits{ContextWindow: 32000})
	if err == nil || fc2.calls != 2 {
		t.Fatalf("a rejected retry degrades typed: err=%v calls=%d", err, fc2.calls)
	}
}

// Tokens sum over every attempt; attempts count returned completions.
func TestDecisionAccountsUsageAcrossAttempts(t *testing.T) {
	fc := &fakeCompleter{
		responses: []string{"garbage", validContract},
		usage:     []Usage{{PromptTokens: 100, CompletionTokens: 10}, {PromptTokens: 110, CompletionTokens: 20}},
	}
	a := Analyzer{Completer: fc, Snapshot: noopSnap()}
	d, err := a.Analyze(context.Background(), "goal", smallPacket(), "p", "m", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Attempts != 2 || d.TokensIn != 210 || d.TokensOut != 30 {
		t.Fatalf("usage: %+v", d)
	}
}

// Without the usage seam the analyzer still runs (zero usage).
func TestPlainCompleterStillWorks(t *testing.T) {
	pc := &plainOnly{fakeCompleter: &fakeCompleter{responses: []string{validContract}}}
	a := Analyzer{Completer: pc, Snapshot: noopSnap()}
	d, err := a.Analyze(context.Background(), "goal", smallPacket(), "p", "m", Limits{ContextWindow: 32000})
	if err != nil || d.TokensIn != 0 || d.Attempts != 1 {
		t.Fatalf("plain completer: %+v %v", d, err)
	}
}

// plainOnly hides the param/usage seams (the bare Completer contract).
type plainOnly struct{ *fakeCompleter }

func (p *plainOnly) Complete(ctx context.Context, _, _, system, user string) (string, error) {
	s, _, err := p.complete(system, user, nil)
	return s, err
}

func TestCapsShrinkMakesProgressAndRespectsFloors(t *testing.T) {
	c := DefaultCaps()
	next := c.Shrink(100, 40) // ratio 0.4
	if next.Skills != 8 || next.Wards != 25 {
		t.Fatalf("shrink by ratio: %+v", next)
	}
	// A ratio near 1 still makes progress (every cap strictly smaller).
	n := next.Shrink(100, 95)
	if n.Skills >= next.Skills || n.Wards >= next.Wards {
		t.Fatalf("near-1 ratio must still shrink: %+v from %+v", n, next)
	}
	// Never below the floors; at the floors it says so.
	if n := floorCaps.Shrink(100, 1); !n.AtFloors() || n != floorCaps {
		t.Fatalf("floors: %+v", n)
	}
	if floorCaps.AtFloors() != true || DefaultCaps().AtFloors() {
		t.Fatal("AtFloors")
	}
}

func TestEstimateTokens(t *testing.T) {
	for chars, want := range map[int]int{0: 0, 1: 1, 4: 1, 5: 2, 4000: 1000} {
		if got := estimateTokens(chars); got != want {
			t.Errorf("estimateTokens(%d) = %d want %d", chars, got, want)
		}
	}
}
