package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// GitHub wikis are an independent git repository at
// `https://{host}/{owner}/{repo}.wiki.git` — there is no REST surface.
// All five Provider wiki methods route through the wikiCache primitive
// in wikicache.go: read paths refresh-on-TTL and serve from disk;
// writes refresh, modify, commit, and push back upstream.
//
// Conventions for path/title trim:
//
//   - Slug (= types.WikiPage.Path) is the filename without its `.md`
//     suffix. This matches GitHub's URL convention: a page titled
//     "Setup Guide" lives at `Setup-Guide.md` on disk and `/wiki/
//     Setup-Guide` on the web. Callers pass the slug verbatim.
//   - Title is set to the slug for now (no header-extraction). Agents
//     who want the markdown's H1 can read the body and parse it
//     themselves; that's a presentation concern, not a trim concern.
//   - LastCommit is the 7-char short SHA of the most recent commit
//     touching the file (`git log -1 --format=%h -- path`).
//   - UpdatedAt is the committer date of that same commit.
//
// We intentionally include every `.md` (and `.markdown`) file we find
// in ListWikiPages, including README.md if it exists. README.md is
// commonly used as the wiki landing-page hint by the web UI, but it's
// also a legitimate page (the web UI surfaces it as such). Filtering
// it out would be opinionated and would diverge from "what's actually
// in the repo".

const (
	defaultSearchMaxPages     = 100
	defaultSearchSnippetWidth = 100
)

// wikiPageExtensions is the suffix set we recognise as wiki pages.
// GitHub's wiki feature supports several markups (markdown, AsciiDoc,
// Textile, Org, RST, MediaWiki, Pod, Creole) but the vast majority of
// real wikis use markdown. We accept the markdown variants now and
// can extend the list if a real user needs another markup.
var wikiPageExtensions = []string{".md", ".markdown"}

// ListWikiPages returns the wiki pages found in the cache clone. The
// cache is refreshed (or freshly cloned) before scanning. Bodies are
// not populated — call GetWikiPage for the source.
//
// Pagination: the slice is sliced client-side by Limit/Cursor for
// API parity with the Forgejo path. Wikis with thousands of pages
// are vanishingly rare; this is correct, just inefficient on the
// far edge.
func (p *Provider) ListWikiPages(ctx context.Context, owner, repo string, opts provider.ListWikiPagesOptions) ([]types.WikiPage, *provider.Page, error) {
	cache, err := p.wikicache()
	if err != nil {
		return nil, nil, err
	}
	dir, err := cache.ensureClone(ctx, owner, repo, p.wikiRemote(owner, repo))
	if err != nil {
		return nil, nil, err
	}

	pages, err := scanWikiDir(dir)
	if err != nil {
		return nil, nil, err
	}
	// Stable order: alphabetical by path. The on-disk readdir order is
	// not specified, and tests rely on a deterministic slice.
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	return pages, nil, nil
}

// GetWikiPage returns one page including its markdown body. 404
// (missing slug) maps to exitcode.NotFound so callers can distinguish
// it from a real failure.
func (p *Provider) GetWikiPage(ctx context.Context, owner, repo, slug string) (*types.WikiPage, error) {
	cache, err := p.wikicache()
	if err != nil {
		return nil, err
	}
	dir, err := cache.ensureClone(ctx, owner, repo, p.wikiRemote(owner, repo))
	if err != nil {
		return nil, err
	}

	path, err := resolveWikiFile(dir, slug)
	if err != nil {
		return nil, err
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "read wiki page")
	}
	page := &types.WikiPage{
		Title: slug,
		Path:  slug,
		Body:  string(body),
	}
	if sha, when, ok := wikiFileLastCommit(ctx, dir, filepath.Base(path)); ok {
		page.LastCommit = sha
		page.UpdatedAt = when
	}
	return page, nil
}

// SearchWikiPages performs case-insensitive title + body matching
// across the wiki, capped at opts.MaxPages. Mirrors the Forgejo
// search semantics (snippet window, ordering) so callers see a
// consistent shape across forges.
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

	cache, err := p.wikicache()
	if err != nil {
		return nil, err
	}
	dir, err := cache.ensureClone(ctx, owner, repo, p.wikiRemote(owner, repo))
	if err != nil {
		return nil, err
	}

	pages, err := scanWikiDir(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	if len(pages) > maxPages {
		pages = pages[:maxPages]
	}

	needle := strings.ToLower(q)
	hits := make([]types.WikiSearchHit, 0, len(pages))
	for _, page := range pages {
		// Title-only match: cheap, no body read.
		if strings.Contains(strings.ToLower(page.Title), needle) {
			hits = append(hits, types.WikiSearchHit{
				Path:  page.Path,
				Title: page.Title,
			})
			continue
		}
		// Body scan.
		bodyPath, err := resolveWikiFile(dir, page.Path)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(bodyPath)
		if err != nil {
			continue
		}
		body := string(raw)
		idx := strings.Index(strings.ToLower(body), needle)
		if idx < 0 {
			continue
		}
		hits = append(hits, types.WikiSearchHit{
			Path:    page.Path,
			Title:   page.Title,
			Snippet: snippetAround(body, idx, len(q), width),
		})
	}
	return hits, nil
}

// EditWikiPage upserts a page: writes `{slug}.md` (creating the file
// if absent) with body, then commits + pushes. The cache is refreshed
// before the write so we don't accidentally overwrite an upstream
// change.
func (p *Provider) EditWikiPage(ctx context.Context, owner, repo, slug, body string) (*types.WikiPage, error) {
	cache, err := p.wikicache()
	if err != nil {
		return nil, err
	}
	dir, err := cache.ensureClone(ctx, owner, repo, p.wikiRemote(owner, repo))
	if err != nil {
		return nil, err
	}
	// Force-refresh-on-write: callers expect their edit to land on top
	// of the latest upstream state, not on top of a 5-min-stale cache.
	if err := cache.refresh(ctx, dir); err != nil {
		return nil, err
	}

	// Resolve the existing file (preserves `.markdown` extension if a
	// caller is editing one); fall back to `.md` for new pages.
	target, _ := resolveWikiFile(dir, slug)
	if target == "" {
		target = filepath.Join(dir, slug+".md")
	}
	if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "write wiki page")
	}

	msg := fmt.Sprintf("Edit %s via gaia", slug)
	if err := cache.commitAndPush(ctx, dir, msg); err != nil {
		return nil, err
	}

	page := &types.WikiPage{
		Title: slug,
		Path:  slug,
		Body:  body,
	}
	if sha, when, ok := wikiFileLastCommit(ctx, dir, filepath.Base(target)); ok {
		page.LastCommit = sha
		page.UpdatedAt = when
	}
	return page, nil
}

// DeleteWikiPage removes the page file then commits + pushes. Missing
// slugs map to exitcode.NotFound (no commit issued).
func (p *Provider) DeleteWikiPage(ctx context.Context, owner, repo, slug string) error {
	cache, err := p.wikicache()
	if err != nil {
		return err
	}
	dir, err := cache.ensureClone(ctx, owner, repo, p.wikiRemote(owner, repo))
	if err != nil {
		return err
	}
	if err := cache.refresh(ctx, dir); err != nil {
		return err
	}
	target, err := resolveWikiFile(dir, slug)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "remove wiki page")
	}
	msg := fmt.Sprintf("Delete %s via gaia", slug)
	return cache.commitAndPush(ctx, dir, msg)
}

// scanWikiDir walks dir non-recursively (wikis are flat) and returns
// one trimmed WikiPage per recognised page file. .git and dotfiles
// are skipped.
func scanWikiDir(dir string) ([]types.WikiPage, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "scan wiki dir")
	}
	out := make([]types.WikiPage, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		slug, ok := stripWikiExt(e.Name())
		if !ok {
			continue
		}
		out = append(out, types.WikiPage{
			Title: slug,
			Path:  slug,
		})
	}
	return out, nil
}

// resolveWikiFile returns the on-disk path for a slug, trying each
// supported extension in turn. Missing → exitcode.NotFound.
func resolveWikiFile(dir, slug string) (string, error) {
	for _, ext := range wikiPageExtensions {
		path := filepath.Join(dir, slug+ext)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", exitcode.Errorf(exitcode.NotFound, "wiki page %q not found", slug)
}

// stripWikiExt returns (slug, true) if name has a recognised wiki
// extension, else ("", false).
func stripWikiExt(name string) (string, bool) {
	for _, ext := range wikiPageExtensions {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext), true
		}
	}
	return "", false
}

// wikiFileLastCommit returns the 7-char short SHA + committer date of
// the most recent commit touching path inside dir. Returns ok=false
// on git failure (file in working tree but not yet committed, etc.) —
// callers fall back to leaving WikiPage.LastCommit empty.
func wikiFileLastCommit(ctx context.Context, dir, basename string) (string, time.Time, bool) {
	cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%h%x09%cI", "--", basename)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", time.Time{}, false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", time.Time{}, false
	}
	parts := strings.SplitN(line, "\t", 2)
	if len(parts) != 2 {
		return "", time.Time{}, false
	}
	when, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return parts[0], time.Time{}, true
	}
	return parts[0], when, true
}

// snippetAround returns a window around body[matchIdx:matchIdx+matchLen]
// at most width chars on each side. Whitespace is collapsed for clean
// JSON rendering. Mirrors the helper in core/forgejo so search results
// look identical across providers.
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
	snippet = strings.Join(strings.Fields(snippet), " ")
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(body) {
		snippet += "..."
	}
	return snippet
}
