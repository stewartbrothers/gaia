package github

import (
	"context"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// GitHub's REST API does not expose issue dependencies. They added
// an `IssueDependency` type to the GraphQL schema in 2024 but it's
// GraphQL-only and not all repos have it enabled. The Provider
// contract (docs/provider-contract.md §10) requires unsupported
// methods return a clear NotImplemented-shaped error rather than a
// wrong-shape stub. When GitHub's GraphQL dependency surface
// stabilises we'll wire it here.
//
// Tracked in issue #317.

func (p *Provider) ListIssueDependencies(_ context.Context, _, _ string, _ int, _ provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.Generic,
		"issue dependencies are not available for the GitHub provider — GitHub's REST API has no equivalent endpoint; tracked in #317")
}

func (p *Provider) ListIssueBlocks(_ context.Context, _, _ string, _ int, _ provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return nil, nil, exitcode.Errorf(exitcode.Generic,
		"issue dependencies are not available for the GitHub provider — GitHub's REST API has no equivalent endpoint; tracked in #317")
}

func (p *Provider) AddIssueDependency(_ context.Context, _, _ string, _, _ int) (*types.Issue, error) {
	return nil, exitcode.Errorf(exitcode.Generic,
		"issue dependencies are not available for the GitHub provider — GitHub's REST API has no equivalent endpoint; tracked in #317")
}

func (p *Provider) RemoveIssueDependency(_ context.Context, _, _ string, _, _ int) error {
	return exitcode.Errorf(exitcode.Generic,
		"issue dependencies are not available for the GitHub provider — GitHub's REST API has no equivalent endpoint; tracked in #317")
}
