package aikestra

import (
	"context"
	"net/url"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Aikestra models dependencies as Issue relationships, read from the
// Issue's own side: `blocked_by` means the other Issue Blocks this one,
// `blocks` the reverse. gaia's dependency graph maps onto exactly those
// two; parent/child, releases_with and related_to are not dependencies.
const (
	relBlocks    = "blocks"
	relBlockedBy = "blocked_by"
)

// apiRelationship is Aikestra's Relationship schema. ID is null for the
// derived parent/child entries, which can't be deleted directly.
type apiRelationship struct {
	ID       *string `json:"id"`
	Relation string  `json:"relation"`
	Issue    int     `json:"issue"`
	Title    string  `json:"title"`
	State    string  `json:"state"`
}

func (a apiRelationship) toIssue() types.Issue {
	return types.Issue{Number: a.Issue, Title: a.Title, State: a.State}
}

// relationships walks every relationship of Issue n. The list is small
// (one Issue's links) and gaia filters it by relation, which would break
// the server's cursor, so it is read whole.
func (p *Provider) relationships(ctx context.Context, owner string, n int) ([]apiRelationship, error) {
	rels, _, err := walk[apiRelationship](ctx, p.client, issuePath(owner, n, "/relationships"), nil)
	return rels, err
}

func relatedIssues(rels []apiRelationship, relation string) []types.Issue {
	var out []types.Issue
	for _, r := range rels {
		if r.Relation == relation {
			out = append(out, r.toIssue())
		}
	}
	return out
}

func capIssues(issues []types.Issue, limit int) []types.Issue {
	if limit > 0 && len(issues) > limit {
		return issues[:limit]
	}
	return issues
}

// ListIssueDependencies returns the Issues blocking n (its `blocked_by`
// relationships).
func (p *Provider) ListIssueDependencies(ctx context.Context, owner, _ string, n int, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return p.listRelated(ctx, owner, n, relBlockedBy, opts.Limit)
}

// ListIssueBlocks returns the Issues n blocks (its `blocks`
// relationships).
func (p *Provider) ListIssueBlocks(ctx context.Context, owner, _ string, n int, opts provider.ListIssueDepsOptions) ([]types.Issue, *provider.Page, error) {
	return p.listRelated(ctx, owner, n, relBlocks, opts.Limit)
}

func (p *Provider) listRelated(ctx context.Context, owner string, n int, relation string, limit int) ([]types.Issue, *provider.Page, error) {
	rels, err := p.relationships(ctx, owner, n)
	if err != nil {
		return nil, nil, err
	}
	all := relatedIssues(rels, relation)
	out := capIssues(all, limit)
	return out, &provider.Page{Truncated: len(out) < len(all)}, nil
}

// AddIssueDependency makes dep block Issue n (a `blocked_by` relationship
// on n). Relationships stay inside one Project, so a dep in another
// owner is refused; a different repo in the same owner is the same
// Project and fine. The server answers 409 `cycle` for a loop.
func (p *Provider) AddIssueDependency(ctx context.Context, owner, _ string, n int, dep provider.IssueDepRef) (*types.Issue, error) {
	if err := sameProject(owner, dep); err != nil {
		return nil, err
	}
	body := struct {
		Relation string `json:"relation"`
		Issue    int    `json:"issue"`
	}{Relation: relBlockedBy, Issue: dep.Number}
	var raw apiRelationship
	if err := p.client.Post(ctx, issuePath(owner, n, "/relationships"), body, &raw); err != nil {
		return nil, err
	}
	out := raw.toIssue()
	return &out, nil
}

// RemoveIssueDependency removes the edge "dep blocks n". Aikestra deletes
// relationships by ID, so the edge is looked up first; a missing edge is
// NotFound.
func (p *Provider) RemoveIssueDependency(ctx context.Context, owner, _ string, n int, dep provider.IssueDepRef) error {
	if err := sameProject(owner, dep); err != nil {
		return err
	}
	rels, err := p.relationships(ctx, owner, n)
	if err != nil {
		return err
	}
	for _, r := range rels {
		if r.Relation == relBlockedBy && r.Issue == dep.Number && r.ID != nil {
			return p.client.Delete(ctx, issuePath(owner, n, "/relationships/"+url.PathEscape(*r.ID)))
		}
	}
	return exitcode.Errorf(exitcode.NotFound, "#%d does not block #%d", dep.Number, n)
}

func sameProject(owner string, dep provider.IssueDepRef) error {
	if dep.Owner != "" && dep.Owner != owner {
		return exitcode.Errorf(exitcode.NotImplemented,
			"Aikestra relationships stay within one Project: %s/%s#%d is outside %s",
			dep.Owner, dep.Repo, dep.Number, owner)
	}
	return nil
}
