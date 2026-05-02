package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiRelease mirrors the Forgejo release record. Forgejo and GitHub
// have nearly identical release shapes, but the time fields are at
// the same JSON keys (created_at, published_at) so a shared type
// would work — keeping them per-provider for symmetry with the rest
// of the package.
type apiRelease struct {
	ID              int64      `json:"id"`
	TagName         string     `json:"tag_name"`
	Name            string     `json:"name"`
	Body            string     `json:"body"`
	Draft           bool       `json:"draft"`
	Prerelease      bool       `json:"prerelease"`
	Author          apiUser    `json:"author"`
	TargetCommitish string     `json:"target_commitish"`
	CreatedAt       time.Time  `json:"created_at"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

func (a *apiRelease) toType() types.Release {
	return types.Release{
		ID:              a.ID,
		TagName:         a.TagName,
		Name:            a.Name,
		Body:            a.Body,
		Draft:           a.Draft,
		Prerelease:      a.Prerelease,
		Author:          types.User{Login: a.Author.Login},
		TargetCommitish: a.TargetCommitish,
		CreatedAt:       a.CreatedAt,
		PublishedAt:     a.PublishedAt,
	}
}

// ListReleases returns releases newest-first (Forgejo's default order).
func (p *Provider) ListReleases(ctx context.Context, owner, repo string, opts provider.ListReleasesOptions) ([]types.Release, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/releases?%s", owner, repo, q.Encode())
	var raw []apiRelease
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Release, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetRelease fetches one release by tag.
func (p *Provider) GetRelease(ctx context.Context, owner, repo, tag string) (*types.Release, error) {
	var raw apiRelease
	path := fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, url.PathEscape(tag))
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// CreateRelease creates a new release.
func (p *Provider) CreateRelease(ctx context.Context, owner, repo string, opts provider.CreateReleaseOptions) (*types.Release, error) {
	var raw apiRelease
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/releases", owner, repo), opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// EditRelease patches a release identified by tag. Forgejo's PATCH
// takes the release ID, so we look up by tag first then PATCH by ID
// — same pattern as labels-by-name.
func (p *Provider) EditRelease(ctx context.Context, owner, repo, tag string, opts provider.EditReleaseOptions) (*types.Release, error) {
	current, err := p.GetRelease(ctx, owner, repo, tag)
	if err != nil {
		return nil, err
	}
	var raw apiRelease
	path := fmt.Sprintf("/repos/%s/%s/releases/%d", owner, repo, current.ID)
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteRelease removes a release by tag (looks up ID first).
func (p *Provider) DeleteRelease(ctx context.Context, owner, repo, tag string) error {
	current, err := p.GetRelease(ctx, owner, repo, tag)
	if err != nil {
		return err
	}
	return p.client.Delete(ctx, fmt.Sprintf("/repos/%s/%s/releases/%d", owner, repo, current.ID))
}
