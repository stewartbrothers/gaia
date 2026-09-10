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
// See docs/provider-contract.md §12 + the gap issue #317.

// depsBody is the wire shape Forgejo accepts on
// POST/DELETE .../dependencies. All three fields are always sent:
// Forgejo compares the body's owner/repo against the URL's repo and
// only takes the same-repo path when they match. Omitting them (the
// pre-#392 shape) made it resolve the repo ""/"" and 404 with
// IsErrRepoNotExist, so same-repo edges echo the host repo and
// cross-repo edges (#325) name the other one.
type depsBody struct {
	Index int    `json:"index"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
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

// AddIssueDependency makes `dep` a blocker of issue `n`. The body is
// always {index, owner, repo}: the host repo for a same-repo ref
// (#392), the ref's own repo for a cross-repo one (#325).
//
// Returns the added blocker issue as Forgejo echoes it back.
func (p *Provider) AddIssueDependency(ctx context.Context, owner, repo string, n int, dep provider.IssueDepRef) (*types.Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies", owner, repo, n)
	var raw apiIssue
	if err := p.client.Post(ctx, path, depBodyFromRef(owner, repo, dep), &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// RemoveIssueDependency removes the edge — `dep` no longer blocks
// `n`. Forgejo's DELETE on this endpoint requires a body identifying
// which dependency to remove; we use the same shape as POST and the
// same cross-repo semantics.
//
// p.client.Delete is body-less; this method goes through the shared
// writeRequest helper directly so we can attach the body.
func (p *Provider) RemoveIssueDependency(ctx context.Context, owner, repo string, n int, dep provider.IssueDepRef) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies", owner, repo, n)
	return p.client.writeRequest(ctx, http.MethodDelete, path, depBodyFromRef(owner, repo, dep), nil)
}

// depBodyFromRef translates the Provider's IssueDepRef into Forgejo's
// wire body, defaulting an unqualified (same-repo) ref to the host
// issue's own owner/repo. Forgejo requires the identity even for
// same-repo edges — see depsBody and #392.
func depBodyFromRef(hostOwner, hostRepo string, dep provider.IssueDepRef) depsBody {
	if dep.SameRepo() {
		return depsBody{Index: dep.Number, Owner: hostOwner, Repo: hostRepo}
	}
	return depsBody{Index: dep.Number, Owner: dep.Owner, Repo: dep.Repo}
}
