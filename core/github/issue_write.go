package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiCreateIssueRequest mirrors the POST /repos/{o}/{r}/issues body.
// GitHub accepts label names (not IDs) on the create endpoint.
// Milestone is the milestone number (surfaced to callers as ID).
type apiCreateIssueRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Milestone int64    `json:"milestone,omitempty"`
}

// apiEditIssueRequest mirrors the PATCH endpoint body. omitempty on
// every field matches GitHub's "fields not present stay unchanged"
// PATCH semantics. Milestone is raw JSON rather than *int64 because
// GitHub — unlike Forgejo — requires a literal `null` (not `0`) to
// detach the current milestone; buildMilestonePatch produces that
// shape from the unified *int64 option (nil=omit, &0=null, &N=N).
type apiEditIssueRequest struct {
	Title     string          `json:"title,omitempty"`
	Body      string          `json:"body,omitempty"`
	State     string          `json:"state,omitempty"`
	Assignees []string        `json:"assignees,omitempty"`
	Milestone json.RawMessage `json:"milestone,omitempty"`
}

// buildMilestonePatch translates the unified EditIssueOptions.Milestone
// tri-state into GitHub's wire shape: nil stays nil (field omitted,
// "no change"); a pointer to 0 becomes literal `null` (detach); any
// other pointer becomes the plain integer (attach that milestone).
func buildMilestonePatch(milestone *int64) json.RawMessage {
	if milestone == nil {
		return nil
	}
	if *milestone == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(fmt.Sprintf("%d", *milestone))
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
		Milestone: opts.Milestone,
	}
	var raw apiIssue
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/issues", owner, repo), body, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindIssue, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditIssue patches an existing issue. Scalar fields go in the PATCH
// body; AddLabels/RemoveLabels are applied via the issue labels
// endpoints after the PATCH. GitHub accepts names (not IDs) in both.
func (p *Provider) EditIssue(ctx context.Context, owner, repo string, n int, opts provider.EditIssueOptions) (*types.Issue, error) {
	body := apiEditIssueRequest{
		Title:     opts.Title,
		Body:      opts.Body,
		State:     opts.State,
		Assignees: opts.Assignees,
		Milestone: buildMilestonePatch(opts.Milestone),
	}
	var raw apiIssue
	if err := p.client.Patch(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, n), body, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindIssue, owner, repo, itoa(n))

	if len(opts.AddLabels) > 0 {
		addPath := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, n)
		addBody := struct {
			Labels []string `json:"labels"`
		}{Labels: opts.AddLabels}
		var ignored []apiLabelFull
		if err := p.client.Post(ctx, addPath, addBody, &ignored); err != nil {
			return nil, err
		}
	}
	for _, name := range opts.RemoveLabels {
		delPath := fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%s", owner, repo, n, url.PathEscape(name))
		if err := p.client.Delete(ctx, delPath); err != nil {
			return nil, err
		}
	}

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
