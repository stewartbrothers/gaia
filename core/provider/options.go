package provider

import "time"

// Page describes the pagination state of a list-call return. Truncated
// is set when the caller-requested Limit was hit and more results exist
// upstream. NextCursor is opaque to callers; pass it back unchanged in
// the next call's options to continue.
type Page struct {
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListIssuesOptions filters and paginates a ListIssues call. State takes
// "open", "closed", "all" (or "" → "open"). Empty Limit takes the
// default (set by the calling layer; see docs/output-format.md).
type ListIssuesOptions struct {
	State    string
	Labels   []string
	Assignee string
	Author   string
	Since    time.Time
	Query    string
	Limit    int
	Cursor   string
}

// GetIssueOptions controls inline comment fetch. WithComments=0 returns
// the issue with no comments; >0 inlines the most recent N.
type GetIssueOptions struct {
	WithComments int
}

// ListPullRequestsOptions filters and paginates a ListPullRequests call.
// Head and Base accept full refs (e.g. "feature/x") or owner:ref for
// cross-fork filtering.
type ListPullRequestsOptions struct {
	State  string
	Labels []string
	Head   string
	Base   string
	Limit  int
	Cursor string
}

// GetPullRequestOptions controls what's reconciled into a single PR
// fetch. WithCISummary=true triggers the extra commit-status call;
// WithComments>0 inlines that many recent comments.
type GetPullRequestOptions struct {
	WithComments  int
	WithCISummary bool
}

// GetPullRequestDiffOptions narrows or controls the diff payload. Paths
// limits to a subset of files; ContextLines overrides the default
// surrounding context (-1 = provider default; 0 = no context).
type GetPullRequestDiffOptions struct {
	Paths        []string
	ContextLines int
}

// ListCommentsOptions filters the unified comment stream. Sources empty
// means all three (issue, review, inline). Limit caps the count;
// time-ordering is provider-stable.
type ListCommentsOptions struct {
	Sources []string
	Limit   int
	Cursor  string
}

// SearchOptions controls the search call. Kinds empty means
// {issue, pull_request} in Phase 1. Repo "" means the current repo
// (whichever the caller bound the operation to); a non-empty owner/name
// scopes to a specific repo.
type SearchOptions struct {
	Kinds  []string
	Repo   string
	Limit  int
	Cursor string
}

// CreateIssueOptions configures CreateIssue. JSON tags drive both
// the dry-run output (so an agent sees the wire shape) and the
// upstream POST body — both line up with what Forgejo expects.
type CreateIssueOptions struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// EditIssueOptions configures EditIssue. Empty string fields are
// dropped by omitempty so they're treated as "no change" by the
// upstream — matching Forgejo's PATCH semantics. AddLabels and
// RemoveLabels apply only the named changes (a Phase 1.5 follow-up
// will route those through /issues/{n}/labels rather than the issue
// PATCH endpoint).
type EditIssueOptions struct {
	Title        string   `json:"title,omitempty"`
	Body         string   `json:"body,omitempty"`
	State        string   `json:"state,omitempty"`
	AddLabels    []string `json:"add_labels,omitempty"`
	RemoveLabels []string `json:"remove_labels,omitempty"`
	Assignees    []string `json:"assignees,omitempty"`
}

// CreatePullRequestOptions configures CreatePullRequest. Head/Base
// take refs; cross-fork heads use "owner:ref".
type CreatePullRequestOptions struct {
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	Head   string   `json:"head"`
	Base   string   `json:"base"`
	Draft  bool     `json:"draft,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// EditPullRequestOptions: Draft is *bool because false != "no change"
// (PRs flip both ways and we need to express each).
type EditPullRequestOptions struct {
	Title        string   `json:"title,omitempty"`
	Body         string   `json:"body,omitempty"`
	State        string   `json:"state,omitempty"`
	AddLabels    []string `json:"add_labels,omitempty"`
	RemoveLabels []string `json:"remove_labels,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
}

// MergePullRequestOptions configures MergePullRequest. Method is
// "merge" (default), "rebase", or "squash". Forgejo names the merge
// method field "do" — the json tag pins that.
type MergePullRequestOptions struct {
	Method       string `json:"do,omitempty"`
	Title        string `json:"MergeTitleField,omitempty"`
	Message      string `json:"MergeMessageField,omitempty"`
	DeleteBranch bool   `json:"delete_branch_after_merge,omitempty"`
}

// CreateLabelOptions: Name + Color required; Color is a hex string
// without the leading "#". Description optional.
type CreateLabelOptions struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

// EditLabelOptions configures EditLabel. NewName allows renames;
// empty means keep the current name.
type EditLabelOptions struct {
	NewName     string `json:"name,omitempty"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// SubmitReviewOptions configures SubmitReview. Event takes one of
// "APPROVED", "REQUEST_CHANGES", "COMMENT". Body is the top-level
// review remark; Comments are inline file:line remarks attached to
// the same review.
type SubmitReviewOptions struct {
	Event    string                `json:"event"`
	Body     string                `json:"body,omitempty"`
	Comments []ReviewInlineComment `json:"comments,omitempty"`
}

// ReviewInlineComment is one inline review remark. Line is the line
// number in the new (post-change) file; Forgejo also exposes
// old_position for left-side comments which we don't yet plumb.
type ReviewInlineComment struct {
	Path string `json:"path"`
	Line int    `json:"new_position"`
	Body string `json:"body"`
}

// ListReleasesOptions paginates ListReleases.
type ListReleasesOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// CreateReleaseOptions configures CreateRelease. TagName is required;
// when Name is empty, the forge typically defaults it to TagName.
// TargetCommitish accepts a branch name or commit SHA; empty means
// the repo's default branch.
type CreateReleaseOptions struct {
	TagName         string `json:"tag_name"`
	Name            string `json:"name,omitempty"`
	Body            string `json:"body,omitempty"`
	TargetCommitish string `json:"target_commitish,omitempty"`
	Draft           bool   `json:"draft,omitempty"`
	Prerelease      bool   `json:"prerelease,omitempty"`
}

// EditReleaseOptions configures EditRelease. Empty/nil fields mean
// "no change". Draft and Prerelease are *bool so explicitly setting
// false works (flip a draft to published, demote a prerelease).
type EditReleaseOptions struct {
	TagName    string `json:"tag_name,omitempty"`
	Name       string `json:"name,omitempty"`
	Body       string `json:"body,omitempty"`
	Draft      *bool  `json:"draft,omitempty"`
	Prerelease *bool  `json:"prerelease,omitempty"`
}

// --- Packages (Phase 4 / #107) -----------------------------------

// ListPackagesOptions filters and paginates a ListPackages call.
// Type narrows to a registry kind ("npm", "maven", "container",
// "generic", ...); empty means all kinds. Q is a name-substring
// filter passed straight through to Forgejo's `q` query parameter.
type ListPackagesOptions struct {
	Type   string
	Q      string
	Limit  int
	Cursor string
}

// --- Packages (Phase 4 / #122) -----------------------------------

// UploadPackageOptions configures UploadPackage. ContentType is the
// optional MIME type of the upload body; "" lets the implementation
// default ("application/octet-stream"). FileName becomes the final
// URL segment in Forgejo's generic-package endpoint
// PUT /packages/{owner}/generic/{name}/{version}/{file}.
//
// On GitHub, package upload semantics are per-registry (npm publish,
// container manifest push, ...) and don't fold into a single struct —
// the GitHub provider returns a documented "not implemented" error
// (see provider-parity.md) until follow-up dispatch lands. Forgejo's
// generic-package endpoint is what this struct targets; npm/maven/
// container kinds on Forgejo have their own endpoints and are out of
// scope for #122.
type UploadPackageOptions struct {
	FileName    string
	ContentType string
}

// --- Wikis (Phase 4 / #108) --------------------------------------

// ListWikiPagesOptions paginates a ListWikiPages call. Forgejo's wiki
// pages endpoint paginates by `page` + `limit`; the provider layer
// translates Cursor through the same pageFromCursor helper used
// elsewhere. Bodies are not returned by the list endpoint — call
// GetWikiPage for the markdown source.
type ListWikiPagesOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// SearchWikiOptions controls the client-side wiki search. Forgejo has
// no native wiki-search endpoint, so the provider implementation lists
// pages and matches `query` against title + body. MaxPages caps how
// many pages we fetch before truncating; 0 means the provider's
// documented default (currently 100). Larger wikis emit a truncation
// signal via the returned slice's length matching the cap.
//
// SnippetWidth is the number of characters of context shown around
// each match (split before/after). 0 means default (~200 chars).
type SearchWikiOptions struct {
	MaxPages     int
	SnippetWidth int
}
