package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiBranch mirrors the binding fields of Forgejo's branch object. The
// tip commit nests under `commit.id`; the forge carries far more (commit
// author, verification, timestamps) that gaia trims away.
type apiBranch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
	Protected bool `json:"protected"`
}

func (a *apiBranch) toType() types.Branch {
	return types.Branch{
		Name:      a.Name,
		Commit:    a.Commit.ID,
		Protected: a.Protected,
	}
}

// ListBranches returns the repo's branches. Forgejo's
// `/repos/{o}/{r}/branches` takes the standard `page` / `limit` pair.
func (p *Provider) ListBranches(ctx context.Context, owner, repo string, opts provider.ListBranchesOptions) ([]types.Branch, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
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

// branchCreatePayload is Forgejo's POST body. old_ref_name is optional —
// when omitted, Forgejo branches from the repo's default branch.
type branchCreatePayload struct {
	NewBranchName string `json:"new_branch_name"`
	OldRefName    string `json:"old_ref_name,omitempty"`
}

// CreateBranch creates `name` from opts.From via a single POST. Forgejo
// resolves old_ref_name (branch or tag) server-side; an empty From
// defaults to the repo's default branch.
func (p *Provider) CreateBranch(ctx context.Context, owner, repo, name string, opts provider.CreateBranchOptions) (*types.Branch, error) {
	body := branchCreatePayload{NewBranchName: name, OldRefName: opts.From}
	var raw apiBranch
	path := fmt.Sprintf("/repos/%s/%s/branches", owner, repo)
	if err := p.client.Post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}
