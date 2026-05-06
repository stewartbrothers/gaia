package forgejo

import (
	"context"
	"fmt"
	"net/url"

	"github.com/stewartbrothers/gaia/core/types"
)

// GetCommitStatus returns the combined CI status for the commit identified
// by ref. Forgejo's /commits/{ref}/status endpoint accepts SHAs, branch
// names, and tag names without prior resolution.
func (p *Provider) GetCommitStatus(ctx context.Context, owner, repo, ref string) (*types.CISummary, error) {
	var status apiCommitStatus
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/status", owner, repo, url.PathEscape(ref))
	if err := p.client.Get(ctx, path, &status); err != nil {
		return nil, err
	}
	return status.toCISummary(), nil
}
