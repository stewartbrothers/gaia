package forgejo

import (
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// init self-registers the Forgejo adapter with the provider registry
// (#309). Importing this package (directly, or via core/forges for the
// side effect) makes "forgejo" available to provider.Build without
// internal/forgebuilder naming this package.
func init() {
	provider.Register(provider.Registration{
		Name: "forgejo",
		// Self-hosted-only: there is no universal Forgejo endpoint, so a
		// missing API URL is a usage error, not a default to substitute.
		TokenEnvNames: []string{"FORGEJO_TOKEN", "GITEA_TOKEN"},
		Factory: func(cfg provider.BuildConfig) (provider.Provider, error) {
			if cfg.APIURL == "" {
				return nil, exitcode.Errorf(exitcode.Usage,
					"no API URL configured — run `gaia auth forgejo <url>` or set --api-url/FORGEJO_API_URL")
			}
			return NewProvider(Options{
				BaseURL: cfg.APIURL,
				Token:   cfg.Token,
				Cache:   cfg.Cache,
			}), nil
		},
	})
}
