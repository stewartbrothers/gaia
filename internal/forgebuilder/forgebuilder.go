// Package forgebuilder builds a configured provider.Provider from
// the same layered config + credentials store the CLI uses, so
// `cmd/gaia-mcp` and `cmd/gaia` stay in lock-step on auth resolution
// without duplicating the resolve logic. Dispatches to either
// core/forgejo or core/github based on the resolved provider name.
//
// Resolution order (same as internal/cli/provider.go):
//
//  1. Explicit overrides (Profile, Provider, APIURL).
//  2. core/config.Resolve (env > YAML).
//  3. core/auth single-credential fallback.
//  4. Token: env (FORGEJO_TOKEN/GITHUB_TOKEN) → credentials store.
package forgebuilder

import (
	"net/url"
	"sort"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/config"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Override mirrors the CLI's globalFlags subset that affects
// provider construction. Token wins over the local credential
// store when set — used by gaia-mcp's HTTP transport to plumb a
// per-request bearer (the client's own forge PAT) all the way
// through to the upstream call without ever staging it in a local
// credential file.
type Override struct {
	Profile  string
	Provider string
	APIURL   string
	Token    string
}

// Info carries the metadata callers display alongside provider
// results (host name in `whoami`, etc.).
type Info struct {
	Provider string
	Host     string
	APIURL   string
}

// Build returns a ready-to-use provider.Provider plus its Info
// metadata, or an error if config + credentials don't yield enough
// to construct one. Dispatches by resolved.Provider:
//
//	"forgejo" → core/forgejo.Provider
//	"github"  → core/github.Provider  (BaseURL defaults to api.github.com)
//
// Any other provider name is rejected with a usage error.
func Build(ov Override) (provider.Provider, *Info, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, nil, exitcode.Wrap(err, exitcode.Generic, "locate config")
	}
	globalCfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, exitcode.Wrap(err, exitcode.Generic, "load config")
	}

	// Project layer: .gaia/config.yaml inside the repo root, when
	// we're inside one. Lets `gaia issue list` work bare in a
	// configured checkout — no --provider, no --api-url, no --repo.
	var projectCfg *config.Config
	if root := auth.ProjectRoot("."); root != "" {
		projectCfg, err = config.Load(config.ProjectPath(root))
		if err != nil {
			return nil, nil, exitcode.Wrap(err, exitcode.Generic, "load project config")
		}
	}
	cfg := config.Merge(globalCfg, projectCfg)

	resolved, err := config.Resolve(cfg, config.Override{
		Profile:  ov.Profile,
		Provider: ov.Provider,
		APIURL:   ov.APIURL,
	})
	if err != nil {
		return nil, nil, exitcode.Wrap(err, exitcode.Usage, "resolve config")
	}

	// Per-request token override wins. The HTTP transport sets this
	// from the client's `Authorization: Bearer …` so each request
	// acts as the caller's identity, not the host's.
	if ov.Token != "" {
		resolved.Token = ov.Token
	}

	creds, err := loadLayeredCredentials()
	if err != nil {
		return nil, nil, err
	}

	if resolved.Provider == "" && resolved.APIURL == "" {
		if soleProvider, _, soleCred, ok := singleCredential(creds); ok {
			resolved.Provider = soleProvider
			resolved.APIURL = soleCred.APIURL
			if resolved.Token == "" {
				resolved.Token = soleCred.Token
			}
		}
	}

	if resolved.Provider == "" {
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"no provider configured — run `gaia auth forgejo <url>` or `gaia auth gh`, or set --provider/GAIA_PROVIDER")
	}

	host := ""
	if u, perr := url.Parse(resolved.APIURL); perr == nil {
		host = u.Host
	}

	if resolved.Token == "" {
		if c, _, ok := creds.Get(resolved.Provider, host); ok {
			resolved.Token = c.Token
		}
	}

	info := &Info{
		Provider: resolved.Provider,
		Host:     host,
		APIURL:   resolved.APIURL,
	}

	switch resolved.Provider {
	case "forgejo":
		if resolved.APIURL == "" {
			return nil, nil, exitcode.Errorf(exitcode.Usage,
				"no API URL configured — run `gaia auth forgejo <url>` or set --api-url/FORGEJO_API_URL")
		}
		return forgejo.NewProvider(forgejo.Options{
			BaseURL: resolved.APIURL,
			Token:   resolved.Token,
		}), info, nil
	case "github":
		// Empty BaseURL means "use api.github.com"; github.New
		// substitutes the production default. Host defaults to
		// api.github.com when APIURL is empty so Info reads cleanly.
		if info.Host == "" {
			info.Host = "api.github.com"
		}
		return github.NewProvider(github.Options{
			BaseURL: resolved.APIURL,
			Token:   resolved.Token,
		}), info, nil
	default:
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"unknown provider %q (supported: forgejo, github)", resolved.Provider)
	}
}

// LoadLayeredCredentials reads the global + project credential
// stores. Exported so internal/cli's `gaia auth status` and
// `gaia auth logout` can reuse the same loader the build path uses.
func LoadLayeredCredentials() (*auth.Layered, error) {
	return loadLayeredCredentials()
}

// SplitProviderHost is a tiny utility exposed for the same callers
// (auth status / logout walk credential keys formatted as
// "provider:host").
func SplitProviderHost(key string) []string {
	return splitProviderHost(key)
}

func loadLayeredCredentials() (*auth.Layered, error) {
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
	return &auth.Layered{Global: g, Project: p}, nil
}

func singleCredential(l *auth.Layered) (provider, host string, cred auth.Credential, ok bool) {
	type item struct {
		provider, host string
		cred           auth.Credential
	}
	seen := map[string]struct{}{}
	var items []item
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
				continue
			}
			seen[pkey] = struct{}{}
			c, _ := s.Get(parts[0], parts[1])
			items = append(items, item{parts[0], parts[1], c})
		}
	}
	collect(l.Project, "project")
	collect(l.Global, "global")
	sort.Slice(items, func(i, j int) bool {
		return items[i].provider+":"+items[i].host < items[j].provider+":"+items[j].host
	})

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
