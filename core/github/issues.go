package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiIssue mirrors GitHub's issue payload. Note: GitHub returns BOTH
// issues and PRs from /repos/{o}/{r}/issues; the `pull_request` field
// is non-null for PRs. We filter to issues client-side (GitHub does
// not have a server-side type filter analogous to Forgejo's).
type apiIssue struct {
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	State       string        `json:"state"`
	User        apiUser       `json:"user"`
	Labels      []apiLabel    `json:"labels"`
	Assignees   []apiUser     `json:"assignees"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	ClosedAt    *time.Time    `json:"closed_at,omitempty"`
	HTMLURL     string        `json:"html_url"`
	PullRequest *apiPRMarker  `json:"pull_request,omitempty"`
	Milestone   *apiMilestone `json:"milestone,omitempty"`
}

// apiUser is just login.
type apiUser struct {
	Login string `json:"login"`
}

// apiLabel mirrors the subset we read from issue+PR payloads. The
// labels endpoint (#g4-equivalent in Phase 2) will return the full
// shape with id/color/description.
type apiLabel struct {
	Name string `json:"name"`
}

// apiPRMarker is the empty marker that distinguishes PR-as-issue
// records from real issues.
type apiPRMarker struct{}

// apiComment is the GitHub issue-comment shape.
type apiComment struct {
	ID        int64     `json:"id"`
	User      apiUser   `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *apiIssue) toType() types.Issue {
	out := types.Issue{
		Number:    a.Number,
		Title:     a.Title,
		Body:      a.Body,
		State:     a.State,
		Author:    types.User{Login: a.User.Login},
		HTMLURL:   a.HTMLURL,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
		ClosedAt:  a.ClosedAt,
	}
	for _, l := range a.Labels {
		out.Labels = append(out.Labels, types.Label{Name: l.Name})
	}
	for _, u := range a.Assignees {
		out.Assignees = append(out.Assignees, types.User{Login: u.Login})
	}
	if a.Milestone != nil {
		m := a.Milestone.toType()
		out.Milestone = &m
	}
	return out
}

func (a *apiComment) toType() types.Comment {
	return types.Comment{
		ID:        a.ID,
		Source:    "issue",
		Author:    types.User{Login: a.User.Login},
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// ListIssues returns issues (not PRs) matching opts. GitHub's
// `/repos/{o}/{r}/issues` endpoint returns BOTH; we filter PRs out
// client-side using the `pull_request` field.
func (p *Provider) ListIssues(ctx context.Context, owner, repo string, opts provider.ListIssuesOptions) ([]types.Issue, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if len(opts.Labels) > 0 {
		q.Set("labels", strings.Join(opts.Labels, ","))
	}
	if opts.Assignee != "" {
		q.Set("assignee", opts.Assignee)
	}
	if opts.Author != "" {
		q.Set("creator", opts.Author)
	}
	if !opts.Since.IsZero() {
		q.Set("since", opts.Since.UTC().Format(time.RFC3339))
	}

	path := fmt.Sprintf("/repos/%s/%s/issues?%s", owner, repo, q.Encode())
	var raw []apiIssue
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}

	out := make([]types.Issue, 0, len(raw))
	for i := range raw {
		if raw[i].PullRequest != nil {
			continue // skip PRs
		}
		item := raw[i].toType()
		item.Body = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetIssue returns a single issue. WithComments inlines that many
// recent comments (mirrors Forgejo's behavior).
//
// Routes through GetCached: a fresh row short-circuits the upstream
// call; a stale row triggers a conditional GET against GitHub's
// strong ETag (#42).
func (p *Provider) GetIssue(ctx context.Context, owner, repo string, n int, opts provider.GetIssueOptions) (*types.Issue, error) {
	var raw apiIssue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, n)
	key := cacheKey(kindIssue, owner, repo, itoa(n))
	if err := p.client.GetCached(ctx, path, &raw, key, CacheTTLSingle); err != nil {
		return nil, err
	}
	out := raw.toType()
	if opts.WithComments > 0 {
		comments, err := p.fetchIssueComments(ctx, owner, repo, n, opts.WithComments)
		if err != nil {
			return nil, err
		}
		out.Comments = comments
	}
	return &out, nil
}

func (p *Provider) fetchIssueComments(ctx context.Context, owner, repo string, n, limit int) ([]types.Comment, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(clampLimit(limit)))
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?%s", owner, repo, n, q.Encode())
	var raw []apiComment
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]types.Comment, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, nil
}

// clampLimit applies the documented default and cap from the envelope
// package. Same helper structure as Forgejo's.
func clampLimit(n int) int {
	if n <= 0 {
		return envelope.DefaultLimit
	}
	if n > envelope.MaxLimit {
		return envelope.MaxLimit
	}
	return n
}

// pageFromCursor parses an opaque page cursor.
func pageFromCursor(cursor string) string {
	if cursor == "" {
		return "1"
	}
	return cursor
}

// makePage signals truncation when the response filled the requested
// limit (heuristic; same as Forgejo's makePage).
func makePage(returned, limit int, cursor string) *provider.Page {
	if returned < limit {
		return &provider.Page{}
	}
	cur := pageFromCursor(cursor)
	curN, _ := strconv.Atoi(cur)
	if curN < 1 {
		curN = 1
	}
	return &provider.Page{
		Truncated:  true,
		NextCursor: strconv.Itoa(curN + 1),
	}
}
