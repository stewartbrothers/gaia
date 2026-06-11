package chain_test

import (
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/chain"
)

// chainPath resolves a shipped .gaia/chains/<name>.yaml from the repo
// root relative to this test file.
func chainPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", ".gaia", "chains", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGateChainParses guards .gaia/chains/gate.yaml — gaia's local
// pre-commit gate chain (gofmt → vet → lint → cover → build). A schema
// change that breaks it should trip here before an agent invokes it.
func TestGateChainParses(t *testing.T) {
	c, err := chain.ParseFile(chainPath(t, "gate.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if c.Name != "gate" {
		t.Errorf("name: got %q want gate", c.Name)
	}
	want := []string{"gofmt", "vet", "lint", "cover", "build"}
	if len(c.Steps) != len(want) {
		t.Fatalf("steps: got %d want %d", len(c.Steps), len(want))
	}
	for i, id := range want {
		if c.Steps[i].ID != id {
			t.Errorf("step %d: got %q want %q", i, c.Steps[i].ID, id)
		}
	}
}

// TestSyncChainParses guards .gaia/chains/sync.yaml — the post-merge
// local cleanup chain. The merged-state guard MUST be the first step so
// the force-delete can't nuke an unmerged branch.
func TestSyncChainParses(t *testing.T) {
	c, err := chain.ParseFile(chainPath(t, "sync.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if c.Name != "sync" {
		t.Errorf("name: got %q want sync", c.Name)
	}
	if len(c.Steps) == 0 || c.Steps[0].ID != "verify-merged" {
		t.Fatalf("first step must be verify-merged (the safety guard); got %+v", stepIDs(c))
	}
	want := []string{"verify-merged", "fetch", "switch-base", "fast-forward", "delete-branch"}
	if len(c.Steps) != len(want) {
		t.Fatalf("steps: got %v want %v", stepIDs(c), want)
	}
	for i, id := range want {
		if c.Steps[i].ID != id {
			t.Errorf("step %d: got %q want %q", i, c.Steps[i].ID, id)
		}
	}
	// pr + branch are required inputs; base defaults to main.
	for _, v := range []string{"pr", "branch", "base"} {
		if _, ok := c.Vars[v]; !ok {
			t.Errorf("missing var %q", v)
		}
	}
}

func stepIDs(c *chain.Chain) []string {
	out := make([]string, len(c.Steps))
	for i, s := range c.Steps {
		out[i] = s.ID
	}
	return out
}
