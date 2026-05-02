// Package config loads gaia's layered configuration:
//
//  1. project   — .gaia/config.yaml in the repo root (when in one)
//  2. global    — $XDG_CONFIG_HOME/gaia/config.yaml or $HOME/.config/gaia/config.yaml
//  3. env       — GAIA_*, FORGEJO_*, GITHUB_*, GITEA_TOKEN, GH_TOKEN
//  4. flags     — explicit CLI overrides
//
// Later layers override earlier ones; project beats global, env beats
// project, flags beat env. The result is a Resolved value the CLI
// hands to a Provider implementation.
//
// Tokens are sourced ONLY from environment variables (or the
// credentials store loaded separately) — never from flags, never from
// the config YAML. The Profile.TokenEnv field names which env var to
// read; falling back to FORGEJO_TOKEN/GITEA_TOKEN for forgejo and
// GITHUB_TOKEN/GH_TOKEN for github when TokenEnv is unset.
//
// Project config carries non-secret host metadata (provider, api_url,
// default_profile, default_repo) so an operator who runs `gaia issue
// list` inside a checkout doesn't have to re-type --provider, --api-url,
// or --repo on every call. The file is **committable** — no secrets,
// just defaults — though some teams gitignore it so each contributor
// can pin their own profile.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the parsed shape of the YAML config file.
type Config struct {
	DefaultProfile string             `yaml:"default_profile"`
	DefaultRepo    string             `yaml:"default_repo,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

// Profile describes one provider context — typically one forge
// instance. Tokens are not stored in the YAML; TokenEnv names the env
// var to read at resolve time.
type Profile struct {
	Provider string `yaml:"provider"`
	APIURL   string `yaml:"api_url"`
	TokenEnv string `yaml:"token_env,omitempty"`
}

// Load reads and parses a config file at path. A missing file is the
// "no config, env-only" case and returns an empty Config with nil
// error; any other read or parse failure returns the error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return &cfg, nil
}

// DefaultPath returns the canonical global config-file location.
// Honors $XDG_CONFIG_HOME; falls back to $HOME/.config when XDG is
// unset.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "gaia", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: locate home: %w", err)
	}
	return filepath.Join(home, ".config", "gaia", "config.yaml"), nil
}

// ProjectPath returns the canonical project config-file location
// inside repoRoot. Empty repoRoot returns "".
func ProjectPath(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}
	return filepath.Join(repoRoot, ".gaia", "config.yaml")
}

// Merge folds the project-layer config on top of the global layer.
// Non-empty fields in project win; empty project fields fall back to
// global. Profile maps are merged key-by-key (project profile shadows
// global with the same name; global-only profiles survive).
//
// Returns a fresh *Config; neither input is mutated. nil inputs are
// treated as empty configs.
func Merge(global, project *Config) *Config {
	out := &Config{Profiles: map[string]Profile{}}
	if global != nil {
		out.DefaultProfile = global.DefaultProfile
		out.DefaultRepo = global.DefaultRepo
		for k, v := range global.Profiles {
			out.Profiles[k] = v
		}
	}
	if project != nil {
		if project.DefaultProfile != "" {
			out.DefaultProfile = project.DefaultProfile
		}
		if project.DefaultRepo != "" {
			out.DefaultRepo = project.DefaultRepo
		}
		for k, v := range project.Profiles {
			out.Profiles[k] = v // project shadows global on key collision
		}
	}
	return out
}
