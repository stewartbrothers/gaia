package aikestra

import (
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// unsupported is everything Aikestra's API v1 does not offer (#404).
// Consumers read it by provider name to hide those commands and tools;
// the matching methods return exitcode.NotImplemented for direct callers.
var unsupported = []provider.Capability{
	provider.CapPullRequests,
	provider.CapWikis,
	provider.CapReleases,
	provider.CapWebhooks,
	provider.CapPackages,
	provider.CapActions,
	provider.CapMilestones,
	provider.CapBranchProtection,
	provider.CapSecrets,
	provider.CapVariables,
	provider.CapRunners,
	provider.CapCollaborators,
	provider.CapLabels,
}

// init self-registers the Aikestra adapter with the provider registry.
func init() {
	provider.Register(provider.Registration{
		Name: "aikestra",
		// Self-hosted-only: there is no universal Aikestra endpoint, so a
		// missing API URL is a usage error. The git clone origin and the
		// API origin can differ, so it is never derived from a remote.
		TokenEnvNames: []string{"AIKESTRA_TOKEN"},
		Unsupported:   unsupported,
		Factory: func(cfg provider.BuildConfig) (provider.Provider, error) {
			if cfg.APIURL == "" {
				return nil, exitcode.Errorf(exitcode.Usage,
					"no API URL configured — run `gaia auth aikestra <api-url>` or set --api-url")
			}
			return NewProvider(Options{
				BaseURL: cfg.APIURL,
				Token:   cfg.Token,
			}), nil
		},
	})
}
