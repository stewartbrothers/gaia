package forgejo

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
// Declared here (rather than in cache_keys.go) so the milestone
// addition is one-file-touch on Forgejo.
const kindMilestone = "milestone"

// apiMilestone mirrors the Forgejo milestone record. Forgejo exposes
// `due_on` as RFC3339; both forges accept missing/null as "no due
// date". Pointer + omitempty handles both shapes.
type apiMilestone struct {
	ID           int64      `json:"id"`
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

func (a *apiMilestone) toType() types.Milestone {
	return types.Milestone{
		ID:           a.ID,
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

// ListMilestones returns milestones matching opts. Forgejo's
// `/repos/{o}/{r}/milestones` endpoint accepts `state` ("open" /
// "closed" / "all"), `name` (title-substring), and the standard
// `page` / `limit` pair.
func (p *Provider) ListMilestones(ctx context.Context, owner, repo string, opts provider.ListMilestonesOptions) ([]types.Milestone, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	state := opts.State
	if state == "" {
		state = "open"
	}
	q.Set("state", state)
	if opts.Name != "" {
		q.Set("name", opts.Name)
	}

	path := fmt.Sprintf("/repos/%s/%s/milestones?%s", owner, repo, q.Encode())
	var raw []apiMilestone
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Milestone, 0, len(raw))
	for i := range raw {
		// Trim description on list — agents fetch one for details.
		item := raw[i].toType()
		item.Description = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetMilestone fetches one milestone by ID.
//
// Routes through GetCached: a fresh cache row short-circuits the
// upstream call entirely; a stale row triggers a conditional GET
// with If-None-Match / If-Modified-Since.
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

// EditMilestone patches a milestone by ID. Forgejo's PATCH accepts
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

// DeleteMilestone removes a milestone by ID. 204 is success.
func (p *Provider) DeleteMilestone(ctx context.Context, owner, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/milestones/%d", owner, repo, id)
	if err := p.client.Delete(ctx, path); err != nil {
		return err
	}
	cache.NewInvalidator(p.client.cache).AfterDelete(ctx, kindMilestone, owner, repo, itoa64(id))
	return nil
}

// ListMilestoneIssues returns issues attached to a milestone. Forgejo
// uses `/repos/{o}/{r}/issues?milestones={id}` (the plural form).
// We thread the same `type=issues` filter ListIssues uses so PRs are
// excluded from the milestone scope.
func (p *Provider) ListMilestoneIssues(ctx context.Context, owner, repo string, id int64, opts provider.ListMilestoneIssuesOptions) ([]types.Issue, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("type", "issues")
	q.Set("milestones", strconv.FormatInt(id, 10))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
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
		item := raw[i].toType()
		item.Body = ""
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}
