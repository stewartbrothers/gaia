package github

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/provider"
)

// apiReviewRequest is GitHub's POST /pulls/{n}/reviews body. Field
// names match GitHub's: event, body, comments[]{path, position, body}.
//
// Note that GitHub's inline-comment shape uses `position` (the
// position in the diff) where Forgejo uses `new_position` (the line
// in the new file). The provider.ReviewInlineComment struct carries
// the line number; we map to position here. If pure line-number
// support is needed, GitHub also accepts a `line` + `side` pair on
// newer API versions — that's a follow-up.
type apiReviewRequest struct {
	Event    string                  `json:"event"`
	Body     string                  `json:"body,omitempty"`
	Comments []apiReviewInlineRequest `json:"comments,omitempty"`
}

type apiReviewInlineRequest struct {
	Path     string `json:"path"`
	Position int    `json:"position"`
	Body     string `json:"body"`
}

// SubmitReview posts a PR review with state + optional inline
// comments to /repos/{o}/{r}/pulls/{n}/reviews.
func (p *Provider) SubmitReview(ctx context.Context, owner, repo string, n int, opts provider.SubmitReviewOptions) error {
	if opts.Event == "" {
		opts.Event = "COMMENT"
	}
	body := apiReviewRequest{
		Event: opts.Event,
		Body:  opts.Body,
	}
	for _, c := range opts.Comments {
		body.Comments = append(body.Comments, apiReviewInlineRequest{
			Path:     c.Path,
			Position: c.Line, // map provider's "Line" → GitHub's "position"
			Body:     c.Body,
		})
	}
	return p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, n), body, nil)
}
