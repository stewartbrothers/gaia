package forgejo

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

// apiIssue mirrors the subset of Forgejo's issue payload we read.
// Fields not listed here are dropped at decode time, which is exactly
// the trim-at-the-boundary contract.
type apiIssue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	User      apiUser       `json:"user"`
	Labels    []apiLabel    `json:"labels"`
	Assignees []apiUser     `json:"assignees"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	ClosedAt  *time.Time    `json:"closed_at"`
	HTMLURL   string        `json:"html_url"`
	Milestone *apiMilestone `json:"milestone,omitempty"`
}

type apiUser struct {
	Login string `json:"login"`
}

type apiLabel struct {
	Name string `json:"name"`
}

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

func (a *apiComment) toType(source string) types.Comment {
	return types.Comment{
		ID:        a.ID,
		Source:    source,
		Author:    types.User{Login: a.User.Login},
		Body:      a.Body,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// ListIssues returns issues matching opts. Forgejo's `/issues`
// endpoint also returns PRs by default; we set `type=issues` so
// callers don't have to filter client-side.
func (p *Provider) ListIssues(ctx context.Context, owner, repo string, opts provider.ListIssuesOptions) ([]types.Issue, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("type", "issues")
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if len(opts.Labels) > 0 {
		q.Set("labels", strings.Join(opts.Labels, ","))
	}
	if opts.Assignee != "" {
		q.Set("assigned_by", opts.Assignee)
	}
	if opts.Author != "" {
		q.Set("created_by", opts.Author)
	}
	if !opts.Since.IsZero() {
		q.Set("since", opts.Since.UTC().Format(time.RFC3339))
	}
	if opts.Query != "" {
		q.Set("q", opts.Query)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues?%s", owner, repo, q.Encode())
	var raw []apiIssue
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}

	out := make([]types.Issue, 0, len(raw))
	for i := range raw {
		item := raw[i].toType()
		item.Body = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetIssue returns a single issue. If opts.WithComments > 0 the most
// recent N comments are fetched and inlined.
//
// Routes through GetCached: a fresh cache row short-circuits the
// upstream call entirely; a stale row triggers a conditional GET
// with If-None-Match / If-Modified-Since (#42).
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
	if opts.WithBlockers > 0 {
		blockers, _, err := p.ListIssueDependencies(ctx, owner, repo, n, provider.ListIssueDepsOptions{Limit: opts.WithBlockers})
		if err != nil {
			return nil, err
		}
		out.Blockers = blockers
	}
	if opts.WithBlocks > 0 {
		blocks, _, err := p.ListIssueBlocks(ctx, owner, repo, n, provider.ListIssueDepsOptions{Limit: opts.WithBlocks})
		if err != nil {
			return nil, err
		}
		out.Blocks = blocks
	}
	return &out, nil
}

func (p *Provider) fetchIssueComments(ctx context.Context, owner, repo string, n, limit int) ([]types.Comment, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(clampLimit(limit)))
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?%s", owner, repo, n, q.Encode())

	var raw []apiComment
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]types.Comment, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType("issue"))
	}
	return out, nil
}

// clampLimit applies the documented default and cap from the envelope
// package. Zero or negative → DefaultLimit; over-cap → MaxLimit.
func clampLimit(n int) int {
	if n <= 0 {
		return envelope.DefaultLimit
	}
	if n > envelope.MaxLimit {
		return envelope.MaxLimit
	}
	return n
}

// pageFromCursor parses an opaque page cursor; empty → page 1.
func pageFromCursor(cursor string) string {
	if cursor == "" {
		return "1"
	}
	return cursor
}

// makePage returns a Page that signals truncation when the response
// filled the requested limit. Forgejo also supports detecting
// next-page via the Link header; the heuristic-on-length is good
// enough for Phase 1 and avoids a full RFC-5988 parser.
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
