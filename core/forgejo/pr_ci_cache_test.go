package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/provider"
)

// TestGetPullRequestWithCISummaryBypassesCache is the #367 regression:
// a CI-monitoring read (WithCISummary) must NOT serve a cached PR, or
// the status lookup is keyed off a stale head SHA and reports the
// previous commit's CI result as current. Simulates a force-push between
// a plain (cache-warming) read and a CI read.
func TestGetPullRequestWithCISummaryBypassesCache(t *testing.T) {
	headSHA := "oldsha"
	var statusReqSHA string
	prFetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			// /repos/o/r/commits/<sha>/status
			parts := strings.Split(r.URL.Path, "/")
			statusReqSHA = parts[len(parts)-2]
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "success", "statuses": []any{}})
		case strings.Contains(r.URL.Path, "/pulls/"):
			prFetches++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 1,
				"state":  "open",
				"head":   map[string]any{"sha": headSHA},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProviderWithCache(t, srv.URL, cache.NewMemory())

	// 1. Plain read warms the cache with head=oldsha.
	if _, err := p.GetPullRequest(context.Background(), "o", "r", 1, provider.GetPullRequestOptions{}); err != nil {
		t.Fatalf("warm read: %v", err)
	}
	// 2. The branch is force-pushed: head moves.
	headSHA = "newsha"
	// 3. A CI read must hit the server again (bypass cache) and key the
	//    status lookup off the NEW head.
	got, err := p.GetPullRequest(context.Background(), "o", "r", 1, provider.GetPullRequestOptions{WithCISummary: true})
	if err != nil {
		t.Fatalf("ci read: %v", err)
	}
	if prFetches != 2 {
		t.Errorf("WithCISummary read served a cached PR (prFetches=%d, want 2)", prFetches)
	}
	if statusReqSHA != "newsha" {
		t.Errorf("CI status keyed off stale head: got %q, want newsha", statusReqSHA)
	}
	if got.Head.SHA != "newsha" {
		t.Errorf("returned stale head: %q", got.Head.SHA)
	}
}

// TestGetPullRequestPlainStillCaches guards that the fix is scoped: a
// plain PR read (no CI) still uses the cache (second read doesn't re-hit
// the server).
func TestGetPullRequestPlainStillCaches(t *testing.T) {
	prFetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pulls/") {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		prFetches++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 1, "state": "open", "head": map[string]any{"sha": "s"},
		})
	}))
	defer srv.Close()

	p := newTestProviderWithCache(t, srv.URL, cache.NewMemory())
	for i := 0; i < 2; i++ {
		if _, err := p.GetPullRequest(context.Background(), "o", "r", 1, provider.GetPullRequestOptions{}); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if prFetches != 1 {
		t.Errorf("plain PR read should cache: prFetches=%d, want 1", prFetches)
	}
}
