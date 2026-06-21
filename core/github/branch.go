// Package github: branch list + create.
//
// GitHub has no single "create branch" endpoint — a branch is just a
// ref. CreateBranch therefore does a three-step dance that gaia hides
// behind the uniform Provider method:
//
//  1. If opts.From is empty, GET /repos/{o}/{r} for the default branch.
//  2. Resolve the source ref to a commit SHA via
//     GET /repos/{o}/{r}/commits/{ref} (works for a branch, tag, or SHA).
//  3. POST /repos/{o}/{r}/git/refs with {ref: "refs/heads/<name>", sha}.
//
// Forgejo does all of this in one POST; the trim boundary papers over
// the difference.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiBranch mirrors the binding fields of GitHub's branch object. On the
// branches list the tip commit nests under `commit.sha`.
type apiBranch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
	Protected bool `json:"protected"`
}

func (a *apiBranch) toType() types.Branch {
	return types.Branch{
		Name:      a.Name,
		Commit:    a.Commit.SHA,
		Protected: a.Protected,
	}
}

// ListBranches returns the repo's branches. GitHub's
// `/repos/{o}/{r}/branches` takes the standard `page` / `per_page` pair.
func (p *Provider) ListBranches(ctx context.Context, owner, repo string, opts provider.ListBranchesOptions) ([]types.Branch, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/branches?%s", owner, repo, q.Encode())
	var raw []apiBranch
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Branch, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// refCreatePayload is GitHub's POST /git/refs body.
type refCreatePayload struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// CreateBranch creates `name` from opts.From (default branch when empty).
func (p *Provider) CreateBranch(ctx context.Context, owner, repo, name string, opts provider.CreateBranchOptions) (*types.Branch, error) {
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
	body := refCreatePayload{Ref: "refs/heads/" + name, SHA: c.SHA}
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), body, &created); err != nil {
		return nil, err
	}
	return &types.Branch{Name: name, Commit: created.Object.SHA}, nil
}
