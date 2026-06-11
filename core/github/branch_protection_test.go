package github_test

import (
	"context"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// TestBranchProtectionNotImplementedGH pins that the GitHub provider
// returns NotImplemented for the branch-protection methods (v1; parity
// is a #345 follow-up), so callers can branch on the code.
func TestBranchProtectionNotImplementedGH(t *testing.T) {
	p := newTestProvider(t, "https://example.test")
	ctx := context.Background()

	if _, err := p.GetBranchProtection(ctx, "o", "r", "main"); exitcode.Of(err) != exitcode.NotImplemented {
		t.Errorf("GetBranchProtection: want NotImplemented, got %v", err)
	}
	if _, err := p.SetBranchProtection(ctx, "o", "r", "main", provider.SetBranchProtectionOptions{}); exitcode.Of(err) != exitcode.NotImplemented {
		t.Errorf("SetBranchProtection: want NotImplemented, got %v", err)
	}
	if err := p.DeleteBranchProtection(ctx, "o", "r", "main"); exitcode.Of(err) != exitcode.NotImplemented {
		t.Errorf("DeleteBranchProtection: want NotImplemented, got %v", err)
	}
}
