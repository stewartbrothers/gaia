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
	if res.Steps[0].Run != "echo hello world" {
		t.Errorf("resolved run: %q", res.Steps[0].Run)
	}
	if res.Steps[0].Status != chain.StepSkipped {
		t.Errorf("dry-run step should be skipped; got %s", res.Steps[0].Status)
	}
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

	_, err := chain.Resume(context.Background(), res1.ResumeToken, "modify", chain.RunOptions{StateDir: dir})
	if err == nil || !strings.Contains(err.Error(), "modify") {
		t.Errorf("expected error for unsupported 'modify' decision; got %v", err)
	}
}

func TestResumeMissingTokenErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := chain.Resume(context.Background(), "no-such-token", "continue", chain.RunOptions{StateDir: dir})
	if err == nil {
		t.Error("expected error for missing token")
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
