package chain_test

import (
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
