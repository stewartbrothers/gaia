package cli

import (
	"net/url"

	"github.com/stewartbrothers/gaia/core/config"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

// providerInfo carries the metadata a subcommand needs to render
// alongside the Provider's own results (host name in `whoami`, etc.).
type providerInfo struct {
	Provider string
	Host     string
	APIURL   string
}

// buildForgejoProvider resolves config + flags into a *forgejo.Provider
// ready to use. Phase 1 only supports forgejo; github surfaces a
// not-implemented error here so callers don't have to special-case
// the unimplemented path themselves.
func buildForgejoProvider(flags *globalFlags) (*forgejo.Provider, *providerInfo, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, nil, exitcode.Wrap(err, exitcode.Generic, "locate config")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, exitcode.Wrap(err, exitcode.Generic, "load config")
	}
	resolved, err := config.Resolve(cfg, config.Override{
		Profile:  flags.Profile,
		Provider: flags.Provider,
		APIURL:   flags.APIURL,
	})
	if err != nil {
		return nil, nil, exitcode.Wrap(err, exitcode.Usage, "resolve config")
	}

	if resolved.Provider == "" {
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"no provider configured — set --provider, GAIA_PROVIDER, or default_profile in config (see docs/configuration.md)")
	}
	if resolved.Provider != "forgejo" {
		return nil, nil, exitcode.Errorf(exitcode.Generic,
			"%s provider not yet implemented (Phase 2 — see https://github.com/stewartbrothers/gaia/issues/2)",
			resolved.Provider)
	}
	if resolved.APIURL == "" {
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"no API URL configured — set --api-url, FORGEJO_API_URL, or api_url in config")
	}

	host := ""
	if u, perr := url.Parse(resolved.APIURL); perr == nil {
		host = u.Host
	}

	p := forgejo.NewProvider(forgejo.Options{
		BaseURL: resolved.APIURL,
		Token:   resolved.Token,
	})
	return p, &providerInfo{
		Provider: resolved.Provider,
		Host:     host,
		APIURL:   resolved.APIURL,
	}, nil
}
