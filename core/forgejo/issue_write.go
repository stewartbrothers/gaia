package forgejo

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiCreateIssueRequest is the POST /repos/{o}/{r}/issues body.
// Forgejo's API requires labels as integer IDs, not names.
type apiCreateIssueRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Labels    []int64  `json:"labels,omitempty"`
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

// CreateIssue opens a new issue. Label names in opts.Labels are
// resolved to IDs via the repo's label list before posting.
func (p *Provider) CreateIssue(ctx context.Context, owner, repo string, opts provider.CreateIssueOptions) (*types.Issue, error) {
	var labelIDs []int64
	if len(opts.Labels) > 0 {
		nameToID, err := p.resolveLabelNames(ctx, owner, repo, opts.Labels)
		if err != nil {
			return nil, err
		}
		labelIDs = make([]int64, len(opts.Labels))
		for i, name := range opts.Labels {
			labelIDs[i] = nameToID[name]
		}
	}
	body := apiCreateIssueRequest{
		Title:     opts.Title,
		Body:      opts.Body,
		Labels:    labelIDs,
		Assignees: opts.Assignees,
	}
	var raw apiIssue
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	if err := p.client.Post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindIssue, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditIssue patches an existing issue. Scalar fields (Title, Body,
// State, Assignees) go in the PATCH body; AddLabels/RemoveLabels are
// applied via the dedicated /issues/{n}/labels endpoints after the
// PATCH.
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
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindIssue, owner, repo, itoa(n))

	if len(opts.AddLabels) > 0 || len(opts.RemoveLabels) > 0 {
		nameToID, err := p.resolveLabelNames(ctx, owner, repo, append(opts.AddLabels, opts.RemoveLabels...))
		if err != nil {
			return nil, err
		}
		if len(opts.AddLabels) > 0 {
			ids := make([]int64, len(opts.AddLabels))
			for i, name := range opts.AddLabels {
				ids[i] = nameToID[name]
			}
			addPath := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, n)
			addBody := struct {
				Labels []int64 `json:"labels"`
			}{Labels: ids}
			var ignored []apiLabelFull
			if err := p.client.Post(ctx, addPath, addBody, &ignored); err != nil {
				return nil, err
			}
		}
		for _, name := range opts.RemoveLabels {
			delPath := fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%d", owner, repo, n, nameToID[name])
			if err := p.client.Delete(ctx, delPath); err != nil {
				return nil, err
			}
		}
	}

	out := raw.toType()
	return &out, nil
}

// resolveLabelNames fetches the repo's label list once and returns a
// name→ID map for the requested names. Returns NotFound if any name
// is absent.
func (p *Provider) resolveLabelNames(ctx context.Context, owner, repo string, names []string) (map[string]int64, error) {
	all, err := p.fetchLabels(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	index := make(map[string]int64, len(all))
	for _, l := range all {
		index[l.Name] = l.ID
	}
	out := make(map[string]int64, len(names))
	for _, name := range names {
		id, ok := index[name]
		if !ok {
			return nil, exitcode.Errorf(exitcode.NotFound, "label %q not found in %s/%s", name, owner, repo)
		}
		out[name] = id
	}
	return out, nil
}
