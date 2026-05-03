package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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

	// SubChainResolver, when non-nil, resolves a `chain:` reference
	// (a saved-chain name or path) into a parsed *Chain. Required
	// for Phase C composition; the CLI wires the same Resolve()
	// path saved-chain dispatch uses, but the core package keeps
	// the policy injection-style so tests can fake it. Without a
	// resolver, a step with chain: != "" fails with a clear
	// `chain_resolver_unavailable` reason.
	SubChainResolver func(name string) (*Chain, error)

	// MaxChainDepth caps recursive chain composition (one chain
	// invoking another invoking another). Default 5. <=0 → use
	// default. A chain whose call tree exceeds the cap fails with
	// reason `chain_recursion_limit`.
	MaxChainDepth int

	// chainDepth is internal — incremented when runChainStep
	// recurses. Operators set MaxChainDepth, the runner manages
	// chainDepth through option-passing.
	chainDepth int

	// chainStack tracks the resolved chain names already on the
	// composition stack for cycle detection ("chain A → chain B →
	// chain A" trips this). Internal; runChainStep manages it.
	chainStack []string

	// pendingInnerToken, when non-empty, tells runChainStep to
	// Resume() the inner chain at its yield point rather than
	// starting fresh. Set by the outer Resume() path when the
	// yielded outer step is a chain: composition whose SubResult
	// preserved an inner ResumeToken. Internal.
	pendingInnerToken string
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

	// Phase C nested yield: if the yielded step is a chain
	// composition (chain: != "") AND its preserved StepResult has
	// a SubResult with a non-empty ResumeToken, hand that token
	// off to the inner chain so it picks up at its yield point.
	// Without this, resume would restart the inner chain from
	// scratch, blowing away whatever the inner had captured pre-
	// yield.
	yieldedStep := state.Chain.Steps[state.YieldedAtStep]
	if yieldedStep.Chain != "" && len(state.Steps) > 0 {
		last := state.Steps[len(state.Steps)-1]
		if last.SubResult != nil && last.SubResult.ResumeToken != "" {
			opts.pendingInnerToken = last.SubResult.ResumeToken
		}
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
// Phase C: each step dispatches on mode (run / parallel / for_each /
// chain) via runStep. Routing semantics (yield / abort / failure)
// apply uniformly regardless of mode.
//
// Side effects on res:
//   - appends to res.Steps for each step that runs
//   - mutates res.Status / res.FailedStep / res.Failure on failure
//   - mutates res.Status / res.ResumeToken / res.YieldReason / etc. on yield
//   - mutates res.Captured for each captured step
func executeSteps(ctx context.Context, c *Chain, stepsList []Step, startAt int, scope Scope, res *Result, opts RunOptions, vars map[string]string) {
	for i := startAt; i < len(stepsList); i++ {
		step := stepsList[i]

		if opts.DryRun {
			sr := dryRunStep(step, scope, opts.Progress)
			res.Steps = append(res.Steps, sr)
			continue
		}

		sr, outcome := runStep(ctx, c, step, scope, opts, vars)

		// Capture outcome's captured value before routing — even on
		// failure the operator can inspect partial output via
		// res.Steps[N].
		res.Steps = append(res.Steps, sr)

		switch outcome.kind {
		case stepOutcomeOK:
			if step.Capture != "" {
				scope.Captures[step.Capture] = outcome.capturedValue
				res.Captured[step.Capture] = outcome.capturedValue
			}
		case stepOutcomeYielded:
			if err := emitYield(c, stepsList, i, outcome.condition, sr, scope, vars, res, opts); err != nil {
				res.Status = StatusFailure
				res.FailedStep = step.ID
				res.Failure = map[string]any{
					"reason": "yield_state_save_failed",
					"step":   step.ID,
					"error":  err.Error(),
				}
			}
			return
		case stepOutcomeAborted:
			res.Status = StatusAborted
			res.AbortReason = outcome.condition
			return
		case stepOutcomeFailed:
			res.Status = StatusFailure
			res.FailedStep = step.ID
			res.Failure = outcome.failure
			return
		}
	}
}

// stepOutcome encodes how the per-step runner wants its result
// routed by the loop in executeSteps. Keeping the routing in one
// place lets the per-mode helpers stay focused on "what did this
// step do" rather than "where does the chain go from here".
type stepOutcome struct {
	kind          stepOutcomeKind
	condition     YieldCondition // for yielded / aborted
	failure       map[string]any // for failed
	capturedValue any            // for OK; nil when no capture
}

type stepOutcomeKind int

const (
	stepOutcomeOK stepOutcomeKind = iota
	stepOutcomeYielded
	stepOutcomeAborted
	stepOutcomeFailed
)

// dryRunStep renders a step's resolved form without executing.
// Mirrors the original dry-run path; doesn't recurse into parallel
// blocks or for_each iterations (they keep the same shape but
// substitute the static template).
func dryRunStep(step Step, scope Scope, progress io.Writer) StepResult {
	resolved, _ := SubstituteShell(step.Run, scope)
	sr := StepResult{ID: step.ID, Run: resolved, Status: StepSkipped}
	if progress != nil {
		_, _ = fmt.Fprintf(progress, "[%s] (dry-run) %s\n", step.ID, resolved)
	}
	return sr
}

// runStep dispatches one step on its mode (run / parallel / for_each
// / chain). Returns the StepResult to record plus a stepOutcome the
// caller routes. Phase C.
func runStep(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions, vars map[string]string) (StepResult, stepOutcome) {
	switch {
	case step.Parallel != nil:
		return runParallelStep(ctx, c, step, scope, opts, vars)
	case step.ForEach != "":
		return runForEachStep(ctx, c, step, scope, opts, vars)
	case step.Chain != "":
		return runChainStep(ctx, c, step, scope, opts, vars)
	default:
		return runLeafStep(ctx, c, step, scope, opts)
	}
}

// runLeafStep handles the original `run:` mode — substitute, exec
// via sh -c (with retry + timeout), classify the exit, route through
// yield_on / abort_on / failure.
func runLeafStep(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions) (StepResult, stepOutcome) {
	// Use SubstituteShell — the resolved string is fed to `sh -c`
	// and substituted values must be treated as shell-literal data.
	// See #135 / docs/chain.md "Security: variable substitution
	// semantics" for the rationale.
	resolved, unresolved := SubstituteShell(step.Run, scope)
	sr := StepResult{ID: step.ID, Run: resolved, Status: StepSkipped}

	if len(unresolved) > 0 {
		sr.Status = StepFailed
		sr.Stderr = "unresolved variable references: " + strings.Join(unresolved, ", ")
		return sr, stepOutcome{
			kind:    stepOutcomeFailed,
			failure: buildFailure(step, scope, "unresolved_variables", sr.Stderr, ""),
		}
	}

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
		condition := MapExitCode(exitCode)
		if timedOut {
			condition = YieldTimeout
		}

		yieldList := step.YieldOn
		if len(yieldList) == 0 {
			yieldList = c.DefaultYieldOn
		}

		if containsCondition(yieldList, condition) {
			sr.Status = StepYielded
			return sr, stepOutcome{kind: stepOutcomeYielded, condition: condition}
		}

		if containsCondition(step.AbortOn, condition) {
			sr.Status = StepFailed
			return sr, stepOutcome{kind: stepOutcomeAborted, condition: condition}
		}

		sr.Status = StepFailed
		reason := "step_exited_nonzero"
		if timedOut {
			reason = "step_timed_out"
		}
		errStr := strings.TrimSpace(stderr)
		if errStr == "" && runErr != nil {
			errStr = runErr.Error()
		}
		return sr, stepOutcome{
			kind:    stepOutcomeFailed,
			failure: buildFailure(step, scope, reason, errStr, stdout),
		}
	}

	sr.Status = StepOK
	var captured any
	if step.Capture != "" {
		captured = decodeCapture(stdout)
	}
	return sr, stepOutcome{kind: stepOutcomeOK, capturedValue: captured}
}

// runParallelStep fans out a parallel block's sub-steps with a
// bounded goroutine pool, collects every sub-step's result, and
// routes outcomes per the priority abort > yield > fail > ok.
//
// Concurrency:
//   - max_concurrent (default 5) caps simultaneous goroutines via
//     a buffered semaphore channel.
//   - fail_fast: when true and any sub-step finishes non-OK, the
//     runner cancels the inner context to short-circuit siblings.
//   - sub-steps see the chain's vars + captures (a deep copy of
//     scope at fan-out time) but NOT each other's captures —
//     parallel siblings have no ordering guarantee, so any
//     dependency must be a serial step.
//
// Routing (priority order):
//  1. abort: any sub-step routes to abort → outer step aborts with
//     that condition.
//  2. yield: any sub-step routes to yield → outer step yields
//     with that condition. (state-save and resume token wiring
//     happens at the loop level, not here.)
//  3. fail: any sub-step routes to fail → outer step fails;
//     `failed_substep` carries the first failed sub-step's ID.
//  4. ok: every sub-step OK → outer step OK. The sub-step capture
//     namespace becomes a map ${outer.<sub-id>.<capture-key>}
//     downstream — via SubSteps on the StepResult, surfaced by
//     the substituter.
//
// Phase C / #149.
func runParallelStep(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions, _ map[string]string) (StepResult, stepOutcome) {
	sr := StepResult{ID: step.ID, Status: StepSkipped}
	stepStart := time.Now()
	defer func() {
		sr.DurationMs = time.Since(stepStart).Milliseconds()
	}()

	pblock := step.Parallel
	maxConc := pblock.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 5
	}

	// Each sub-step runs against its own scope clone so concurrent
	// goroutines don't fight over scope.Captures during writes.
	// Sub-step captures are then aggregated back into the outer
	// SubSteps slice in declaration order.
	results := make([]StepResult, len(pblock.Steps))
	outcomes := make([]stepOutcome, len(pblock.Steps))

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	for i, sub := range pblock.Steps {
		wg.Add(1)
		go func(idx int, sub Step) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-subCtx.Done():
				// Cancelled before we even started — record a
				// skipped status and bail.
				results[idx] = StepResult{ID: sub.ID, Status: StepSkipped, Stderr: "cancelled by sibling fail_fast"}
				return
			}
			defer func() { <-sem }()

			subScope := cloneScope(scope)
			r, o := runStep(subCtx, c, sub, subScope, opts, nil)
			results[idx] = r
			outcomes[idx] = o
			if pblock.FailFast && o.kind != stepOutcomeOK {
				cancel()
			}
		}(i, sub)
	}
	wg.Wait()
	sr.SubSteps = results

	// Aggregate captures from sub-steps with non-empty Capture into
	// a map keyed by sub-step ID. Each sub-step's captured value
	// becomes a field on the outer step's captured object so
	// downstream `${outer.<sub>.<field>}` substitutions resolve.
	subCapture := map[string]any{}
	for i, sub := range pblock.Steps {
		if sub.Capture != "" && outcomes[i].kind == stepOutcomeOK {
			subCapture[sub.ID] = outcomes[i].capturedValue
		}
		// Also expose the sub-step's full result tree under its ID
		// (so ${outer.<sub>.exit_code} works) when no capture was
		// explicit — convenience for parallel fan-outs that don't
		// pipe values forward.
		if _, exists := subCapture[sub.ID]; !exists {
			subCapture[sub.ID] = subStepCaptureView(results[i])
		}
	}

	// Route by priority abort > yield > fail.
	var firstYield, firstAbort, firstFail int = -1, -1, -1
	for i, o := range outcomes {
		switch o.kind {
		case stepOutcomeAborted:
			if firstAbort < 0 {
				firstAbort = i
			}
		case stepOutcomeYielded:
			if firstYield < 0 {
				firstYield = i
			}
		case stepOutcomeFailed:
			if firstFail < 0 {
				firstFail = i
			}
		}
	}

	if firstAbort >= 0 {
		sr.Status = StepFailed
		return sr, stepOutcome{kind: stepOutcomeAborted, condition: outcomes[firstAbort].condition}
	}
	if firstYield >= 0 {
		sr.Status = StepYielded
		return sr, stepOutcome{kind: stepOutcomeYielded, condition: outcomes[firstYield].condition}
	}
	if firstFail >= 0 {
		sr.Status = StepFailed
		fail := outcomes[firstFail].failure
		if fail == nil {
			fail = map[string]any{}
		}
		fail["failed_substep"] = pblock.Steps[firstFail].ID
		fail["step"] = step.ID
		return sr, stepOutcome{kind: stepOutcomeFailed, failure: fail}
	}

	sr.Status = StepOK
	return sr, stepOutcome{kind: stepOutcomeOK, capturedValue: subCapture}
}

// runForEachStep iterates a captured array, running the step's
// per-iteration body (run: or chain:) once per element. Each
// iteration sees a scope that adds `item` and `index` to the
// captures namespace so substitutions like ${item}, ${item.field},
// or ${index} resolve as expected.
//
// Modes:
//   - parallel: false (default) → serial iteration in declaration
//     order. A failed iteration stops further iterations and
//     propagates failure.
//   - parallel: true            → concurrent iteration up to
//     MaxConcurrent (default 5). All iterations run before the
//     overall outcome is computed; routing follows the same abort
//     > yield > fail priority parallel blocks use.
//
// The iterable source is taken from step.ForEach (a substitution
// reference like `${items}`). It MUST resolve to a JSON array; a
// non-array (string / object / scalar) is a hard failure with a
// clear reason. Empty array → step OK with empty SubSteps.
//
// Phase C / #149.
func runForEachStep(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions, _ map[string]string) (StepResult, stepOutcome) {
	sr := StepResult{ID: step.ID, Status: StepSkipped}
	stepStart := time.Now()
	defer func() {
		sr.DurationMs = time.Since(stepStart).Milliseconds()
	}()

	// Resolve the iterable. step.ForEach is a literal `${ref}` — we
	// peel the wrapper and look up against the scope's captures.
	items, err := resolveIterable(step.ForEach, scope)
	if err != nil {
		sr.Status = StepFailed
		sr.Stderr = err.Error()
		return sr, stepOutcome{
			kind: stepOutcomeFailed,
			failure: map[string]any{
				"reason": "for_each_not_iterable",
				"step":   step.ID,
				"error":  err.Error(),
			},
		}
	}

	if len(items) == 0 {
		// Empty iterable is fine — the step succeeds with no work.
		sr.Status = StepOK
		return sr, stepOutcome{kind: stepOutcomeOK, capturedValue: []any{}}
	}

	// Build the per-iteration step body. Per the parser, exactly
	// one of step.Run or step.Chain is set alongside for_each.
	// Each iteration uses a synthesized Step with the same body and
	// inheritable knobs (timeout, retry, yield_on, abort_on,
	// on_failure) — so a transient failure in one iteration can
	// retry inside its own iteration without re-running siblings.
	results := make([]StepResult, len(items))
	outcomes := make([]stepOutcome, len(items))

	var ranCount int
	if step.ParallelIter {
		runForEachConcurrent(ctx, c, step, scope, opts, items, results, outcomes)
		ranCount = len(items)
	} else {
		ranCount = runForEachSerial(ctx, c, step, scope, opts, items, results, outcomes)
	}
	// Trim trailing un-run iterations off SubSteps so the outer
	// envelope matches what actually executed.
	sr.SubSteps = results[:ranCount]

	// Aggregate the outer captured value as a list of per-iteration
	// captures. Operators reference ${step-id.0.field} for the 0th
	// iteration's capture, ${step-id.1.field} for the 1st, etc.
	// The per-iteration capture is the iteration's run output
	// (parsed via decodeCapture) when step.Capture is set.
	captured := make([]any, ranCount)
	for i := 0; i < ranCount; i++ {
		if outcomes[i].kind == stepOutcomeOK && step.Capture != "" {
			captured[i] = outcomes[i].capturedValue
		} else {
			captured[i] = subStepCaptureView(results[i])
		}
	}

	// Route by priority abort > yield > fail.
	for i, o := range outcomes {
		if o.kind == stepOutcomeAborted {
			sr.Status = StepFailed
			return sr, stepOutcome{kind: stepOutcomeAborted, condition: o.condition}
		}
		_ = i
	}
	for i, o := range outcomes {
		if o.kind == stepOutcomeYielded {
			sr.Status = StepYielded
			return sr, stepOutcome{kind: stepOutcomeYielded, condition: o.condition}
		}
		_ = i
	}
	for i, o := range outcomes {
		if o.kind == stepOutcomeFailed {
			sr.Status = StepFailed
			fail := o.failure
			if fail == nil {
				fail = map[string]any{}
			}
			fail["failed_iteration"] = i
			fail["step"] = step.ID
			return sr, stepOutcome{kind: stepOutcomeFailed, failure: fail}
		}
	}

	sr.Status = StepOK
	return sr, stepOutcome{kind: stepOutcomeOK, capturedValue: captured}
}

// runForEachSerial runs iterations in declaration order, stopping
// at the first non-OK outcome. Returns the number of iterations
// that actually ran (so the caller can trim the result slice).
func runForEachSerial(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions, items []any, results []StepResult, outcomes []stepOutcome) int {
	for i, item := range items {
		iterScope := scopeWithItem(scope, item, i)
		iter := iterationStep(step, i)
		r, o := runStep(ctx, c, iter, iterScope, opts, nil)
		results[i] = r
		outcomes[i] = o
		if o.kind != stepOutcomeOK {
			return i + 1
		}
	}
	return len(items)
}

// runForEachConcurrent runs iterations in parallel, bounded by
// max_concurrent. All iterations complete before routing — same
// shape as parallel-block fan-out.
func runForEachConcurrent(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions, items []any, results []StepResult, outcomes []stepOutcome) {
	maxConc := step.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 5
	}
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(idx int, it any) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-subCtx.Done():
				results[idx] = StepResult{ID: fmt.Sprintf("%s[%d]", step.ID, idx), Status: StepSkipped}
				return
			}
			defer func() { <-sem }()
			iterScope := scopeWithItem(scope, it, idx)
			iter := iterationStep(step, idx)
			r, o := runStep(subCtx, c, iter, iterScope, opts, nil)
			results[idx] = r
			outcomes[idx] = o
			if step.FailFast && o.kind != stepOutcomeOK {
				cancel()
			}
		}(i, item)
	}
	wg.Wait()
}

// iterationStep clones the Step into a per-iteration leaf step for
// the runner to dispatch. The synthesized step has its for_each /
// parallel knobs cleared (the dispatcher would otherwise recurse
// into for_each forever) and its ID suffixed with [<index>] so
// per-iteration log lines and StepResult IDs distinguish.
func iterationStep(step Step, index int) Step {
	// Strip iteration-only knobs so the inner runStep call sees a
	// pure leaf or pure chain step.
	out := step
	out.ID = fmt.Sprintf("%s[%d]", step.ID, index)
	out.ForEach = ""
	out.Parallel = nil
	out.ParallelIter = false
	// Capture is preserved when set so per-iteration capture lookup
	// works through subStepCaptureView fallback.
	return out
}

// scopeWithItem returns a new Scope with `item` and `index` added
// to the captures namespace. The original scope is left untouched
// so concurrent iterations don't fight.
//
// `item`: the per-iteration value (any JSON shape).
// `index`: the 0-based ordinal as an int (rendered as "0", "1", ...).
func scopeWithItem(s Scope, item any, index int) Scope {
	out := cloneScope(s)
	out.Captures["item"] = item
	out.Captures["index"] = index
	return out
}

// resolveIterable looks up the for_each reference against the scope
// and returns the result as a slice. The reference must point at a
// JSON array (typed []any after decodeCapture). Anything else is an
// error with the actual type named.
//
// The reference syntax is the standard substitution form — typically
// `${captured_name}` or `${captured.subfield}` — but parser-level we
// just resolve it via the same lookup path and check the resulting
// shape.
func resolveIterable(ref string, scope Scope) ([]any, error) {
	// Strip the ${...} wrapper so we resolve directly against the
	// scope. If the operator wrote `for_each: items` (no wrapper),
	// we tolerate that too.
	trimmed := strings.TrimSpace(ref)
	if strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}") {
		trimmed = trimmed[2 : len(trimmed)-1]
	}
	val, ok := lookupRaw(trimmed, scope)
	if !ok {
		return nil, fmt.Errorf("for_each: reference %q did not resolve to anything in scope", ref)
	}
	switch v := val.(type) {
	case []any:
		return v, nil
	case nil:
		return nil, fmt.Errorf("for_each: reference %q resolved to null (not iterable)", ref)
	default:
		return nil, fmt.Errorf("for_each: reference %q must resolve to a JSON array; got %T", ref, val)
	}
}

// runChainStep dispatches a saved chain as a single step. The inner
// chain runs with vars derived from substitution against the outer
// scope (so `vars: {pr: "${pr.number}"}` pipes the outer step's
// captured pr.number down). The inner chain's final captured map
// becomes this step's captured value; downstream steps see
// ${this-step.<inner-capture>.<field>} for any field the inner
// chain captured.
//
// Recursion + cycle protection:
//   - opts.MaxChainDepth (default 5) caps the call tree depth.
//     Hitting the cap fails with reason `chain_recursion_limit`.
//   - opts.chainStack tracks the resolved chain names already on
//     the path. A repeat name fails with reason `chain_cycle`,
//     listing the cycle for the operator.
//
// Failure / yield bubbling:
//   - Inner chain success → outer step OK; capture = inner.Captured
//     plus the inner Result attached as SubResult for richer agent
//     introspection.
//   - Inner chain failure → outer step fails; the inner Failure +
//     FailedStep land in the outer failure envelope under
//     `inner_failure` and `inner_failed_step`.
//   - Inner chain abort → outer step aborts with the inner
//     AbortReason.
//   - Inner chain yield → outer step yields with the inner
//     YieldReason. The inner ResumeToken is preserved on
//     SubResult; resume of the OUTER token re-runs the chain
//     step, which in turn resumes the inner chain via its own
//     Resume() call (see chainResumeFromToken). State persists at
//     the outer level so the inner state isn't double-tracked.
//
// Phase C / #149.
func runChainStep(ctx context.Context, c *Chain, step Step, scope Scope, opts RunOptions, _ map[string]string) (StepResult, stepOutcome) {
	sr := StepResult{ID: step.ID, Status: StepSkipped}
	stepStart := time.Now()
	defer func() {
		sr.DurationMs = time.Since(stepStart).Milliseconds()
	}()

	// Resolver guard: composition needs a way to load the inner
	// chain. The CLI wires this; tests fake it.
	if opts.SubChainResolver == nil {
		sr.Status = StepFailed
		return sr, stepOutcome{
			kind: stepOutcomeFailed,
			failure: map[string]any{
				"reason": "chain_resolver_unavailable",
				"step":   step.ID,
				"hint":   "RunOptions.SubChainResolver must be set for chain: composition",
			},
		}
	}

	maxDepth := opts.MaxChainDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if opts.chainDepth >= maxDepth {
		sr.Status = StepFailed
		return sr, stepOutcome{
			kind: stepOutcomeFailed,
			failure: map[string]any{
				"reason":    "chain_recursion_limit",
				"step":      step.ID,
				"max_depth": maxDepth,
				"stack":     append([]string(nil), opts.chainStack...),
			},
		}
	}

	// Cycle check: if this chain is already on the stack, reject
	// before resolving (we'd otherwise loop until depth limit).
	for _, name := range opts.chainStack {
		if name == step.Chain {
			sr.Status = StepFailed
			return sr, stepOutcome{
				kind: stepOutcomeFailed,
				failure: map[string]any{
					"reason": "chain_cycle",
					"step":   step.ID,
					"chain":  step.Chain,
					"stack":  append([]string(nil), opts.chainStack...),
				},
			}
		}
	}

	inner, err := opts.SubChainResolver(step.Chain)
	if err != nil {
		sr.Status = StepFailed
		return sr, stepOutcome{
			kind: stepOutcomeFailed,
			failure: map[string]any{
				"reason": "chain_resolve_failed",
				"step":   step.ID,
				"chain":  step.Chain,
				"error":  err.Error(),
			},
		}
	}

	// Substitute outer-scope refs into the var values being passed
	// into the inner chain. SubstituteRaw — these values are not
	// shell-bound, they're plain strings the inner runner will
	// shell-quote when ITS sub-steps interpolate them.
	innerVars := map[string]string{}
	for k, v := range step.Vars {
		resolved, _ := SubstituteRaw(v, scope)
		innerVars[k] = resolved
	}

	innerOpts := opts
	innerOpts.chainDepth++
	innerOpts.chainStack = append(append([]string(nil), opts.chainStack...), step.Chain)
	// On resume, an inner-chain token piggybacks on the outer
	// resume call via opts.pendingInnerToken. Consume it here so a
	// yielded inner chain picks up at its yield point rather than
	// restarting from scratch.
	var innerRes *Result
	var runErr error
	if opts.pendingInnerToken != "" {
		// Reset the pending token so a recursive composition
		// doesn't accidentally apply it twice.
		consumed := opts.pendingInnerToken
		innerOpts.pendingInnerToken = ""
		innerRes, runErr = Resume(ctx, consumed, "continue", innerOpts)
	} else {
		innerRes, runErr = Run(ctx, inner, innerVars, innerOpts)
	}
	if runErr != nil {
		sr.Status = StepFailed
		return sr, stepOutcome{
			kind: stepOutcomeFailed,
			failure: map[string]any{
				"reason": "inner_chain_setup_failed",
				"step":   step.ID,
				"chain":  step.Chain,
				"error":  runErr.Error(),
			},
		}
	}

	sr.SubResult = innerRes
	sr.DurationMs = innerRes.DurationMs

	switch innerRes.Status {
	case StatusSuccess:
		sr.Status = StepOK
		// Capture exposes the inner Captured map as a single value;
		// downstream steps reference it via this step's capture
		// name + dotted lookup.
		return sr, stepOutcome{kind: stepOutcomeOK, capturedValue: innerRes.Captured}

	case StatusFailure:
		sr.Status = StepFailed
		fail := map[string]any{
			"reason":            "inner_chain_failed",
			"step":              step.ID,
			"chain":             step.Chain,
			"inner_failed_step": innerRes.FailedStep,
			"inner_failure":     innerRes.Failure,
		}
		// Surface structural failures (recursion / cycle) at the
		// outer level too — the operator's grep is for the cause,
		// not the wrapping chain. Without this lift the test below
		// has to walk the failure tree to find the root reason.
		if innerRes.Failure != nil {
			if r, ok := innerRes.Failure["reason"].(string); ok {
				if r == "chain_recursion_limit" || r == "chain_cycle" {
					fail["reason"] = r
				}
			}
		}
		return sr, stepOutcome{kind: stepOutcomeFailed, failure: fail}

	case StatusAborted:
		sr.Status = StepFailed
		return sr, stepOutcome{
			kind:      stepOutcomeAborted,
			condition: innerRes.AbortReason,
		}

	case StatusYielded:
		// Inner chain paused. The outer chain yields too — the
		// emitYield hook at the executeSteps level packages the
		// outer state (which embeds the inner ResumeToken via
		// SubResult). On resume, runChainStep recognizes the
		// preserved inner token and Resume()s the inner chain at
		// its yield point rather than starting fresh.
		sr.Status = StepYielded
		return sr, stepOutcome{
			kind:      stepOutcomeYielded,
			condition: innerRes.YieldReason,
		}
	}

	// Unrecognized inner status — defensive.
	sr.Status = StepFailed
	return sr, stepOutcome{
		kind: stepOutcomeFailed,
		failure: map[string]any{
			"reason":       "inner_chain_unknown_status",
			"step":         step.ID,
			"inner_status": innerRes.Status,
		},
	}
}

// cloneScope makes a shallow copy of vars + captures. Concurrent
// goroutines mutate their own scope.Captures during sub-runs;
// without the clone, two siblings writing the same capture key
// would race.
func cloneScope(s Scope) Scope {
	out := Scope{
		Vars:     make(map[string]string, len(s.Vars)),
		Captures: make(map[string]any, len(s.Captures)),
	}
	for k, v := range s.Vars {
		out.Vars[k] = v
	}
	for k, v := range s.Captures {
		out.Captures[k] = v
	}
	return out
}

// subStepCaptureView projects a sub-step's StepResult into a small
// map so ${outer.<sub>.exit_code} / ${outer.<sub>.stdout} resolve
// even when the sub-step didn't declare a capture. Keeps the
// mental model simple: every sub-step is addressable.
func subStepCaptureView(r StepResult) any {
	out := map[string]any{
		"id":          r.ID,
		"status":      r.Status,
		"exit_code":   r.ExitCode,
		"stdout":      r.Stdout,
		"stderr":      r.Stderr,
		"duration_ms": r.DurationMs,
	}
	return out
}

// Phase C note on env handling: every Phase C spawn site
// (parallel sub-step, for_each iteration, sub-chain dispatch)
// flows through execShell — the same code path leaf `run:` steps
// already use. When hygiene-bundle (#140) lands env-scrub
// semantics in execShell, every Phase C call site inherits the
// allowlist (PATH / HOME / USER / LANG / LC_ALL / TERM + step-
// declared env) without any additional wiring here. That
// invariant is what lets the Phase C runner reuse runWithRetry /
// execShell unchanged — no bypass, no second spawn surface.

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
//
// Security: the child process inherits a *scrubbed* env, not the
// gaia process's full env. Only the small allowlist in
// scrubbedChildEnv() (PATH, HOME, USER, LANG, LC_ALL, TERM) makes
// it through. Forge tokens (GITEA_TOKEN, FORGEJO_TOKEN, GH_TOKEN,
// GITHUB_TOKEN), cloud creds (AWS_*, GCP_*, AZURE_*), and any other
// operator-scope secrets that happen to be in the parent env are
// stripped.
//
// Why: combined with #135 (now fixed) the alternative was an
// attacker-controlled chain step that does `env | exfiltrate ...`
// reading the operator's PAT directly out of its own environment.
// Now even a hostile step (or one constructed via a future
// shell-injection regression) sees only the allowlist. (#140
// part 4.)
//
// Phase C uses this same execShell call path for parallel sub-steps,
// for_each iterations, and sub-chain dispatch — every spawn site
// shares the scrubbed env, no per-call-site duplication.
func execShell(ctx context.Context, cmd string) (stdout, stderr string, exitCode int, err error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env = scrubbedChildEnv()
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

// allowedChildEnvKeys is the strict allowlist of env vars passed
// through to chain step children. The principle is "if removing it
// breaks chain steps that don't need secrets, allow it; otherwise
// drop". Specifically:
//
//   - PATH: chain steps need to find binaries (gaia, env, sh
//     builtins, etc.). Without PATH the shell can't even invoke
//     `env` to read the rest, so chains break immediately.
//   - HOME: `gaia` itself reads ~/.config/gaia/credentials.yaml
//     when invoked from a chain step; without HOME the gaia
//     subprocess loses its credential resolution path.
//   - USER / LOGNAME: tools that build paths or commit metadata
//     (`git`, etc.) read these. Inert from a secrets-leak
//     standpoint — the username isn't a secret.
//   - LANG / LC_ALL: locale. Some tools (sort, awk, gettext-aware
//     CLIs) misbehave or change output format without a locale.
//   - TERM: terminal type. CLIs that emit colour respect TERM;
//     dropping it sometimes flips them into a verbose ANSI-escape
//     fallback that pollutes captures.
//
// Forge tokens, cloud creds, and arbitrary operator vars are NOT
// on the list. If a chain author legitimately needs a secret in a
// step, the right path is a per-step env declaration on the chain
// schema (Phase 4) — not silent inheritance.
var allowedChildEnvKeys = []string{
	"PATH",
	"HOME",
	"USER",
	"LOGNAME",
	"LANG",
	"LC_ALL",
	"TERM",
}

// scrubbedChildEnv returns the env slice (in `KEY=VALUE` shape that
// exec.Cmd.Env wants) that chain step children should inherit. Only
// keys in allowedChildEnvKeys with a non-empty value are included;
// everything else is stripped. Nil/empty result is allowed — the
// child runs with literally no env, which is the safest fallback.
func scrubbedChildEnv() []string {
	out := make([]string, 0, len(allowedChildEnvKeys))
	for _, k := range allowedChildEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
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
