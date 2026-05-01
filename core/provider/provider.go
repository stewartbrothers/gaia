// Package provider defines the contract every gaia backend implements.
// `cmd/gaia` and `cmd/gaia-mcp` both depend only on this interface and on
// `core/types`; per-forge code (Forgejo, GitHub) lives behind it as
// implementations chosen at runtime by config or git-remote auto-detect.
//
// Methods take owner+repo as parameters rather than baking them into the
// Provider value, so a single Provider can serve cross-repo flows
// (notably Search) without holding repo state.
package provider

import (
	"context"

	"github.com/stewartbrothers/gaia/core/types"
)

// Provider is the unified API surface the CLI and MCP server both call.
// Implementations are responsible for translating their forge's REST
// shape into the trimmed core/types values, and for reconciling
// multi-endpoint reads (PR + checks, three comment endpoints, etc.) into
// a single return.
type Provider interface {
	// Whoami returns the login of the authenticated user. Used by
	// `gaia whoami` to verify the token works.
	Whoami(ctx context.Context) (string, error)

	// ListIssues returns issues matching opts. The returned Page
	// indicates whether the result was truncated and supplies the
	// cursor for the next call.
	ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOptions) ([]types.Issue, *Page, error)

	// GetIssue returns a single issue. If opts.WithComments > 0 the
	// most recent N comments are fetched and inlined.
	GetIssue(ctx context.Context, owner, repo string, n int, opts GetIssueOptions) (*types.Issue, error)

	// ListPullRequests returns PRs matching opts.
	ListPullRequests(ctx context.Context, owner, repo string, opts ListPullRequestsOptions) ([]types.PullRequest, *Page, error)

	// GetPullRequest returns a single PR with its CI summary
	// reconciled. If opts.WithComments > 0 the most recent N comments
	// are inlined.
	GetPullRequest(ctx context.Context, owner, repo string, n int, opts GetPullRequestOptions) (*types.PullRequest, error)

	// GetPullRequestDiff returns the structured diff for a PR. Binary
	// files are marshaled with Binary=true and no Hunks.
	GetPullRequestDiff(ctx context.Context, owner, repo string, n int, opts GetPullRequestDiffOptions) ([]types.DiffFile, error)

	// ListComments returns the unified time-ordered comment stream for
	// an issue or PR, drawing from issue comments, PR review comments,
	// and inline review comments as appropriate. Each Comment carries
	// a Source discriminator.
	ListComments(ctx context.Context, owner, repo string, n int, opts ListCommentsOptions) ([]types.Comment, error)

	// Search returns hits across the kinds named in opts.Kinds (issues
	// and PRs in Phase 1; code added in Phase 4).
	Search(ctx context.Context, query string, opts SearchOptions) ([]types.SearchResult, *Page, error)
}
