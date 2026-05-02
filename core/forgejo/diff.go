package forgejo

import (
	"context"
	"fmt"

	gaiadiff "github.com/stewartbrothers/gaia/core/diff"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// ParseUnifiedDiff is re-exported for backwards-compatibility with
// callers that imported this name from core/forgejo before the
// parser moved to core/diff. New callers should import core/diff
// directly.
func ParseUnifiedDiff(raw string) ([]types.DiffFile, error) {
	return gaiadiff.ParseUnifiedDiff(raw)
}

// GetPullRequestDiff fetches the raw unified diff from
// /repos/{owner}/{repo}/pulls/{n}.diff and parses it into structured
// DiffFile values. Binary files marshal with Binary=true and no
// Hunks. opts.Paths narrows the result to a subset of file paths
// (matched against either Path or OldPath, so renamed files match by
// either side).
func (p *Provider) GetPullRequestDiff(ctx context.Context, owner, repo string, n int, opts provider.GetPullRequestDiffOptions) ([]types.DiffFile, error) {
	raw, err := p.client.GetRaw(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d.diff", owner, repo, n))
	if err != nil {
		return nil, err
	}
	files, err := gaiadiff.ParseUnifiedDiff(string(raw))
	if err != nil {
		return nil, err
	}
	return gaiadiff.FilterByPaths(files, opts.Paths), nil
}
