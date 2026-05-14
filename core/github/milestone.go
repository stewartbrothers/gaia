package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// kindMilestone is the cache "kind" label for milestone reads.
const kindMilestone = "milestone"

// apiMilestone mirrors GitHub's milestone record. The shape is
// effectively identical to Forgejo's (number/title/state/due_on/
// open_issues/closed_issues/created_at/updated_at/closed_at),
// modulo GitHub keying lookups by milestone `number` rather than
// `id`. We expose `number` as ID at the type boundary so the
// Provider contract stays uniform with Forgejo.
type apiMilestone struct {
	ID           int64      `json:"id"`
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        string     `json:"state"`
	OpenIssues   int        `json:"open_issues"`
	ClosedIssues int        `json:"closed_issues"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	DueOn        *time.Time `json:"due_on,omitempty"`
}

// toType maps GitHub's milestone to the trimmed types.Milestone.
// GitHub's PATCH/DELETE endpoints take the milestone `number` (not
// the database `id`), so we surface `Number` as `ID` on the type —
// callers always pass back the value they got, no matter which
// forge is underneath.
func (a *apiMilestone) toType() types.Milestone {
	return types.Milestone{
		ID:           int64(a.Number),
		Title:        a.Title,
		Description:  a.Description,
		State:        a.State,
		OpenIssues:   a.OpenIssues,
		ClosedIssues: a.ClosedIssues,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
		ClosedAt:     a.ClosedAt,
		DueOn:        a.DueOn,
	}
}

// ListMilestones returns milestones matching opts. GitHub's
// `/repos/{o}/{r}/milestones` accepts `state` (open/closed/all),
// `sort`, `direction`, and the standard `page` / `per_page` pair.
// GitHub doesn't support the Forgejo-style `name` filter (the option
// is silently ignored for parity with Forgejo's interface; client-side
// filtering can be added in the CLI layer if needed).
func (p *Provider) ListMilestones(ctx context.Context, owner, repo string, opts provider.ListMilestonesOptions) ([]types.Milestone, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	state := opts.State
	if state == "" {
		state = "open"
	}
	q.Set("state", state)

	path := fmt.Sprintf("/repos/%s/%s/milestones?%s", owner, repo, q.Encode())
	var raw []apiMilestone
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Milestone, 0, len(raw))
	for i := range raw {
		item := raw[i].toType()
		item.Description = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetMilestone fetches one milestone by number.
func (p *Provider) GetMilestone(ctx context.Context, owner, repo string, id int64) (*types.Milestone, error) {
	var raw apiMilestone
	path := fmt.Sprintf("/repos/%s/%s/milestones/%d", owner, repo, id)
	key := cacheKey(kindMilestone, owner, repo, itoa64(id))
	if err := p.client.GetCached(ctx, path, &raw, key, CacheTTLSingle); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// CreateMilestone creates a new milestone.
func (p *Provider) CreateMilestone(ctx context.Context, owner, repo string, opts provider.CreateMilestoneOptions) (*types.Milestone, error) {
	var raw apiMilestone
	path := fmt.Sprintf("/repos/%s/%s/milestones", owner, repo)
	if err := p.client.Post(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindMilestone, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditMilestone patches a milestone by number. GitHub's PATCH accepts
// title, description, due_on, state ("open"/"closed").
func (p *Provider) EditMilestone(ctx context.Context, owner, repo string, id int64, opts provider.EditMilestoneOptions) (*types.Milestone, error) {
	var raw apiMilestone
	path := fmt.Sprintf("/repos/%s/%s/milestones/%d", owner, repo, id)
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindMilestone, owner, repo, itoa64(id))
	out := raw.toType()
	return &out, nil
}

// DeleteMilestone removes a milestone by number. 204 is success.
func (p *Provider) DeleteMilestone(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/milestones/%d", owner, repo, id)
	if err := p.client.Delete(ctx, path); err != nil {
		return err
	}
	cache.NewInvalidator(p.client.cache).AfterDelete(ctx, kindMilestone, owner, repo, itoa64(id))
	return nil
}

// ListMilestoneIssues returns issues attached to a milestone. GitHub
// uses `/repos/{o}/{r}/issues?milestone={number}` (singular form,
// vs. Forgejo's plural `milestones=`). PRs are filtered client-side
// the same way ListIssues does.
func (p *Provider) ListMilestoneIssues(ctx context.Context, owner, repo string, id int64, opts provider.ListMilestoneIssuesOptions) ([]types.Issue, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	q.Set("milestone", strconv.FormatInt(id, 10))
	if opts.State != "" {
		q.Set("state", opts.State)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues?%s", owner, repo, q.Encode())
	var raw []apiIssue
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Issue, 0, len(raw))
	for i := range raw {
		if raw[i].PullRequest != nil {
			continue
		}
		item := raw[i].toType()
		item.Body = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}
