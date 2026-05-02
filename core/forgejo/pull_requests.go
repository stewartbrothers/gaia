package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

type apiPullRequest struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	State     string       `json:"state"` // open | closed (Forgejo collapses merged into closed)
	User      apiUser      `json:"user"`
	Labels    []apiLabel   `json:"labels"`
	Head      apiBranchRef `json:"head"`
	Base      apiBranchRef `json:"base"`
	Merged    bool         `json:"merged"`
	Mergeable *bool        `json:"mergeable,omitempty"`
	Draft     bool         `json:"draft"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	ClosedAt  *time.Time   `json:"closed_at,omitempty"`
	MergedAt  *time.Time   `json:"merged_at,omitempty"`
}

type apiBranchRef struct {
	Ref  string  `json:"ref"`
	Sha  string  `json:"sha"`
	Repo apiRepo `json:"repo"`
}

type apiRepo struct {
	FullName string `json:"full_name"`
}

type apiCommitStatus struct {
	State    string          `json:"state"`
	Statuses []apiStatusItem `json:"statuses"`
}

type apiStatusItem struct {
	State string `json:"state"`
}

func (a *apiPullRequest) toType() types.PullRequest {
	state := a.State
	if a.Merged {
		state = "merged"
	}

	out := types.PullRequest{
		Number:    a.Number,
		Title:     a.Title,
		Body:      a.Body,
		State:     state,
		Author:    types.User{Login: a.User.Login},
		Head:      types.BranchRef{Ref: a.Head.Ref, SHA: a.Head.Sha},
		Base:      types.BranchRef{Ref: a.Base.Ref, SHA: a.Base.Sha},
		Mergeable: a.Mergeable,
		Draft:     a.Draft,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
		ClosedAt:  a.ClosedAt,
		MergedAt:  a.MergedAt,
	}
	for _, l := range a.Labels {
		out.Labels = append(out.Labels, types.Label{Name: l.Name})
	}
	// BranchRef.Repo populated only for cross-fork PRs (head from a
	// fork). Same-repo PRs leave it empty so the common case stays
	// short on the wire.
	if a.Head.Repo.FullName != "" && a.Head.Repo.FullName != a.Base.Repo.FullName {
		out.Head.Repo = a.Head.Repo.FullName
	}
	return out
}

func (s *apiCommitStatus) toCISummary() *types.CISummary {
	out := &types.CISummary{State: s.State, Total: len(s.Statuses)}
	for _, st := range s.Statuses {
		switch st.State {
		case "success":
			out.Successful++
		case "failure", "error":
			out.Failed++
		case "pending":
			out.Pending++
		}
	}
	return out
}

// ListPullRequests returns PRs matching opts.
func (p *Provider) ListPullRequests(ctx context.Context, owner, repo string, opts provider.ListPullRequestsOptions) ([]types.PullRequest, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if len(opts.Labels) > 0 {
		q.Set("labels", strings.Join(opts.Labels, ","))
	}
	if opts.Head != "" {
		q.Set("head", opts.Head)
	}
	if opts.Base != "" {
		q.Set("base", opts.Base)
	}

	path := fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, repo, q.Encode())
	var raw []apiPullRequest
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}

	out := make([]types.PullRequest, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetPullRequest returns a single PR. WithCISummary triggers an extra
// /commits/{sha}/status round-trip; WithComments inlines top-level
// (issue-thread) comments. Inline review comments are not fetched
// here — that's #18's unified-comments method.
func (p *Provider) GetPullRequest(ctx context.Context, owner, repo string, n int, opts provider.GetPullRequestOptions) (*types.PullRequest, error) {
	var raw apiPullRequest
	if err := p.client.Get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n), &raw); err != nil {
		return nil, err
	}
	out := raw.toType()

	if opts.WithCISummary {
		var status apiCommitStatus
		statusPath := fmt.Sprintf("/repos/%s/%s/commits/%s/status", owner, repo, raw.Head.Sha)
		if err := p.client.Get(ctx, statusPath, &status); err != nil {
			return nil, err
		}
		out.CISummary = status.toCISummary()
	}

	if opts.WithComments > 0 {
		// Forgejo treats PR top-level comments as issue comments — the
		// /pulls/{n}/comments endpoint is for inline review comments
		// and lands in #18.
		comments, err := p.fetchIssueComments(ctx, owner, repo, n, opts.WithComments)
		if err != nil {
			return nil, err
		}
		out.Comments = comments
	}

	return &out, nil
}
