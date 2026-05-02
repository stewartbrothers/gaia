package forgejo

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// CreatePullRequest opens a new PR.
func (p *Provider) CreatePullRequest(ctx context.Context, owner, repo string, opts provider.CreatePullRequestOptions) (*types.PullRequest, error) {
	var raw apiPullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	if err := p.client.Post(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditPullRequest patches a PR. Same omit-on-empty semantics as
// EditIssue. Draft is *bool because false != "no change".
func (p *Provider) EditPullRequest(ctx context.Context, owner, repo string, n int, opts provider.EditPullRequestOptions) (*types.PullRequest, error) {
	var raw apiPullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n)
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// MergePullRequest performs the merge with the requested method.
// Forgejo returns 200 (with no body) on success, 405 if the PR isn't
// mergeable. The error mapping in core/exitcode handles the surfacing
// (405 → Generic; consider Auth or Usage if Forgejo specifies which
// case applied).
func (p *Provider) MergePullRequest(ctx context.Context, owner, repo string, n int, opts provider.MergePullRequestOptions) error {
	if opts.Method == "" {
		opts.Method = "merge"
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, n)
	return p.client.Post(ctx, path, opts, nil)
}
