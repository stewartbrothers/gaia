package chain_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		"GITEA_TOKEN":     "secret-gitea-token",
		"FORGEJO_TOKEN":   "secret-forgejo-token",
		"GH_TOKEN":        "secret-gh-token",
		"GITHUB_TOKEN":    "secret-github-token",
		"AWS_ACCESS_KEY":  "AKIA-secret-aws-key",
		"AWS_SECRET":      "secret-aws-shh",
		"GAIA_RANDOM_VAR": "should-also-be-stripped",
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
		"AWS_ACCESS_KEY", "AWS_SECRET", "GAIA_RANDOM_VAR",
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
