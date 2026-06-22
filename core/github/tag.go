// Package github: tag list + create + delete.
//
// GitHub has no "create tag" endpoint that takes a source ref — a
// lightweight tag is just a ref. CreateTag therefore reuses the exact
// resolution CreateBranch does, then writes a `refs/tags/<name>` ref:
//
//  1. If opts.From is empty, GET /repos/{o}/{r} for the default branch.
//  2. Resolve the source ref to a commit SHA via
//     GET /repos/{o}/{r}/commits/{ref} (works for a branch, tag, or SHA).
//  3. POST /repos/{o}/{r}/git/refs with {ref: "refs/tags/<name>", sha}.
//
// Forgejo does all of this in one POST; the trim boundary papers over
// the difference. Deletion is the inverse: DELETE on the ref path.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiTag mirrors the binding fields of GitHub's tag-list object. On the
// tags list the target commit nests under `commit.sha`. GitHub's list
// endpoint does NOT return the annotated-tag message, so Message stays
// empty for listed tags (matching the trimmed shape — callers wanting
// the message read the tag object directly).
type apiTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func (a *apiTag) toType() types.Tag {
	return types.Tag{
		Name:   a.Name,
		Commit: a.Commit.SHA,
	}
}

// ListTags returns the repo's tags. GitHub's `/repos/{o}/{r}/tags` takes
// the standard `page` / `per_page` pair.
func (p *Provider) ListTags(ctx context.Context, owner, repo string, opts provider.ListTagsOptions) ([]types.Tag, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
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

// CreateTag creates `name` from opts.From (default branch when empty).
// The resolution is identical to CreateBranch's — resolve the source ref
// to a SHA, then write a lightweight tag ref.
func (p *Provider) CreateTag(ctx context.Context, owner, repo, name string, opts provider.CreateTagOptions) (*types.Tag, error) {
	from := opts.From
	if from == "" {
		var r struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := p.client.Get(ctx, fmt.Sprintf("/repos/%s/%s", owner, repo), &r); err != nil {
			return nil, err
		}
		from = r.DefaultBranch
	}

	// Resolve the source ref to a commit SHA. The commits endpoint
	// accepts a branch, tag, or SHA and returns the resolved commit.
	var c struct {
		SHA string `json:"sha"`
	}
	if err := p.client.Get(ctx, fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, url.PathEscape(from)), &c); err != nil {
		return nil, err
	}

	var created struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	body := refCreatePayload{Ref: "refs/tags/" + name, SHA: c.SHA}
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), body, &created); err != nil {
		return nil, err
	}
	return &types.Tag{Name: name, Commit: created.Object.SHA}, nil
}

// DeleteTag removes the lightweight tag ref. GitHub deletes a tag by its
// ref path: DELETE /repos/{o}/{r}/git/refs/tags/{tag}. 204 is success.
func (p *Provider) DeleteTag(ctx context.Context, owner, repo, name string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs/tags/%s", owner, repo, url.PathEscape(name))
	return p.client.Delete(ctx, path)
}
