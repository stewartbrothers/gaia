// Package auth manages gaia's stored credentials. Two locations are
// supported: the canonical global file at $XDG_CONFIG_HOME/gaia/
// credentials.yaml (or $HOME/.config/gaia/credentials.yaml when XDG
// is unset), and a per-project .gaia/credentials.yaml inside a
// repository. The Layered type combines them with project values
// winning per host. Tokens are redacted in any String() output and
// never appear in logged Sprintf("%v", ...) or Sprintf("%+v", ...)
// formatting; the corresponding tests pin that contract.
package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// Credential is a single host's authentication state. Stored on
// disk; never logged.
type Credential struct {
	Token string `yaml:"token"`
	User  string `yaml:"user,omitempty"`
}

// String returns a redacted, log-safe representation. The token's
// presence is reported as a boolean; the value never appears.
func (c Credential) String() string {
	return fmt.Sprintf("Credential{User:%q, TokenSet:%v}", c.User, c.Token != "")
}

// GoString satisfies fmt.GoStringer so `%#v` (used by some debugging
// helpers) is also redacted.
func (c Credential) GoString() string {
	return c.String()
}

// Store is the on-disk shape: provider → host → Credential.
type Store struct {
	mu        sync.RWMutex
	providers map[string]map[string]Credential
}

// MarshalYAML emits the providers map directly so the file structure
// is `forgejo: {host: {token, user}}` without a wrapping key.
func (s *Store) MarshalYAML() (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.providers == nil {
		return map[string]map[string]Credential{}, nil
	}
	return s.providers, nil
}

// UnmarshalYAML decodes the same shape MarshalYAML emits.
func (s *Store) UnmarshalYAML(value *yaml.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]map[string]Credential{}
	if err := value.Decode(&out); err != nil {
		return err
	}
	s.providers = out
	return nil
}

// String returns a redacted, log-safe representation. Counts only.
func (s *Store) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := 0
	for _, m := range s.providers {
		hosts += len(m)
	}
	return fmt.Sprintf("Store{providers:%d, hosts:%d}", len(s.providers), hosts)
}

// Get returns (cred, true) when an entry exists, or (zero, false).
func (s *Store) Get(provider, host string) (Credential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if hosts, ok := s.providers[provider]; ok {
		c, ok := hosts[host]
		return c, ok
	}
	return Credential{}, false
}

// Set inserts or replaces an entry.
func (s *Store) Set(provider, host string, cred Credential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providers == nil {
		s.providers = map[string]map[string]Credential{}
	}
	if _, ok := s.providers[provider]; !ok {
		s.providers[provider] = map[string]Credential{}
	}
	s.providers[provider][host] = cred
}

// Remove deletes an entry. Missing entries are no-ops.
func (s *Store) Remove(provider, host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hosts, ok := s.providers[provider]; ok {
		delete(hosts, host)
		if len(hosts) == 0 {
			delete(s.providers, provider)
		}
	}
}

// Hosts returns "provider:host" identifiers in deterministic order so
// `gaia auth status` output is stable.
func (s *Store) Hosts() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for p, hosts := range s.providers {
		for h := range hosts {
			out = append(out, p+":"+h)
		}
	}
	sort.Strings(out)
	return out
}

// Load reads a credentials YAML file. A missing file is the "no
// credentials yet" case and returns an empty Store with nil error.
// Any other read or parse error is returned wrapped.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Store{providers: map[string]map[string]Credential{}}, nil
		}
		return nil, fmt.Errorf("auth: read %s: %w", path, err)
	}
	s := &Store{}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("auth: parse %s: %w", path, err)
	}
	if s.providers == nil {
		s.providers = map[string]map[string]Credential{}
	}
	return s, nil
}

// Save writes the store to path with 0600 permissions, atomically via
// temp-file + rename. Parent directories are created with 0700.
func Save(path string, s *Store) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir %s: %w", filepath.Dir(path), err)
	}
	body, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("auth: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("auth: temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("auth: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("auth: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("auth: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("auth: rename %s → %s: %w", tmpName, path, err)
	}
	return nil
}
