// Package forgebuilder builds a configured provider.Provider from a
// resolved [settings.Settings] handle. Dispatches to either
// core/forgejo or core/github based on s.Provider().
//
// Resolution of the layered config + credentials + env happens in
// core/settings. forgebuilder is the small adapter that turns a
// Settings handle into a Provider — owning only the per-call bearer
// override path (gaia-mcp's HTTP transport plumbs a per-request PAT
// here) and the SQLite cache open. (#311)
package forgebuilder

import (
	"net/url"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/cache/sqlite"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/settings"
)

// BuildOverride carries call-scoped overrides Build applies on top of
// the resolved Settings. Empty fields mean "no override at this
// layer." Settings owns the process-scoped state (config, credentials,
// env); BuildOverride owns only what genuinely varies per call.
type BuildOverride struct {
	// Token is a per-call bearer that supersedes s.Token(). gaia-mcp's
	// HTTP transport sets this from the client's Authorization header
	// so each request acts as the caller's identity, not the host's.
	Token string
}

// Info carries the metadata callers display alongside provider
// results (host name in `whoami`, etc.).
type Info struct {
	Provider string
	Host     string
	APIURL   string
}

// Build returns a ready-to-use provider.Provider plus its Info
// metadata, or an error if Settings + overrides don't yield enough to
// construct one. Dispatches by s.Provider():
//
//	"forgejo" → core/forgejo.Provider
//	"github"  → core/github.Provider  (BaseURL defaults to api.github.com)
//
// Any other provider name is rejected with a usage error.
func Build(s settings.Settings, ov BuildOverride) (provider.Provider, *Info, error) {
	provName := s.Provider()
	apiURL := s.APIURL()
	token := s.Token()
	if ov.Token != "" {
		token = ov.Token
	}

	if provName == "" {
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"no provider configured — run `gaia auth forgejo <url>` or `gaia auth gh`, or set --provider/GAIA_PROVIDER")
	}

	host := ""
	if u, perr := url.Parse(apiURL); perr == nil {
		host = u.Host
	}
	info := &Info{Provider: provName, Host: host, APIURL: apiURL}

	// Cache: opened lazily so a missing cache dir or permission error
	// here doesn't block the read path. Errors degrade silently to
	// "no cache" — the provider still works, just without the caching
	// layer. (#42)
	//
	// The config knob (`cache.enabled: false`) and the env-var bypass
	// (`GAIA_CACHE_ENABLED=false`) were absorbed into settings.Cache()
	// at Load time; we only consult NoCache here.
	cacheSettings := s.Cache()
	var ch cache.Cache
	if !cacheSettings.NoCache {
		if path, perr := cache.PathFor(provName, apiURL); perr == nil {
			if c, oerr := sqlite.Open(path); oerr == nil {
				ch = c
			}
		}
	}

	switch provName {
	case "forgejo":
		if apiURL == "" {
			return nil, nil, exitcode.Errorf(exitcode.Usage,
				"no API URL configured — run `gaia auth forgejo <url>` or set --api-url/FORGEJO_API_URL")
		}
		return forgejo.NewProvider(forgejo.Options{
			BaseURL: apiURL,
			Token:   token,
			Cache:   ch,
		}), info, nil
	case "github":
		// Empty BaseURL means "use api.github.com"; github.New
		// substitutes the production default. Host defaults to
		// api.github.com when APIURL is empty so Info reads cleanly.
		if info.Host == "" {
			info.Host = "api.github.com"
		}
		return github.NewProvider(github.Options{
			BaseURL: apiURL,
			Token:   token,
			Cache:   ch,
		}), info, nil
	default:
		return nil, nil, exitcode.Errorf(exitcode.Usage,
			"unknown provider %q (supported: forgejo, github)", provName)
	}
}

// LoadLayeredCredentials reads the global + project credential
// stores. Exported so internal/cli's `gaia auth status` and
// `gaia auth logout` can reuse the same loader the build path used
// before Settings absorbed credential loading. Kept on the
// forgebuilder surface for backward compatibility with those two
// callers; new code reads s.Inspector().Credentials() instead.
func LoadLayeredCredentials() (*auth.Layered, error) {
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

// SplitProviderHost is a tiny utility exposed for the same callers
// (auth status / logout walk credential keys formatted as
// "provider:host").
func SplitProviderHost(key string) []string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return nil
}
