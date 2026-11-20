package providers

import (
	"net/url"

	"zbot/backend/internal/config"
)

// configProviderType names the single supported connection family today.
const configProviderType = config.ProviderTypeOpenAICompatible

// maxProviders mirrors the contract's provider-list bound across both
// declaration sources.
var maxProviders = config.MaxProviders

// validBaseURL enforces the shared entry rule: an absolute http(s) URL.
func validBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
