package chain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// State is the on-disk shape of a yielded chain. Written when a step
// yields, read back by `gaia chain resume`.
//
// Schema is versioned so a future Phase B-2 / C change can add fields
// without orphaning in-flight chains. Resume against a state file
// from an older schema fails loud with a clear message.
type State struct {
	SchemaVersion int            `yaml:"schema_version"`
	Token         string         `yaml:"token"`
	CreatedAt     time.Time      `yaml:"created_at"`
	YieldedAt     time.Time      `yaml:"yielded_at"`
	YieldedAtStep int            `yaml:"yielded_at_step"`
	YieldReason   YieldCondition `yaml:"yield_reason"`
	YieldPayload  map[string]any `yaml:"yield_payload,omitempty"`

	// Full chain definition captured at yield time. Re-resolving
	// against the on-disk YAML at resume would race against
	// operator edits; freezing the spec here keeps the in-flight
	// chain self-contained.
	Chain Chain `yaml:"chain"`

	// Already-resolved vars + accumulated captures + completed
	// step results. Resume uses these to re-build the runner's
	// scope and append.
	Vars     map[string]string `yaml:"vars"`
	Captures map[string]any    `yaml:"captures,omitempty"`
	Steps    []StepResult      `yaml:"steps"`
}

// CurrentSchemaVersion is the State.SchemaVersion this code writes.
// Bump when adding a field that older readers can't ignore.
const CurrentSchemaVersion = 1

// StateFile = where one chain's state lives. Returns "" when root
// is empty (caller decides whether that's an error).
func StateFile(stateDir, token string) string {
	if stateDir == "" || token == "" {
		return ""
	}
	return filepath.Join(stateDir, token+".yaml")
}

// DefaultStateDir resolves the per-user chain state directory:
//
//	$XDG_STATE_HOME/gaia/chains/    when XDG_STATE_HOME is set
//	$HOME/.local/state/gaia/chains/ otherwise
//
// Created with 0700 on first write. Same path family as
// ~/.config/gaia/credentials.yaml so the dotfile manager / backup
// story is consistent.
func DefaultStateDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "gaia", "chains"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("chain: locate home: %w", err)
	}
	return filepath.Join(home, ".local", "state", "gaia", "chains"), nil
}

// NewToken returns a fresh resume token. 16 random bytes hex-encoded
// → 32 chars. Plenty of entropy; short enough to fit in an agent's
// short-term memory.
func NewToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("chain: generate token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// SaveState writes state to its file (StateFile(dir, state.Token)),
// creating the parent directory as 0700 if absent. Atomic via temp
// + rename so an interrupted save never leaves a partial file.
//
// Mode is 0600 so other users on a multi-user system can't read
// captured payloads (which may contain sensitive forge data even
// without a token).
func SaveState(dir string, state *State) error {
	if state.Token == "" {
		return errors.New("chain: SaveState requires a non-empty token")
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = CurrentSchemaVersion
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("chain: mkdir %s: %w", dir, err)
	}
	body, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("chain: marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".chain-*.tmp")
	if err != nil {
		return fmt.Errorf("chain: temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chain: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chain: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chain: close %s: %w", tmpName, err)
	}
	target := StateFile(dir, state.Token)
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chain: rename %s → %s: %w", tmpName, target, err)
	}
	return nil
}

// LoadState reads a state file by token. ErrNotExist propagates
// directly so callers can distinguish "no such chain" from
// "couldn't read." Other errors are wrapped with the path.
func LoadState(dir, token string) (*State, error) {
	if token == "" {
		return nil, errors.New("chain: LoadState requires a non-empty token")
	}
	path := StateFile(dir, token)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err // pass through fs.ErrNotExist for caller
	}
	var s State
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("chain: parse %s: %w", path, err)
	}
	if s.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("chain: state %s has schema_version %d, this gaia only understands %d (please upgrade)",
			path, s.SchemaVersion, CurrentSchemaVersion)
	}
	return &s, nil
}

// DeleteState removes a state file. Idempotent: missing file is not
// an error. Used after resume succeeds (chain completed) or after
// explicit `gaia chain abort`.
func DeleteState(dir, token string) error {
	path := StateFile(dir, token)
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("chain: remove %s: %w", path, err)
	}
	return nil
}

// ListStates returns the tokens of every state file in dir, sorted by
// modification time (newest first). Used by `gaia chain list`.
// Missing dir returns an empty slice.
func ListStates(dir string) ([]StateInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("chain: list %s: %w", dir, err)
	}
	out := []StateInfo{}
	for _, e := range entries {
		name := e.Name()
		// Skip the .tmp files SaveState may leave around if a write
		// got interrupted.
		if filepath.Ext(name) != ".yaml" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		token := name[:len(name)-len(".yaml")]
		out = append(out, StateInfo{Token: token, ModTime: info.ModTime()})
	}
	// Sort newest first so an operator running `gaia chain list` sees
	// their most recent yield at the top.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].ModTime.Before(out[j].ModTime); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// StateInfo is the lightweight summary `gaia chain list` returns —
// just the token + when the chain yielded. Loading the full state
// (chain definition + captures) is reserved for resume.
type StateInfo struct {
	Token   string    `json:"token"`
	ModTime time.Time `json:"mod_time"`
}

// CleanupStale walks dir, removes state files older than maxAge.
// Returns the count removed. Errors during individual removals are
// swallowed (best-effort cleanup), but a directory-listing error
// propagates.
//
// Called at the start of every chain command so an operator with an
// abandoned yield from last week doesn't accumulate cruft. No cron,
// no daemon — the chain runner cleans up after itself opportunistically.
func CleanupStale(dir string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			removed++
		}
	}
	return removed, nil
}
