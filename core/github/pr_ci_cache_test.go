package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

// newTestProviderWithCache builds a GitHub Provider backed by an
// in-memory cache (parallel to the Forgejo test helper).
func newTestProviderWithCache(t *testing.T, baseURL string, c *cache.Memory) *github.Provider {
	t.Helper()
	return github.NewProvider(github.Options{
		BaseURL:   baseURL,
		Token:     "TEST",
		UserAgent: "gaia-test/1.0",
		RetryWait: 1 * time.Millisecond,
		Cache:     c,
	})
}

// TestGetPullRequestWithCISummaryBypassesCache is the #367 regression for
// GitHub: a CI-monitoring read must not serve a cached PR, else the
// check-runs lookup keys off a stale head SHA after a force-push.
func TestGetPullRequestWithCISummaryBypassesCache(t *testing.T) {
	headSHA := "oldsha"
	var checksReqSHA string
	prFetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			parts := strings.Split(r.URL.Path, "/")
			checksReqSHA = parts[len(parts)-2]
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "check_runs": []any{}})
		case strings.Contains(r.URL.Path, "/pulls/"):
			prFetches++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 1, "state": "open", "head": map[string]any{"sha": headSHA},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProviderWithCache(t, srv.URL, cache.NewMemory())

	if _, err := p.GetPullRequest(context.Background(), "o", "r", 1, provider.GetPullRequestOptions{}); err != nil {
		t.Fatalf("warm read: %v", err)
	}
	headSHA = "newsha"
	if _, err := p.GetPullRequest(context.Background(), "o", "r", 1, provider.GetPullRequestOptions{WithCISummary: true}); err != nil {
		t.Fatalf("ci read: %v", err)
	}
	if prFetches != 2 {
		t.Errorf("WithCISummary read served a cached PR (prFetches=%d, want 2)", prFetches)
	}
	if checksReqSHA != "newsha" {
		t.Errorf("check-runs keyed off stale head: got %q, want newsha", checksReqSHA)
	}
}
