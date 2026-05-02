package chain

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse decodes a chain definition from YAML bytes. Validation
// failures return a single error naming the problem field;
// callers surface the message directly.
func Parse(raw []byte) (*Chain, error) {
	var c Chain
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("chain: parse yaml: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// ParseFile reads a YAML file and parses it. A missing file is the
// caller's problem to handle.
func ParseFile(path string) (*Chain, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("chain: read %s: %w", path, err)
	}
	c, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("chain %s: %w", path, err)
	}
	return c, nil
}

// Validate enforces structural rules ParseFile / Parse can't catch
// at the YAML layer:
//
//   - non-empty name
//   - at least one step
//   - unique, non-empty step IDs
//   - non-empty step.run
//   - capture names are valid identifiers (no spaces / dots; later
//     steps reference them as ${name.field})
func (c *Chain) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("chain: name is required")
	}
	if len(c.Steps) == 0 {
		return errors.New("chain: at least one step is required")
	}
	seen := map[string]struct{}{}
	for i, s := range c.Steps {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("chain: step %d: id is required", i)
		}
		if _, dup := seen[s.ID]; dup {
			return fmt.Errorf("chain: step %d: duplicate id %q", i, s.ID)
		}
		seen[s.ID] = struct{}{}
		if strings.TrimSpace(s.Run) == "" {
			return fmt.Errorf("chain: step %q: run is required", s.ID)
		}
		if s.Capture != "" {
			if !isValidIdent(s.Capture) {
				return fmt.Errorf("chain: step %q: capture %q must be a simple identifier (letters/digits/underscore, no dots)", s.ID, s.Capture)
			}
		}
	}
	return nil
}

// isValidIdent returns true for [A-Za-z_][A-Za-z0-9_]* — the same
// shape we expect for shell-style variable names. Capture names use
// this so ${capture.field} parses cleanly without quoting tricks.
func isValidIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case (r >= 'a' && r <= 'z'),
			(r >= 'A' && r <= 'Z'),
			r == '_':
			// always ok
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
