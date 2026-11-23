package main

import (
	"strings"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
	"zbot/backend/internal/wardtools"
)

// wireChatWardPlane mounts the ward tool plane + Active Ward section on
// the chat service (the chat IS the research surface — operator
// direction): one sticky host for the chat session; the conversation
// absorbs the ward's constitution when a ward is entered.
func wireChatWardPlane(chatSvc *chat.Service, vault *wards.Vault) {
	host := wardtools.NewHost(vault)
	chatSvc.SetExtraTools(func() []tools.Tool { return wardtools.ChatToolSet(host) })
	chatSvc.SetActiveWard(func() string {
		view := host.Active()
		if view == nil {
			return ""
		}
		return strings.Replace(wardtools.Packet(view), "ACTIVE WARD:", "## Active Ward\n", 1)
	})
}
