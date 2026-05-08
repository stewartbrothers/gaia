package forgejo_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// wikiPageMetaJSON returns the shape Forgejo's "GET /wiki/pages"
// returns: an array of WikiPageMetaData (no body).
func wikiPageMetaJSON(title, sha, date string) map[string]any {
	return map[string]any{
		"title":    title,
		"sub_url":  title,
		"html_url": "https://example/" + title,
		"last_commit": map[string]any{
			"sha":     sha,
			"message": "edit " + title,
			"commiter": map[string]any{
				"name":  "alice",
				"email": "alice@example",
				"date":  date,
			},
		},
	}
}

// wikiPageJSON returns the full WikiPage shape (with content_base64).
func wikiPageJSON(title, body, sha, date string) map[string]any {
	return map[string]any{
		"title":          title,
		"sub_url":        title,
		"html_url":       "https://example/" + title,
		"content_base64": base64.StdEncoding.EncodeToString([]byte(body)),
		"last_commit": map[string]any{
			"sha":     sha,
			"message": "edit " + title,
			"commiter": map[string]any{
				"name":  "alice",
				"email": "alice@example",
				"date":  date,
			},
		},
	}
}

func TestListWikiPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/wiki/pages" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			wikiPageMetaJSON("Home", "deadbeef1234567", "2026-04-01T00:00:00Z"),
			wikiPageMetaJSON("Setup", "cafebabe9876543", "2026-04-02T00:00:00Z"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListWikiPages(context.Background(), "o", "r", provider.ListWikiPagesOptions{})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Title != "Home" || got[0].Path != "Home" {
		t.Errorf("first: %+v", got[0])
	}
	if got[0].LastCommit != "deadbee" {
		t.Errorf("last_commit should be 7-char short SHA; got %q", got[0].LastCommit)
	}
	if got[0].Body != "" {
		t.Errorf("list endpoint returns no body; got %q", got[0].Body)
	}
}

func TestListWikiPagesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListWikiPages(context.Background(), "o", "r", provider.ListWikiPagesOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound", got)
	}
}

func TestGetWikiPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/wiki/page/Home" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(wikiPageJSON("Home", "# Welcome\n\nbody text", "deadbeef1234567", "2026-04-01T00:00:00Z"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetWikiPage(context.Background(), "o", "r", "Home")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Title != "Home" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Body != "# Welcome\n\nbody text" {
		t.Errorf("body should be base64-decoded; got %q", got.Body)
	}
	if got.LastCommit != "deadbee" {
		t.Errorf("last_commit short SHA: %q", got.LastCommit)
	}
}

func TestGetWikiPageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWikiPage(context.Background(), "o", "r", "Missing")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound", got)
	}
}

func TestGetWikiPageAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetWikiPage(context.Background(), "o", "r", "Home")
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("got code %d, want Auth", got)
	}
}

func TestGetWikiPageEscapesSlug(t *testing.T) {
	// Slugs with spaces or odd characters must be path-escaped so the
	// server sees the raw bytes, not the literal characters mangled
	// by Go's URL parser.
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Path
		_ = json.NewEncoder(w).Encode(wikiPageJSON("Some Page", "body", "abc1234", "2026-04-01T00:00:00Z"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetWikiPage(context.Background(), "o", "r", "Some Page"); err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	// The httptest server unescapes URL path before exposing it via r.URL.Path,
	// so we should see the raw "Some Page" form here.
	if captured != "/repos/o/r/wiki/page/Some Page" {
		t.Errorf("path: %q", captured)
	}
}

func TestEditWikiPageCreatesIfMissing(t *testing.T) {
	posts := int32(0)
	patches := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/o/r/wiki/page/"):
			// Slug-existence probe: 404 → page must be created.
			w.WriteHeader(404)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/wiki/new":
			atomic.AddInt32(&posts, 1)
			body, _ := io.ReadAll(r.Body)
			var got map[string]any
			_ = json.Unmarshal(body, &got)
			if got["title"] != "Home" {
				t.Errorf("title in body: %v", got["title"])
			}
			decoded, err := base64.StdEncoding.DecodeString(got["content_base64"].(string))
			if err != nil {
				t.Errorf("content_base64 not valid base64: %v", err)
			}
			if string(decoded) != "new body" {
				t.Errorf("decoded body: %q", decoded)
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Home", "new body", "newsha1234567", "2026-05-03T00:00:00Z"))
		case r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.EditWikiPage(context.Background(), "o", "r", "Home", "new body")
	if err != nil {
		t.Fatalf("EditWikiPage: %v", err)
	}
	if got.Body != "new body" {
		t.Errorf("body: %q", got.Body)
	}
	if atomic.LoadInt32(&posts) != 1 || atomic.LoadInt32(&patches) != 0 {
		t.Errorf("expected 1 POST + 0 PATCH; got %d/%d", posts, patches)
	}
}

func TestEditWikiPagePatchesIfExisting(t *testing.T) {
	posts := int32(0)
	patches := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/o/r/wiki/page/"):
			// Slug-existence probe: 200 → page already exists, must PATCH.
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Home", "old body", "oldsha1234567", "2026-04-01T00:00:00Z"))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/wiki/page/Home":
			atomic.AddInt32(&patches, 1)
			body, _ := io.ReadAll(r.Body)
			var got map[string]any
			_ = json.Unmarshal(body, &got)
			decoded, _ := base64.StdEncoding.DecodeString(got["content_base64"].(string))
			if string(decoded) != "new body" {
				t.Errorf("decoded body: %q", decoded)
			}
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Home", "new body", "newsha1234567", "2026-05-03T00:00:00Z"))
		case r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.EditWikiPage(context.Background(), "o", "r", "Home", "new body")
	if err != nil {
		t.Fatalf("EditWikiPage: %v", err)
	}
	if got.Body != "new body" {
		t.Errorf("body: %q", got.Body)
	}
	if atomic.LoadInt32(&posts) != 0 || atomic.LoadInt32(&patches) != 1 {
		t.Errorf("expected 0 POST + 1 PATCH; got %d/%d", posts, patches)
	}
}

// TestEditWikiPageHyphenatedTitleUsesListFallback is a regression test
// for issue #178. Forgejo may return a sub_url that differs from the
// user-supplied title (e.g. "Quick-Start" → "Quick-Start.-"). When
// GET by slug 404s but the list shows a page with a matching title,
// EditWikiPage must PATCH using the canonical sub_url, not POST again.
func TestEditWikiPageHyphenatedTitleUsesListFallback(t *testing.T) {
	patches := int32(0)
	posts := int32(0)
	var patchPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/o/r/wiki/page/"):
			// Slug probe always 404s — simulates Forgejo storing the page
			// under the mangled path "Quick-Start.-" instead of "Quick-Start".
			w.WriteHeader(404)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/wiki/pages"):
			// List returns the page with the canonical (mangled) sub_url.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"title":   "Quick-Start",
					"sub_url": "Quick-Start.-",
				},
			})
		case r.Method == http.MethodPatch:
			atomic.AddInt32(&patches, 1)
			patchPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Quick-Start", "updated", "abc1234", "2026-05-01T00:00:00Z"))
		case r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.EditWikiPage(context.Background(), "o", "r", "Quick-Start", "updated")
	if err != nil {
		t.Fatalf("EditWikiPage: %v", err)
	}
	if got.Body != "updated" {
		t.Errorf("body: %q", got.Body)
	}
	if atomic.LoadInt32(&patches) != 1 || atomic.LoadInt32(&posts) != 0 {
		t.Errorf("expected 1 PATCH + 0 POST; got PATCH=%d POST=%d", patches, posts)
	}
	if patchPath != "/repos/o/r/wiki/page/Quick-Start.-" {
		t.Errorf("PATCH path: %q (want canonical /wiki/page/Quick-Start.-)", patchPath)
	}
}

func TestDeleteWikiPage(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		deletePath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteWikiPage(context.Background(), "o", "r", "Home"); err != nil {
		t.Fatalf("DeleteWikiPage: %v", err)
	}
	if deletePath != "/repos/o/r/wiki/page/Home" {
		t.Errorf("path: %q", deletePath)
	}
}

func TestDeleteWikiPageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeleteWikiPage(context.Background(), "o", "r", "Gone")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound", got)
	}
}

func TestSearchWikiPagesMatchesTitleAndBody(t *testing.T) {
	// List returns 3 pages; per-page GET returns full body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				wikiPageMetaJSON("Home", "sha1234567", "2026-04-01T00:00:00Z"),
				wikiPageMetaJSON("Setup-Guide", "sha2345678", "2026-04-02T00:00:00Z"),
				wikiPageMetaJSON("Other", "sha3456789", "2026-04-03T00:00:00Z"),
			})
		case r.URL.Path == "/repos/o/r/wiki/page/Home":
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Home", "Welcome to the project. Body has FOO sprinkled in.", "sha1234567", "2026-04-01T00:00:00Z"))
		case r.URL.Path == "/repos/o/r/wiki/page/Setup-Guide":
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Setup-Guide", "FOO is the magic config knob you need.", "sha2345678", "2026-04-02T00:00:00Z"))
		case r.URL.Path == "/repos/o/r/wiki/page/Other":
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Other", "totally unrelated content", "sha3456789", "2026-04-03T00:00:00Z"))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "FOO", provider.SearchWikiOptions{})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits; got %d (%+v)", len(hits), hits)
	}
	titles := []string{hits[0].Title, hits[1].Title}
	if !sliceHas(titles, "Home") || !sliceHas(titles, "Setup-Guide") {
		t.Errorf("matched titles: %v", titles)
	}
	for _, h := range hits {
		if !strings.Contains(h.Snippet, "FOO") {
			t.Errorf("snippet should contain match; got %q", h.Snippet)
		}
	}
}

func TestSearchWikiPagesCaseInsensitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				wikiPageMetaJSON("MixedCase", "sha", "2026-04-01T00:00:00Z"),
			})
		case r.URL.Path == "/repos/o/r/wiki/page/MixedCase":
			_ = json.NewEncoder(w).Encode(wikiPageJSON("MixedCase", "Hello WORLD", "sha", "2026-04-01T00:00:00Z"))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "world", provider.SearchWikiOptions{})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("count: %d", len(hits))
	}
}

func TestSearchWikiPagesNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				wikiPageMetaJSON("Home", "sha", "2026-04-01T00:00:00Z"),
			})
		case r.URL.Path == "/repos/o/r/wiki/page/Home":
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Home", "totally unrelated", "sha", "2026-04-01T00:00:00Z"))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "missing-term", provider.SearchWikiOptions{})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits; got %d", len(hits))
	}
}

func TestSearchWikiPagesEmptyQueryIsUsageError(t *testing.T) {
	p := newTestProvider(t, "http://127.0.0.1:1")
	_, err := p.SearchWikiPages(context.Background(), "o", "r", "  ", provider.SearchWikiOptions{})
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("got code %d, want Usage", got)
	}
}

func TestSearchWikiPagesRespectsMaxPagesCap(t *testing.T) {
	// Return 5 pages of meta data; cap MaxPages at 2 → only the
	// first two are fetched and scanned.
	getCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			pages := []map[string]any{}
			for i := 0; i < 5; i++ {
				pages = append(pages, wikiPageMetaJSON("Page-"+string(rune('A'+i)), "sha", "2026-04-01T00:00:00Z"))
			}
			_ = json.NewEncoder(w).Encode(pages)
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/wiki/page/"):
			atomic.AddInt32(&getCalls, 1)
			title := strings.TrimPrefix(r.URL.Path, "/repos/o/r/wiki/page/")
			_ = json.NewEncoder(w).Encode(wikiPageJSON(title, "body of "+title+" mentioning FOO", "sha", "2026-04-01T00:00:00Z"))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "FOO", provider.SearchWikiOptions{MaxPages: 2})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("expected exactly 2 hits with cap=2; got %d", len(hits))
	}
	// Title-only matches don't trigger a body fetch, so call count
	// should be at most 2 (one per scanned page).
	if got := atomic.LoadInt32(&getCalls); got > 2 {
		t.Errorf("scanned more pages than cap allowed; got %d GETs", got)
	}
}

func TestSearchWikiPagesSnippetIncludesContext(t *testing.T) {
	long := strings.Repeat("a ", 150) + "needle " + strings.Repeat("b ", 150)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/wiki/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				wikiPageMetaJSON("Big", "sha", "2026-04-01T00:00:00Z"),
			})
		case r.URL.Path == "/repos/o/r/wiki/page/Big":
			_ = json.NewEncoder(w).Encode(wikiPageJSON("Big", long, "sha", "2026-04-01T00:00:00Z"))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	hits, err := p.SearchWikiPages(context.Background(), "o", "r", "needle", provider.SearchWikiOptions{})
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("count: %d", len(hits))
	}
	// Snippet ~200 chars wide and contains the match.
	if !strings.Contains(hits[0].Snippet, "needle") {
		t.Errorf("snippet missing needle: %q", hits[0].Snippet)
	}
	if len(hits[0].Snippet) > 250 {
		t.Errorf("snippet too long (%d chars): %q", len(hits[0].Snippet), hits[0].Snippet)
	}
}

func sliceHas(haystack []string, want string) bool {
	for _, s := range haystack {
		if s == want {
			return true
		}
	}
	return false
}

// io.Discard import keeper — referenced in case the file's helpers grow.
var _ = io.Discard
