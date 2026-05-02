package config

import (
	"fmt"
	"os"
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
}

// String returns a redacted, log-safe representation. Token presence is
// reported as a boolean; the token value itself never appears.
func (r *Resolved) String() string {
	return fmt.Sprintf("Resolved{Profile:%q, Provider:%q, APIURL:%q, TokenSet:%v}",
		r.Profile, r.Provider, r.APIURL, r.Token != "")
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
	apiURL := firstNonEmpty(ov.APIURL, os.Getenv("FORGEJO_API_URL"), profile.APIURL)
	token := resolveToken(profile, provider)

	return &Resolved{
		Profile:  profileName,
		Provider: provider,
		APIURL:   apiURL,
		Token:    token,
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
	switch provider {
	case "forgejo":
		return os.Getenv("FORGEJO_TOKEN")
	case "github":
		return os.Getenv("GITHUB_TOKEN")
	}
	return ""
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
