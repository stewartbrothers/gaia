package forgejo

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiCreateIssueRequest is the POST /repos/{o}/{r}/issues body.
// Forgejo accepts `labels: []string` (names) on Forgejo 1.21+; we
// rely on that. For older Gitea hosts users may need to pre-create
// the labels via `gaia label create` and we surface the error.
type apiCreateIssueRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// apiEditIssueRequest is the PATCH /repos/{o}/{r}/issues/{n} body.
// Empty fields are dropped by omitempty so they're treated as
// "no change" by the upstream.
type apiEditIssueRequest struct {
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty"`
	State     string   `json:"state,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// CreateIssue opens a new issue.
func (p *Provider) CreateIssue(ctx context.Context, owner, repo string, opts provider.CreateIssueOptions) (*types.Issue, error) {
	body := apiCreateIssueRequest{
		Title:     opts.Title,
		Body:      opts.Body,
		Labels:    opts.Labels,
		Assignees: opts.Assignees,
	}
	var raw apiIssue
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	if err := p.client.Post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	// New issue could appear in any cached list query for this repo;
	// flush the issue list_index (#42).
	p.client.cache.Invalidator().AfterCreate(ctx, kindIssue, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditIssue patches an existing issue. Title, Body, State, Assignees
// each become PATCH fields when non-empty/non-nil. AddLabels and
// RemoveLabels are NOT applied here — label list mutation goes
// through the dedicated /issues/{n}/labels endpoints in a Phase 1.5
// follow-up; for now this method only changes scalar fields.
func (p *Provider) EditIssue(ctx context.Context, owner, repo string, n int, opts provider.EditIssueOptions) (*types.Issue, error) {
	body := apiEditIssueRequest{
		Title:     opts.Title,
		Body:      opts.Body,
		State:     opts.State,
		Assignees: opts.Assignees,
	}
	var raw apiIssue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, n)
	if err := p.client.Patch(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	// State/title/body change can move the issue between lists; flush
	// both the object row and the repo's issue list_index (#42).
	p.client.cache.Invalidator().AfterObjectMutation(ctx, kindIssue, owner, repo, itoa(n))
	out := raw.toType()
	return &out, nil
}
