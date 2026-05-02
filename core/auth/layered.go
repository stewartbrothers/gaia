package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

// Layered overlays a project store onto a global store. Project
// entries take precedence per (provider, host) pair.
type Layered struct {
	Global  *Store
	Project *Store
}

// Get returns (cred, source, true) where source is "project" or
// "global" depending on which layer satisfied the lookup. Project is
// consulted first.
func (l *Layered) Get(provider, host string) (Credential, string, bool) {
	if l.Project != nil {
		if c, ok := l.Project.Get(provider, host); ok {
			return c, "project", true
		}
	}
	if l.Global != nil {
		if c, ok := l.Global.Get(provider, host); ok {
			return c, "global", true
		}
	}
	return Credential{}, "", false
}

// DefaultGlobalPath returns the canonical global credentials file
// path. Honors $XDG_CONFIG_HOME; falls back to $HOME/.config when
// XDG is unset.
func DefaultGlobalPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "gaia", "credentials.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("auth: locate home: %w", err)
	}
	return filepath.Join(home, ".config", "gaia", "credentials.yaml"), nil
}

// ProjectRoot walks up from dir looking for a `.git` directory.
// Returns the absolute path of the first ancestor containing one, or
// "" when dir isn't inside a git repo.
func ProjectRoot(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			if info.IsDir() {
				return d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// ProjectPath returns the canonical project credentials file path
// inside repoRoot.
func ProjectPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".gaia", "credentials.yaml")
}
