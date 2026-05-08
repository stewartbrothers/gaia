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
func (p *Provider) GetWikiPage(ctx context.Context, owner, repo, slug string) (*types.WikiPage, error) {
	path := fmt.Sprintf("/repos/%s/%s/wiki/page/%s", owner, repo, url.PathEscape(slug))
	var raw apiWikiPage
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out, err := raw.toType()
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// EditWikiPage upserts a wiki page. Tries GET by slug first; if the
// page exists, PATCHes it. On 404, lists wiki pages and matches by
// title before falling back to POST — Forgejo may store pages under a
// slug that differs from the title (e.g. "Quick-Start" → "Quick-Start.-"),
// so a direct slug lookup can 404 even when the page exists (#178).
func (p *Provider) EditWikiPage(ctx context.Context, owner, repo, slug, body string) (*types.WikiPage, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))

	// Fast path: exact slug match.
	_, err := p.GetWikiPage(ctx, owner, repo, slug)
	if err != nil && exitcode.Of(err) != exitcode.NotFound {
		return nil, err
	}
	if err == nil {
		return p.patchWikiPage(ctx, owner, repo, slug, slug, encoded)
	}

	// Slug miss: Forgejo may have stored this page under a canonicalised
	// slug that differs from the title. List pages and match by title.
	if canonicalSlug, found := p.findWikiSlugByTitle(ctx, owner, repo, slug); found {
		return p.patchWikiPage(ctx, owner, repo, canonicalSlug, slug, encoded)
	}

	// Truly new page → POST /wiki/new.
	wireBody := map[string]any{
		"title":          slug,
		"content_base64": encoded,
	}
	var raw apiWikiPage
	path := fmt.Sprintf("/repos/%s/%s/wiki/new", owner, repo)
	if err := p.client.Post(ctx, path, wireBody, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindWiki, owner, repo)
	out, derr := raw.toType()
	if derr != nil {
		return nil, derr
	}
	return &out, nil
}

// patchWikiPage issues PATCH /wiki/page/{canonicalSlug}. title is the
// human title sent in the body (keeps the page title unchanged).
func (p *Provider) patchWikiPage(ctx context.Context, owner, repo, canonicalSlug, title, encoded string) (*types.WikiPage, error) {
	wireBody := map[string]any{
		"title":          title,
		"content_base64": encoded,
	}
	var raw apiWikiPage
	path := fmt.Sprintf("/repos/%s/%s/wiki/page/%s", owner, repo, url.PathEscape(canonicalSlug))
	if err := p.client.Patch(ctx, path, wireBody, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindWiki, owner, repo, canonicalSlug)
	out, derr := raw.toType()
	if derr != nil {
		return nil, derr
	}
	return &out, nil
}

// findWikiSlugByTitle lists wiki pages and returns the sub_url for the
// first page whose title matches slug (case-sensitive). Returns ("", false)
// if not found or if the list call fails.
func (p *Provider) findWikiSlugByTitle(ctx context.Context, owner, repo, slug string) (string, bool) {
	pages, _, err := p.ListWikiPages(ctx, owner, repo, provider.ListWikiPagesOptions{Limit: 200})
	if err != nil {
		return "", false
	}
	for _, page := range pages {
		if page.Title == slug && page.Path != "" && page.Path != slug {
			return page.Path, true
		}
	}
	return "", false
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
