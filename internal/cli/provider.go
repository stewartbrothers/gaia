package cli

import (
	"net/url"

	"github.com/stewartbrothers/gaia/core/auth"
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

// buildForgejoProvider resolves config + flags + stored credentials
// into a *forgejo.Provider ready to use.
//
// Resolution order:
//
//  1. config.Resolve gives us the user's explicit choices (flags > env > config).
//  2. If the resulting Resolved is incomplete (missing provider, URL, or
//     token), we look up stored credentials from core/auth.
//  3. The first single-credential case (no provider/URL specified, exactly
//     one credential stored) becomes the implicit choice — this is the
//     post-`gaia auth forgejo` dogfood path.
//
// Phase 1 only supports forgejo; github surfaces a not-implemented
// error here so callers don't have to special-case it.
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

	creds, err := loadLayeredCredentials()
	if err != nil {
		return nil, nil, err
	}

	// If config gave us nothing usable, fall back to a stored credential.
	if resolved.Provider == "" && resolved.APIURL == "" {
		if soleProvider, soleHost, soleCred, ok := creds.SingleCredential(); ok {
			resolved.Provider = soleProvider
			resolved.APIURL = soleCred.APIURL
			if resolved.Token == "" {
				resolved.Token = soleCred.Token
			}
			_ = soleHost // host re-derived from APIURL below for symmetry
		}
	}

	if resolved.Provider == "" {
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"no provider configured — run `gaia auth forgejo <url>` or set --provider/GAIA_PROVIDER (see docs/configuration.md)")
	}
	if resolved.Provider != "forgejo" {
		return nil, nil, exitcode.Errorf(exitcode.Generic,
			"%s provider not yet implemented (Phase 2 — see https://github.com/stewartbrothers/gaia/issues/2)",
			resolved.Provider)
	}
	if resolved.APIURL == "" {
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"no API URL configured — run `gaia auth forgejo <url>` or set --api-url/FORGEJO_API_URL")
	}

	host := ""
	if u, perr := url.Parse(resolved.APIURL); perr == nil {
		host = u.Host
	}

	// Fill in token from credentials store when env/config didn't provide one.
	if resolved.Token == "" {
		if c, _, ok := creds.Get(resolved.Provider, host); ok {
			resolved.Token = c.Token
		}
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

// layeredCredentials wraps auth.Layered with a small helper.
type layeredCredentials struct{ *auth.Layered }

// SingleCredential returns the one stored credential when there's
// exactly one across both layers (project entries shadow same-host
// global entries). Used by buildForgejoProvider to make
// `gaia <cmd>` work after `gaia auth forgejo <url>` with no other
// config or flags.
func (l *layeredCredentials) SingleCredential() (provider, host string, cred auth.Credential, ok bool) {
	seen := map[string]struct{}{}
	var items []struct {
		provider, host string
		cred           auth.Credential
	}
	collect := func(s *auth.Store, source string) {
		if s == nil {
			return
		}
		for _, key := range s.Hosts() {
			parts := splitProviderHost(key)
			if parts == nil {
				continue
			}
			pkey := parts[0] + ":" + parts[1]
			if _, dup := seen[pkey]; dup && source == "global" {
				// project already shadowed it
				continue
			}
			seen[pkey] = struct{}{}
			c, _ := s.Get(parts[0], parts[1])
			items = append(items, struct {
				provider, host string
				cred           auth.Credential
			}{parts[0], parts[1], c})
		}
	}
	collect(l.Project, "project")
	collect(l.Global, "global")

	if len(items) != 1 {
		return "", "", auth.Credential{}, false
	}
	return items[0].provider, items[0].host, items[0].cred, true
}

func splitProviderHost(key string) []string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return nil
}

// loadLayeredCredentials reads global and project credential stores
// (best-effort; missing stores yield empty values, not errors).
func loadLayeredCredentials() (*layeredCredentials, error) {
	globalPath, err := auth.DefaultGlobalPath()
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "locate global credentials")
	}
	g, err := auth.Load(globalPath)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "load global credentials")
	}
	var p *auth.Store
	if root := auth.ProjectRoot("."); root != "" {
		p, err = auth.Load(auth.ProjectPath(root))
		if err != nil {
			return nil, exitcode.Wrap(err, exitcode.Generic, "load project credentials")
		}
	}
	return &layeredCredentials{Layered: &auth.Layered{Global: g, Project: p}}, nil
}
