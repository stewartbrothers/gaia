package forgejo

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/provider"
)

// SubmitReview submits a PR review with state + optional inline
// comments. Forgejo's endpoint accepts the body, event, and a
// `comments` array all in one POST — so a multi-comment review only
// costs one round-trip.
func (p *Provider) SubmitReview(ctx context.Context, owner, repo string, n int, opts provider.SubmitReviewOptions) error {
	if opts.Event == "" {
		opts.Event = "COMMENT"
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, n)
	return p.client.Post(ctx, path, opts, nil)
}
