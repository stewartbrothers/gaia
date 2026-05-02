package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func TestSearchGHWrapsItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/issues" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":        2,
			"incomplete_results": false,
			"items": []map[string]any{
				{"number": 1, "title": "issue one", "html_url": "https://github.com/o/r/issues/1"},
				{"number": 7, "title": "PR seven", "html_url": "https://github.com/o/r/pull/7", "pull_request": map[string]any{}},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.Search(context.Background(), "feature", provider.SearchOptions{Repo: "o/r"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Kind != "issue" || got[1].Kind != "pull_request" {
		t.Errorf("kind discrim: got %+v", got)
	}
	if got[0].RepoFull != "o/r" {
		t.Errorf("repo: got %q", got[0].RepoFull)
	}
}

func TestSearchGHQueryComposition(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		opts        provider.SearchOptions
		mustContain []string
	}{
		{
			"repo qualifier",
			"memory leak",
			provider.SearchOptions{Repo: "cli/cli"},
			[]string{"memory leak", "repo:cli/cli"},
		},
		{
			"issue kind only",
			"x",
			provider.SearchOptions{Kinds: []string{"issue"}},
			[]string{"x", "is:issue"},
		},
		{
			"pull_request kind",
			"y",
			provider.SearchOptions{Kinds: []string{"pull_request"}},
			[]string{"y", "is:pr"},
		},
		{
			"both kinds = no is qualifier",
			"z",
			provider.SearchOptions{Kinds: []string{"issue", "pull_request"}},
			[]string{"z"}, // must NOT add is:*
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var capturedQ string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedQ = r.URL.Query().Get("q")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"total_count": 0,
					"items":       []any{},
				})
			}))
			defer srv.Close()

			p := newTestProvider(t, srv.URL)
			_, _, _ = p.Search(context.Background(), c.query, c.opts)
			for _, want := range c.mustContain {
				if !strings.Contains(capturedQ, want) {
					t.Errorf("q should contain %q; got %q", want, capturedQ)
				}
			}
			if c.name == "both kinds = no is qualifier" {
				if strings.Contains(capturedQ, "is:") {
					t.Errorf("q should NOT contain is: qualifier; got %q", capturedQ)
				}
			}
		})
	}
}
