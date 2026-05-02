package forgejo

import (
	"context"
	"fmt"
	"time"

	"github.com/stewartbrothers/gaia/core/types"
)

// apiCommentRequest is the body for POST /issues/{n}/comments and
// PATCH /issues/comments/{id} — both endpoints take just `{body}`.
type apiCommentRequest struct {
	Body string `json:"body"`
}

// apiCreatedComment mirrors the Forgejo response shape for a created
// or edited issue comment. Forgejo doesn't return SubmittedAt for
// issue comments (only for reviews) so we read created_at/updated_at.
type apiCreatedComment struct {
	ID        int64     `json:"id"`
	User      apiUser   `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *apiCreatedComment) toType() types.Comment {
	return types.Comment{
		ID:        a.ID,
		Source:    "issue",
		Author:    types.User{Login: a.User.Login},
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// CreateIssueComment posts a top-level thread comment on issue or PR n.
func (p *Provider) CreateIssueComment(ctx context.Context, owner, repo string, n int, body string) (*types.Comment, error) {
	var raw apiCreatedComment
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, n)
	if err := p.client.Post(ctx, path, apiCommentRequest{Body: body}, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditIssueComment patches an existing comment by ID.
func (p *Provider) EditIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (*types.Comment, error) {
	var raw apiCreatedComment
	path := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, commentID)
	if err := p.client.Patch(ctx, path, apiCommentRequest{Body: body}, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteIssueComment removes a comment by ID.
func (p *Provider) DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, commentID)
	return p.client.Delete(ctx, path)
}
