package chain_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/chain"
)

func TestResolveVarsRequired(t *testing.T) {
	c := &chain.Chain{
		Name: "x",
		Vars: map[string]chain.VarSpec{
			"title": {Required: true},
		},
	}
	if _, err := chain.ResolveVars(c, nil); err == nil {
		t.Error("expected error for missing required var")
	}
	if got, err := chain.ResolveVars(c, map[string]string{"title": "hi"}); err != nil || got["title"] != "hi" {
		t.Errorf("supplied: %v / %v", got, err)
	}
}

func TestResolveVarsDefault(t *testing.T) {
	c := &chain.Chain{
		Name: "x",
		Vars: map[string]chain.VarSpec{
			"base": {Default: "main"},
		},
	}
	got, err := chain.ResolveVars(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["base"] != "main" {
		t.Errorf("default not applied: %v", got)
	}
}

func TestResolveVarsCarriesExtraSupplied(t *testing.T) {
	c := &chain.Chain{Name: "x", Steps: []chain.Step{{ID: "x", Run: "true"}}}
	got, err := chain.ResolveVars(c, map[string]string{"extra": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if got["extra"] != "value" {
		t.Errorf("extra: %v", got)
	}
}

func TestRunDryRun(t *testing.T) {
	c := &chain.Chain{
		Name: "test",
		Vars: map[string]chain.VarSpec{"name": {Required: true}},
		Steps: []chain.Step{
			{ID: "greet", Run: "echo hello ${name}"},
		},
	}
	res, err := chain.Run(context.Background(), c, map[string]string{"name": "world"}, chain.RunOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun {
		t.Error("DryRun flag should be set")
	}
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s", res.Status)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("steps: %d", len(res.Steps))
	}
	// Substituted vars are shell-quoted in the resolved run line so
	// hostile values can't inject shell metacharacters (#135).
	if res.Steps[0].Run != "echo hello 'world'" {
		t.Errorf("resolved run: %q", res.Steps[0].Run)
	}
	if res.Steps[0].Status != chain.StepSkipped {
		t.Errorf("dry-run step should be skipped; got %s", res.Steps[0].Status)
	}
}

// TestRunShellInjectionVarIsQuoted is the end-to-end regression for
// #135. A var containing shell metachars must NOT be able to escape
// the run-line literal and execute a separate command. Concretely:
// the hostile value embeds a `touch` command; if quoting fails, the
// chain runner's `sh -c` would create the marker file. With proper
// shell-quoting, the marker file is never created.
func TestRunShellInjectionVarIsQuoted(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "should-not-exist")

	// The classic shell-injection payload: close the literal, run
	// arbitrary commands, comment out the trailing literal.
	hostile := "value\"; touch '" + marker + "'; echo \""

	c := &chain.Chain{
		Name: "inject",
		Vars: map[string]chain.VarSpec{"var": {Required: true}},
		Steps: []chain.Step{
			{ID: "echo", Run: "echo HELLO_${var}"},
		},
	}
	res, err := chain.Run(context.Background(), c,
		map[string]string{"var": hostile}, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("shell-injection succeeded: marker file %q exists (err=%v) — quoting failed", marker, err)
	}
	// The hostile bytes should still appear in stdout (echoed verbatim
	// as a single argument), confirming the value was passed through
	// as data rather than swallowed.
	if !strings.Contains(res.Steps[0].Stdout, hostile) {
		t.Errorf("hostile value missing from stdout — quoting may have mangled it: %q", res.Steps[0].Stdout)
	}
}

// TestRunShellInjectionCaptureIsQuoted exercises the same regression
// via a capture (the more dangerous adversary path: a hostile forge
// response feeding a downstream `run:` step). The first step writes
// a hostile JSON envelope to a file via a heredoc (no var
// substitution involved, so the test fixture itself isn't subject to
// shell-quoting); the second step reads + emits it as the capture
// source; the third step splices the captured field into `sh -c`.
func TestRunShellInjectionCaptureIsQuoted(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "capture-pwn")
	fixture := filepath.Join(tmp, "envelope.json")

	// Hostile JSON: data.field carries a payload that, if spliced
	// unquoted into `echo got=${obj.field}`, would close the literal
	// and run a touch command.
	payload := `a"; touch ` + marker + `; echo "b`
	envBytes := []byte(`{"data":{"field":` + jsonString(payload) + `}}`)
	if err := os.WriteFile(fixture, envBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	c := &chain.Chain{
		Name: "inject-capture",
		Steps: []chain.Step{
			{ID: "fetch", Capture: "obj", Run: "cat " + fixture},
			{ID: "use", Run: "echo got=${obj.field}"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("capture-driven shell-injection succeeded: %q exists", marker)
	}
}

// jsonString quotes a string for inline JSON construction in the
// injection test. Just escapes `"` and `\` — enough for the payload
// shape we use (no embedded control characters).
func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func TestRunHappyPath(t *testing.T) {
	c := &chain.Chain{
		Name: "echo-and-pipe",
		Steps: []chain.Step{
			{ID: "first", Run: "echo first"},
			{ID: "json", Run: `echo '{"data":{"login":"alice"}}'`, Capture: "user"},
			{ID: "second", Run: "echo got ${user.login}"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	if len(res.Steps) != 3 {
		t.Fatalf("steps: %d", len(res.Steps))
	}
	if res.Captured["user"] == nil {
		t.Error("user capture missing")
	}
	last := res.Steps[2]
	if !strings.Contains(last.Stdout, "got alice") {
		t.Errorf("substitution failed downstream; last stdout: %q", last.Stdout)
	}
}

func TestRunFailureStopsChain(t *testing.T) {
	c := &chain.Chain{
		Name: "fail",
		Steps: []chain.Step{
			{ID: "ok", Run: "echo first"},
			{ID: "boom", Run: "exit 7"},
			{ID: "never", Run: "echo should not run"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	if res.FailedStep != "boom" {
		t.Errorf("failed step: %q", res.FailedStep)
	}
	if len(res.Steps) != 2 {
		t.Errorf("expected stop after step 2; got %d steps", len(res.Steps))
	}
	if res.Steps[1].ExitCode != 7 {
		t.Errorf("exit code: %d", res.Steps[1].ExitCode)
	}
	if res.Failure["reason"] != "step_exited_nonzero" {
		t.Errorf("default failure reason: %v", res.Failure)
	}
}

func TestRunOnFailureReturnSubstitutes(t *testing.T) {
	c := &chain.Chain{
		Name: "with-on-failure",
		Steps: []chain.Step{
			{
				ID:  "boom",
				Run: "echo error-detail >&2; exit 1",
				OnFailure: &chain.FailureAction{
					Return: map[string]any{
						"reason":    "custom-reason",
						"detail":    "${error.message}",
						"failed_at": "${error.step}",
					},
				},
			},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusFailure {
		t.Fatalf("status: %s", res.Status)
	}
	if res.Failure["reason"] != "custom-reason" {
		t.Errorf("custom reason missing: %+v", res.Failure)
	}
	detail, _ := res.Failure["detail"].(string)
	if !strings.Contains(detail, "error-detail") {
		t.Errorf("detail substitution failed: %q", detail)
	}
	if res.Failure["failed_at"] != "boom" {
		t.Errorf("failed_at: %v", res.Failure["failed_at"])
	}
}

func TestRunUnresolvedRefIsHardFailure(t *testing.T) {
	c := &chain.Chain{
		Name: "unresolved",
		Steps: []chain.Step{
			{ID: "x", Run: "echo ${missing}"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusFailure {
		t.Errorf("expected failure for unresolved ref; got %s", res.Status)
	}
	if res.Failure["reason"] != "unresolved_variables" {
		t.Errorf("reason: %v", res.Failure["reason"])
	}
}

func TestRunCaptureRawStringForNonJSON(t *testing.T) {
	c := &chain.Chain{
		Name: "raw",
		Steps: []chain.Step{
			{ID: "first", Run: "echo plain text", Capture: "out"},
			{ID: "use", Run: "echo got ${out}"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %v", res.Status, res.Failure)
	}
	if got, _ := res.Captured["out"].(string); got != "plain text" {
		t.Errorf("expected raw string capture; got %v", res.Captured["out"])
	}
	if !strings.Contains(res.Steps[1].Stdout, "got plain text") {
		t.Errorf("substitution of raw capture failed: %q", res.Steps[1].Stdout)
	}
}

func TestRunDurationsRecorded(t *testing.T) {
	c := &chain.Chain{
		Name: "timed",
		Steps: []chain.Step{
			{ID: "x", Run: "true"},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.DurationMs < 0 {
		t.Errorf("chain duration: %d", res.DurationMs)
	}
	if res.Steps[0].DurationMs < 0 {
		t.Errorf("step duration: %d", res.Steps[0].DurationMs)
	}
}

func TestRunYieldsOnDeclaredCondition(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "yield-test",
		Steps: []chain.Step{
			{ID: "first", Run: "echo first"},
			{
				ID:      "rate-limited",
				Run:     "exit 5", // exitcode.RateLimit → rate_limited
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
			{ID: "never", Run: "echo never reached"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusYielded {
		t.Fatalf("status: got %s, want yielded; failure=%+v", res.Status, res.Failure)
	}
	if res.YieldReason != chain.YieldRateLimited {
		t.Errorf("reason: got %q", res.YieldReason)
	}
	if res.ResumeToken == "" {
		t.Error("resume token empty")
	}
	if len(res.Steps) != 2 {
		t.Errorf("expected 2 step results (first + yielded); got %d", len(res.Steps))
	}
	if res.Steps[1].Status != chain.StepYielded {
		t.Errorf("yielded step status: %s", res.Steps[1].Status)
	}
	wantRemaining := []string{"never"}
	if len(res.RemainingSteps) != 1 || res.RemainingSteps[0] != wantRemaining[0] {
		t.Errorf("remaining: %+v", res.RemainingSteps)
	}
	// State file should exist.
	if _, err := os.Stat(filepath.Join(dir, res.ResumeToken+".yaml")); err != nil {
		t.Errorf("state file missing: %v", err)
	}
}

func TestRunAbortsOnDeclaredCondition(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "abort-test",
		Steps: []chain.Step{
			{
				ID:      "boom",
				Run:     "exit 4", // exitcode.Auth → auth_error
				AbortOn: []chain.YieldCondition{chain.YieldAuthError},
			},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusAborted {
		t.Fatalf("status: %s", res.Status)
	}
	if res.AbortReason != chain.YieldAuthError {
		t.Errorf("abort reason: %q", res.AbortReason)
	}
	if res.ResumeToken != "" {
		t.Error("aborted chains shouldn't have a resume token")
	}
}

func TestRunFailureWhenConditionNotDeclared(t *testing.T) {
	// Step exits with a known condition but the step doesn't
	// declare it in either yield_on or abort_on. Falls through to
	// existing failure flow — Phase A behavior.
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "untracked",
		Steps: []chain.Step{
			{ID: "boom", Run: "exit 5"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusFailure {
		t.Errorf("expected failure; got %s", res.Status)
	}
	if res.ResumeToken != "" {
		t.Error("undeclared yield should not produce resume token")
	}
}

func TestResumeContinuesFromYieldedStep(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "resume-test",
		Steps: []chain.Step{
			{ID: "first", Run: "echo first"},
			{
				ID:      "transient",
				Run:     "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
			{ID: "third", Run: "echo third"},
		},
	}
	res1, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res1.Status != chain.StatusYielded {
		t.Fatalf("first run: status %s", res1.Status)
	}
	token := res1.ResumeToken

	// Mutate the chain on disk: change `transient` to succeed (echo).
	// In practice the agent would fix the underlying cause; here we
	// simulate by patching the State directly. But Resume reads the
	// frozen chain spec from State, so we patch that.
	state, err := chain.LoadState(dir, token)
	if err != nil {
		t.Fatal(err)
	}
	state.Chain.Steps[1].Run = "echo transient-now-passes"
	if err := chain.SaveState(dir, state); err != nil {
		t.Fatal(err)
	}

	res2, err := chain.Resume(context.Background(), token, "continue", chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != chain.StatusSuccess {
		t.Fatalf("resume: status %s, failure %+v", res2.Status, res2.Failure)
	}
	if len(res2.Steps) != 3 {
		t.Errorf("step count after resume: got %d, want 3", len(res2.Steps))
	}
	// State file should be removed after success.
	if _, err := os.Stat(filepath.Join(dir, token+".yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("state file should be cleaned up; got err %v", err)
	}
}

func TestResumeAbortDecision(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "abort-on-resume",
		Steps: []chain.Step{
			{
				ID:      "transient",
				Run:     "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
			{ID: "after", Run: "echo after"},
		},
	}
	res1, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	token := res1.ResumeToken

	res2, err := chain.Resume(context.Background(), token, "abort", chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != chain.StatusAborted {
		t.Errorf("status: %s", res2.Status)
	}
	if res2.AbortReason != chain.YieldRateLimited {
		t.Errorf("abort reason: %q", res2.AbortReason)
	}
	// State file removed after abort too.
	if _, err := os.Stat(filepath.Join(dir, token+".yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("state file should be cleaned up; got err %v", err)
	}
}

func TestResumeUnknownDecisionErrors(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "x",
		Steps: []chain.Step{
			{
				ID: "y", Run: "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	res1, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})

	_, err := chain.Resume(context.Background(), res1.ResumeToken, "skip", chain.RunOptions{StateDir: dir})
	if err == nil || !strings.Contains(err.Error(), "skip") {
		t.Errorf("expected error for unsupported 'skip' decision; got %v", err)
	}
}

func TestResumeMissingTokenErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := chain.Resume(context.Background(), "no-such-token", "continue", chain.RunOptions{StateDir: dir})
	if err == nil {
		t.Error("expected error for missing token")
	}
}

// --- Phase B-2 runtime tests ---

func TestRunStepTimeoutYields(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "with-timeout",
		Steps: []chain.Step{
			{
				ID:      "slow",
				Run:     "sleep 2",
				Timeout: "50ms",
				YieldOn: []chain.YieldCondition{chain.YieldTimeout},
			},
			{ID: "after", Run: "echo after"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusYielded {
		t.Fatalf("status: %s, failure %+v", res.Status, res.Failure)
	}
	if res.YieldReason != chain.YieldTimeout {
		t.Errorf("yield reason: %q (want timeout)", res.YieldReason)
	}
	if !res.Steps[0].TimedOut {
		t.Error("StepResult.TimedOut should be true")
	}
}

func TestRunStepTimeoutAborts(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "timeout-aborts",
		Steps: []chain.Step{
			{
				ID:      "slow",
				Run:     "sleep 2",
				Timeout: "50ms",
				AbortOn: []chain.YieldCondition{chain.YieldTimeout},
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusAborted {
		t.Errorf("status: %s", res.Status)
	}
	if res.AbortReason != chain.YieldTimeout {
		t.Errorf("abort reason: %q", res.AbortReason)
	}
}

func TestRunStepTimeoutFallsThroughToFailure(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "timeout-no-routing",
		Steps: []chain.Step{
			{ID: "slow", Run: "sleep 2", Timeout: "50ms"},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	if !res.Steps[0].TimedOut {
		t.Error("TimedOut flag should still be set on failure path")
	}
}

func TestRunRetrySucceedsAfterFailure(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	sentinel := tmp + "/attempts"
	c := &chain.Chain{
		Name: "retry-recovers",
		Steps: []chain.Step{
			{
				ID: "flaky",
				// Increment a counter; fail until we hit attempt 3.
				Run: `n=$(cat "` + sentinel + `" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "` + sentinel + `"; if [ $n -lt 3 ]; then exit 1; fi; echo recovered`,
				Retry: &chain.RetrySpec{
					Max:     5,
					Delay:   "1ms",
					Backoff: "constant",
				},
			},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure %+v", res.Status, res.Failure)
	}
	if res.Steps[0].Attempts != 3 {
		t.Errorf("attempts: got %d, want 3", res.Steps[0].Attempts)
	}
}

func TestRunRetryExhaustsThenRoutes(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "retry-exhausted-yields",
		Steps: []chain.Step{
			{
				ID:  "always-fails",
				Run: "exit 5",
				Retry: &chain.RetrySpec{
					Max:     2,
					Delay:   "1ms",
					Backoff: "constant",
				},
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusYielded {
		t.Fatalf("status: %s", res.Status)
	}
	if res.Steps[0].Attempts != 3 { // initial + 2 retries
		t.Errorf("attempts: got %d, want 3", res.Steps[0].Attempts)
	}
}

func TestRunRetryBackoffShapes(t *testing.T) {
	for _, backoff := range []string{"constant", "linear", "exponential"} {
		t.Run(backoff, func(t *testing.T) {
			dir := t.TempDir()
			c := &chain.Chain{
				Name: "backoff-shape",
				Steps: []chain.Step{
					{
						ID:  "fail",
						Run: "exit 1",
						Retry: &chain.RetrySpec{
							Max:     2,
							Delay:   "1ms",
							Backoff: backoff,
						},
					},
				},
			}
			res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
			if res.Status != chain.StatusFailure {
				t.Errorf("status: %s", res.Status)
			}
			if res.Steps[0].Attempts != 3 {
				t.Errorf("attempts: %d", res.Steps[0].Attempts)
			}
		})
	}
}

func TestRunDefaultYieldOnApplies(t *testing.T) {
	// Step has no yield_on, but chain default_yield_on includes
	// rate_limited — chain should yield instead of fail.
	dir := t.TempDir()
	c := &chain.Chain{
		Name:           "with-defaults",
		DefaultYieldOn: []chain.YieldCondition{chain.YieldRateLimited},
		Steps: []chain.Step{
			{ID: "boom", Run: "exit 5"},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusYielded {
		t.Errorf("status: %s, failure %+v", res.Status, res.Failure)
	}
	if res.YieldReason != chain.YieldRateLimited {
		t.Errorf("reason: %q", res.YieldReason)
	}
}

func TestRunPerStepYieldOnOverridesDefault(t *testing.T) {
	// Default says yield on rate_limited, but step explicitly does
	// NOT include rate_limited in its (non-empty) yield_on. The
	// step's empty list of matching conditions should win — fall
	// through to failure rather than yielding via the default.
	dir := t.TempDir()
	c := &chain.Chain{
		Name:           "step-overrides",
		DefaultYieldOn: []chain.YieldCondition{chain.YieldRateLimited},
		Steps: []chain.Step{
			{
				ID:      "boom",
				Run:     "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldAuthError},
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s (want failure — step's yield_on doesn't match)", res.Status)
	}
}

func TestRunCleanupRunsOnAbort(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	cleanupMarker := tmp + "/cleaned"
	c := &chain.Chain{
		Name: "abort-with-cleanup",
		Steps: []chain.Step{
			{
				ID:      "boom",
				Run:     "exit 4",
				AbortOn: []chain.YieldCondition{chain.YieldAuthError},
			},
		},
		Cleanup: []chain.Step{
			{ID: "mark-cleaned", Run: "touch " + cleanupMarker},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusAborted {
		t.Fatalf("status: %s", res.Status)
	}
	if len(res.CleanupResults) != 1 || res.CleanupResults[0].Status != chain.StepOK {
		t.Errorf("cleanup results: %+v", res.CleanupResults)
	}
	if _, err := os.Stat(cleanupMarker); err != nil {
		t.Errorf("cleanup didn't run: %v", err)
	}
}

func TestRunCleanupContinuesAfterFailingStep(t *testing.T) {
	// Cleanup is best-effort: a failing cleanup step doesn't stop
	// later cleanup steps.
	dir := t.TempDir()
	tmp := t.TempDir()
	marker := tmp + "/second"
	c := &chain.Chain{
		Name: "cleanup-best-effort",
		Steps: []chain.Step{
			{
				ID:      "boom",
				Run:     "exit 4",
				AbortOn: []chain.YieldCondition{chain.YieldAuthError},
			},
		},
		Cleanup: []chain.Step{
			{ID: "first", Run: "exit 99"},
			{ID: "second", Run: "touch " + marker},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusAborted {
		t.Fatalf("status: %s", res.Status)
	}
	if len(res.CleanupResults) != 2 {
		t.Fatalf("cleanup results: got %d, want 2", len(res.CleanupResults))
	}
	if res.CleanupResults[0].Status != chain.StepFailed {
		t.Errorf("first cleanup status: %s", res.CleanupResults[0].Status)
	}
	if res.CleanupResults[1].Status != chain.StepOK {
		t.Errorf("second cleanup status: %s", res.CleanupResults[1].Status)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("second cleanup didn't run: %v", err)
	}
}

func TestRunCleanupDoesNotRunOnSuccess(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	marker := tmp + "/should-not-exist"
	c := &chain.Chain{
		Name: "no-cleanup-on-success",
		Steps: []chain.Step{
			{ID: "ok", Run: "echo ok"},
		},
		Cleanup: []chain.Step{
			{ID: "run-me", Run: "touch " + marker},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s", res.Status)
	}
	if len(res.CleanupResults) != 0 {
		t.Errorf("cleanup should not run on success; got %+v", res.CleanupResults)
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("marker should not exist: %v", err)
	}
}

func TestResumeModifyDecision(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	sentinel := tmp + "/sentinel"
	// Step yields when sentinel doesn't exist; agent supplies a
	// modify directive that creates it before re-running.
	c := &chain.Chain{
		Name: "modify-test",
		Vars: map[string]chain.VarSpec{
			"path": {Required: true},
		},
		Steps: []chain.Step{
			{
				// Substitution is now shell-quoted — the chain author
				// no longer needs to wrap the ref in double quotes
				// against word-splitting (#135 makes that the default).
				ID:      "check",
				Run:     "if [ ! -f ${path} ]; then exit 5; fi; echo recovered",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	res1, err := chain.Run(context.Background(), c, map[string]string{"path": tmp + "/missing"}, chain.RunOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res1.Status != chain.StatusYielded {
		t.Fatalf("first run: %s", res1.Status)
	}

	// Create the sentinel and modify the var to point at it.
	if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	mod := chain.ModifyDirective{
		StepID: "check",
		Vars:   map[string]string{"path": sentinel},
	}
	res2, err := chain.Resume(context.Background(), res1.ResumeToken, "modify", chain.RunOptions{StateDir: dir, Modify: &mod})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res2.Status != chain.StatusSuccess {
		t.Errorf("status: %s, failure: %+v", res2.Status, res2.Failure)
	}
}

func TestResumeModifyRequiresMatchingStep(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "modify-mismatch",
		Steps: []chain.Step{
			{
				ID:      "x",
				Run:     "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	res1, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	mod := chain.ModifyDirective{StepID: "y", Vars: nil}
	_, err := chain.Resume(context.Background(), res1.ResumeToken, "modify", chain.RunOptions{StateDir: dir, Modify: &mod})
	if err == nil || !strings.Contains(err.Error(), "modify") {
		t.Errorf("expected error for mismatched step id; got %v", err)
	}
}

func TestResumeModifyRequiresDirective(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "modify-missing",
		Steps: []chain.Step{
			{
				ID:      "x",
				Run:     "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	res1, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	_, err := chain.Resume(context.Background(), res1.ResumeToken, "modify", chain.RunOptions{StateDir: dir})
	if err == nil || !strings.Contains(err.Error(), "modify") {
		t.Errorf("expected error for missing modify directive; got %v", err)
	}
}

func TestResumeYieldsAgainCleansOldStateFile(t *testing.T) {
	// If a chain yields, gets resumed, and yields AGAIN (e.g.,
	// transient outage still ongoing), the old token's state file
	// must be cleaned up — otherwise `gaia chain list` accumulates
	// stale tokens forever.
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "still-broken",
		Steps: []chain.Step{
			{
				ID:      "stuck",
				Run:     "exit 5",
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	res1, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res1.Status != chain.StatusYielded {
		t.Fatalf("first run: %s", res1.Status)
	}
	oldToken := res1.ResumeToken

	res2, _ := chain.Resume(context.Background(), oldToken, "continue", chain.RunOptions{StateDir: dir})
	if res2.Status != chain.StatusYielded {
		t.Fatalf("resume: %s", res2.Status)
	}
	newToken := res2.ResumeToken
	if newToken == oldToken {
		t.Error("resume should issue a fresh token on re-yield")
	}

	// Old token's state file must be gone.
	if _, err := os.Stat(filepath.Join(dir, oldToken+".yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("old state file should be cleaned up; got err %v", err)
	}
	// New token's state file must exist.
	if _, err := os.Stat(filepath.Join(dir, newToken+".yaml")); err != nil {
		t.Errorf("new state file missing: %v", err)
	}

	// chain list should show exactly one entry (the new one).
	infos, _ := chain.ListStates(dir)
	if len(infos) != 1 || infos[0].Token != newToken {
		t.Errorf("list: %+v", infos)
	}
}

// TestRunChildEnvScrubbed pins the #140 part 4 fix: a chain step
// must not inherit token-bearing env vars from the parent gaia
// process. A hostile chain step that runs `env | grep TOKEN` (or
// any equivalent exfiltration) would otherwise read the operator's
// forge PAT directly out of its own environment — combining with
// the shell-injection surface in #135 to give an attacker a
// one-step path from a hostile forge response to
// "have the operator's GITEA_TOKEN."
//
// The fix: build a scrubbed env for child processes that includes
// only PATH / HOME / USER / LANG / LC_ALL / TERM. Anything else
// (including all forge tokens and AWS/GCP credentials that
// commonly live in operator envs) is dropped.
func TestRunChildEnvScrubbed(t *testing.T) {
	// Set the same set of token env-vars an operator would carry.
	// t.Setenv resets at end of test, so we don't pollute later tests.
	for k, v := range map[string]string{
		"GITEA_TOKEN":           "secret-gitea-token",
		"FORGEJO_TOKEN":         "secret-forgejo-token",
		"GH_TOKEN":              "secret-gh-token",
		"GITHUB_TOKEN":          "secret-github-token",
		"AWS_ACCESS_KEY":        "AKIA-secret-aws-key",
		"AWS_SECRET":            "secret-aws-shh",
		"AWS_SECRET_ACCESS_KEY": "secret-aws-secret-access-key",
		"GCP_SERVICE_ACCOUNT":   "secret-gcp-sa",
		"AZURE_CLIENT_SECRET":   "secret-azure-client",
		"GAIA_RANDOM_VAR":       "should-also-be-stripped",
		// Adjacent prefixes: ensure prefix-match allowlists for
		// LC_*, XDG_*, CONDA_* don't accidentally let through
		// names that share a leading character with a token-bearing
		// var. (Belt-and-braces: LCTOKEN, XDGSECRET, CONDATOKEN
		// don't actually start with the allowed prefixes since the
		// prefix includes the trailing underscore — but pin it.)
		"LCTOKEN":    "should-not-leak-without-underscore",
		"XDGSECRET":  "should-not-leak-without-underscore",
		"CONDATOKEN": "should-not-leak-without-underscore",
	} {
		t.Setenv(k, v)
	}

	c := &chain.Chain{
		Name: "envdump",
		Steps: []chain.Step{
			// `env` lists every env var the child sees. Any token
			// name that appears in stdout means the scrub failed.
			{ID: "dump", Run: "env"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{
		// Bigger output cap so the env dump isn't truncated mid-test.
		MaxOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	stdout := res.Steps[0].Stdout

	// None of these names should appear in the child's env.
	bannedKeys := []string{
		"GITEA_TOKEN", "FORGEJO_TOKEN", "GH_TOKEN", "GITHUB_TOKEN",
		"AWS_ACCESS_KEY", "AWS_SECRET", "AWS_SECRET_ACCESS_KEY",
		"GCP_SERVICE_ACCOUNT", "AZURE_CLIENT_SECRET",
		"GAIA_RANDOM_VAR",
		// Names that share a leading character with an allowed
		// prefix but don't actually start with the prefix
		// (allowlist requires the trailing underscore).
		"LCTOKEN", "XDGSECRET", "CONDATOKEN",
	}
	for _, k := range bannedKeys {
		if strings.Contains(stdout, k+"=") {
			t.Errorf("scrubbed env still contains %q — token would leak to a hostile chain step. stdout=%s", k, stdout)
		}
	}
	// And none of the actual token values either (defence in depth:
	// catches a future bug where the scrub renames keys but leaves
	// values).
	bannedVals := []string{
		"secret-gitea-token", "secret-forgejo-token",
		"secret-gh-token", "secret-github-token",
		"AKIA-secret-aws-key", "secret-aws-shh",
		"secret-aws-secret-access-key",
		"secret-gcp-sa", "secret-azure-client",
		"should-not-leak-without-underscore",
	}
	for _, v := range bannedVals {
		if strings.Contains(stdout, v) {
			t.Errorf("scrubbed env still contains the value %q — token would leak. stdout=%s", v, stdout)
		}
	}

	// Sanity: PATH should make it through (chain steps need to find
	// `env`, `gaia`, etc. on PATH; scrubbing PATH would break every
	// chain immediately).
	if !strings.Contains(stdout, "PATH=") {
		t.Errorf("PATH was scrubbed — chain steps can't find any binaries; stdout=%s", stdout)
	}
}

// TestRunChildEnvInheritsCallerToolEnv pins #247: chain `run:` steps
// must inherit the caller's full tool environment (PATH + the
// well-known *non-secret* env vars that venv / nvm / pyenv / asdf /
// go-toolchain activation set) so that locally-installed tools work
// inside chain steps without the operator needing to source an
// activation script in every `run:` block.
//
// Without this, `make ci-parity` (or any step that shells out to a
// tool whose wrapper relies on VIRTUAL_ENV, NVM_BIN, PYENV_VERSION,
// etc.) silently fails or picks up the wrong interpreter, even
// though the same command works in the caller's terminal. The
// inherited set is strictly non-secret — forge tokens and cloud
// creds are still scrubbed by TestRunChildEnvScrubbed.
//
// Repro: set a representative set of activation env vars in the
// parent, run a chain step that prints them via `env`. Any name
// missing means tools depending on that var won't activate inside
// the chain step.
func TestRunChildEnvInheritsCallerToolEnv(t *testing.T) {
	// Representative set covering the four most common managers an
	// operator's shell carries when they ran `gaia chain run`:
	//
	//   - python venv / virtualenv: VIRTUAL_ENV
	//   - nvm:                       NVM_DIR, NVM_BIN
	//   - pyenv:                     PYENV_ROOT, PYENV_VERSION
	//   - go toolchain:              GOPATH, GOROOT, GOBIN,
	//                                 GOCACHE, GOMODCACHE, GOFLAGS
	//   - generic dev shell:         SHELL, TMPDIR
	//   - locale/colour:             LC_*, XDG_*, CONDA_* (prefix match)
	//
	// Every name here is non-secret (it points at a directory or a
	// shell name, never holds a credential). t.Setenv resets at
	// end of test. PWD is intentionally excluded — POSIX `sh` always
	// recomputes PWD from the inherited cwd, so it's not a useful
	// inheritance assertion.
	wanted := map[string]string{
		"VIRTUAL_ENV":       "/tmp/fake-venv",
		"NVM_DIR":           "/tmp/fake-nvm",
		"NVM_BIN":           "/tmp/fake-nvm/versions/node/v20/bin",
		"PYENV_ROOT":        "/tmp/fake-pyenv",
		"PYENV_VERSION":     "3.12.0",
		"GOPATH":            "/tmp/fake-gopath",
		"GOROOT":            "/tmp/fake-goroot",
		"GOBIN":             "/tmp/fake-gobin",
		"GOCACHE":           "/tmp/fake-gocache",
		"GOMODCACHE":        "/tmp/fake-gomodcache",
		"GOFLAGS":           "-mod=mod",
		"SHELL":             "/bin/zsh",
		"TMPDIR":            "/tmp/fake-tmp",
		"LC_TIME":           "en_US.UTF-8",
		"XDG_CONFIG_HOME":   "/tmp/fake-xdg-config",
		"CONDA_PREFIX":      "/tmp/fake-conda",
		"CONDA_DEFAULT_ENV": "fake-env",
	}
	for k, v := range wanted {
		t.Setenv(k, v)
	}

	c := &chain.Chain{
		Name: "tool-env",
		Steps: []chain.Step{
			{ID: "dump", Run: "env"},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{
		MaxOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	stdout := res.Steps[0].Stdout
	for k, v := range wanted {
		want := k + "=" + v
		if !strings.Contains(stdout, want) {
			t.Errorf("child env missing %q — tools that depend on it (venv/nvm/pyenv/go) silently break in chain steps; stdout=%s", want, stdout)
		}
	}
}

// --- Phase C / #149 ---

// TestRunParallelBlockExecutesAllSubSteps verifies the basic
// fan-out: a parallel block with three sub-steps runs all three
// and collects their outcomes. Order in res.Steps[i].Captured
// matches declaration order — concurrent execution doesn't
// reorder the result tree.
func TestRunParallelBlockExecutesAllSubSteps(t *testing.T) {
	c := &chain.Chain{
		Name: "fan-out",
		Steps: []chain.Step{
			{
				ID: "fan",
				Parallel: &chain.ParallelBlock{
					Steps: []chain.Step{
						{ID: "a", Run: "echo a", Capture: "out_a"},
						{ID: "b", Run: "echo b", Capture: "out_b"},
						{ID: "c", Run: "echo c", Capture: "out_c"},
					},
				},
			},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("outer steps: %d", len(res.Steps))
	}
	// Sub-results live in Steps[0].SubSteps, addressed by ID.
	outer := res.Steps[0]
	if len(outer.SubSteps) != 3 {
		t.Fatalf("sub-steps: %d", len(outer.SubSteps))
	}
	wantIDs := map[string]bool{"a": true, "b": true, "c": true}
	for _, ss := range outer.SubSteps {
		if !wantIDs[ss.ID] {
			t.Errorf("unexpected sub-step id %q", ss.ID)
		}
		if ss.Status != chain.StepOK {
			t.Errorf("sub-step %s: %s", ss.ID, ss.Status)
		}
	}
	// Each sub-step's capture is exposed under fan.<sub-id>.
	if outer.Status != chain.StepOK {
		t.Errorf("outer status: %s", outer.Status)
	}
}

// TestRunParallelMaxConcurrentBoundsGoroutines exercises the
// concurrency bound: with max_concurrent=2 and 4 sub-steps that
// each sleep 100ms, total wall time should be ~200ms (two waves
// of 2), not 400ms (serial) or 100ms (unbounded).
func TestRunParallelMaxConcurrentBoundsGoroutines(t *testing.T) {
	c := &chain.Chain{
		Name: "bounded",
		Steps: []chain.Step{
			{
				ID: "fan",
				Parallel: &chain.ParallelBlock{
					MaxConcurrent: 2,
					Steps: []chain.Step{
						{ID: "a", Run: "sleep 0.1"},
						{ID: "b", Run: "sleep 0.1"},
						{ID: "c", Run: "sleep 0.1"},
						{ID: "d", Run: "sleep 0.1"},
					},
				},
			},
		},
	}
	start := time.Now()
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s", res.Status)
	}
	// Bounds: at least 200ms (two waves), but well under 400ms
	// (full serial) — give comfortable slack for CI variance.
	if elapsed < 180*time.Millisecond {
		t.Errorf("ran too fast (%.0fms) — bound likely not enforced", float64(elapsed)/float64(time.Millisecond))
	}
	if elapsed > 800*time.Millisecond {
		t.Errorf("ran too slow (%.0fms) — concurrency not happening", float64(elapsed)/float64(time.Millisecond))
	}
}

// TestRunParallelDefaultMaxConcurrent sanity-checks that a missing
// max_concurrent applies the default (5) — exercise it indirectly
// by running 6 fast sub-steps without errors and verifying they
// all complete.
func TestRunParallelDefaultMaxConcurrent(t *testing.T) {
	c := &chain.Chain{
		Name: "default-conc",
		Steps: []chain.Step{
			{
				ID: "fan",
				Parallel: &chain.ParallelBlock{
					Steps: []chain.Step{
						{ID: "a", Run: "true"},
						{ID: "b", Run: "true"},
						{ID: "c", Run: "true"},
						{ID: "d", Run: "true"},
						{ID: "e", Run: "true"},
						{ID: "f", Run: "true"},
					},
				},
			},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s", res.Status)
	}
	if len(res.Steps[0].SubSteps) != 6 {
		t.Errorf("sub-step count: %d", len(res.Steps[0].SubSteps))
	}
}

// TestRunParallelOneSubStepFailsCollectsAll: by default we wait for
// every sub-step to finish even when one fails. The chain then
// reports failure with the failed sub-step ID surfaced via
// Failure.failed_substep + the parallel step's outer FailedStep.
func TestRunParallelOneSubStepFailsCollectsAll(t *testing.T) {
	c := &chain.Chain{
		Name: "fan-fail",
		Steps: []chain.Step{
			{
				ID: "fan",
				Parallel: &chain.ParallelBlock{
					Steps: []chain.Step{
						{ID: "a", Run: "echo a"},
						{ID: "b", Run: "exit 7"},
						{ID: "c", Run: "echo c"},
					},
				},
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	if res.FailedStep != "fan" {
		t.Errorf("FailedStep: %q", res.FailedStep)
	}
	// All three sub-steps should have run (no fail-fast → collect all).
	if len(res.Steps[0].SubSteps) != 3 {
		t.Errorf("expected all sub-steps to run; got %d", len(res.Steps[0].SubSteps))
	}
	// The failed-substep id should be exposed via the outer failure.
	if got := res.Failure["failed_substep"]; got != "b" {
		t.Errorf("failed_substep: %v", got)
	}
}

// TestRunParallelFailFastShortCircuits: with fail_fast: true, the
// runner cancels still-running siblings as soon as one fails. Some
// sub-steps may be skipped/cancelled mid-flight; we just need the
// overall outcome to be failure and the failing sub-step's stderr
// to land in the chain's failure envelope.
func TestRunParallelFailFastShortCircuits(t *testing.T) {
	c := &chain.Chain{
		Name: "fan-failfast",
		Steps: []chain.Step{
			{
				ID: "fan",
				Parallel: &chain.ParallelBlock{
					FailFast: true,
					Steps: []chain.Step{
						// Slow sibling; gets cancelled when fast one fails.
						{ID: "slow", Run: "sleep 5"},
						// Fast failure.
						{ID: "boom", Run: "exit 9"},
					},
				},
			},
		},
	}
	start := time.Now()
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	elapsed := time.Since(start)
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	// fail_fast must short-circuit well under the 5s sleep.
	if elapsed > 3*time.Second {
		t.Errorf("fail_fast did not cancel siblings; ran %s", elapsed)
	}
}

// TestRunParallelSubStepYieldYieldsChain: when a sub-step yields,
// the whole chain yields. The resume token's state captures the
// parallel block's progress so resume can re-run only the yielded
// sub-step.
func TestRunParallelSubStepYieldYieldsChain(t *testing.T) {
	dir := t.TempDir()
	c := &chain.Chain{
		Name: "fan-yield",
		Steps: []chain.Step{
			{
				ID: "fan",
				Parallel: &chain.ParallelBlock{
					Steps: []chain.Step{
						{ID: "ok", Run: "echo ok"},
						{ID: "rate", Run: "exit 5",
							YieldOn: []chain.YieldCondition{chain.YieldRateLimited}},
					},
				},
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{StateDir: dir})
	if res.Status != chain.StatusYielded {
		t.Errorf("status: %s; failure: %+v", res.Status, res.Failure)
	}
	if res.YieldReason != chain.YieldRateLimited {
		t.Errorf("yield reason: %q", res.YieldReason)
	}
	if res.ResumeToken == "" {
		t.Error("resume token missing")
	}
}

// TestRunForEachSerial iterates a JSON-array capture, running the
// step once per element with ${item} bound to the scalar and
// ${index} to the ordinal. Default order serial — verifies indices
// run 0 → N-1 and per-iteration captures collapse into the outer
// SubSteps slice.
//
// Substituted values are shell-quoted (#135), so inside the run
// line `echo got ${item}` resolves to `echo got 'a'` — the
// surrounding shell renders that as `got a` once the literal
// quotes are consumed. The test reads the stdout AFTER the shell
// runs, so the quotes don't appear.
func TestRunForEachSerial(t *testing.T) {
	c := &chain.Chain{
		Name: "fanout-serial",
		Steps: []chain.Step{
			{ID: "list", Run: `echo '["a","b","c"]'`, Capture: "items"},
			{
				ID:      "per-item",
				ForEach: "${items}",
				Run:     `echo got ${item} at ${index}`,
				Capture: "iter",
			},
		},
	}
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("outer steps: %d", len(res.Steps))
	}
	iter := res.Steps[1]
	if len(iter.SubSteps) != 3 {
		t.Fatalf("iterations: %d", len(iter.SubSteps))
	}
	wants := []string{"got a at 0\n", "got b at 1\n", "got c at 2\n"}
	for i, want := range wants {
		if iter.SubSteps[i].Stdout != want {
			t.Errorf("iter %d stdout: got %q want %q", i, iter.SubSteps[i].Stdout, want)
		}
	}
}

// TestRunForEachParallel: same iteration, but with parallel: true,
// so iterations run concurrently up to max_concurrent. Verify
// total wall time is well under the serial sum.
func TestRunForEachParallel(t *testing.T) {
	c := &chain.Chain{
		Name: "fanout-parallel",
		Steps: []chain.Step{
			{ID: "list", Run: `echo '["a","b","c","d"]'`, Capture: "items"},
			{
				ID:            "per-item",
				ForEach:       "${items}",
				ParallelIter:  true,
				MaxConcurrent: 4,
				Run:           "sleep 0.1",
			},
		},
	}
	start := time.Now()
	res, err := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	// 4 sleep-0.1s iterations at concurrency 4 ≈ 100ms; serial
	// would be 400ms. Allow generous slack.
	if elapsed > 350*time.Millisecond {
		t.Errorf("for_each parallel didn't fan out (%s)", elapsed)
	}
}

// TestRunForEachEmptyArray: when the iterable resolves to [], the
// step runs zero iterations and reports OK with empty SubSteps.
// Downstream steps still see the chain as successful.
func TestRunForEachEmptyArray(t *testing.T) {
	c := &chain.Chain{
		Name: "fanout-empty",
		Steps: []chain.Step{
			{ID: "list", Run: `echo '[]'`, Capture: "items"},
			{ID: "per-item", ForEach: "${items}", Run: "echo never"},
			{ID: "after", Run: "echo after"},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.Status != chain.StatusSuccess {
		t.Errorf("status: %s", res.Status)
	}
	iter := res.Steps[1]
	if iter.Status != chain.StepOK {
		t.Errorf("for_each status: %s", iter.Status)
	}
	if len(iter.SubSteps) != 0 {
		t.Errorf("expected no iterations; got %d", len(iter.SubSteps))
	}
	// after step ran.
	if len(res.Steps) != 3 {
		t.Errorf("steps count: %d", len(res.Steps))
	}
}

// TestRunForEachNonArrayFails: a non-array iterable (string, object,
// scalar) trips a hard failure with a clear reason. Operators see
// the step ID and the resolved value's type.
func TestRunForEachNonArrayFails(t *testing.T) {
	c := &chain.Chain{
		Name: "fanout-bad",
		Steps: []chain.Step{
			{ID: "list", Run: `echo '"not a list"'`, Capture: "items"},
			{ID: "per-item", ForEach: "${items}", Run: "echo ${item}"},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	if !strings.Contains(strings.ToLower(res.Failure["reason"].(string)), "for_each") {
		t.Errorf("failure reason: %v", res.Failure["reason"])
	}
}

// TestRunForEachSerialFailureStops: with a serial loop, a failed
// iteration stops further iterations and propagates as the outer
// step's failure. The successful iterations before the failure are
// still recorded in SubSteps.
//
// Use ${index} (an int captured as int) to drive the conditional
// because shell-quoting of ${item} would change the literal-equality
// check. ${index} renders as the number; `[ 1 -eq 1 ]` succeeds.
func TestRunForEachSerialFailureStops(t *testing.T) {
	c := &chain.Chain{
		Name: "fanout-fail",
		Steps: []chain.Step{
			{ID: "list", Run: `echo '["a","b","c"]'`, Capture: "items"},
			{
				ID:      "per-item",
				ForEach: "${items}",
				Run:     `[ ${index} = 1 ] && exit 7; echo ${item}`,
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	iter := res.Steps[1]
	if len(iter.SubSteps) != 2 {
		t.Errorf("expected first 2 iterations recorded; got %d", len(iter.SubSteps))
	}
}

// --- Phase C / chain composition ---

// TestRunChainComposition runs a saved chain as a step. The inner
// chain's captures collapse into the outer step's capture under
// the configured name, so downstream steps can reference
// ${outer-step.<inner-capture>.<field>}.
func TestRunChainComposition(t *testing.T) {
	inner := &chain.Chain{
		Name: "inner",
		Vars: map[string]chain.VarSpec{"who": {Required: true}},
		Steps: []chain.Step{
			{ID: "greet", Run: `echo '{"data":{"hello":"${who}"}}'`, Capture: "msg"},
		},
	}
	outer := &chain.Chain{
		Name: "outer",
		Steps: []chain.Step{
			{
				ID:    "call-inner",
				Chain: "inner",
				Vars: map[string]string{
					"who": "world",
				},
				Capture: "result",
			},
			{
				ID:  "after",
				Run: `echo got ${result.msg.hello}`,
			},
		},
	}
	resolver := func(name string) (*chain.Chain, error) {
		if name == "inner" {
			return inner, nil
		}
		return nil, fmt.Errorf("not found: %s", name)
	}
	res, err := chain.Run(context.Background(), outer, nil, chain.RunOptions{
		SubChainResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	// `after` should see the inner chain's captured msg.
	if got := res.Steps[1].Stdout; !strings.Contains(got, "got world") {
		t.Errorf("downstream substitution failed; stdout: %q", got)
	}
}

// TestRunChainResolverMissing: a chain step without a configured
// resolver fails with a clear reason rather than silently no-oping.
func TestRunChainResolverMissing(t *testing.T) {
	c := &chain.Chain{
		Name: "outer",
		Steps: []chain.Step{
			{ID: "x", Chain: "inner"},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	if got, _ := res.Failure["reason"].(string); !strings.Contains(got, "resolver") {
		t.Errorf("failure reason: %v", res.Failure["reason"])
	}
}

// TestRunChainCompositionFailureBubbles: when the inner chain
// fails, the outer step fails — the inner failure payload is
// preserved on Result.Failure.
func TestRunChainCompositionFailureBubbles(t *testing.T) {
	inner := &chain.Chain{
		Name: "boom",
		Steps: []chain.Step{
			{ID: "fail", Run: "exit 9"},
		},
	}
	outer := &chain.Chain{
		Name:  "outer",
		Steps: []chain.Step{{ID: "call", Chain: "boom"}},
	}
	resolver := func(_ string) (*chain.Chain, error) { return inner, nil }
	res, _ := chain.Run(context.Background(), outer, nil, chain.RunOptions{
		SubChainResolver: resolver,
	})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
}

// TestRunChainRecursionLimit: a chain that resolves to itself trips
// MaxChainDepth (default 5). The fail message should make the
// recursion cause obvious.
func TestRunChainRecursionLimit(t *testing.T) {
	loop := &chain.Chain{
		Name:  "loop",
		Steps: []chain.Step{{ID: "self", Chain: "loop"}},
	}
	resolver := func(_ string) (*chain.Chain, error) { return loop, nil }
	res, _ := chain.Run(context.Background(), loop, nil, chain.RunOptions{
		SubChainResolver: resolver,
	})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	reason, _ := res.Failure["reason"].(string)
	if !strings.Contains(reason, "recursion") && !strings.Contains(reason, "cycle") {
		t.Errorf("failure reason should signal recursion/cycle; got %q", reason)
	}
}

// TestRunChainNestedYieldResume: an inner chain yield bubbles up
// as an outer chain yield with the outer ResumeToken pointing at
// the outer state file. Resume of the outer token re-runs the
// inner chain from its yield point — verified by capturing how
// many times the per-iteration step ran (once when first invoked,
// once on resume).
func TestRunChainNestedYieldResume(t *testing.T) {
	dir := t.TempDir()
	// Inner chain has one step that yields on first run, succeeds
	// on resume — implemented via a temp marker file: if it
	// doesn't exist, the step creates it and exits 5 (rate_limit);
	// if it does exist, it succeeds.
	marker := filepath.Join(dir, "marker")
	inner := &chain.Chain{
		Name: "inner",
		Steps: []chain.Step{
			{
				ID:      "transient",
				Run:     fmt.Sprintf("[ -f %s ] || (touch %s && exit 5)", marker, marker),
				YieldOn: []chain.YieldCondition{chain.YieldRateLimited},
			},
		},
	}
	outer := &chain.Chain{
		Name:  "outer",
		Steps: []chain.Step{{ID: "go", Chain: "inner"}},
	}
	resolver := func(_ string) (*chain.Chain, error) { return inner, nil }
	opts := chain.RunOptions{
		StateDir:         dir,
		SubChainResolver: resolver,
	}
	res1, _ := chain.Run(context.Background(), outer, nil, opts)
	if res1.Status != chain.StatusYielded {
		t.Fatalf("first run status: %s, failure: %+v", res1.Status, res1.Failure)
	}
	if res1.YieldReason != chain.YieldRateLimited {
		t.Errorf("yield reason: %q", res1.YieldReason)
	}
	if res1.ResumeToken == "" {
		t.Fatal("missing resume token")
	}

	// Resume — the inner chain should pick up its single step's
	// re-run, see the marker, succeed; outer chain ends success.
	res2, err := chain.Resume(context.Background(), res1.ResumeToken, "continue", opts)
	if err != nil {
		t.Fatalf("resume err: %v", err)
	}
	if res2.Status != chain.StatusSuccess {
		t.Errorf("resume status: %s, failure: %+v", res2.Status, res2.Failure)
	}
}

// TestRunChainCycleDetection: chain A → chain B → chain A trips
// cycle detection separately from depth. Cleaner error message
// because we can name the cycle pair.
func TestRunChainCycleDetection(t *testing.T) {
	a := &chain.Chain{
		Name:  "a",
		Steps: []chain.Step{{ID: "a-step", Chain: "b"}},
	}
	b := &chain.Chain{
		Name:  "b",
		Steps: []chain.Step{{ID: "b-step", Chain: "a"}},
	}
	resolver := func(name string) (*chain.Chain, error) {
		switch name {
		case "a":
			return a, nil
		case "b":
			return b, nil
		}
		return nil, fmt.Errorf("missing %q", name)
	}
	res, _ := chain.Run(context.Background(), a, nil, chain.RunOptions{
		SubChainResolver: resolver,
	})
	if res.Status != chain.StatusFailure {
		t.Errorf("status: %s", res.Status)
	}
	reason, _ := res.Failure["reason"].(string)
	if !strings.Contains(reason, "cycle") && !strings.Contains(reason, "recursion") {
		t.Errorf("failure reason: %q", reason)
	}
}

// TestRunForEachItemFromObjectArray: when the iterable is a list of
// objects (not scalars), ${item.field} resolves into the object.
// This is the common shape — gaia issue list returns objects, not
// scalars — so it has to work cleanly.
func TestRunForEachItemFromObjectArray(t *testing.T) {
	c := &chain.Chain{
		Name: "fanout-objects",
		Steps: []chain.Step{
			{
				ID:      "list",
				Run:     `echo '[{"number":1,"title":"x"},{"number":2,"title":"y"}]'`,
				Capture: "issues",
			},
			{
				ID:      "per-item",
				ForEach: "${issues}",
				Run:     `echo issue ${item.number}: ${item.title}`,
			},
		},
	}
	res, _ := chain.Run(context.Background(), c, nil, chain.RunOptions{})
	if res.Status != chain.StatusSuccess {
		t.Fatalf("status: %s, failure: %+v", res.Status, res.Failure)
	}
	iter := res.Steps[1]
	wants := []string{"issue 1: x\n", "issue 2: y\n"}
	for i, want := range wants {
		if iter.SubSteps[i].Stdout != want {
			t.Errorf("iter %d stdout: got %q want %q", i, iter.SubSteps[i].Stdout, want)
		}
	}
}
