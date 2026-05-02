package cli

import (
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/internal/forgebuilder"
)

// providerInfo carries the metadata a subcommand needs to render
// alongside the Provider's own results (host name in `whoami`, etc.).
// Local alias of forgebuilder.Info so subcommands keep their existing
// type reference; the underlying value is identical.
type providerInfo = forgebuilder.Info

// buildForgejoProvider is the legacy name kept for backward-compat
// with subcommand call sites. It now returns the provider.Provider
// interface (was *forgejo.Provider) so calls dispatch by the
// resolved provider — Forgejo or GitHub. The name stays
// "buildForgejoProvider" to avoid touching every CLI file in this
// commit; renaming is a follow-up cosmetic.
func buildForgejoProvider(flags *globalFlags) (provider.Provider, *providerInfo, error) {
	return forgebuilder.Build(forgebuilder.Override{
		Profile:  flags.Profile,
		Provider: flags.Provider,
		APIURL:   flags.APIURL,
	})
}
