package cli

import (
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/internal/forgebuilder"
)

// providerInfo carries the metadata a subcommand needs to render
// alongside the Provider's own results (host name in `whoami`, etc.).
// Local alias of forgebuilder.Info so subcommands keep their existing
// type reference; the underlying value is identical.
type providerInfo = forgebuilder.Info

// buildForgejoProvider resolves config + flags + stored credentials
// into a *forgejo.Provider ready to use. Delegates to
// internal/forgebuilder so cmd/gaia and cmd/gaia-mcp stay in
// lock-step on auth resolution.
func buildForgejoProvider(flags *globalFlags) (*forgejo.Provider, *providerInfo, error) {
	return forgebuilder.Build(forgebuilder.Override{
		Profile:  flags.Profile,
		Provider: flags.Provider,
		APIURL:   flags.APIURL,
	})
}
