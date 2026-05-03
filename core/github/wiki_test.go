package github_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

// GitHub wikis aren't a REST resource — they live as a separate git
// repo at {owner}/{repo}.wiki.git. The provider clones the wiki repo
// into $XDG_CACHE_HOME/gaia/wikis/{owner}/{repo}/ on first use, then
// serves reads from disk and pushes writes back through git. This
// test file exercises every wiki Provider method against a local
// bare repo so the suite stays offline.

// initBareWiki creates a bare git repo (the "remote") populated with
// the given page → markdown body map, exposed at file:// URL via the
// returned path.
func initBareWiki(t *testing.T, pages map[string]string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.wiki.git")
	seed := filepath.Join(root, "seed")
	mustGit(t, root, "git", "init", "--bare", "--initial-branch=master", bare)
	mustGit(t, root, "git", "init", "--initial-branch=master", seed)
	mustGit(t, seed, "git", "config", "user.email", "test@example")
	mustGit(t, seed, "git", "config", "user.name", "test")
	for name, body := range pages {
		if err := os.WriteFile(filepath.Join(seed, name), []byte(body), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if len(pages) == 0 {
		// git init refuses to push an empty tree; ensure at least one
		// file so the bare repo has a master branch.
		if err := os.WriteFile(filepath.Join(seed, ".keep"), []byte(""), 0o600); err != nil {
			t.Fatalf("seed .keep: %v", err)
		}
	}
	mustGit(t, seed, "git", "add", "-A")
	mustGit(t, seed, "git", "commit", "-m", "seed")
	mustGit(t, seed, "git", "remote", "add", "origin", bare)
	mustGit(t, seed, "git", "push", "origin", "master")
	return bare
}

func mustGit(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// newWikiTestProvider builds a Provider configured to point at the
// supplied bare repo as the wiki remote. The cache root is a fresh
// temp dir per test so cache interactions stay isolated.
func newWikiTestProvider(t *testing.T, bare string) *github.Provider {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	p := github.NewProvider(github.Options{
		BaseURL:   "https://api.example",
		Token:     "TEST",
		UserAgent: "gaia-test/1.0",
		WikiRemoteFunc: func(_, _ string) string {
			return bare
		},
	})
	return p
}

func TestListWikiPagesReturnsCachedFiles(t *testing.T) {
	bare := initBareWiki(t, map[string]string{
		"Home.md":        "# Welcome\n\nbody",
		"Setup-Guide.md": "# Setup\n\nsteps",
		"README.md":      "ignored",
	})
	p := newWikiTestProvider(t, bare)
	got, _, err := p.ListWikiPages(context.Background(), "o", "r", provider.ListWikiPagesOptions{})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	// README.md is GitHub's "wiki landing page" file but doesn't appear
	// in the wiki page list — it's a sidebar/footer hint, not a page.
	// Our trim filters anything that isn't a recognisable wiki page
	// file (.md/.markdown/.textile/.org etc.). For Phase 1 we just
	// list all .md files so README.md makes the cut; it's fine — agents
	// who care about precise page-vs-meta distinctions can filter
	// client-side. We do exclude dotfiles though.
	if len(got) < 2 {
		t.Fatalf("expected at least 2 pages; got %d (%+v)", len(got), got)
	}
	names := make(map[string]bool)
	for _, p := range got {
		names[p.Path] = true
	}
	if !names["Home"] || !names["Setup-Guide"] {
		t.Errorf("missing expected pages: %v", names)
	}
	// Body should NOT be populated by list — matches Forgejo behaviour.
	for _, p := range got {
		if p.Body != "" {
			t.Errorf("list endpoint must not populate body; got %q for %q", p.Body, p.Path)
		}
	}
}

func TestGetWikiPageReturnsBody(t *testing.T) {
	bare := initBareWiki(t, map[string]string{
		"Home.md": "# Welcome\n\nthis is the body",
	})
	p := newWikiTestProvider(t, bare)
	got, err := p.GetWikiPage(context.Background(), "o", "r", "Home")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Path != "Home" {
		t.Errorf("path: %q", got.Path)
	}
	if got.Body != "# Welcome\n\nthis is the body" {
		t.Errorf("body: %q", got.Body)
	}
	if got.LastCommit == "" {
		t.Errorf("last_commit should be set to the short SHA")
	}
	if len(got.LastCommit) != 7 {
		t.Errorf("last_commit should be 7-char short SHA; got %q", got.LastCommit)
	}
}

func TestGetWikiPageNotFound(t *testing.T) {
	bare := initBareWiki(t, map[string]string{"Home.md": "body"})
	p := newWikiTestProvider(t, bare)
	_, err := p.GetWikiPage(context.Background(), "o", "r", "Missing")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound; err=%v", got, err)
	}
}

func TestGetWikiPageHandlesSlugWithSpaces(t *testing.T) {
	// GitHub's wiki convention: spaces in page titles become hyphens
	// in filenames. Callers pass the slug verbatim (the URL form).
	bare := initBareWiki(t, map[string]string{
		"Setup-Guide.md": "guide body",
	})
	p := newWikiTestProvider(t, bare)
	got, err := p.GetWikiPage(context.Background(), "o", "r", "Setup-Guide")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Body != "guide body" {
		t.Errorf("body: %q", got.Body)
	}
}

// TestGetWikiPageRejectsTraversalSlug is the end-to-end regression
// for #136's read-side path-traversal: a hostile slug like
// `../../../etc/passwd` MUST return an error before any os.Stat /
// os.ReadFile is attempted on a path outside the cache. The fix
// validates slug at the boundary; this test asserts the error is
// surfaced and no body is returned.
func TestGetWikiPageRejectsTraversalSlug(t *testing.T) {
	bare := initBareWiki(t, map[string]string{
		"Home.md": "# Welcome\nbody",
	})
	p := newWikiTestProvider(t, bare)
	cases := []string{
		"../escape",
		"../../etc/passwd",
		"..",
		".git",
		".hidden",
		"slug/with/slash",
		"slug\\with\\backslash",
	}
	for _, slug := range cases {
		t.Run(slug, func(t *testing.T) {
			got, err := p.GetWikiPage(context.Background(), "o", "r", slug)
			if err == nil {
				t.Fatalf("expected error for slug %q; got page %+v", slug, got)
			}
			if got != nil {
				t.Errorf("page should be nil on error; got %+v", got)
			}
		})
	}
}

// TestEditWikiPageRejectsTraversalSlug — write-side counterpart.
// Without the boundary validation, EditWikiPage would write the body
// to a file path constructed from `dir + slug + ".md"` — meaning
// `slug = "../../../tmp/owned"` would write `<tmp>/owned.md`. The fix
// rejects the slug before any filesystem write happens.
func TestEditWikiPageRejectsTraversalSlug(t *testing.T) {
	bare := initBareWiki(t, map[string]string{"Home.md": "first"})
	p := newWikiTestProvider(t, bare)
	cases := []string{"../escape", "../../etc/passwd", "..", ".git", ".rc"}
	for _, slug := range cases {
		t.Run(slug, func(t *testing.T) {
			_, err := p.EditWikiPage(context.Background(), "o", "r", slug, "body")
			if err == nil {
				t.Fatalf("expected error for slug %q", slug)
			}
		})
	}
}

func TestSearchWikiPagesMatchesTitleAndBody(t *testing.T) {
	bare := initBareWiki(t, map[string]string{
		"Home.md":        "Welcome to the project. Body has FOO sprinkled in.",
		"Setup-Guide.md": "FOO is the magic config knob.",
		"Other.md":       "totally unrelated content",
	})
	p := newWikiTestProvider(t, bare)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "FOO", provider.SearchWikiOptions{})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits; got %d (%+v)", len(hits), hits)
	}
	titles := []string{hits[0].Path, hits[1].Path}
	if !sliceHasStr(titles, "Home") || !sliceHasStr(titles, "Setup-Guide") {
		t.Errorf("matched paths: %v", titles)
	}
	for _, h := range hits {
		if h.Snippet != "" && !strings.Contains(h.Snippet, "FOO") {
			t.Errorf("snippet should contain match if non-empty: %q", h.Snippet)
		}
	}
}

func TestSearchWikiPagesEmptyQueryIsUsageError(t *testing.T) {
	bare := initBareWiki(t, map[string]string{"Home.md": "body"})
	p := newWikiTestProvider(t, bare)
	_, err := p.SearchWikiPages(context.Background(), "o", "r", "  ", provider.SearchWikiOptions{})
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("got code %d, want Usage", got)
	}
}

func TestSearchWikiPagesNoMatch(t *testing.T) {
	bare := initBareWiki(t, map[string]string{
		"Home.md": "totally unrelated",
	})
	p := newWikiTestProvider(t, bare)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "missing-term", provider.SearchWikiOptions{})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits; got %+v", hits)
	}
}

func TestEditWikiPageCreatesIfMissing(t *testing.T) {
	bare := initBareWiki(t, map[string]string{"Existing.md": "old"})
	p := newWikiTestProvider(t, bare)
	got, err := p.EditWikiPage(context.Background(), "o", "r", "NewPage", "fresh body")
	if err != nil {
		t.Fatalf("EditWikiPage: %v", err)
	}
	if got.Body != "fresh body" {
		t.Errorf("body: %q", got.Body)
	}
	if got.Path != "NewPage" {
		t.Errorf("path: %q", got.Path)
	}
	// Verify the bare repo received the new commit.
	logOut := captureGit(t, bare, "log", "--format=%s", "master")
	if !strings.Contains(logOut, "NewPage") {
		t.Errorf("upstream log should mention NewPage; got %q", logOut)
	}
}

func TestEditWikiPageUpdatesExisting(t *testing.T) {
	bare := initBareWiki(t, map[string]string{"Home.md": "old body"})
	p := newWikiTestProvider(t, bare)
	got, err := p.EditWikiPage(context.Background(), "o", "r", "Home", "new body")
	if err != nil {
		t.Fatalf("EditWikiPage: %v", err)
	}
	if got.Body != "new body" {
		t.Errorf("body: %q", got.Body)
	}
	// Re-read via Get to verify it persisted upstream — clone fresh
	// from the bare repo to bypass the in-test cache.
	tmp := t.TempDir()
	mustGit(t, tmp, "git", "clone", bare, "verify")
	body, err := os.ReadFile(filepath.Join(tmp, "verify", "Home.md"))
	if err != nil {
		t.Fatalf("read verify: %v", err)
	}
	if string(body) != "new body" {
		t.Errorf("upstream body: %q", body)
	}
}

func TestDeleteWikiPageRemovesFile(t *testing.T) {
	bare := initBareWiki(t, map[string]string{
		"Home.md":  "stay",
		"Stale.md": "go away",
	})
	p := newWikiTestProvider(t, bare)
	if err := p.DeleteWikiPage(context.Background(), "o", "r", "Stale"); err != nil {
		t.Fatalf("DeleteWikiPage: %v", err)
	}
	// Verify upstream no longer has Stale.md.
	tmp := t.TempDir()
	mustGit(t, tmp, "git", "clone", bare, "verify")
	if _, err := os.Stat(filepath.Join(tmp, "verify", "Stale.md")); !os.IsNotExist(err) {
		t.Errorf("Stale.md should be gone upstream: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "verify", "Home.md")); err != nil {
		t.Errorf("Home.md should still exist: %v", err)
	}
}

func TestDeleteWikiPageNotFound(t *testing.T) {
	bare := initBareWiki(t, map[string]string{"Home.md": "stay"})
	p := newWikiTestProvider(t, bare)
	err := p.DeleteWikiPage(context.Background(), "o", "r", "Missing")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound; err=%v", got, err)
	}
}

func captureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func sliceHasStr(haystack []string, want string) bool {
	for _, s := range haystack {
		if s == want {
			return true
		}
	}
	return false
}
