package github_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

// TestPackagesAreUnimplemented pins that the GitHub provider returns
// an exit-coded "not implemented" error from each Packages method.
// The follow-up issue (filed alongside #107) tracks the real impl.
func TestPackagesAreUnimplemented(t *testing.T) {
	p := github.NewProvider(github.Options{BaseURL: "https://example", Token: "X"})

	if _, _, err := p.ListPackages(context.Background(), "owner", provider.ListPackagesOptions{}); err == nil {
		t.Error("ListPackages should error")
	} else if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error: got %q, want substring 'not implemented'", err.Error())
	}

	if _, err := p.GetPackage(context.Background(), "owner", "generic", "n", "v"); err == nil {
		t.Error("GetPackage should error")
	} else if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error: got %q, want substring 'not implemented'", err.Error())
	}

	if err := p.DeletePackage(context.Background(), "owner", "generic", "n", "v"); err == nil {
		t.Error("DeletePackage should error")
	} else if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error: got %q, want substring 'not implemented'", err.Error())
	}
}
