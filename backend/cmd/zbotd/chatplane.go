package main

// The quick-chat tool plane (operator direction, ward-v2 addendum):
// the lean exchange offers the WARD plane — ward_use/ward_create/
// read/write/ward_list/shell — plus the live MCP
// retrieval tools. The builtins (clock, echo) and the chitta tools
// are OFF this surface: memory still rides the ## Context section,
// not tools. One sticky host serves the default session; entering a
// ward composes the ## Active Ward section after the persona.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/eventbus"
	"zbot/backend/internal/mcpclient"
	zbotstore "zbot/backend/internal/store"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
	"zbot/backend/internal/wardtools"
)

// emptyProvider offers nothing (the chat base plane is the ward+MCP
// set below — clock/echo/chitta stay off quick chat).
type emptyProvider struct{}

func (emptyProvider) Tools() []tools.Tool { return nil }

// wireChatToolPlane mounts the ward + MCP planes on the lean exchange.
func wireChatToolPlane(chatSvc *chat.Service, vault *wards.Vault, mcpExec *mcpclient.Executor, db *zbotstore.Store, bus *eventbus.Bus) {
	host := wardtools.NewHost(vault)
	// Hosting transitions are observable on the chat surface too (the
	// lean exchange emits session/tool rows, not Timeline events — the
	// host's own emitter covers the ward_entered / ward_created events
	// for the default session). Attempted-durable-first, then published
	// regardless (the adapter's F-15 posture).
	host.SetOnTransition(func(tr wardtools.Transition) {
		kind, text := domain.EventWardEntered, "entered ward "+tr.Ward
		if tr.Previous != "" {
			text = "switched ward " + tr.Previous + " → " + tr.Ward
		}
		if tr.Created {
			kind, text = domain.EventWardCreated, "created ward "+tr.Ward
		}
		payload := &domain.WardEventPayload{Ward: tr.Ward, Archetype: tr.Archetype, Previous: tr.Previous, Digest: tr.Digest}
		id := fmt.Sprintf("evt-ward-%d", time.Now().UnixNano())
		raw, _ := json.Marshal(map[string]any{"text": text, "ward": payload})
		_ = db.AppendEvent(zbotstore.Event{ID: id, SessionID: chat.DefaultSessionID, Kind: string(kind), Payload: string(raw)}) //nolint:errcheck // attempted-durable-first
		_ = bus.Publish(context.Background(), domain.TimelineEvent{                                                             //nolint:errcheck // fire-and-forget // ctx-ok: F-15
			ID: id, SessionID: chat.DefaultSessionID, Kind: kind, Text: text, Ward: payload, Timestamp: time.Now().UTC(),
		})
	})
	chatSvc.SetExtraTools(func() []tools.Tool {
		set := wardtools.ChatToolSet(host)
		if mcpExec != nil {
			set = append(set, mcpExec.Tools()...)
		}
		return set
	})
	chatSvc.SetActiveWard(func() string {
		view := host.Active()
		if view == nil {
			return ""
		}
		return strings.Replace(wardtools.Packet(view), "ACTIVE WARD:", "## Active Ward\n", 1)
	})
}
