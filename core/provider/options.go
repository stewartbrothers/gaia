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
