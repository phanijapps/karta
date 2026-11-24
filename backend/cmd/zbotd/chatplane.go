package main

// The quick-chat tool plane (operator direction, ward-v2 addendum):
// the lean exchange offers the WARD plane — ward_use/ward_create/
// ward_read/ward_write/ward_list/ward_shell — plus the live MCP
// retrieval tools. The builtins (clock, echo) and the chitta tools
// are OFF this surface: memory still rides the ## Context section,
// not tools. One sticky host serves the default session; entering a
// ward composes the ## Active Ward section after the persona.

import (
	"strings"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/mcpclient"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
	"zbot/backend/internal/wardtools"
)

// emptyProvider offers nothing (the chat base plane is the ward+MCP
// set below — clock/echo/chitta stay off quick chat).
type emptyProvider struct{}

func (emptyProvider) Tools() []tools.Tool { return nil }

// wireChatToolPlane mounts the ward + MCP planes on the lean exchange.
func wireChatToolPlane(chatSvc *chat.Service, vault *wards.Vault, mcpExec *mcpclient.Executor) {
	host := wardtools.NewHost(vault)
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
