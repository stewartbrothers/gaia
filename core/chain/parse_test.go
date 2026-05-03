package chain_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/chain"
)

func TestParseValidChain(t *testing.T) {
	yaml := `
name: pr-and-merge
description: Open a PR, then auto-merge once CI is green.
vars:
  title:
    required: true
  body:
    required: true
  base:
    default: main
steps:
  - id: open
    run: gaia pr create --title "${title}" --body "${body}" --base "${base}"
    capture: pr
    on_failure:
      return:
        reason: pr-create-failed
  - id: merge
    run: gaia pr merge ${pr.number} --method squash
`
	c, err := chain.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Name != "pr-and-merge" {
		t.Errorf("Name: got %q", c.Name)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("Steps count: got %d, want 2", len(c.Steps))
	}
	if c.Steps[0].Capture != "pr" {
		t.Errorf("step 0 capture: %q", c.Steps[0].Capture)
	}
	if c.Steps[0].OnFailure == nil || c.Steps[0].OnFailure.Return["reason"] != "pr-create-failed" {
		t.Errorf("step 0 on_failure: %+v", c.Steps[0].OnFailure)
	}
	if c.Vars["title"].Required != true {
		t.Errorf("title required: %+v", c.Vars["title"])
	}
	if c.Vars["base"].Default != "main" {
		t.Errorf("base default: %+v", c.Vars["base"])
	}
}

func TestParseFailsOnMissingName(t *testing.T) {
	yaml := `steps:
  - id: x
    run: echo hi
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("expected missing-name error, got %v", err)
	}
}

func TestParseFailsOnNoSteps(t *testing.T) {
	yaml := `name: empty
steps: []
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "step") {
		t.Errorf("expected missing-steps error, got %v", err)
	}
}

func TestParseFailsOnDuplicateStepID(t *testing.T) {
	yaml := `name: dup
steps:
  - id: x
    run: echo a
  - id: x
    run: echo b
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-id error, got %v", err)
	}
}

func TestParseFailsOnEmptyStepID(t *testing.T) {
	yaml := `name: noid
steps:
  - run: echo hi
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "id") {
		t.Errorf("expected missing-id error, got %v", err)
	}
}

func TestParseFailsOnEmptyStepRun(t *testing.T) {
	yaml := `name: norun
steps:
  - id: x
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "run") {
		t.Errorf("expected missing-run error, got %v", err)
	}
}

func TestParseFailsOnInvalidCaptureName(t *testing.T) {
	cases := []string{
		"with spaces",
		"with.dot",
		"123-leading-digit",
		"ends-with-hyphen-",
	}
	for _, capture := range cases {
		yaml := "name: c\nsteps:\n  - id: x\n    run: echo\n    capture: \"" + capture + "\"\n"
		if _, err := chain.Parse([]byte(yaml)); err == nil {
			t.Errorf("capture %q should be rejected", capture)
		}
	}
}

func TestParseAcceptsValidCaptureNames(t *testing.T) {
	cases := []string{"pr", "merge_sha", "abc123", "_underscore_first"}
	for _, capture := range cases {
		yaml := "name: c\nsteps:\n  - id: x\n    run: echo\n    capture: " + capture + "\n"
		if _, err := chain.Parse([]byte(yaml)); err != nil {
			t.Errorf("capture %q should be accepted; got %v", capture, err)
		}
	}
}

func TestParseFailsOnMalformedYAML(t *testing.T) {
	if _, err := chain.Parse([]byte("not valid yaml: {")); err == nil {
		t.Error("expected yaml parse error")
	}
}

func TestParseAcceptsKnownYieldConditions(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo hi
    yield_on:
      - auth_error
      - not_found
      - rate_limited
    abort_on:
      - timeout
`
	c, err := chain.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Steps[0].YieldOn) != 3 {
		t.Errorf("yield_on count: %d", len(c.Steps[0].YieldOn))
	}
	if len(c.Steps[0].AbortOn) != 1 {
		t.Errorf("abort_on count: %d", len(c.Steps[0].AbortOn))
	}
}

func TestParseRejectsUnknownYieldCondition(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo hi
    yield_on:
      - typo_here
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "typo_here") {
		t.Errorf("expected rejection of unknown condition; got %v", err)
	}
}

func TestParseRejectsConditionInBothYieldAndAbort(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo hi
    yield_on: [timeout]
    abort_on: [timeout]
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("expected rejection of duplicate; got %v", err)
	}
}

func TestAllYieldConditionsCoversIsKnown(t *testing.T) {
	for _, c := range chain.AllYieldConditions() {
		if !c.IsKnown() {
			t.Errorf("condition %q not in IsKnown", c)
		}
	}
	if chain.YieldCondition("invented").IsKnown() {
		t.Error("invented condition should not be IsKnown")
	}
}

// --- Phase B-2 parse tests ---

func TestParseAcceptsTimeoutAndRetry(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo hi
    timeout: 5m
    retry:
      max: 3
      delay: 30s
      backoff: exponential
`
	c, err := chain.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Steps[0].Timeout != "5m" {
		t.Errorf("timeout: %q", c.Steps[0].Timeout)
	}
	if c.Steps[0].Retry == nil || c.Steps[0].Retry.Max != 3 ||
		c.Steps[0].Retry.Delay != "30s" || c.Steps[0].Retry.Backoff != "exponential" {
		t.Errorf("retry: %+v", c.Steps[0].Retry)
	}
}

func TestParseRejectsBadTimeoutDuration(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo
    timeout: nonsense
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout-format error; got %v", err)
	}
}

func TestParseRejectsBadRetryDelay(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo
    retry:
      max: 2
      delay: forever
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "delay") {
		t.Errorf("expected retry.delay error; got %v", err)
	}
}

func TestParseRejectsNegativeRetryMax(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo
    retry:
      max: -1
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "max") {
		t.Errorf("expected retry.max error; got %v", err)
	}
}

func TestParseRejectsBadRetryBackoff(t *testing.T) {
	yaml := `name: c
steps:
  - id: x
    run: echo
    retry:
      max: 2
      backoff: weird
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "backoff") {
		t.Errorf("expected retry.backoff error; got %v", err)
	}
}

func TestParseAcceptsDefaultYieldOn(t *testing.T) {
	yaml := `name: c
default_yield_on: [rate_limited, timeout]
steps:
  - id: x
    run: echo
`
	c, err := chain.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.DefaultYieldOn) != 2 {
		t.Errorf("default_yield_on: %+v", c.DefaultYieldOn)
	}
}

func TestParseRejectsUnknownDefaultYieldOn(t *testing.T) {
	yaml := `name: c
default_yield_on: [bogus]
steps:
  - id: x
    run: echo
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected unknown-condition error; got %v", err)
	}
}

func TestParseAcceptsCleanupBlock(t *testing.T) {
	yaml := `name: c
steps:
  - id: open
    run: 'echo open'
cleanup:
  - id: close
    run: 'echo close'
`
	c, err := chain.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Cleanup) != 1 || c.Cleanup[0].ID != "close" {
		t.Errorf("cleanup: %+v", c.Cleanup)
	}
}

func TestParseRejectsCleanupWithDuplicateID(t *testing.T) {
	yaml := `name: c
steps:
  - id: open
    run: echo open
cleanup:
  - id: cleanup1
    run: echo a
  - id: cleanup1
    run: echo b
`
	if _, err := chain.Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-id error in cleanup; got %v", err)
	}
}

// --- B-3 / #112: saved-chain resolution ---

func TestResolveLiteralPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "literal.yaml")
	if err := os.WriteFile(path, []byte("name: x\nsteps:\n  - id: a\n    run: echo a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := chain.Resolve(path, chain.ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != path {
		t.Errorf("got %q want %q", got, path)
	}
}

func TestResolveProjectLocalWins(t *testing.T) {
	root := t.TempDir()
	chains := filepath.Join(root, ".gaia", "chains")
	if err := os.MkdirAll(chains, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(chains, "ship.yaml")
	if err := os.WriteFile(want, []byte("name: ship\nsteps:\n  - id: a\n    run: echo a\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Also seed the global location with a different file — project must win.
	globalDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(globalDir, "ship.yaml"), []byte("name: global\nsteps:\n  - id: b\n    run: echo b\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := chain.Resolve("ship", chain.ResolveOptions{ProjectRoot: root, GlobalDir: globalDir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Errorf("project-local should win: got %q want %q", got, want)
	}
}

func TestResolveGlobalFallback(t *testing.T) {
	root := t.TempDir() // no .gaia/chains here
	globalDir := t.TempDir()
	want := filepath.Join(globalDir, "deploy.yaml")
	if err := os.WriteFile(want, []byte("name: deploy\nsteps:\n  - id: a\n    run: echo a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := chain.Resolve("deploy", chain.ResolveOptions{ProjectRoot: root, GlobalDir: globalDir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolveNotFoundListsAttempts(t *testing.T) {
	root := t.TempDir()
	globalDir := t.TempDir()
	_, err := chain.Resolve("missing", chain.ResolveOptions{ProjectRoot: root, GlobalDir: globalDir})
	if err == nil {
		t.Fatal("expected error")
	}
	var rerr *chain.ResolveError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *chain.ResolveError, got %T", err)
	}
	if len(rerr.Attempts) != 2 {
		t.Errorf("expected 2 attempts (project + global); got %d: %v", len(rerr.Attempts), rerr.Attempts)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error message should mention name: %q", err.Error())
	}
}

func TestResolveAcceptsBareIdentifierWithExtension(t *testing.T) {
	// "ship.yaml" passed as the name should still match the saved
	// chain at .gaia/chains/ship.yaml — but the looksLikePath
	// heuristic short-circuits to literal-path interpretation, so
	// it's tried as a path first. With a relative path that doesn't
	// exist in cwd, the fallback project lookup kicks in.
	root := t.TempDir()
	chains := filepath.Join(root, ".gaia", "chains")
	if err := os.MkdirAll(chains, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(chains, "ship.yaml")
	if err := os.WriteFile(want, []byte("name: ship\nsteps:\n  - id: a\n    run: echo a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := chain.Resolve("ship.yaml", chain.ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolveEmptyName(t *testing.T) {
	if _, err := chain.Resolve("", chain.ResolveOptions{}); err == nil {
		t.Fatal("expected error on empty name")
	}
}
