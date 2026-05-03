package chain_test

import (
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/chain"
)

// TestCannedPRCreateAndLandParses verifies the canned chain shipped at
// .gaia/chains/pr-create-and-land.yaml parses + validates. Acts as a
// guardrail — refactors of the chain schema (YAML field renames,
// vocabulary tightening, etc.) that would break the canned chain
// trip this test before they reach an operator's checkout.
//
// Phase B-3 / #112.
func TestCannedPRCreateAndLandParses(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", ".gaia", "chains", "pr-create-and-land.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := chain.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if c.Name != "pr-create-and-land" {
		t.Errorf("name: got %q want pr-create-and-land", c.Name)
	}
	wantSteps := []string{"open", "wait-checks", "merge"}
	if len(c.Steps) != len(wantSteps) {
		t.Fatalf("steps: got %d, want %d", len(c.Steps), len(wantSteps))
	}
	for i, want := range wantSteps {
		if c.Steps[i].ID != want {
			t.Errorf("step %d id: got %q want %q", i, c.Steps[i].ID, want)
		}
	}

	// wait-checks must declare check_failed in abort_on (the
	// non-recoverable branch) and check_flaky / rate_limited / timeout
	// in yield_on (the recoverable branches). A future re-organization
	// that flips these is a behaviour change agents would notice.
	wait := c.Steps[1]
	if !containsCondition(wait.AbortOn, chain.YieldCheckFailed) {
		t.Errorf("wait-checks: abort_on missing check_failed; got %v", wait.AbortOn)
	}
	for _, want := range []chain.YieldCondition{chain.YieldCheckFlaky, chain.YieldRateLimited, chain.YieldTimeout} {
		if !containsCondition(wait.YieldOn, want) {
			t.Errorf("wait-checks: yield_on missing %q; got %v", want, wait.YieldOn)
		}
	}

	// merge step must yield on merge_conflict + review_required so a
	// chain pause is the response to "needs rebase" or "needs another
	// reviewer", not a hard failure.
	merge := c.Steps[2]
	for _, want := range []chain.YieldCondition{chain.YieldMergeConflict, chain.YieldReviewRequired} {
		if !containsCondition(merge.YieldOn, want) {
			t.Errorf("merge: yield_on missing %q; got %v", want, merge.YieldOn)
		}
	}
}

func containsCondition(list []chain.YieldCondition, c chain.YieldCondition) bool {
	for _, x := range list {
		if x == c {
			return true
		}
	}
	return false
}
