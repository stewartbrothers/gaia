package aikestra

import (
	"context"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiComment is Aikestra's Comment schema. Its id is a string (UUID),
// which gaia's int64 Comment.ID can't hold, so the trimmed view leaves
// ID zero. Editing and deleting comments aren't in API v1 anyway.
type apiComment struct {
	Author    *string   `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a apiComment) toType() types.Comment {
	out := types.Comment{
		Source:    "issue",
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
	if a.Author != nil {
		out.Author = types.User{Login: *a.Author}
	}
	return out
}

// ListComments returns Issue n's comments, oldest first, walking the
// cursor to the end. opts.Limit caps the count. Aikestra has one comment
// stream per Issue, so opts.Sources is ignored.
func (p *Provider) ListComments(ctx context.Context, owner, _ string, n int, opts provider.ListCommentsOptions) ([]types.Comment, error) {
	raw, _, err := walk[apiComment](ctx, p.client, issuePath(owner, n, "/comments"), nil)
	if err != nil {
		return nil, err
	}
	if opts.Limit > 0 && len(raw) > opts.Limit {
		raw = raw[:opts.Limit]
	}
	out := make([]types.Comment, len(raw))
	for i, c := range raw {
		out[i] = c.toType()
	}
	return out, nil
}

// CreateIssueComment posts a comment on Issue n.
func (p *Provider) CreateIssueComment(ctx context.Context, owner, _ string, n int, body string) (*types.Comment, error) {
	req := struct {
		Body string `json:"body"`
	}{Body: body}
	var raw apiComment
	if err := p.client.Post(ctx, issuePath(owner, n, "/comments"), req, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditIssueComment is not in Aikestra API v1.
func (p *Provider) EditIssueComment(_ context.Context, _, _ string, _ int64, _ string) (*types.Comment, error) {
	return nil, notImplemented("editing comments")
}

// DeleteIssueComment is not in Aikestra API v1.
func (p *Provider) DeleteIssueComment(_ context.Context, _, _ string, _ int64) error {
	return notImplemented("deleting comments")
}
