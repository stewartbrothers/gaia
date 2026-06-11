package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Branch protection is NotImplemented on GitHub in this v1 (#345). The
// trimmed shape maps cleanly to GitHub's
// /repos/{o}/{r}/branches/{branch}/protection API, but GitHub provider
// parity is its own phase; the methods return NotImplemented so the
// contract holds and the CLI/MCP surface the gap rather than a
// wrong-shape result. Tracked as the #345 GitHub follow-up.

const bpNotImpl = "branch protection is not yet implemented for the GitHub provider (#345 follow-up)"

func (p *Provider) GetBranchProtection(_ context.Context, _, _, _ string) (*types.BranchProtection, error) {
	return nil, exitcode.Errorf(exitcode.NotImplemented, bpNotImpl)
}

func (p *Provider) SetBranchProtection(_ context.Context, _, _, _ string, _ provider.SetBranchProtectionOptions) (*types.BranchProtection, error) {
	return nil, exitcode.Errorf(exitcode.NotImplemented, bpNotImpl)
}

func (p *Provider) DeleteBranchProtection(_ context.Context, _, _, _ string) error {
	return exitcode.Errorf(exitcode.NotImplemented, bpNotImpl)
}
