package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/store"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
	"zbot/backend/internal/wardtools"
)

// wardToolSet builds this run's ward tool plane over the sticky host:
//
//   - ROOT runs (no agent, or the root default wiring) get the
//     STRUCTURAL tools (ward_use/ward_create — entering, switching, and
//     creating wards are root-only structural actions; the delegation
//     guard) plus the file tools.
//   - CHILD runs (delegated agents) get the FILE tools only: they act
//     inside the active ward through the bounded plane and read its
//     layout; structural ward mutations belong to the root.
//
// Runs without a host (wards unwired) get nothing.
func wardToolSet(in runInput) []tools.Tool {
	if in.wardHost == nil {
		return nil
	}
	files := wardtools.FileTools{Host: in.wardHost}
	set := []tools.Tool{
		wardtools.ReadTool{F: files},
		wardtools.WriteTool{F: files},
		wardtools.ListTool{F: files},
		wardtools.ShellTool{Host: in.wardHost}, // the execution plane (ward-v2)
	}
	if !in.isChild {
		set = append(set,
			wardtools.UseTool{Host: in.wardHost, Vault: in.wardHost.Vault()},
			wardtools.CreateTool{Host: in.wardHost, Vault: in.wardHost.Vault()},
		)
	}
	return set
}

// SetWardVault wires the ward tool plane: when fn returns a vault,
// root goal runs gain ward_use/ward_create + the ward file tools, and
// the session's active ward is STICKY across runs. Nil disables.
func (a *Adapter) SetWardVault(fn func() *wards.Vault) {
	a.wardVault = fn
}

// wardHost returns the session's sticky ward host, creating it on
// first use (thread-safe; the host itself is mutexed).
func (a *Adapter) wardHost(sessionID string) *wardtools.Host {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.wardHosts == nil {
		a.wardHosts = map[string]*wardtools.Host{}
	}
	h, ok := a.wardHosts[sessionID]
	if !ok {
		h = wardtools.NewHost(a.wardVault())
		if a.wardBinder != nil {
			binder := a.wardBinder
			h.SetOnEnter(func(ward string) { binder(sessionID, ward) })
		}
		// Hosting transitions are observable (constitution rule 4): the
		// session's Timeline carries ward_entered / ward_created with
		// the typed payload — persisted durable-first, then published.
		h.SetOnTransition(func(tr wardtools.Transition) { a.emitWardEvent(sessionID, tr) })
		a.wardHosts[sessionID] = h
	}
	return h
}

// emitWardEvent persists + publishes a ward_entered / ward_created
// Timeline event for the session (attempted-durable-first, the
// adapter's emit posture).
func (a *Adapter) emitWardEvent(sessionID string, tr wardtools.Transition) {
	kind := domain.EventWardEntered
	text := "entered ward " + tr.Ward
	if tr.Previous != "" {
		text = "switched ward " + tr.Previous + " → " + tr.Ward
	}
	if tr.Created {
		kind, text = domain.EventWardCreated, "created ward "+tr.Ward
	}
	payload := &domain.WardEventPayload{Ward: tr.Ward, Archetype: tr.Archetype, Previous: tr.Previous, Digest: tr.Digest}
	// Attributed to the run that triggered the transition (set at every
	// run's start; a session has one live run at a time).
	agent, execID := a.runAttribution(sessionID)
	evt := domain.TimelineEvent{
		ID: a.idGen(), SessionID: sessionID, ExecutionID: execID, Kind: kind, Agent: agent, Text: text, Ward: payload,
		Timestamp: time.Now().UTC(),
	}
	raw, _ := json.Marshal(map[string]any{"text": text, "agent": agent, "ward": payload})
	_ = a.st.AppendEvent(store.Event{ID: evt.ID, SessionID: sessionID, ExecutionID: execID, Kind: string(kind), Payload: string(raw)}) //nolint:errcheck // attempted-durable-first (F-15 posture)
	if a.bus != nil {
		_ = a.bus.Publish(context.Background(), evt) //nolint:errcheck // fire-and-forget emit (F-15) // ctx-ok: F-15
	}
}

// runAttribution remembers the run currently executing in a session
// (agent + execution) so host-level events raised without a runInput —
// ward transitions — still carry attribution (run-legibility).
type runAttribution struct{ agent, execID string }

// setRunAttribution is called at every run's start (root or child).
func (a *Adapter) setRunAttribution(sessionID string, in runInput) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runAttr == nil {
		a.runAttr = map[string]runAttribution{}
	}
	a.runAttr[sessionID] = runAttribution{agent: in.agent(), execID: in.execID}
}

func (a *Adapter) runAttribution(sessionID string) (agent, execID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r, ok := a.runAttr[sessionID]; ok {
		return r.agent, r.execID
	}
	return domain.AgentRoot, ""
}

// filterWardTools applies an agent's tool allowlist to a mounted
// plane (ward tools incl. shell, MCP tools); nil allow = all.
func filterWardTools(set []tools.Tool, allow []string) []tools.Tool {
	if allow == nil {
		return set
	}
	keep := make(map[string]bool, len(allow))
	for _, n := range allow {
		keep[n] = true
	}
	out := set[:0:0]
	for _, t := range set {
		if keep[t.Def().Name] {
			out = append(out, t)
		}
	}
	return out
}

// SetMCPTools wires the retrieval plane: fn returns the live MCP
// tools (nil/empty = none). Mounted on ROOT runs only.
func (a *Adapter) SetMCPTools(fn func() []tools.Tool) { a.mcpTools = fn }

// SetWardBinder wires the session-row ward binding: every ward enter
// (ward_use/ward_create, or EnsureWard) in a session fires fn with the
// session id and ward name (add-chat-modes — the sidebar re-groups and
// the next run pre-loads from the binding). Best-effort by contract.
func (a *Adapter) SetWardBinder(fn func(sessionID, ward string)) { a.wardBinder = fn }

// DropWard clears the session's in-memory ward (Leave): the next run
// starts unhosted instead of pre-loading the ward the user left.
func (a *Adapter) DropWard(sessionID string) {
	if a.wardVault == nil {
		return
	}
	a.wardHost(sessionID).Leave()
}

// EnsureWard re-enters a session's bound ward on a fresh host (daemon
// restart recovery: the binding survives in the session row, the host
// does not). A host that already carries a ward keeps it — its state
// is newer than the row.
func (a *Adapter) EnsureWard(ctx context.Context, sessionID, ward string) error {
	if a.wardVault == nil {
		return fmt.Errorf("engine: wards not wired")
	}
	h := a.wardHost(sessionID)
	if h.Active() != nil {
		return nil
	}
	_, err := h.Reenter(ctx, ward)
	return err
}

// activeWardSection renders the ## Active Ward prompt section for a
// hosted run (the ward's own constitution + layout packet — the
// mini-bot model: a hosted run executes as the ward's agent). Empty
// when the run carries no ward.
func activeWardSection(in runInput) string {
	if in.wardHost == nil {
		return ""
	}
	view := in.wardHost.Active()
	if view == nil {
		return ""
	}
	return strings.Replace(wardtools.Packet(view), "ACTIVE WARD:", "## Active Ward\n", 1)
}
