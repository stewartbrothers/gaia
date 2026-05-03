package chain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// ResolveOptions tunes Resolve's lookup. ProjectRoot, when non-empty,
// gates the project-local `.gaia/chains/<name>.yaml` candidate;
// callers pass the discovered git/project root or "" to skip that
// layer entirely. GlobalDir is the home-rooted fallback (typically
// `~/.config/gaia/chains/`); empty disables the global layer.
type ResolveOptions struct {
	ProjectRoot string
	GlobalDir   string
}

// ResolveError carries the resolution attempts that failed when
// Resolve can't find a chain. The CLI surfaces the attempts so the
// operator sees exactly which paths were tried (project, global,
// literal-path).
type ResolveError struct {
	Name     string
	Attempts []string
}

func (e *ResolveError) Error() string {
	if len(e.Attempts) == 0 {
		return fmt.Sprintf("chain %q: not found", e.Name)
	}
	return fmt.Sprintf("chain %q: not found (tried: %s)",
		e.Name, strings.Join(e.Attempts, ", "))
}

// Resolve maps a name-or-path argument to a chain YAML file path.
//
// Lookup order (first existing wins):
//
//  1. Literal path (contains a path separator OR has a YAML extension):
//     used as-is. Lets `gaia chain run --chain-file ./pipeline.yaml`
//     and `gaia chain run ./pipeline.yaml` keep working.
//  2. Project-local: `${ProjectRoot}/.gaia/chains/<name>.yaml`
//     (skipped when ProjectRoot is empty).
//  3. Global: `${GlobalDir}/<name>.yaml` (typically
//     `~/.config/gaia/chains/<name>.yaml`; skipped when empty).
//
// Bare identifiers fall through to the project + global lookup;
// anything that "looks like a path" (separator or .yml/.yaml suffix)
// short-circuits to layer 1. None-found returns *ResolveError with
// the attempted paths populated.
func Resolve(name string, opts ResolveOptions) (string, error) {
	if name == "" {
		return "", errors.New("chain: name is required")
	}

	// Layer 1: literal path. Heuristic — if the argument contains a
	// path separator or a YAML extension, it's a path, not a name.
	if looksLikePath(name) {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
		// Not found at the literal path — fall through to the
		// saved-chain layers, but record the attempt.
		attempts := []string{name}
		if p := tryLayer(name, opts.ProjectRoot, ".gaia/chains"); p != "" {
			return p, nil
		}
		if opts.ProjectRoot != "" {
			attempts = append(attempts, filepath.Join(opts.ProjectRoot, ".gaia", "chains", appendYAMLExt(name)))
		}
		if p := tryGlobal(name, opts.GlobalDir); p != "" {
			return p, nil
		}
		if opts.GlobalDir != "" {
			attempts = append(attempts, filepath.Join(opts.GlobalDir, appendYAMLExt(name)))
		}
		return "", &ResolveError{Name: name, Attempts: attempts}
	}

	// Layer 2: project-local.
	var attempts []string
	if opts.ProjectRoot != "" {
		candidate := filepath.Join(opts.ProjectRoot, ".gaia", "chains", appendYAMLExt(name))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		attempts = append(attempts, candidate)
	}

	// Layer 3: global.
	if opts.GlobalDir != "" {
		candidate := filepath.Join(opts.GlobalDir, appendYAMLExt(name))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		attempts = append(attempts, candidate)
	}

	return "", &ResolveError{Name: name, Attempts: attempts}
}

// looksLikePath reports whether name is best treated as a literal
// filesystem path rather than a saved-chain identifier. Returns true
// for absolute paths, paths containing a separator, or names ending
// in .yaml / .yml.
func looksLikePath(name string) bool {
	if filepath.IsAbs(name) {
		return true
	}
	if strings.ContainsAny(name, `/\`) {
		return true
	}
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml")
}

// tryLayer probes <root>/<dir>/<name>.yaml; returns the path when
// it exists, "" otherwise. root == "" means "skip this layer".
func tryLayer(name, root, dir string) string {
	if root == "" {
		return ""
	}
	candidate := filepath.Join(root, dir, appendYAMLExt(name))
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// tryGlobal probes <globalDir>/<name>.yaml. globalDir == "" means
// "skip the global layer".
func tryGlobal(name, globalDir string) string {
	if globalDir == "" {
		return ""
	}
	candidate := filepath.Join(globalDir, appendYAMLExt(name))
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// appendYAMLExt adds .yaml when name has no .yaml/.yml suffix.
// Bare identifiers like "pr-create-and-land" become
// "pr-create-and-land.yaml"; explicit "ci.yaml" stays as-is.
func appendYAMLExt(name string) string {
	low := strings.ToLower(name)
	if strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml") {
		return name
	}
	return name + ".yaml"
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
