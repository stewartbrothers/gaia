package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiTag mirrors the binding fields of Forgejo's tag object. The target
// commit nests under `commit.sha`; annotated tags carry `message`. The
// forge carries far more (the tagger, the zipball/tarball URLs, the
// nested commit object) that gaia trims away.
type apiTag struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Commit  struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func (a *apiTag) toType() types.Tag {
	return types.Tag{
		Name:    a.Name,
		Commit:  a.Commit.SHA,
		Message: a.Message,
	}
}

// ListTags returns the repo's tags. Forgejo's `/repos/{o}/{r}/tags`
// takes the standard `page` / `limit` pair.
func (p *Provider) ListTags(ctx context.Context, owner, repo string, opts provider.ListTagsOptions) ([]types.Tag, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/tags?%s", owner, repo, q.Encode())
	var raw []apiTag
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Tag, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// tagCreatePayload is Forgejo's POST body. target is optional — when
// omitted, Forgejo tags the repo's default branch.
type tagCreatePayload struct {
	TagName string `json:"tag_name"`
	Target  string `json:"target,omitempty"`
}

// CreateTag creates `name` from opts.From via a single POST. Forgejo
// resolves target (branch, tag, or commit) server-side; an empty From
// defaults to the repo's default branch.
func (p *Provider) CreateTag(ctx context.Context, owner, repo, name string, opts provider.CreateTagOptions) (*types.Tag, error) {
	body := tagCreatePayload{TagName: name, Target: opts.From}
	var raw apiTag
	path := fmt.Sprintf("/repos/%s/%s/tags", owner, repo)
	if err := p.client.Post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// DeleteTag removes tag `name`. Forgejo returns 204 on success.
func (p *Provider) DeleteTag(ctx context.Context, owner, repo, name string) error {
	path := fmt.Sprintf("/repos/%s/%s/tags/%s", owner, repo, url.PathEscape(name))
	return p.client.Delete(ctx, path)
}
