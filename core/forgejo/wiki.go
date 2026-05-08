package forgejo

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// Forgejo wiki API surface implemented here:
//
//	GET    /repos/{owner}/{repo}/wiki/pages          → []WikiPageMetaData
//	GET    /repos/{owner}/{repo}/wiki/page/{slug}    → WikiPage (with content_base64)
//	POST   /repos/{owner}/{repo}/wiki/new            → WikiPage  (create)
//	PATCH  /repos/{owner}/{repo}/wiki/page/{slug}    → WikiPage  (replace body)
//	DELETE /repos/{owner}/{repo}/wiki/page/{slug}    → 204
//
// Bodies travel base64-encoded inside `content_base64` per Forgejo's
// schema (so non-UTF8 content can round-trip), and we decode/encode
// transparently in this layer. Slugs are the URL path segment Forgejo
// derives from the page title — callers pass them verbatim.

// defaultSearchMaxPages caps the client-side wiki scan. Wikis with
// more pages than this surface a truncation signal via the slice
// length matching the cap; bigger scans need a clone-cache strategy
// (see follow-up #120 for the GitHub story which forces that anyway).
const defaultSearchMaxPages = 100

// defaultSearchSnippetWidth is the on-each-side context for
// snippet windows. Total snippet length is therefore ~2 * this.
const defaultSearchSnippetWidth = 100

// apiWikiCommit mirrors the Forgejo WikiCommit shape. Only `sha` and
// the committer date are used — `commiter` is the on-the-wire spelling
// (sic; Forgejo's API has the typo, we match it).
type apiWikiCommit struct {
	SHA      string `json:"sha"`
	Commiter struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Date  string `json:"date"`
	} `json:"commiter"`
	Author struct {
		Date string `json:"date"`
	} `json:"author"`
	Message string `json:"message"`
}

// apiWikiPageMeta is the list-call shape: title, sub_url, last_commit,
// no body. SubURL is the path segment Forgejo uses; we surface it as
// types.WikiPage.Path.
type apiWikiPageMeta struct {
	Title      string         `json:"title"`
	SubURL     string         `json:"sub_url"`
	HTMLURL    string         `json:"html_url"`
	LastCommit *apiWikiCommit `json:"last_commit"`
}

// apiWikiPage is the full single-page shape with content_base64.
type apiWikiPage struct {
	Title         string         `json:"title"`
	SubURL        string         `json:"sub_url"`
	HTMLURL       string         `json:"html_url"`
	ContentBase64 string         `json:"content_base64"`
	LastCommit    *apiWikiCommit `json:"last_commit"`
}

// shortSHA returns the conventional 7-char SHA prefix. Empty in →
// empty out so missing values don't grow a stray prefix.
func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

// commitDate plucks the most reliable timestamp from a WikiCommit
// (committer date, falling back to author date), parsing the
// Forgejo-emitted RFC3339 string. Returns the zero time on parse
// failure rather than aborting the whole operation.
func commitDate(c *apiWikiCommit) time.Time {
	if c == nil {
		return time.Time{}
	}
	for _, s := range []string{c.Commiter.Date, c.Author.Date} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (a *apiWikiPageMeta) toType() types.WikiPage {
	wp := types.WikiPage{
		Title:     a.Title,
		Path:      a.SubURL,
		UpdatedAt: commitDate(a.LastCommit),
	}
	if a.LastCommit != nil {
		wp.LastCommit = shortSHA(a.LastCommit.SHA)
	}
	if wp.Path == "" {
		wp.Path = a.Title
	}
	return wp
}

func (a *apiWikiPage) toType() (types.WikiPage, error) {
	wp := types.WikiPage{
		Title:     a.Title,
		Path:      a.SubURL,
		UpdatedAt: commitDate(a.LastCommit),
	}
	if a.LastCommit != nil {
		wp.LastCommit = shortSHA(a.LastCommit.SHA)
	}
	if wp.Path == "" {
		wp.Path = a.Title
	}
	if a.ContentBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(a.ContentBase64)
		if err != nil {
			return wp, exitcode.Wrap(err, exitcode.Generic, "decode wiki page content_base64")
		}
		wp.Body = string(decoded)
	}
	return wp, nil
}

// ListWikiPages returns wiki page summaries for the repo. Bodies are
// not populated by the list endpoint — callers wanting the markdown
// source must follow up with GetWikiPage per slug of interest.
func (p *Provider) ListWikiPages(ctx context.Context, owner, repo string, opts provider.ListWikiPagesOptions) ([]types.WikiPage, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/wiki/pages?%s", owner, repo, q.Encode())
	var raw []apiWikiPageMeta
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.WikiPage, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toType())
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}

// GetWikiPage fetches one wiki page by slug, decoding its body from
// base64.
//
// Routes through GetCached: a fresh cache row short-circuits the
// upstream call entirely; a stale row triggers a conditional GET
// with If-None-Match / If-Modified-Since (#153).
func (p *Provider) GetWikiPage(ctx context.Context, owner, repo, slug string) (*types.WikiPage, error) {
	path := fmt.Sprintf("/repos/%s/%s/wiki/page/%s", owner, repo, url.PathEscape(slug))
	var raw apiWikiPage
	key := cacheKey(kindWiki, owner, repo, slug)
	if err := p.client.GetCached(ctx, path, &raw, key, CacheTTLSingle); err != nil {
		return nil, err
	}
	out, err := raw.toType()
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// EditWikiPage upserts a wiki page. Probes for existence via
// GetWikiPage first; 404 → POST /wiki/new, 200 → PATCH the existing
// slug. Both paths return the post-write WikiPage with body decoded.
//
// Title in the create POST is set to the slug (Forgejo doesn't allow
// an empty title); callers wanting a specific human title should
// supply the slug they want the URL to use, since Forgejo derives the
// slug from the title at creation time anyway.
func (p *Provider) EditWikiPage(ctx context.Context, owner, repo, slug, body string) (*types.WikiPage, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	wireBody := map[string]any{
		"title":          slug,
		"content_base64": encoded,
	}

	// Probe: does the page exist? Cheaper to PATCH-on-existing than
	// to try POST and recover from the conflict, and the GET also
	// validates the user's auth before we mutate.
	_, err := p.GetWikiPage(ctx, owner, repo, slug)
	if err != nil && exitcode.Of(err) != exitcode.NotFound {
		return nil, err
	}
	if err != nil {
		// 404 → create.
		var raw apiWikiPage
		path := fmt.Sprintf("/repos/%s/%s/wiki/new", owner, repo)
		if err := p.client.Post(ctx, path, wireBody, &raw); err != nil {
			return nil, err
		}
		// New page may show up in any wiki list query.
		cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindWiki, owner, repo)
		out, derr := raw.toType()
		if derr != nil {
			return nil, derr
		}
		return &out, nil
	}

	// Existing → patch. Forgejo's PATCH /wiki/page/{slug} replaces
	// the body; the slug stays unchanged because we send the same
	// title.
	var raw apiWikiPage
	path := fmt.Sprintf("/repos/%s/%s/wiki/page/%s", owner, repo, url.PathEscape(slug))
	if err := p.client.Patch(ctx, path, wireBody, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindWiki, owner, repo, slug)
	out, derr := raw.toType()
	if derr != nil {
		return nil, derr
	}
	return &out, nil
}

// DeleteWikiPage removes a wiki page by slug. 204 from the API is
// success.
func (p *Provider) DeleteWikiPage(ctx context.Context, owner, repo, slug string) error {
	path := fmt.Sprintf("/repos/%s/%s/wiki/page/%s", owner, repo, url.PathEscape(slug))
	if err := p.client.Delete(ctx, path); err != nil {
		return err
	}
	cache.NewInvalidator(p.client.cache).AfterDelete(ctx, kindWiki, owner, repo, slug)
	return nil
}

// SearchWikiPages performs a client-side title + body match across
// every wiki page (capped at opts.MaxPages, default 100). Forgejo
// has no native wiki-search endpoint; this trades a single API call
// for N+1 to give agents a one-tool primitive over wiki content.
//
// Matching is case-insensitive on both title and body. Hit ordering
// matches the underlying ListWikiPages order. When the match is in
// the body, the snippet is a window of opts.SnippetWidth chars on
// each side of the match (default 100, total ~200). When the match
// is title-only, snippet is empty.
func (p *Provider) SearchWikiPages(ctx context.Context, owner, repo, query string, opts provider.SearchWikiOptions) ([]types.WikiSearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, exitcode.Errorf(exitcode.Usage, "wiki search query must not be empty")
	}
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = defaultSearchMaxPages
	}
	width := opts.SnippetWidth
	if width <= 0 {
		width = defaultSearchSnippetWidth
	}

	// Pull the page list. The list itself is paginated; we only
	// scan the first MaxPages entries — beyond that the call's
	// answer becomes "scan-truncated", and the agent should narrow
	// its query rather than ask gaia to download an unbounded
	// repository.
	pages, _, err := p.ListWikiPages(ctx, owner, repo, provider.ListWikiPagesOptions{Limit: maxPages})
	if err != nil {
		return nil, err
	}
	if len(pages) > maxPages {
		pages = pages[:maxPages]
	}

	needle := strings.ToLower(q)
	hits := make([]types.WikiSearchHit, 0, len(pages))

	for _, page := range pages {
		titleMatches := strings.Contains(strings.ToLower(page.Title), needle)
		// Only fetch the body if the title didn't already match —
		// keeps the call count to "title-match pages: 1; body-scan
		// pages: 1+1" instead of "every page: always 1+1".
		if titleMatches {
			hits = append(hits, types.WikiSearchHit{
				Path:  page.Path,
				Title: page.Title,
			})
			continue
		}

		full, err := p.GetWikiPage(ctx, owner, repo, page.Path)
		if err != nil {
			// Per-page failure shouldn't abort the whole search; an
			// agent will still see what we found. We swallow the
			// error rather than logging because the provider layer
			// has no logger plumbed.
			continue
		}
		idx := strings.Index(strings.ToLower(full.Body), needle)
		if idx < 0 {
			continue
		}
		hits = append(hits, types.WikiSearchHit{
			Path:    full.Path,
			Title:   full.Title,
			Snippet: snippetAround(full.Body, idx, len(q), width),
		})
	}
	return hits, nil
}

// snippetAround returns a window around the match at body[matchIdx:
// matchIdx+matchLen]. The result is at most width on each side, plus
// the match itself; whitespace is collapsed so a snippet on a wide
// page reads as a single line.
func snippetAround(body string, matchIdx, matchLen, width int) string {
	start := matchIdx - width
	if start < 0 {
		start = 0
	}
	end := matchIdx + matchLen + width
	if end > len(body) {
		end = len(body)
	}
	snippet := body[start:end]
	// Collapse runs of whitespace (newlines, tabs) to single spaces
	// so the snippet renders cleanly in JSON output.
	snippet = strings.Join(strings.Fields(snippet), " ")
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(body) {
		snippet += "..."
	}
	return snippet
}
