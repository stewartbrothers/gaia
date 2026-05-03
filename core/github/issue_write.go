package github

import (
	"context"
	"fmt"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiCreateIssueRequest mirrors the POST /repos/{o}/{r}/issues body.
// GitHub accepts label names (not IDs) on the create endpoint.
type apiCreateIssueRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// apiEditIssueRequest mirrors the PATCH endpoint body. omitempty on
// every field matches GitHub's "fields not present stay unchanged"
// PATCH semantics.
type apiEditIssueRequest struct {
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty"`
	State     string   `json:"state,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// apiCreatedComment is the response shape for create + edit comment.
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

// CreateIssue opens a new issue on the named repo.
func (p *Provider) CreateIssue(ctx context.Context, owner, repo string, opts provider.CreateIssueOptions) (*types.Issue, error) {
	body := apiCreateIssueRequest{
		Title:     opts.Title,
		Body:      opts.Body,
		Labels:    opts.Labels,
		Assignees: opts.Assignees,
	}
	var raw apiIssue
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/issues", owner, repo), body, &raw); err != nil {
		return nil, err
	}
	p.client.cache.Invalidator().AfterCreate(ctx, kindIssue, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditIssue patches an existing issue. AddLabels/RemoveLabels are
// not applied here — like the Forgejo provider, label-list mutation
// is a separate concern that lives in a follow-up.
func (p *Provider) EditIssue(ctx context.Context, owner, repo string, n int, opts provider.EditIssueOptions) (*types.Issue, error) {
	body := apiEditIssueRequest{
		Title:     opts.Title,
		Body:      opts.Body,
		State:     opts.State,
		Assignees: opts.Assignees,
	}
	var raw apiIssue
	if err := p.client.Patch(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, n), body, &raw); err != nil {
		return nil, err
	}
	p.client.cache.Invalidator().AfterObjectMutation(ctx, kindIssue, owner, repo, itoa(n))
	out := raw.toType()
	return &out, nil
}

// CreateIssueComment posts a top-level thread comment on issue or PR n.
func (p *Provider) CreateIssueComment(ctx context.Context, owner, repo string, n int, body string) (*types.Comment, error) {
	var raw apiCreatedComment
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, n)
	if err := p.client.Post(ctx, path, map[string]string{"body": body}, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditIssueComment patches an existing comment by ID.
func (p *Provider) EditIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (*types.Comment, error) {
	var raw apiCreatedComment
	path := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, commentID)
	if err := p.client.Patch(ctx, path, map[string]string{"body": body}, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteIssueComment removes a comment by ID. 204 is success.
func (p *Provider) DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error {
	return p.client.Delete(ctx, fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, commentID))
}
