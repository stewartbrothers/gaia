package chain

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
//   - timeout / retry shapes are well-formed (Phase B-2)
//   - default_yield_on conditions are in the vocabulary (Phase B-2)
//   - cleanup steps satisfy the same rules as regular steps (Phase B-2)
func (c *Chain) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("chain: name is required")
	}
	if len(c.Steps) == 0 {
		return errors.New("chain: at least one step is required")
	}
	if err := validateSteps(c.Steps, "step"); err != nil {
		return err
	}
	for _, y := range c.DefaultYieldOn {
		if !y.IsKnown() {
			return fmt.Errorf("chain: default_yield_on contains unknown condition %q (see chain.AllYieldConditions for the vocabulary)", y)
		}
	}
	if len(c.Cleanup) > 0 {
		if err := validateSteps(c.Cleanup, "cleanup step"); err != nil {
			return err
		}
	}
	return nil
}

// validateSteps walks a slice of steps (regular or cleanup) and
// applies the per-step structural rules: unique non-empty IDs,
// non-empty run, valid capture identifier, known yield/abort
// conditions, no overlap between yield_on and abort_on, well-formed
// timeout + retry. The kind label distinguishes "step" vs "cleanup
// step" in error messages so an operator can find the bad block.
func validateSteps(steps []Step, kind string) error {
	seen := map[string]struct{}{}
	for i, s := range steps {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("chain: %s %d: id is required", kind, i)
		}
		if _, dup := seen[s.ID]; dup {
			return fmt.Errorf("chain: %s %d: duplicate id %q", kind, i, s.ID)
		}
		seen[s.ID] = struct{}{}
		if strings.TrimSpace(s.Run) == "" {
			return fmt.Errorf("chain: %s %q: run is required", kind, s.ID)
		}
		if s.Capture != "" {
			if !isValidIdent(s.Capture) {
				return fmt.Errorf("chain: %s %q: capture %q must be a simple identifier (letters/digits/underscore, no dots)", kind, s.ID, s.Capture)
			}
		}
		// yield_on / abort_on entries must be in the fixed vocabulary
		// — typos otherwise silently never fire. Reject at parse time
		// so operators get an immediate red flag.
		for _, y := range s.YieldOn {
			if !y.IsKnown() {
				return fmt.Errorf("chain: %s %q: yield_on contains unknown condition %q (see chain.AllYieldConditions for the vocabulary)", kind, s.ID, y)
			}
		}
		for _, a := range s.AbortOn {
			if !a.IsKnown() {
				return fmt.Errorf("chain: %s %q: abort_on contains unknown condition %q (see chain.AllYieldConditions for the vocabulary)", kind, s.ID, a)
			}
		}
		// Same condition can't be in both yield_on AND abort_on for the
		// same step — that's a contradiction.
		for _, y := range s.YieldOn {
			for _, a := range s.AbortOn {
				if y == a {
					return fmt.Errorf("chain: %s %q: condition %q can't be in both yield_on and abort_on", kind, s.ID, y)
				}
			}
		}
		if s.Timeout != "" {
			if _, err := time.ParseDuration(s.Timeout); err != nil {
				return fmt.Errorf("chain: %s %q: timeout %q is not a valid duration: %w", kind, s.ID, s.Timeout, err)
			}
		}
		if s.Retry != nil {
			if s.Retry.Max < 0 {
				return fmt.Errorf("chain: %s %q: retry.max must be ≥ 0 (got %d)", kind, s.ID, s.Retry.Max)
			}
			if s.Retry.Delay != "" {
				if _, err := time.ParseDuration(s.Retry.Delay); err != nil {
					return fmt.Errorf("chain: %s %q: retry.delay %q is not a valid duration: %w", kind, s.ID, s.Retry.Delay, err)
				}
			}
			switch s.Retry.Backoff {
			case "", "constant", "linear", "exponential":
				// ok
			default:
				return fmt.Errorf("chain: %s %q: retry.backoff %q must be one of constant|linear|exponential", kind, s.ID, s.Retry.Backoff)
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
