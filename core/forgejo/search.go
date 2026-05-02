package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiSearchResult mirrors the shape Forgejo returns from /repos/issues/search
// and /repos/{owner}/{repo}/issues. Only the fields we use are listed;
// the rest decode-skip per Go's json default.
type apiSearchResult struct {
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	Repository  apiRepo      `json:"repository"`
	PullRequest *apiPRMarker `json:"pull_request"`
}

// apiPRMarker is just a non-nil marker — Forgejo populates this object
// when the result is a PR and leaves it null for issues. We don't read
// any of its fields; presence is the signal.
type apiPRMarker struct{}

func (a *apiSearchResult) toType() types.SearchResult {
	kind := "issue"
	if a.PullRequest != nil {
		kind = "pull_request"
	}
	return types.SearchResult{
		Kind:     kind,
		Number:   a.Number,
		Title:    a.Title,
		RepoFull: a.Repository.FullName,
	}
}

// Search returns hits across the kinds named in opts.Kinds. Two
// underlying endpoints are used:
//
//   - opts.Repo == ""        → /repos/issues/search (cross-repo).
//   - opts.Repo == "o/r"     → /repos/o/r/issues   (repo-scoped).
//
// For Kinds: ["issue"] / ["pull_request"] are translated to the
// upstream `type=issues|pulls` filter. Empty or `["issue",
// "pull_request"]` sends no filter so both kinds come back.
func (p *Provider) Search(ctx context.Context, query string, opts provider.SearchOptions) ([]types.SearchResult, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))
	if t := upstreamTypeForKinds(opts.Kinds); t != "" {
		q.Set("type", t)
	}

	var path string
	if opts.Repo != "" {
		path = fmt.Sprintf("/repos/%s/issues?%s", opts.Repo, q.Encode())
	} else {
		path = "/repos/issues/search?" + q.Encode()
	}

	var raw []apiSearchResult
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}

	out := make([]types.SearchResult, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// upstreamTypeForKinds maps gaia's Kinds slice to the Forgejo `type`
// query param. Returns "" when no upstream filter should be applied —
// which is the case for empty Kinds AND for "both kinds" (since
// Forgejo's endpoint already returns both by default).
func upstreamTypeForKinds(kinds []string) string {
	if len(kinds) == 0 || len(kinds) == 2 {
		return ""
	}
	switch kinds[0] {
	case "issue":
		return "issues"
	case "pull_request":
		return "pulls"
	}
	return ""
}
