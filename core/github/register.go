package github

import "github.com/stewartbrothers/gaia/core/provider"

// init self-registers the GitHub adapter with the provider registry
// (#309). Importing this package (directly, or via core/forges for the
// side effect) makes "github" available to provider.Build without
// internal/forgebuilder naming this package.
func init() {
	provider.Register(provider.Registration{
		Name: "github",
		// GitHub.com has a well-known endpoint, so an empty API URL is
		// not an error — NewProvider substitutes the production default.
		// DefaultAPIURL lets forgebuilder show the right host in Info
		// when none is configured.
		DefaultAPIURL: "https://api.github.com",
		TokenEnvNames: []string{"GITHUB_TOKEN", "GH_TOKEN"},
		Factory: func(cfg provider.BuildConfig) (provider.Provider, error) {
			return NewProvider(Options{
				BaseURL: cfg.APIURL,
				Token:   cfg.Token,
				Cache:   cfg.Cache,
			}), nil
		},
	})
}
