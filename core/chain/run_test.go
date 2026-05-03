package chain_test

import (
	"context"
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
