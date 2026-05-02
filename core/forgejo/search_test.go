package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func searchResultJSON(number int, title, repo string, isPR bool) map[string]any {
	r := map[string]any{
		"number": number,
		"title":  title,
		"repository": map[string]any{
			"full_name": repo,
		},
	}
	if isPR {
		r["pull_request"] = map[string]any{}
	}
	return r
}

func TestSearchCrossRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/issues/search" {
			t.Errorf("path: got %q, want /repos/issues/search", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "memory leak" {
			t.Errorf("query: got %q", r.URL.Query().Get("q"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			searchResultJSON(1, "fix issue", "Gerwood/gaia", false),
			searchResultJSON(7, "feat: things", "Gerwood/gaia", true),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, page, err := p.Search(context.Background(), "memory leak", provider.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2", len(got))
	}
	if got[0].Kind != "issue" || got[1].Kind != "pull_request" {
		t.Errorf("kind discrimination: %+v", got)
	}
	if got[0].Number != 1 || got[0].Title != "fix issue" || got[0].RepoFull != "Gerwood/gaia" {
		t.Errorf("first: %+v", got[0])
	}
	if page == nil {
		t.Errorf("page should be non-nil")
	}
}

func TestSearchRepoScoped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Gerwood/gaia/issues" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			searchResultJSON(42, "title", "Gerwood/gaia", false),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.Search(context.Background(), "x", provider.SearchOptions{
		Repo: "Gerwood/gaia",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Number != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestSearchKindsFilter(t *testing.T) {
	cases := []struct {
		name string
		kind []string
		want string // expected `type` query param
	}{
		{"issues only", []string{"issue"}, "issues"},
		{"pulls only", []string{"pull_request"}, "pulls"},
		{"both explicit", []string{"issue", "pull_request"}, ""}, // no filter when both
		{"empty defaults", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var captured url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = r.URL.Query()
				_ = json.NewEncoder(w).Encode([]map[string]any{})
			}))
			defer srv.Close()

			p := newTestProvider(t, srv.URL)
			_, _, err := p.Search(context.Background(), "x", provider.SearchOptions{
				Kinds: c.kind,
			})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if got := captured.Get("type"); got != c.want {
				t.Errorf("type: got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSearchPaginationParams(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, _ = p.Search(context.Background(), "x", provider.SearchOptions{
		Limit:  10,
		Cursor: "3",
	})
	if got := captured.Get("limit"); got != "10" {
		t.Errorf("limit: got %q", got)
	}
	if got := captured.Get("page"); got != "3" {
		t.Errorf("page: got %q", got)
	}
}

func TestSearchAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.Search(context.Background(), "x", provider.SearchOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("expected Auth on 401; got %d", got)
	}
}
