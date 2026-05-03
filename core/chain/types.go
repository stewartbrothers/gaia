// Package chain implements gaia's step-chaining engine. A chain is a
// sequence of steps an agent describes once, gaia runs in one CLI
// invocation, and reports back as a single envelope. The win for
// agents is round-trip count: instead of "open PR → check status →
// poll → check again → merge" eating five conversation turns, it's
// one `gaia chain ...` call returning the final state.
//
// Phase A scope (this commit):
//
//   - Linear sequences only (no parallel, no for_each, no
//     conditional branching).
//   - Shell-style variable substitution: ${var} for chain inputs,
//     ${id.field} for previous steps' captured outputs.
//   - on_failure with a structured `return:` block so agents can
//     branch on failure shape rather than parsing free text.
//   - --dry-run: substitute vars, render the resolved plan, exit.
//
// Phase B (later): saved chains, named chain composition.
// Phase C (later): parallel steps, retries, conditionals.
//
// Tracks #112.
package chain

// Chain is a parsed YAML chain definition. Validate() enforces the
// invariants ParseFile / Parse can't catch syntactically (unique
// step IDs, etc.).
type Chain struct {
	Name        string             `yaml:"name"`
	Description string             `yaml:"description,omitempty"`
	Vars        map[string]VarSpec `yaml:"vars,omitempty"`
	Steps       []Step             `yaml:"steps"`
}

// VarSpec describes one chain input. Required vars must be supplied
// at run time (via --var or programmatic input); Default values fill
// in when an optional var is omitted.
type VarSpec struct {
	Required bool   `yaml:"required,omitempty"`
	Default  string `yaml:"default,omitempty"`
}

// Step is one shell-out in the chain. Run is the command string
// (subject to ${var} substitution at run time). Capture, when
// non-empty, names a slot in the chain's capture namespace into
// which the step's stdout (parsed as a gaia envelope) lands —
// later steps can reference it as ${capture.<field>}.
//
// OnFailure is consulted when the step exits non-zero. If
// OnFailure.Return is set, those keys (with substitution applied)
// become the chain's `failure` payload and the chain stops; if it
// isn't set, a default failure envelope is emitted with the step
// ID + exit code + stderr tail.
type Step struct {
	ID        string         `yaml:"id"`
	Run       string         `yaml:"run"`
	Capture   string         `yaml:"capture,omitempty"`
	OnFailure *FailureAction `yaml:"on_failure,omitempty"`
}

// FailureAction is the on_failure block. v1 supports `return` only
// (the shape an agent reads when the chain stops). Later phases may
// add `retry`, `continue_on_error`, etc.
type FailureAction struct {
	Return map[string]any `yaml:"return,omitempty"`
}

// Result is the JSON envelope `gaia chain` produces. Status is
// "success" or "failure"; on failure the FailedStep + Failure
// fields carry the actionable payload.
type Result struct {
	Chain      string         `json:"chain"`
	Status     string         `json:"status"`
	FailedStep string         `json:"failed_step,omitempty"`
	Failure    map[string]any `json:"failure,omitempty"`
	Steps      []StepResult   `json:"steps"`
	Captured   map[string]any `json:"captured,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	DryRun     bool           `json:"dry_run,omitempty"`
}

// StepResult records what happened for one step. Stdout/stderr are
// truncated to a reasonable size to keep the result envelope small;
// agents who need full logs can re-run with --verbose or inspect the
// captured field.
type StepResult struct {
	ID         string `json:"id"`
	Run        string `json:"run"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// Result.Status values.
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
)

// StepResult.Status values.
const (
	StepOK      = "ok"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)
