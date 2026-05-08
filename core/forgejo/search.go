package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

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
//
// When the client has a cache and opts.Repo is non-empty (repo-scoped),
// the cache is consulted first. A warm cache (at least one entry for
// the repo) short-circuits the upstream call entirely. A cold cache
// (zero entries) falls through to the upstream path unchanged.
func (p *Provider) Search(ctx context.Context, query string, opts provider.SearchOptions) ([]types.SearchResult, *provider.Page, error) {
	// Cache-backed path: repo-scoped only, falls through if cold.
	if p.client.cache != nil && opts.Repo != "" {
		if results, ok, err := p.searchFromCache(ctx, query, opts); err != nil {
			return nil, nil, err
		} else if ok {
			return results, nil, nil
		}
	}

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

// cachePayload is the minimal shape of a cached object payload that
// searchFromCache needs. It covers both apiIssue and apiPullRequest —
// both have number, title, and body as plain strings in their cached form.
type cachePayload struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// searchFromCache scans the local cache for issues/PRs matching query.
// Returns (results, true, nil) when the cache is warm (at least one entry
// existed for the requested kinds), or (nil, false, nil) when the cache
// is cold — the caller must fall through to the upstream path.
func (p *Provider) searchFromCache(ctx context.Context, query string, opts provider.SearchOptions) ([]types.SearchResult, bool, error) {
	owner, repo, ok := strings.Cut(opts.Repo, "/")
	if !ok {
		// Malformed repo slug — fall through to upstream.
		return nil, false, nil
	}
	needle := strings.ToLower(query)
	var hits []types.SearchResult
	total := 0

	kinds := opts.Kinds
	if len(kinds) == 0 {
		kinds = []string{"issue", "pull_request"}
	}

	for _, kind := range kinds {
		cacheKind := kindIssue // "issue"
		if kind == "pull_request" {
			cacheKind = kindPR // "pr"
		}
		payloads, err := p.client.cache.Scan(ctx, cacheKind, owner, repo)
		if err != nil {
			return nil, false, err
		}
		total += len(payloads)
		for _, raw := range payloads {
			var item cachePayload
			if err := json.Unmarshal(raw, &item); err != nil {
				// Skip malformed entries rather than failing the whole
				// operation — the upstream path would succeed regardless.
				continue
			}
			titleLower := strings.ToLower(item.Title)
			bodyLower := strings.ToLower(item.Body)
			if !strings.Contains(titleLower, needle) && !strings.Contains(bodyLower, needle) {
				continue
			}
			hits = append(hits, types.SearchResult{
				Kind:     kind,
				Number:   item.Number,
				Title:    item.Title,
				RepoFull: opts.Repo,
			})
		}
	}

	if total == 0 {
		// Cold cache — signal the caller to fall through.
		return nil, false, nil
	}

	// Apply limit.
	limit := clampLimit(opts.Limit)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, true, nil
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
