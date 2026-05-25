package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Forgejo (inherited from Gitea) exposes issue-dependency edges as
// two symmetric resources rooted on each issue:
//
//   /repos/{o}/{r}/issues/{n}/dependencies — issues blocking n
//   /repos/{o}/{r}/issues/{n}/blocks       — issues n blocks
//
// "X blocks Y" and "Y depends on X" describe the same edge from
// different framings, so we only expose Add/Remove on the dependency
// direction; the CLI / MCP layers map both framings to the same call.
// See docs/provider-contract.md §11 + the gap issue #317.

// depsBody is the wire shape Forgejo accepts on
// POST/DELETE .../dependencies for same-repo edges. Cross-repo edges
// extend this with `owner` + `repo` fields; v1 ships same-repo only
// per the design call in the gap issue.
type depsBody struct {
	Index int `json:"index"`
}

// ListIssueDependencies returns issues blocking n.
func (p *Provider) ListIssueDependencies(ctx context.Context, owner, repo string, n int, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return p.listIssueEdges(ctx, owner, repo, n, "dependencies", opts)
}

// ListIssueBlocks returns issues that n blocks.
func (p *Provider) ListIssueBlocks(ctx context.Context, owner, repo string, n int, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return p.listIssueEdges(ctx, owner, repo, n, "blocks", opts)
}

// listIssueEdges is the shared GET path for both `/dependencies` and
// `/blocks`. Both endpoints return the same []Issue shape, so the
// only difference is the URL segment.
func (p *Provider) listIssueEdges(ctx context.Context, owner, repo string, n int, edge string, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/issues/%d/%s?%s", owner, repo, n, edge, q.Encode())
	var raw []apiIssue
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Issue, 0, len(raw))
	for i := range raw {
		item := raw[i].toType()
		item.Body = "" // trim on list — match ListIssues contract
		out = append(out, item)
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// AddIssueDependency makes issue `dep` a blocker of issue `n`.
// Returns the added blocker issue as Forgejo echoes it back.
func (p *Provider) AddIssueDependency(ctx context.Context, owner, repo string, n, dep int) (*types.Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies", owner, repo, n)
	var raw apiIssue
	if err := p.client.Post(ctx, path, depsBody{Index: dep}, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// RemoveIssueDependency removes the edge — `dep` no longer blocks
// `n`. Forgejo's DELETE on this endpoint requires a body identifying
// which dependency to remove; we use the same shape as POST.
//
// p.client.Delete is body-less; this method goes through the shared
// writeRequest helper directly so we can attach the body.
func (p *Provider) RemoveIssueDependency(ctx context.Context, owner, repo string, n, dep int) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies", owner, repo, n)
	return p.client.writeRequest(ctx, http.MethodDelete, path, depsBody{Index: dep}, nil)
}
