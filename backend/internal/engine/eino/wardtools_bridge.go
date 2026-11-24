package eino

import (
	"context"
	"fmt"
	"strings"
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
		a.wardHosts[sessionID] = h
	}
	return h
}

// SetWardBinder wires the session-row ward binding: every ward enter
// (ward_use/ward_create, or EnsureWard) in a session fires fn with the
// session id and ward name (add-chat-modes — the sidebar re-groups and
// the next run pre-loads from the binding). Best-effort by contract.
func (a *Adapter) SetWardBinder(fn func(sessionID, ward string)) { a.wardBinder = fn }

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
