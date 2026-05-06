package github

import (
	"context"
	"fmt"
	"net/url"

	"github.com/stewartbrothers/gaia/core/types"
)

// GetCommitStatus returns the combined CI status for the commit identified
// by ref. GitHub's /commits/{ref}/check-runs endpoint accepts SHAs, branch
// names, and tag names without prior resolution.
func (p *Provider) GetCommitStatus(ctx context.Context, owner, repo, ref string) (*types.CISummary, error) {
	var checks apiCheckRuns
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, url.PathEscape(ref))
	if err := p.client.Get(ctx, path, &checks); err != nil {
		return nil, err
	}
	return checks.toCISummary(), nil
}
