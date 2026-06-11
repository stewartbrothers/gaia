package config

import (
	"fmt"
	"os"

	"github.com/stewartbrothers/gaia/core/provider"
)

// Override carries the CLI-flag layer of overrides. Empty fields mean
// "no override at this layer" — the env and config layers are consulted
// in turn.
type Override struct {
	Profile  string
	Provider string
	APIURL   string
}

// Resolved is the final, env-overlaid configuration the CLI hands to
// a Provider implementation. Token is opaque and MUST NOT appear in
// log output; use String() (which redacts) when printing.
type Resolved struct {
	Profile  string
	Provider string
	APIURL   string
	Token    string
	// DefaultRepo is the project-config-supplied owner/name shortcut
	// for repo-scoped commands. Flag --repo wins; this only fills in
	// when neither --repo nor git-remote autodetect has supplied one.
	DefaultRepo string
}

// String returns a redacted, log-safe representation. Token presence is
// reported as a boolean; the token value itself never appears.
func (r *Resolved) String() string {
	return fmt.Sprintf("Resolved{Profile:%q, Provider:%q, APIURL:%q, DefaultRepo:%q, TokenSet:%v}",
		r.Profile, r.Provider, r.APIURL, r.DefaultRepo, r.Token != "")
}

// Resolve combines a parsed Config with environment variables and
// flag-supplied overrides, producing the final Resolved view.
//
// Precedence (highest wins): CLI flag > env var > config file.
//
// If the chosen profile name doesn't exist in cfg.Profiles, returns
// an error. A nil cfg or an empty Profiles map means "no config" —
// env vars and flags must supply everything.
func Resolve(cfg *Config, ov Override) (*Resolved, error) {
	profileName := pickProfileName(cfg, ov)

	var profile Profile
	if cfg != nil && len(cfg.Profiles) > 0 {
		p, ok := cfg.Profiles[profileName]
		if !ok {
			return nil, fmt.Errorf("config: profile %q not found in config (have: %v)", profileName, profileKeys(cfg))
		}
		profile = p
	} else if profileName != "" && (cfg == nil || len(cfg.Profiles) == 0) && ov.Profile != "" {
		// User explicitly asked for a profile via flag, but no config
		// file is present. That's a misconfiguration worth surfacing.
		return nil, fmt.Errorf("config: profile %q requested but no config file with profiles is present", profileName)
	}

	provider := firstNonEmpty(ov.Provider, os.Getenv("GAIA_PROVIDER"), profile.Provider)
	// Only carry the profile's api_url through when the provider hasn't
	// been overridden away from the profile's own provider. If the caller
	// passes --provider github but the active profile targets forgejo, the
	// forgejo api_url must not bleed through to GitHub API requests.
	profileAPIURL := profile.APIURL
	if ov.Provider != "" && profile.Provider != "" && ov.Provider != profile.Provider {
		profileAPIURL = ""
	}
	apiURL := firstNonEmpty(ov.APIURL, os.Getenv("FORGEJO_API_URL"), profileAPIURL)
	token := resolveToken(profile, provider)

	defaultRepo := ""
	if cfg != nil {
		defaultRepo = cfg.DefaultRepo
	}

	return &Resolved{
		Profile:     profileName,
		Provider:    provider,
		APIURL:      apiURL,
		Token:       token,
		DefaultRepo: defaultRepo,
	}, nil
}

func pickProfileName(cfg *Config, ov Override) string {
	if ov.Profile != "" {
		return ov.Profile
	}
	if env := os.Getenv("GAIA_PROFILE"); env != "" {
		return env
	}
	if cfg != nil && cfg.DefaultProfile != "" {
		return cfg.DefaultProfile
	}
	return ""
}

func resolveToken(profile Profile, provider string) string {
	if profile.TokenEnv != "" {
		if v := os.Getenv(profile.TokenEnv); v != "" {
			return v
		}
	}
	// Fallback chain per provider. The first env name is gaia's
	// canonical, the second is the upstream-CLI convention so users
	// who already export GITEA_TOKEN (for `tea`) or GH_TOKEN (for
	// `gh`) don't need to set a second variable. Common-path bug fix
	// from #102: prior code only checked the first name, so an agent
	// with GITEA_TOKEN set in their shell got 401s on every call.
	for _, name := range envNamesFor(provider) {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// envNamesFor returns the env var names checked, in order, for a given
// provider. The list is registry-driven (#340): the forge packages each
// declare their TokenEnvNames in init() via provider.Register, so the
// registry is the single source of truth for "what forges exist + their
// token env fallbacks." Adding a forge (GitLab, Bitbucket) needs no edit
// here. An unregistered provider — or one declaring none — yields nil,
// exactly as the prior hard-coded switch did for unknown names.
//
// Population caveat: the registry is filled by forge init(), so any
// caller must have imported core/forges (in production, every settings.Load
// caller transitively does, via internal/forgebuilder). core/config's own
// black-box tests blank-import the forges to populate it — see
// registry_blackbox_test.go.
func envNamesFor(p string) []string {
	return provider.TokenEnvNames(p)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func profileKeys(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Profiles))
	for k := range cfg.Profiles {
		out = append(out, k)
	}
	return out
}
