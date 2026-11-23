package eino

import (
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
		a.wardHosts[sessionID] = h
	}
	return h
}
