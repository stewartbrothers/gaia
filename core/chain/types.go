// Package chain implements gaia's step-chaining engine. A chain is a
// sequence of steps an agent describes once, gaia runs in one CLI
// invocation, and reports back as a single envelope. The win for
// agents is round-trip count: instead of "open PR → check status →
// poll → check again → merge" eating five conversation turns, it's
// one `gaia chain ...` call returning the final state.
//
// Phase A (shipped, PR #116):
//
//   - Linear sequences only (no parallel, no for_each, no
//     conditional branching).
//   - Shell-style variable substitution: ${var} for chain inputs,
//     ${id.field} for previous steps' captured outputs.
//   - on_failure with a structured `return:` block so agents can
//     branch on failure shape rather than parsing free text.
//   - --dry-run: substitute vars, render the resolved plan, exit.
//   - Fail-fast: chain stops on first non-zero step exit.
//
// Phase B-1 (this commit lands part of it):
//
//   - Yield/resume primitive: a step pauses on a declared
//     condition, gaia returns state + a resume_token to disk,
//     `gaia chain resume <token>` picks up where it left off.
//     State is local-only (~/.local/state/gaia/chains/<token>.yaml).
//   - Fixed yield-condition vocabulary (auth_error, not_found,
//     rate_limited, timeout, unknown_error) mapped from gaia's
//     existing exit codes. CI/merge-specific conditions
//     (check_failed, merge_conflict, ...) ship later as the
//     underlying gaia commands gain structured exits.
//
// Phase B-2 (later): per-step timeout + retry, chain-level
// default_yield_on, cleanup: block.
// Phase B-3 (later): saved chains in .gaia/chains/, dogfood
// pr-create-and-land canned chain.
// Phase C (later): parallel steps, named chain composition,
// for_each.
//
// Local-tool boundary: gaia is a CLI + local MCP. Chain state is
// per-developer-laptop disk-backed. No daemon. No cross-machine
// resume. No multi-tenant routing. See #112's body for the full
// design + scope discussion.
package chain

// Chain is a parsed YAML chain definition. Validate() enforces the
// invariants ParseFile / Parse can't catch syntactically (unique
// step IDs, etc.).
type Chain struct {
	Name        string             `yaml:"name"`
	Description string             `yaml:"description,omitempty"`
	Vars        map[string]VarSpec `yaml:"vars,omitempty"`
	Steps       []Step             `yaml:"steps"`

	// DefaultYieldOn applies to every step that does not declare
	// its own yield_on. Per-step yield_on (and abort_on) override.
	// Useful for the common pattern "yield on rate-limit or
	// timeout for everything in this chain" without repeating the
	// declaration on each step. Phase B-2.
	DefaultYieldOn []YieldCondition `yaml:"default_yield_on,omitempty"`

	// Cleanup runs on abort, in declared order, on a best-effort
	// basis (each step's success/failure is recorded but a failing
	// cleanup step doesn't stop later cleanup steps from running).
	// Cleanup steps share the chain's resolved vars + captures.
	// Result.CleanupResults carries per-step records for the agent
	// to inspect. Cleanup does NOT run on success / yield / failure
	// — only on abort, where a partially-completed chain may have
	// left orphan state behind. Phase B-2.
	Cleanup []Step `yaml:"cleanup,omitempty"`
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
// OnFailure is consulted when the step exits non-zero AND the
// failure isn't routed to yield/abort by YieldOn/AbortOn. If
// OnFailure.Return is set, those keys (with substitution applied)
// become the chain's `failure` payload and the chain stops; if it
// isn't set, a default failure envelope is emitted with the step
// ID + exit code + stderr tail.
//
// YieldOn names conditions that pause the chain and return a
// resume_token instead of failing. AbortOn names conditions that
// stop the chain (any cleanup steps run, then the chain returns
// status: aborted). When a condition is in neither, the runner
// applies the chain-level default (or, lacking that, treats it as
// a failure → on_failure / default failure envelope).
//
// Conditions reference the YieldCondition vocabulary below.
type Step struct {
	ID        string           `yaml:"id"`
	Run       string           `yaml:"run"`
	Capture   string           `yaml:"capture,omitempty"`
	YieldOn   []YieldCondition `yaml:"yield_on,omitempty"`
	AbortOn   []YieldCondition `yaml:"abort_on,omitempty"`
	OnFailure *FailureAction   `yaml:"on_failure,omitempty"`

	// Timeout caps a single step's wall-clock duration. On expiry,
	// the runner kills the subprocess and yields with condition
	// `timeout` (which routes through the step's yield_on /
	// abort_on / chain default_yield_on chain). Format is any
	// time.ParseDuration string (e.g. "30s", "5m", "1h"). Empty
	// string disables the per-step cap. Phase B-2.
	Timeout string `yaml:"timeout,omitempty"`

	// Retry tunes per-step retry-on-failure. nil disables retries
	// (default). When set, a non-zero exit re-runs the step up to
	// Max times with Delay between attempts (scaled per Backoff).
	// Final-attempt failure routes normally (yield_on, abort_on,
	// on_failure). Retries do NOT yield/abort between attempts —
	// only the final outcome routes. Phase B-2.
	Retry *RetrySpec `yaml:"retry,omitempty"`
}

// RetrySpec configures per-step retry. Sensible defaults: max 3,
// delay 1s, exponential backoff. Operators tune the knobs that
// matter for their step.
//
// Backoff strategies:
//
//	"constant"    — wait Delay between every attempt.
//	"linear"      — Delay, 2*Delay, 3*Delay, ...
//	"exponential" — Delay, 2*Delay, 4*Delay, 8*Delay, ... (default)
type RetrySpec struct {
	Max     int    `yaml:"max,omitempty"`
	Delay   string `yaml:"delay,omitempty"`
	Backoff string `yaml:"backoff,omitempty"`
}

// YieldCondition is the fixed vocabulary chains use to label step
// outcomes. Named enums beat free-form text — agents branch on a
// stable identifier without re-parsing stderr. See #112 for the
// full mapping table.
//
// Categories:
//
//	auth_error       — exitcode.Auth (4): credentials missing/invalid.
//	not_found        — exitcode.NotFound (3): resource missing upstream.
//	rate_limited     — exitcode.RateLimit (5): forge rate-limit hit.
//	timeout          — step exceeded its own timeout, or chain hit
//	                   total_timeout while waiting on this step.
//	unknown_error    — exitcode.Generic (1) or any unmapped non-zero.
//	check_failed     — non-flaky CI check failed (Phase B-3+, requires
//	                   gaia pr ci-wait support).
//	check_flaky      — flaky/retryable CI check failed (Phase B-3+).
//	merge_conflict   — gaia pr merge got 409 (Phase B-3+, requires
//	                   structured exits from gaia pr merge).
//	review_required  — protected branch needs human review (Phase B-3+).
//	policy_violation — write op blocked by branch protection or similar
//	                   (Phase B-3+).
type YieldCondition string

// Vocabulary constants. Keep lower_snake_case to match YAML idiom
// (yield_on lists pure tokens, no quoting needed).
const (
	YieldAuthError       YieldCondition = "auth_error"
	YieldNotFound        YieldCondition = "not_found"
	YieldRateLimited     YieldCondition = "rate_limited"
	YieldTimeout         YieldCondition = "timeout"
	YieldUnknownError    YieldCondition = "unknown_error"
	YieldCheckFailed     YieldCondition = "check_failed"
	YieldCheckFlaky      YieldCondition = "check_flaky"
	YieldMergeConflict   YieldCondition = "merge_conflict"
	YieldReviewRequired  YieldCondition = "review_required"
	YieldPolicyViolation YieldCondition = "policy_violation"
)

// AllYieldConditions returns the full vocabulary. Used by Validate()
// to reject unknown identifiers in YAML and by docs/tests.
func AllYieldConditions() []YieldCondition {
	return []YieldCondition{
		YieldAuthError, YieldNotFound, YieldRateLimited,
		YieldTimeout, YieldUnknownError,
		YieldCheckFailed, YieldCheckFlaky,
		YieldMergeConflict, YieldReviewRequired, YieldPolicyViolation,
	}
}

// IsKnown reports whether c is in the vocabulary. Anything else is
// a typo or a forward-incompatible chain definition; Validate()
// rejects it.
func (c YieldCondition) IsKnown() bool {
	for _, k := range AllYieldConditions() {
		if c == k {
			return true
		}
	}
	return false
}

// FailureAction is the on_failure block. v1 supports `return` only
// (the shape an agent reads when the chain stops). Later phases may
// add `retry`, `continue_on_error`, etc.
type FailureAction struct {
	Return map[string]any `yaml:"return,omitempty"`
}

// Result is the JSON envelope `gaia chain` produces.
//
// Status values:
//
//	"success"  — all steps completed.
//	"failure"  — a step exited non-zero and wasn't routed to
//	             yield/abort. FailedStep + Failure fields carry
//	             actionable detail.
//	"yielded"  — a step hit a YieldOn condition. ResumeToken +
//	             YieldReason + YieldPayload + RemainingSteps tell
//	             the agent what's pending. Calling
//	             `gaia chain resume <token>` picks up.
//	"aborted"  — a step hit an AbortOn condition (or chain hit
//	             total_timeout). AbortReason + CleanupResults
//	             carry detail. Not resumable.
type Result struct {
	Chain      string         `json:"chain"`
	Status     string         `json:"status"`
	FailedStep string         `json:"failed_step,omitempty"`
	Failure    map[string]any `json:"failure,omitempty"`
	Steps      []StepResult   `json:"steps"`
	Captured   map[string]any `json:"captured,omitempty"`
	DurationMs int64          `json:"duration_ms"`
	DryRun     bool           `json:"dry_run,omitempty"`

	// Yield fields (Status == "yielded")
	ResumeToken    string         `json:"resume_token,omitempty"`
	YieldReason    YieldCondition `json:"yield_reason,omitempty"`
	YieldPayload   map[string]any `json:"yield_payload,omitempty"`
	RemainingSteps []string       `json:"remaining_steps,omitempty"`

	// Abort fields (Status == "aborted")
	AbortReason    YieldCondition `json:"abort_reason,omitempty"`
	CleanupResults []StepResult   `json:"cleanup_results,omitempty"`
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
	DurationMs int64  `json:"duration_ms"`

	// Attempts records how many tries the runner made for this
	// step (1 = no retry needed, ≥2 = retried). Set only when
	// Step.Retry is configured. Phase B-2.
	Attempts int `json:"attempts,omitempty"`

	// TimedOut is true when the step's per-step Timeout was
	// reached and the subprocess was killed. The condition the
	// runner routes through is `timeout`. Phase B-2.
	TimedOut bool `json:"timed_out,omitempty"`
}

// Result.Status values.
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
	StatusYielded = "yielded"
	StatusAborted = "aborted"
)

// StepResult.Status values.
const (
	StepOK      = "ok"
	StepFailed  = "failed"
	StepSkipped = "skipped"
	StepYielded = "yielded"
)
