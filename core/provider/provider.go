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
	"io"

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

	// GetCommitStatus returns the combined CI status for the commit
	// identified by ref (a full SHA, branch name, or tag name). Both
	// Forgejo and GitHub accept tag names directly in this endpoint so
	// no ref-to-SHA resolution is needed on the caller's side.
	//
	// Used by `gaia pr ci-wait --ref <ref>` to poll tag-triggered
	// release workflows. Returns a CISummary with State="" when no
	// status has been registered yet (the workflow hasn't started);
	// callers should treat "" as "pending".
	GetCommitStatus(ctx context.Context, owner, repo, ref string) (*types.CISummary, error)

	// Search returns hits across the kinds named in opts.Kinds (issues
	// and PRs in Phase 1; code added in Phase 4).
	Search(ctx context.Context, query string, opts SearchOptions) ([]types.SearchResult, *Page, error)

	// --- Write methods (Phase 1.5) ----------------------------------

	// CreateIssue opens a new issue and returns the trimmed view.
	CreateIssue(ctx context.Context, owner, repo string, opts CreateIssueOptions) (*types.Issue, error)

	// EditIssue patches an existing issue. Empty option fields are
	// "no change"; AddLabels/RemoveLabels apply incrementally.
	EditIssue(ctx context.Context, owner, repo string, n int, opts EditIssueOptions) (*types.Issue, error)

	// CreateIssueComment posts a top-level thread comment on issue or
	// PR n. Returns the resulting Comment with Source="issue".
	CreateIssueComment(ctx context.Context, owner, repo string, n int, body string) (*types.Comment, error)

	// EditIssueComment patches an existing comment by its ID (not the
	// issue number; comment IDs are forge-global within a repo).
	EditIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (*types.Comment, error)

	// DeleteIssueComment removes a comment by ID. 204 is success.
	DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error

	// CreatePullRequest opens a new PR.
	CreatePullRequest(ctx context.Context, owner, repo string, opts CreatePullRequestOptions) (*types.PullRequest, error)

	// EditPullRequest patches a PR. Same semantics as EditIssue plus
	// Draft (*bool because false is meaningful).
	EditPullRequest(ctx context.Context, owner, repo string, n int, opts EditPullRequestOptions) (*types.PullRequest, error)

	// MergePullRequest performs the merge with the requested method.
	// Returns nil on 200/204; the caller can re-fetch via
	// GetPullRequest if it needs the updated state.
	MergePullRequest(ctx context.Context, owner, repo string, n int, opts MergePullRequestOptions) error

	// ListLabels returns every label on the repo (not paginated;
	// repos rarely exceed the default page size for labels).
	ListLabels(ctx context.Context, owner, repo string) ([]types.Label, error)

	// CreateLabel makes a new label.
	CreateLabel(ctx context.Context, owner, repo string, opts CreateLabelOptions) (*types.Label, error)

	// EditLabel patches a label by current name.
	EditLabel(ctx context.Context, owner, repo string, name string, opts EditLabelOptions) (*types.Label, error)

	// DeleteLabel removes a label by name. 204 is success.
	DeleteLabel(ctx context.Context, owner, repo string, name string) error

	// SubmitReview submits a PR review with state (APPROVED /
	// REQUEST_CHANGES / COMMENT) and optional inline file:line
	// comments. Returns nil on success; the caller can re-fetch
	// comments via ListComments to see the new review.
	SubmitReview(ctx context.Context, owner, repo string, n int, opts SubmitReviewOptions) error

	// --- Releases (Phase 3) -----------------------------------------

	// ListReleases returns releases on the repo, newest first.
	ListReleases(ctx context.Context, owner, repo string, opts ListReleasesOptions) ([]types.Release, *Page, error)

	// GetRelease fetches one release by tag name.
	GetRelease(ctx context.Context, owner, repo, tag string) (*types.Release, error)

	// CreateRelease creates a new release.
	CreateRelease(ctx context.Context, owner, repo string, opts CreateReleaseOptions) (*types.Release, error)

	// EditRelease patches an existing release identified by tag.
	EditRelease(ctx context.Context, owner, repo, tag string, opts EditReleaseOptions) (*types.Release, error)

	// DeleteRelease removes a release by tag.
	DeleteRelease(ctx context.Context, owner, repo, tag string) error

	// UploadReleaseAsset attaches a file to an existing release. Both
	// forges expose this differently (Forgejo: multipart on the same
	// API host; GitHub: raw body to uploads.github.com), but the
	// caller-facing contract is the same: stream `body` into the
	// release identified by `releaseID` under `name`.
	//
	// `contentType` is optional — pass "" to let the implementation
	// default to "application/octet-stream". `size` must be the exact
	// byte length of `body`; GitHub's upload API rejects requests that
	// lack a Content-Length header (HTTP 400 "Bad Content-Length").
	UploadReleaseAsset(ctx context.Context, owner, repo string, releaseID int64, name, contentType string, size int64, body io.Reader) error

	// --- Packages (Phase 4 / #107) -----------------------------------
	//
	// Packages are scoped to a user/org, NOT to a repo — Forgejo's
	// endpoints all live under `/packages/{owner}` regardless of which
	// repo (if any) the package was published from. The CLI/MCP
	// surfaces use `--owner` instead of `--repo` for that reason.

	// ListPackages returns packages matching opts under the given
	// owner. Forgejo paginates these.
	ListPackages(ctx context.Context, owner string, opts ListPackagesOptions) ([]types.Package, *Page, error)

	// GetPackage fetches one package version. (pkgType, name, version)
	// uniquely identifies a package on Forgejo; GitHub keys per-registry
	// differently.
	GetPackage(ctx context.Context, owner, pkgType, name, version string) (*types.Package, error)

	// DeletePackage removes one package version. 204 is success.
	DeletePackage(ctx context.Context, owner, pkgType, name, version string) error

	// UploadPackage publishes a single artifact to a package version.
	// On Forgejo this targets the generic-package endpoint:
	//   PUT /packages/{owner}/generic/{name}/{version}/{file}
	// streaming `body` as the binary payload. On GitHub the
	// per-registry upload surface (npm publish, container manifest
	// push, ...) doesn't fold into one shape; the GitHub provider
	// returns a "not implemented" error until per-kind dispatch
	// lands as a follow-up.
	//
	// `pkgType` selects the registry. PR #122 ships only "generic"
	// on Forgejo; non-generic types are rejected with a usage error
	// so callers don't see a confusing 404 from a path that doesn't
	// exist for their forge+registry combo.
	UploadPackage(ctx context.Context, owner, pkgType, name, version string, opts UploadPackageOptions, body io.Reader) error

	// --- Wikis (Phase 4 / #108) --------------------------------------

	// ListWikiPages returns wiki page summaries (Title + Path +
	// LastCommit + UpdatedAt; Body is left empty by the list
	// endpoint).
	ListWikiPages(ctx context.Context, owner, repo string, opts ListWikiPagesOptions) ([]types.WikiPage, *Page, error)

	// GetWikiPage returns one wiki page by slug, including its
	// markdown body.
	GetWikiPage(ctx context.Context, owner, repo, slug string) (*types.WikiPage, error)

	// SearchWikiPages performs client-side title + body matching
	// across the repo's wiki pages. Forgejo has no native wiki-search
	// endpoint; this implementation lists pages then fetches each one
	// to scan its body, capped at opts.MaxPages (default 100). The
	// returned slice length hitting the cap indicates the wiki was
	// truncated mid-scan.
	SearchWikiPages(ctx context.Context, owner, repo, query string, opts SearchWikiOptions) ([]types.WikiSearchHit, error)

	// EditWikiPage upserts a wiki page: creates it if no page exists
	// at that slug, replaces the body if it does. Returns the
	// resulting WikiPage with its post-write body.
	EditWikiPage(ctx context.Context, owner, repo, slug, body string) (*types.WikiPage, error)

	// DeleteWikiPage removes a wiki page by slug. 204 is success.
	DeleteWikiPage(ctx context.Context, owner, repo, slug string) error

	// --- Webhooks (Phase 4 / #85) ---------------------------------

	// ListWebhooks returns the repo's configured webhooks.
	ListWebhooks(ctx context.Context, owner, repo string, opts ListWebhooksOptions) ([]types.Webhook, *Page, error)

	// GetWebhook returns one webhook by ID.
	GetWebhook(ctx context.Context, owner, repo string, id int64) (*types.Webhook, error)

	// CreateWebhook installs a new webhook on the repo.
	CreateWebhook(ctx context.Context, owner, repo string, opts CreateWebhookOptions) (*types.Webhook, error)

	// EditWebhook patches an existing webhook by ID. AddEvents and
	// RemoveEvents apply incrementally over the current event list
	// (the implementation does the merge by pre-fetching).
	EditWebhook(ctx context.Context, owner, repo string, id int64, opts EditWebhookOptions) (*types.Webhook, error)

	// DeleteWebhook removes a webhook by ID. 204 is success.
	DeleteWebhook(ctx context.Context, owner, repo string, id int64) error

	// ListWebhookDeliveries returns recent delivery summaries for the
	// given webhook. Bodies are NOT inlined; fetch
	// GetWebhookDelivery for the per-delivery full payload.
	ListWebhookDeliveries(ctx context.Context, owner, repo string, id int64, opts ListDeliveriesOptions) ([]types.WebhookDelivery, *Page, error)

	// GetWebhookDelivery returns one delivery's full request +
	// response payload. Use sparingly: a single delivery for a busy
	// repo can be 50–200 KB.
	GetWebhookDelivery(ctx context.Context, owner, repo string, id, deliveryID int64) (*types.WebhookDeliveryDetail, error)

	// RedeliverWebhook re-fires a previously-sent delivery. The
	// receiver sees the same payload + signature, with a header
	// flag indicating it's a redelivery.
	RedeliverWebhook(ctx context.Context, owner, repo string, id, deliveryID int64) error

	// TestWebhook sends a synthetic ping event to the webhook so
	// the operator can confirm the receiver is reachable.
	TestWebhook(ctx context.Context, owner, repo string, id int64) error
}
