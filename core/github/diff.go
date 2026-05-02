package github

import (
	"context"
	"fmt"

	gaiadiff "github.com/stewartbrothers/gaia/core/diff"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// GetPullRequestDiff fetches the unified diff from
// /repos/{owner}/{repo}/pulls/{n}. Unlike the Forgejo path which
// uses a `.diff` URL suffix, GitHub uses the same URL but requires
// `Accept: application/vnd.github.v3.diff` to get raw diff text
// instead of the JSON PR shape.
func (p *Provider) GetPullRequestDiff(ctx context.Context, owner, repo string, n int, opts provider.GetPullRequestDiffOptions) ([]types.DiffFile, error) {
	raw, err := p.client.GetRaw(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n),
		diffAccept,
	)
	if err != nil {
		return nil, err
	}
	files, err := gaiadiff.ParseUnifiedDiff(string(raw))
	if err != nil {
		return nil, err
	}
	return gaiadiff.FilterByPaths(files, opts.Paths), nil
}
