package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiSearchResponse mirrors GitHub's /search/issues response wrapper.
// Forgejo returns a bare array; GitHub wraps it in {total_count,
// incomplete_results, items}.
type apiSearchResponse struct {
	TotalCount int               `json:"total_count"`
	Items      []apiSearchResult `json:"items"`
}

// apiSearchResult is one result from /search/issues. Like the issues
// endpoint, the `pull_request` field discriminates issue vs PR.
type apiSearchResult struct {
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	HTMLURL     string       `json:"html_url"`
	PullRequest *apiPRMarker `json:"pull_request"`
}

// repoFromHTMLURL extracts owner/name from a GitHub HTML URL like
// "https://github.com/owner/name/issues/123" since the search results
// don't carry repository.full_name directly.
func repoFromHTMLURL(htmlURL string) string {
	// Strip protocol + host
	rest := strings.TrimPrefix(htmlURL, "https://github.com/")
	rest = strings.TrimPrefix(rest, "http://github.com/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func (a *apiSearchResult) toType() types.SearchResult {
	kind := "issue"
	if a.PullRequest != nil {
		kind = "pull_request"
	}
	return types.SearchResult{
		Kind:     kind,
		Number:   a.Number,
		Title:    a.Title,
		RepoFull: repoFromHTMLURL(a.HTMLURL),
	}
}

// Search queries /search/issues. GitHub doesn't have separate
// repo-scoped endpoints; instead, opts.Repo is folded into the query
// as a `repo:owner/name` qualifier. opts.Kinds adds `is:issue` /
// `is:pr` qualifiers.
//
// The user's query string passes through verbatim, so power-users
// can supply arbitrary GitHub search qualifiers (`label:bug
// state:closed author:alice` etc.) directly.
func (p *Provider) Search(ctx context.Context, query string, opts provider.SearchOptions) ([]types.SearchResult, *provider.Page, error) {
	limit := clampLimit(opts.Limit)

	q := buildSearchQuery(query, opts)

	values := url.Values{}
	values.Set("q", q)
	values.Set("per_page", strconv.Itoa(limit))
	values.Set("page", pageFromCursor(opts.Cursor))

	var resp apiSearchResponse
	if err := p.client.Get(ctx, "/search/issues?"+values.Encode(), &resp); err != nil {
		return nil, nil, err
	}

	out := make([]types.SearchResult, 0, len(resp.Items))
	for i := range resp.Items {
		out = append(out, resp.Items[i].toType())
	}
	return out, makePage(len(resp.Items), limit, opts.Cursor), nil
}

// buildSearchQuery composes GitHub's q-parameter from a user query +
// the structured opts (Repo, Kinds).
func buildSearchQuery(query string, opts provider.SearchOptions) string {
	parts := []string{strings.TrimSpace(query)}
	if opts.Repo != "" {
		parts = append(parts, "repo:"+opts.Repo)
	}
	if len(opts.Kinds) == 1 {
		switch opts.Kinds[0] {
		case "issue":
			parts = append(parts, "is:issue")
		case "pull_request":
			parts = append(parts, "is:pr")
		}
	}
	// Empty Kinds or both kinds present → no `is:` qualifier; both
	// types come back.
	return strings.TrimSpace(joinNonEmpty(parts))
}

func joinNonEmpty(s []string) string {
	out := ""
	for _, x := range s {
		if x == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += x
	}
	return out
}

// build path-escape-friendly versions to keep the file self-contained.
var _ = fmt.Sprintf
