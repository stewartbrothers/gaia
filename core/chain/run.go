package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// RunOptions tunes a chain run.
type RunOptions struct {
	// DryRun substitutes vars + renders each step's resolved
	// command line, but does NOT execute. The Result still has
	// Steps populated (with Status=skipped) so dry-run output is
	// directly comparable to a real-run shape.
	DryRun bool

	// Progress, when non-nil, gets a one-line summary written for
	// each step as it runs. Useful for long chains where the
	// operator wants visibility before the final envelope is
	// emitted. Default: nil (quiet).
	Progress io.Writer

	// MaxOutputBytes caps stdout + stderr captured into each
	// StepResult. Default 4096 (4 KB). Output beyond this is
	// truncated with a `... [truncated]` marker. Captures (parsed
	// JSON envelopes) are not affected — they hold the full data.
	MaxOutputBytes int

	// StateDir is where yielded chain state is written / read.
	// Empty defaults to DefaultStateDir() at run time. Tests pass
	// a tempdir.
	StateDir string

	// Modify carries a `--decision modify` directive on Resume.
	// Required when decision == "modify"; ignored otherwise. Phase B-2.
	Modify *ModifyDirective
}

// ModifyDirective tells Resume to mutate the yielded step's vars
// before re-running. StepID must match the step that yielded —
// otherwise the operator is editing a step that hasn't run, which
// is a different feature (and would race with later substitution).
//
// Vars are merged into the chain's resolved scope, replacing any
// existing key. Captures aren't editable: they're outputs of prior
// steps, not inputs the operator can rebind.
type ModifyDirective struct {
	StepID string
	Vars   map[string]string
}

const defaultMaxOutputBytes = 4096

// ResolveVars applies the chain's `vars:` schema to the supplied
// inputs: required-but-missing returns an error; optional-with-
// default fills in. Returns the merged map ready for Substitute.
func ResolveVars(c *Chain, supplied map[string]string) (map[string]string, error) {
	out := map[string]string{}
	// Carry every supplied var through, even if the chain doesn't
	// declare it. Operators sometimes pass extra context that
	// downstream commands look for via env; preserving them is
	// cheaper than rejecting.
	for k, v := range supplied {
		out[k] = v
	}
	for name, spec := range c.Vars {
		if _, ok := out[name]; ok {
			continue
		}
		if spec.Default != "" {
			out[name] = spec.Default
			continue
		}
		if spec.Required {
			return nil, fmt.Errorf("chain %q: required var %q not supplied", c.Name, name)
		}
	}
	return out, nil
}

// Run starts a chain from scratch. Returns a non-nil *Result; outer
// error is for setup failures only (var resolution, state-dir
// resolution, etc.). Chain outcome lives in Result.Status —
// caller branches on success / failure / yielded / aborted.
func Run(ctx context.Context, c *Chain, supplied map[string]string, opts RunOptions) (*Result, error) {
	if opts.MaxOutputBytes == 0 {
		opts.MaxOutputBytes = defaultMaxOutputBytes
	}

	vars, err := ResolveVars(c, supplied)
	if err != nil {
		return nil, err
	}

	scope := Scope{Vars: vars, Captures: map[string]any{}}
	res := &Result{
		Chain:    c.Name,
		Status:   StatusSuccess,
		Steps:    make([]StepResult, 0, len(c.Steps)),
		Captured: map[string]any{},
		DryRun:   opts.DryRun,
	}

	chainStart := time.Now()
	executeSteps(ctx, c, c.Steps, 0, scope, res, opts, vars)
	if res.Status == StatusAborted {
		runCleanup(ctx, c, scope, res, opts)
	}
	res.DurationMs = time.Since(chainStart).Milliseconds()

	return res, nil
}

// Resume continues a previously-yielded chain. Loads state from
// StateDir, re-builds scope from the persisted vars + captures,
// runs from the step AFTER the one that yielded.
//
// On success, the on-disk state file is removed. On another yield,
// it's overwritten with the new state. On abort/failure, the file
// is removed too (chain is over either way).
//
// `decision`:
//
//	"continue" — re-run the step that yielded (with the same args,
//	             unless the operator modified them). Default.
//	"abort"    — skip the rest of the chain; return Status=aborted
//	             with the original yield reason as abort_reason.
//
// Phase B-1 supports continue + abort. "modify" (change next step's
// args before resuming) lands in B-2.
func Resume(ctx context.Context, token, decision string, opts RunOptions) (*Result, error) {
	if opts.MaxOutputBytes == 0 {
		opts.MaxOutputBytes = defaultMaxOutputBytes
	}
	if opts.StateDir == "" {
		dir, err := DefaultStateDir()
		if err != nil {
			return nil, err
		}
		opts.StateDir = dir
	}
	if decision == "" {
		decision = "continue"
	}

	state, err := LoadState(opts.StateDir, token)
	if err != nil {
		return nil, fmt.Errorf("chain: load state %q: %w", token, err)
	}

	if decision == "abort" {
		// Honor the operator's abort: build the aborted envelope,
		// run any cleanup: steps best-effort, then clean state.
		res := &Result{
			Chain:       state.Chain.Name,
			Status:      StatusAborted,
			AbortReason: state.YieldReason,
			Steps:       state.Steps,
			Captured:    state.Captures,
		}
		runCleanup(ctx, &state.Chain, Scope{
			Vars:     copyStringMap(state.Vars),
			Captures: copyAnyMap(state.Captures),
		}, res, opts)
		_ = DeleteState(opts.StateDir, token)
		return res, nil
	}

	if decision == "modify" {
		// Operator supplies new var values for the yielded step.
		// We validate the directive points at the right step (the
		// one that's about to re-run), merge the var changes into
		// the persisted scope, and persist them so a later yield
		// re-uses the modified vars rather than reverting.
		if opts.Modify == nil {
			return nil, fmt.Errorf("chain: --decision modify requires a Modify directive")
		}
		yieldedStep := state.Chain.Steps[state.YieldedAtStep]
		if opts.Modify.StepID != yieldedStep.ID {
			return nil, fmt.Errorf("chain: modify directive targets step %q, but the yielded step is %q (only the yielded step can be modified)", opts.Modify.StepID, yieldedStep.ID)
		}
		for k, v := range opts.Modify.Vars {
			state.Vars[k] = v
		}
		// Persist the modified state so a re-yield carries forward
		// the new vars. Save before we proceed to executeSteps so
		// a subsequent panic doesn't strand the modification.
		if err := SaveState(opts.StateDir, state); err != nil {
			return nil, fmt.Errorf("chain: persist modified state: %w", err)
		}
		// Fall through to the same "continue" path below.
	} else if decision != "continue" {
		return nil, fmt.Errorf("chain: unknown resume decision %q (want continue|abort|modify)", decision)
	}

	// Re-build scope from state. Vars are flat strings (yaml.v3
	// decodes them that way); captures may be richer (maps / scalars).
	scope := Scope{
		Vars:     copyStringMap(state.Vars),
		Captures: copyAnyMap(state.Captures),
	}

	res := &Result{
		Chain:    state.Chain.Name,
		Status:   StatusSuccess,
		Steps:    append([]StepResult(nil), state.Steps...),
		Captured: copyAnyMap(state.Captures),
	}
	// Trim the yielded step's prior result — we're re-running it.
	if n := len(res.Steps); n > 0 {
		res.Steps = res.Steps[:n-1]
	}

	chainStart := time.Now()
	executeSteps(ctx, &state.Chain, state.Chain.Steps, state.YieldedAtStep, scope, res, opts, state.Vars)
	if res.Status == StatusAborted {
		runCleanup(ctx, &state.Chain, scope, res, opts)
	}
	res.DurationMs = time.Since(chainStart).Milliseconds()

	// Always remove the OLD state file. If the chain yielded again,
	// executeSteps wrote a fresh state file with a NEW token (the new
	// canonical resume target). The old token represents stale state
	// and would otherwise leak into `gaia chain list`.
	_ = DeleteState(opts.StateDir, token)
	return res, nil
}

// executeSteps drives the step loop shared by Run and Resume.
// `startAt` is the index in stepsList to begin at (0 for Run, the
// yielded-step index for Resume). `vars` is the resolved var map
// (passed separately so we can persist it on yield).
//
// Per-step semantics (Phase B-2 additions in parens):
//   - substitute ${vars} / ${captures.field} into step.Run
//   - unresolved refs → chain failure (unresolved_variables)
//   - (run with optional Step.Timeout via context.WithTimeout)
//   - (retry up to Step.Retry.Max times with delay/backoff between
//     attempts; only the FINAL attempt routes through yield/abort)
//   - on non-zero: route by yield_on / abort_on / chain
//     default_yield_on, then on_failure, then default failure
//
// Side effects on res:
//   - appends to res.Steps for each step that runs
//   - mutates res.Status / res.FailedStep / res.Failure on failure
//   - mutates res.Status / res.ResumeToken / res.YieldReason / etc. on yield
//   - mutates res.Captured for each captured step
func executeSteps(ctx context.Context, c *Chain, stepsList []Step, startAt int, scope Scope, res *Result, opts RunOptions, vars map[string]string) {
	for i := startAt; i < len(stepsList); i++ {
		step := stepsList[i]
		// Use SubstituteShell — the resolved string is fed to `sh -c`
		// and substituted values must be treated as shell-literal data.
		// See #135 / docs/chain.md "Security: variable substitution
		// semantics" for the rationale.
		resolved, unresolved := SubstituteShell(step.Run, scope)
		sr := StepResult{
			ID:     step.ID,
			Run:    resolved,
			Status: StepSkipped,
		}

		if opts.DryRun {
			res.Steps = append(res.Steps, sr)
			if opts.Progress != nil {
				_, _ = fmt.Fprintf(opts.Progress, "[%s] (dry-run) %s\n", step.ID, resolved)
			}
			continue
		}

		// Hard failure: an unresolved ref is a chain-design error
		// rather than a runtime condition; can't yield/retry our way
		// out. Goes through the failure path.
		if len(unresolved) > 0 {
			sr.Status = StepFailed
			sr.Stderr = "unresolved variable references: " + strings.Join(unresolved, ", ")
			res.Steps = append(res.Steps, sr)
			res.Status = StatusFailure
			res.FailedStep = step.ID
			res.Failure = buildFailure(step, scope, "unresolved_variables", sr.Stderr, "")
			return
		}

		// Run-with-retry. Each attempt may set its own deadline via
		// Step.Timeout. The final attempt's outcome is what routes
		// through yield/abort/failure. Intermediate attempts just
		// sleep and try again. DurationMs covers the whole
		// retry sequence so an operator sees real wall-clock cost.
		stepStart := time.Now()
		stdout, stderr, exitCode, attempts, timedOut, runErr := runWithRetry(ctx, step, resolved)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		sr.Stdout = truncate(stdout, opts.MaxOutputBytes)
		sr.Stderr = truncate(stderr, opts.MaxOutputBytes)
		sr.ExitCode = exitCode
		if step.Retry != nil {
			sr.Attempts = attempts
		}
		sr.TimedOut = timedOut

		if opts.Progress != nil {
			status := "ok"
			if runErr != nil || exitCode != 0 {
				status = "failed"
			}
			_, _ = fmt.Fprintf(opts.Progress, "[%s] %s\n", step.ID, status)
		}

		if runErr != nil || exitCode != 0 {
			// Timeout outranks exit-code mapping: when the runner
			// killed the subprocess, the condition is `timeout`
			// regardless of what exit code the SIGKILL'd shell
			// happened to surface.
			condition := MapExitCode(exitCode)
			if timedOut {
				condition = YieldTimeout
			}

			// Routing order: per-step yield_on first (caller wants pause),
			// per-step abort_on second (caller wants stop), chain
			// default_yield_on third (only when step.YieldOn is empty),
			// fall-through to failure last.
			//
			// default_yield_on does NOT apply when the step has its
			// own non-empty yield_on — the per-step list is the
			// authoritative whitelist for that step.
			yieldList := step.YieldOn
			if len(yieldList) == 0 {
				yieldList = c.DefaultYieldOn
			}

			if containsCondition(yieldList, condition) {
				sr.Status = StepYielded
				res.Steps = append(res.Steps, sr)
				if err := emitYield(c, stepsList, i, condition, sr, scope, vars, res, opts); err != nil {
					// State save failed — surface as a chain failure
					// with a clear reason.
					res.Status = StatusFailure
					res.FailedStep = step.ID
					res.Failure = map[string]any{
						"reason": "yield_state_save_failed",
						"step":   step.ID,
						"error":  err.Error(),
					}
				}
				return
			}

			if containsCondition(step.AbortOn, condition) {
				sr.Status = StepFailed
				res.Steps = append(res.Steps, sr)
				res.Status = StatusAborted
				res.AbortReason = condition
				return
			}

			// No declared routing: existing failure flow.
			sr.Status = StepFailed
			res.Steps = append(res.Steps, sr)
			res.Status = StatusFailure
			res.FailedStep = step.ID
			reason := "step_exited_nonzero"
			if timedOut {
				reason = "step_timed_out"
			}
			errStr := strings.TrimSpace(stderr)
			if errStr == "" && runErr != nil {
				errStr = runErr.Error()
			}
			res.Failure = buildFailure(step, scope, reason, errStr, stdout)
			return
		}

		sr.Status = StepOK
		if step.Capture != "" {
			scope.Captures[step.Capture] = decodeCapture(stdout)
			res.Captured[step.Capture] = scope.Captures[step.Capture]
		}
		res.Steps = append(res.Steps, sr)
	}
}

// runWithRetry runs a single step with optional per-step timeout and
// retry. Returns the FINAL attempt's outcome plus the total attempts
// made and whether the final attempt timed out.
//
// Retry behavior:
//   - Step.Retry == nil: one attempt, no sleep.
//   - Step.Retry.Max == N: up to N+1 total attempts (initial + N retries).
//   - Between attempts: sleep Step.Retry.Delay scaled by backoff.
//
// Timeout behavior:
//   - Step.Timeout != "": each attempt gets a fresh
//     context.WithTimeout. Expiry → kill subprocess, route via
//     YieldTimeout. Retries do NOT extend across attempts — each
//     attempt has its own deadline.
//
// Final-attempt-only routing means a transient failure that recovers
// on retry produces a clean StepOK — the chain doesn't yield in the
// middle of a retry sequence.
func runWithRetry(ctx context.Context, step Step, resolved string) (stdout, stderr string, exitCode, attempts int, timedOut bool, runErr error) {
	max := 0
	var delay time.Duration
	backoff := "exponential"
	if step.Retry != nil {
		max = step.Retry.Max
		if step.Retry.Delay != "" {
			delay, _ = time.ParseDuration(step.Retry.Delay) // already validated
		}
		if step.Retry.Backoff != "" {
			backoff = step.Retry.Backoff
		}
	}

	var stepTimeout time.Duration
	if step.Timeout != "" {
		stepTimeout, _ = time.ParseDuration(step.Timeout) // already validated
	}

	for attempt := 0; attempt <= max; attempt++ {
		attempts = attempt + 1

		// Fresh per-attempt context if the step declares a timeout.
		// Without the per-attempt fresh context, a long-running first
		// attempt would consume the whole step's deadline budget.
		runCtx := ctx
		var cancel context.CancelFunc
		if stepTimeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, stepTimeout)
		}

		stdout, stderr, exitCode, runErr = execShell(runCtx, resolved)
		// timedOut: we consider an attempt timed-out when our
		// context's deadline expired AND the run errored. The
		// shell typically exits 137/143/-1 in this case.
		timedOut = false
		if runCtx.Err() != nil && (runErr != nil || exitCode != 0) {
			timedOut = true
		}
		if cancel != nil {
			cancel()
		}

		// Success? Done.
		if runErr == nil && exitCode == 0 {
			return stdout, stderr, exitCode, attempts, false, nil
		}

		// Final attempt? Return what we have.
		if attempt >= max {
			return stdout, stderr, exitCode, attempts, timedOut, runErr
		}

		// Sleep with backoff before the next attempt. Honor parent
		// ctx cancellation so a cancelled chain doesn't keep
		// retrying.
		sleep := backoffDelay(delay, attempt, backoff)
		if sleep > 0 {
			select {
			case <-ctx.Done():
				return stdout, stderr, exitCode, attempts, timedOut, runErr
			case <-time.After(sleep):
			}
		}
	}
	return stdout, stderr, exitCode, attempts, timedOut, runErr
}

// backoffDelay computes the per-attempt sleep before the (attempt+1)-th
// retry. attempt is 0-based: attempt=0 is the gap after the FIRST
// failure, attempt=1 after the second, etc.
//
// constant     — base every time
// linear       — base * (attempt+1)         (1×, 2×, 3×, ...)
// exponential  — base * 2^attempt           (1×, 2×, 4×, 8×, ...)
func backoffDelay(base time.Duration, attempt int, backoff string) time.Duration {
	if base <= 0 {
		return 0
	}
	switch backoff {
	case "constant":
		return base
	case "linear":
		return base * time.Duration(attempt+1)
	case "exponential", "":
		return base * (1 << attempt)
	default:
		return base
	}
}

// runCleanup runs the chain's cleanup steps best-effort, recording
// each in res.CleanupResults. Failing cleanup steps don't stop later
// cleanup steps from running — the goal is to clean up as much as
// possible even if some pieces are already broken.
//
// Cleanup steps share the chain's resolved scope (vars + captures
// from the main run), so an operator can reference ${pr.number} in
// `gaia pr close` to close whatever the main run created. Cleanup
// steps cannot capture (no later steps to consume it) and cannot
// yield (the chain is already aborted).
//
// Errors during cleanup are recorded but not propagated; a failing
// cleanup step is just a StepResult{Status: failed, ...} entry. A
// cleanup step with unresolved vars is similarly recorded as failed
// without halting the rest of cleanup.
func runCleanup(ctx context.Context, c *Chain, scope Scope, res *Result, opts RunOptions) {
	if len(c.Cleanup) == 0 {
		return
	}
	for _, step := range c.Cleanup {
		// Cleanup steps run via `sh -c` exactly like main steps; same
		// shell-quoting requirement applies (#135).
		resolved, unresolved := SubstituteShell(step.Run, scope)
		sr := StepResult{ID: step.ID, Run: resolved}

		if len(unresolved) > 0 {
			sr.Status = StepFailed
			sr.Stderr = "unresolved variable references: " + strings.Join(unresolved, ", ")
			res.CleanupResults = append(res.CleanupResults, sr)
			continue
		}

		stepStart := time.Now()
		stdout, stderr, exitCode, runErr := execShell(ctx, resolved)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		sr.ExitCode = exitCode
		sr.Stdout = truncate(stdout, opts.MaxOutputBytes)
		sr.Stderr = truncate(stderr, opts.MaxOutputBytes)
		if runErr != nil || exitCode != 0 {
			sr.Status = StepFailed
		} else {
			sr.Status = StepOK
		}
		res.CleanupResults = append(res.CleanupResults, sr)
	}
}

// emitYield serializes the yielded chain to disk and populates the
// yield-related fields on res. Returns an error if state-save fails.
func emitYield(c *Chain, stepsList []Step, yieldIdx int, condition YieldCondition, sr StepResult, scope Scope, vars map[string]string, res *Result, opts RunOptions) error {
	stateDir := opts.StateDir
	if stateDir == "" {
		dir, err := DefaultStateDir()
		if err != nil {
			return err
		}
		stateDir = dir
	}

	token, err := NewToken()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	state := &State{
		SchemaVersion: CurrentSchemaVersion,
		Token:         token,
		CreatedAt:     now,
		YieldedAt:     now,
		YieldedAtStep: yieldIdx,
		YieldReason:   condition,
		YieldPayload: map[string]any{
			"step":      stepsList[yieldIdx].ID,
			"exit_code": sr.ExitCode,
			"stderr":    sr.Stderr,
			"stdout":    sr.Stdout,
		},
		Chain:    *c,
		Vars:     copyStringMap(vars),
		Captures: copyAnyMap(scope.Captures),
		Steps:    append([]StepResult(nil), res.Steps...),
	}

	if err := SaveState(stateDir, state); err != nil {
		return err
	}

	res.Status = StatusYielded
	res.ResumeToken = token
	res.YieldReason = condition
	res.YieldPayload = state.YieldPayload
	res.RemainingSteps = remainingStepIDs(stepsList, yieldIdx+1)
	return nil
}

// remainingStepIDs lists step IDs from start onward — what's left
// to run. Empty when start is past the end.
func remainingStepIDs(steps []Step, start int) []string {
	if start >= len(steps) {
		return nil
	}
	out := make([]string, 0, len(steps)-start)
	for i := start; i < len(steps); i++ {
		out = append(out, steps[i].ID)
	}
	return out
}

func containsCondition(list []YieldCondition, c YieldCondition) bool {
	for _, x := range list {
		if x == c {
			return true
		}
	}
	return false
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// execShell runs cmd via "sh -c". Returns stdout, stderr, exit code,
// and any unexpected runtime error (process couldn't be started,
// etc.). A normal non-zero exit is reported via the exit code; runErr
// stays nil.
func execShell(ctx context.Context, cmd string) (stdout, stderr string, exitCode int, err error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	runErr := c.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return stdout, stderr, exitErr.ExitCode(), nil
		}
		return stdout, stderr, -1, runErr
	}
	return stdout, stderr, 0, nil
}

// decodeCapture parses stdout as JSON, returning the envelope's
// `data` field if present, the whole JSON value otherwise, and
// the trimmed string when stdout isn't JSON. This lets gaia
// commands (envelope-shaped) and arbitrary shell tools coexist as
// step outputs.
func decodeCapture(stdout string) any {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return ""
	}
	var anyVal any
	if err := json.Unmarshal([]byte(trimmed), &anyVal); err != nil {
		return trimmed
	}
	if obj, ok := anyVal.(map[string]any); ok {
		if data, hasData := obj["data"]; hasData {
			return data
		}
	}
	return anyVal
}

// buildFailure assembles the failure payload that goes into
// Result.Failure. If the step has on_failure.return, those keys
// are substituted (recursively) using the scope plus a synthesized
// ${error} that holds the step's stderr tail. If on_failure isn't
// set, returns a default shape with the reason + the step ID.
func buildFailure(step Step, scope Scope, defaultReason, errMsg, stdout string) map[string]any {
	if step.OnFailure == nil || step.OnFailure.Return == nil {
		return map[string]any{
			"reason": defaultReason,
			"step":   step.ID,
			"stderr": truncate(errMsg, defaultMaxOutputBytes),
		}
	}
	// Synthesize ${error} for on_failure substitution. Operators
	// who write `on_failure.return.error: "${error}"` get the
	// stderr tail in their failure envelope.
	failScope := scope
	failScope.Captures = map[string]any{}
	for k, v := range scope.Captures {
		failScope.Captures[k] = v
	}
	failScope.Captures["error"] = map[string]any{
		"message": errMsg,
		"stdout":  truncate(stdout, defaultMaxOutputBytes),
		"step":    step.ID,
	}
	return substAny(step.OnFailure.Return, failScope).(map[string]any)
}

// substAny recursively substitutes ${...} refs in every string
// inside a YAML-decoded value tree. Used for on_failure.return map
// values which are emitted as JSON, not handed to a shell — so this
// path uses SubstituteRaw deliberately (the values must round-trip
// verbatim into the failure envelope, not be shell-quoted).
func substAny(v any, scope Scope) any {
	switch x := v.(type) {
	case string:
		out, _ := SubstituteRaw(x, scope)
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = substAny(vv, scope)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = substAny(vv, scope)
		}
		return out
	default:
		return x
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + " ... [truncated]"
}
