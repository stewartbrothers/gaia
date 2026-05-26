package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// GitHub REST issue-dependency endpoints (API version 2026-03-10):
//
//   GET    /repos/{o}/{r}/issues/{n}/dependencies/blocked_by
//   POST   /repos/{o}/{r}/issues/{n}/dependencies/blocked_by         body: {"issue_id": <int>}
//   DELETE /repos/{o}/{r}/issues/{n}/dependencies/blocked_by/{id}
//   GET    /repos/{o}/{r}/issues/{n}/dependencies/blocking
//
// Two differences from Forgejo worth flagging:
//
//   1. The POST body and DELETE path take `issue_id` — the issue's
//      INTERNAL stable primary key, not the user-facing `number`.
//      The Provider contract takes a `dep int` parameter callers
//      think of as a number; this implementation resolves number → id
//      via an extra GET /issues/{dep} before each write op.
//
//   2. DELETE has no body (id in path). Forgejo puts it in a body.
//      Wrapper hides the difference at the Provider boundary.
//
// Closes #326 (the GitHub side of the issue-dependency surface
// originally tracked in #317).

// depAddBody is the wire shape GitHub accepts on POST
// .../dependencies/blocked_by. issue_id is the BLOCKER's internal ID
// (not its user-facing number).
type depAddBody struct {
	IssueID int64 `json:"issue_id"`
}

// apiIssueIDOnly is a minimal local struct for the number→ID
// resolution path. We don't add `ID` to the canonical apiIssue
// struct to keep that struct's blast radius minimal — only this
// file needs the field.
type apiIssueIDOnly struct {
	ID int64 `json:"id"`
}

// ListIssueDependencies returns issues blocking n.
func (p *Provider) ListIssueDependencies(ctx context.Context, owner, repo string, n int, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return p.listIssueEdges(ctx, owner, repo, n, "blocked_by", opts)
}

// ListIssueBlocks returns issues that n blocks.
func (p *Provider) ListIssueBlocks(ctx context.Context, owner, repo string, n int, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return p.listIssueEdges(ctx, owner, repo, n, "blocking", opts)
}

func (p *Provider) listIssueEdges(ctx context.Context, owner, repo string, n int, edge string, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies/%s?%s", owner, repo, n, edge, q.Encode())
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

// AddIssueDependency makes issue `dep` a blocker of issue `n`. GitHub
// requires `issue_id` (internal stable primary key) in the body, so
// we resolve `dep` (issue number) → id first via a GET /issues/{dep}
// — one extra round-trip per add op. The resolve must succeed
// before the POST runs; failures bubble up unchanged so the caller
// sees the resolve's NotFound rather than a confusing post-error.
func (p *Provider) AddIssueDependency(ctx context.Context, owner, repo string, n, dep int) (*types.Issue, error) {
	depID, err := p.resolveIssueID(ctx, owner, repo, dep)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies/blocked_by", owner, repo, n)
	var raw apiIssue
	if err := p.client.Post(ctx, path, depAddBody{IssueID: depID}, &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// RemoveIssueDependency removes the edge. GitHub's DELETE takes the
// blocker's ID in the URL path (no body), so we resolve number → id
// the same way AddIssueDependency does.
func (p *Provider) RemoveIssueDependency(ctx context.Context, owner, repo string, n, dep int) error {
	depID, err := p.resolveIssueID(ctx, owner, repo, dep)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/dependencies/blocked_by/%d", owner, repo, n, depID)
	return p.client.Delete(ctx, path)
}

// resolveIssueID fetches an issue by its user-facing number and
// returns its internal `id` primary key. GitHub's dependency write
// endpoints key by `id`, not `number`; this is the bridge between
// our Provider contract (which talks numbers, Forgejo's framing) and
// GitHub's wire requirement.
func (p *Provider) resolveIssueID(ctx context.Context, owner, repo string, number int) (int64, error) {
	if number <= 0 {
		return 0, exitcode.Errorf(exitcode.Usage,
			"issue number must be positive; got %d", number)
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	var raw apiIssueIDOnly
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return 0, err
	}
	if raw.ID == 0 {
		// Defensive — GitHub always returns a non-zero id on a real
		// issue, but if the response shape ever changes (or a fake
		// fixture omits it), the caller should see a clear error
		// rather than a 422 from a {"issue_id": 0} POST.
		return 0, exitcode.Errorf(exitcode.Generic,
			"github returned issue #%d without an id field — cannot map to dependency body", number)
	}
	return raw.ID, nil
}
