package github

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
	State     string       `json:"state"`
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

// apiCheckRuns is the shape returned by /commits/{sha}/check-runs.
// Unlike Forgejo's /status (which has a unified state field), GitHub
// reports per-check status + conclusion separately and we roll up
// client-side.
type apiCheckRuns struct {
	TotalCount int           `json:"total_count"`
	CheckRuns  []apiCheckRun `json:"check_runs"`
}

type apiCheckRun struct {
	Name       string `json:"name"`       // human-readable check name
	Status     string `json:"status"`     // queued | in_progress | completed
	Conclusion string `json:"conclusion"` // success | failure | timed_out | ...
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
	if a.Head.Repo.FullName != "" && a.Head.Repo.FullName != a.Base.Repo.FullName {
		out.Head.Repo = a.Head.Repo.FullName
	}
	return out
}

// toCISummary rolls per-check-run records up. GitHub splits the
// "did this finish" axis (status) from the "did this pass" axis
// (conclusion). We map:
//   - conclusion=success → successful
//   - conclusion in {failure, timed_out, cancelled, action_required, stale} → failed
//   - status != completed → pending (regardless of conclusion)
//
// State is the worst-case roll-up: any failed → "failure"; else any
// pending → "pending"; else "success".
func (c *apiCheckRuns) toCISummary() *types.CISummary {
	out := &types.CISummary{Total: c.TotalCount}
	for _, r := range c.CheckRuns {
		var checkState string
		if r.Status != "completed" {
			out.Pending++
			checkState = "pending"
		} else {
			switch r.Conclusion {
			case "success":
				out.Successful++
				checkState = "success"
			case "failure", "timed_out", "cancelled", "action_required", "stale":
				out.Failed++
				checkState = r.Conclusion
			case "skipped", "neutral":
				out.Successful++ // treat skip/neutral as not-failing for the rollup
				checkState = r.Conclusion
			default:
				out.Pending++ // unknown/null
				checkState = "pending"
			}
		}
		if r.Name != "" {
			out.Checks = append(out.Checks, types.CheckItem{Name: r.Name, State: checkState})
		}
	}
	switch {
	case out.Failed > 0:
		out.State = "failure"
	case out.Pending > 0:
		out.State = "pending"
	default:
		out.State = "success"
	}
	return out
}

// ListPullRequests returns PRs matching opts.
func (p *Provider) ListPullRequests(ctx context.Context, owner, repo string, opts provider.ListPullRequestsOptions) ([]types.PullRequest, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.Head != "" {
		q.Set("head", opts.Head)
	}
	if opts.Base != "" {
		q.Set("base", opts.Base)
	}
	// Note: GitHub's /pulls endpoint does NOT accept a labels filter.
	// To filter by label, callers must use the issues endpoint with
	// the PR-marker filter inverted, or use Search. We silently drop
	// opts.Labels here; the gaia search command handles label-based
	// PR queries.
	_ = strings.Join // keep strings import live for the future label work

	path := fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, repo, q.Encode())
	var raw []apiPullRequest
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}

	out := make([]types.PullRequest, 0, len(raw))
	for i := range raw {
		item := raw[i].toType()
		item.Body = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetPullRequest returns a single PR. WithCISummary triggers an
// extra GET to /commits/{sha}/check-runs and rolls the result into
// types.CISummary. WithComments inlines top-level thread comments
// from the issue-comments endpoint (PRs share that endpoint with
// issues, same as Forgejo).
func (p *Provider) GetPullRequest(ctx context.Context, owner, repo string, n int, opts provider.GetPullRequestOptions) (*types.PullRequest, error) {
	var raw apiPullRequest
	prPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n)
	prKey := cacheKey(kindPR, owner, repo, itoa(n))
	if err := p.client.GetCached(ctx, prPath, &raw, prKey, CacheTTLSingle); err != nil {
		return nil, err
	}
	out := raw.toType()

	if opts.WithCISummary {
		var checks apiCheckRuns
		statusPath := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, raw.Head.Sha)
		if err := p.client.Get(ctx, statusPath, &checks); err != nil {
			return nil, err
		}
		out.CISummary = checks.toCISummary()
	}

	if opts.WithComments > 0 {
		comments, err := p.fetchIssueComments(ctx, owner, repo, n, opts.WithComments)
		if err != nil {
			return nil, err
		}
		out.Comments = comments
	}

	return &out, nil
}
