// Package config loads gaia's layered configuration: a YAML file at
// $XDG_CONFIG_HOME/gaia/config.yaml (or $HOME/.config/gaia/config.yaml
// when XDG isn't set), overridden by environment variables, overridden
// by CLI flags. The result is a Resolved value the CLI hands to a
// Provider implementation.
//
// Tokens are sourced ONLY from environment variables — never from
// flags, never from the YAML file. The Profile.TokenEnv field names
// which env var to read; falling back to FORGEJO_TOKEN / GITHUB_TOKEN
// for the canonical providers when TokenEnv is unset.
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

// DefaultPath returns the canonical config-file location. Honors
// $XDG_CONFIG_HOME; falls back to $HOME/.config when XDG is unset.
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
