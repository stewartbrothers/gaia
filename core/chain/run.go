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

// Run executes the chain. Returns a non-nil *Result describing what
// happened, plus an error only for setup-level failures (var
// resolution, etc.) — chain-execution outcomes are encoded in
// Result.Status. Caller checks the status before acting.
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
	for _, step := range c.Steps {
		resolved, unresolved := Substitute(step.Run, scope)
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

		// On real runs, an unresolved ref in the run line is a hard
		// failure for that step — we don't want to silently send a
		// literal `${var}` into a child command.
		if len(unresolved) > 0 {
			sr.Status = StepFailed
			sr.Stderr = "unresolved variable references: " + strings.Join(unresolved, ", ")
			res.Steps = append(res.Steps, sr)
			res.Status = StatusFailure
			res.FailedStep = step.ID
			res.Failure = buildFailure(step, scope, "unresolved_variables", sr.Stderr, "")
			break
		}

		stepStart := time.Now()
		stdout, stderr, exitCode, runErr := execShell(ctx, resolved)
		sr.DurationMs = time.Since(stepStart).Milliseconds()
		sr.ExitCode = exitCode
		sr.Stdout = truncate(stdout, opts.MaxOutputBytes)
		sr.Stderr = truncate(stderr, opts.MaxOutputBytes)

		if opts.Progress != nil {
			status := "ok"
			if runErr != nil || exitCode != 0 {
				status = "failed"
			}
			_, _ = fmt.Fprintf(opts.Progress, "[%s] %s in %dms\n", step.ID, status, sr.DurationMs)
		}

		if runErr != nil || exitCode != 0 {
			sr.Status = StepFailed
			res.Steps = append(res.Steps, sr)
			res.Status = StatusFailure
			res.FailedStep = step.ID
			reason := "step_exited_nonzero"
			errStr := strings.TrimSpace(stderr)
			if errStr == "" && runErr != nil {
				errStr = runErr.Error()
			}
			res.Failure = buildFailure(step, scope, reason, errStr, stdout)
			break
		}

		sr.Status = StepOK

		// Capture: parse stdout as a gaia envelope; fall back to
		// raw JSON, then to raw string.
		if step.Capture != "" {
			scope.Captures[step.Capture] = decodeCapture(stdout)
			res.Captured[step.Capture] = scope.Captures[step.Capture]
		}

		res.Steps = append(res.Steps, sr)
	}
	res.DurationMs = time.Since(chainStart).Milliseconds()

	return res, nil
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
// inside a YAML-decoded value tree.
func substAny(v any, scope Scope) any {
	switch x := v.(type) {
	case string:
		out, _ := Substitute(x, scope)
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
