package aikestra

import (
	"context"
	"net/url"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiRef is Aikestra's Ref schema: branches and tags share one list,
// told apart by kind ("branch" or "tag").
type apiRef struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
}

func repoPath(owner, repo, suffix string) string {
	return projectPath(owner, "/repos/%s%s", url.PathEscape(repo), suffix)
}

// refs walks the Repo's refs and keeps one kind. Filtering client-side
// breaks the server cursor, so the list is read whole; truncated is set
// when the server stopped at its hard limit or the result was capped.
func (p *Provider) refs(ctx context.Context, owner, repo, kind string, limit int) ([]apiRef, *provider.Page, error) {
	all, truncated, err := walk[apiRef](ctx, p.client, repoPath(owner, repo, "/refs"), nil)
	if err != nil {
		return nil, nil, err
	}
	var out []apiRef
	for _, r := range all {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	if limit > 0 && len(out) > limit {
		out, truncated = out[:limit], true
	}
	return out, &provider.Page{Truncated: truncated}, nil
}

// ListBranches lists the Repo's branches.
func (p *Provider) ListBranches(ctx context.Context, owner, repo string, opts provider.ListBranchesOptions) ([]types.Branch, *provider.Page, error) {
	refs, page, err := p.refs(ctx, owner, repo, "branch", opts.Limit)
	if err != nil {
		return nil, nil, err
	}
	out := make([]types.Branch, len(refs))
	for i, r := range refs {
		out[i] = types.Branch{Name: r.Name, Commit: r.Commit}
	}
	return out, page, nil
}

// ListTags lists the Repo's tags.
func (p *Provider) ListTags(ctx context.Context, owner, repo string, opts provider.ListTagsOptions) ([]types.Tag, *provider.Page, error) {
	refs, page, err := p.refs(ctx, owner, repo, "tag", opts.Limit)
	if err != nil {
		return nil, nil, err
	}
	out := make([]types.Tag, len(refs))
	for i, r := range refs {
		out[i] = types.Tag{Name: r.Name, Commit: r.Commit}
	}
	return out, page, nil
}

// CreateBranch is not in Aikestra API v1: refs are created by git push.
func (p *Provider) CreateBranch(_ context.Context, _, _, _ string, _ provider.CreateBranchOptions) (*types.Branch, error) {
	return nil, notImplemented("creating branches (push the branch with git instead)")
}

// CreateTag is not in Aikestra API v1: refs are created by git push.
func (p *Provider) CreateTag(_ context.Context, _, _, _ string, _ provider.CreateTagOptions) (*types.Tag, error) {
	return nil, notImplemented("creating tags (push the tag with git instead)")
}

// DeleteTag is not in Aikestra API v1.
func (p *Provider) DeleteTag(_ context.Context, _, _, _ string) error {
	return notImplemented("deleting tags (delete the tag with git push instead)")
}
